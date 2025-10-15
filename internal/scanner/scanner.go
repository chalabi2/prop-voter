package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scanner handles scanning multiple Cosmos chains for proposals
type Scanner struct {
	db     *gorm.DB
	config *config.Config
	logger *zap.Logger
	client *http.Client
}

// PaginationInfo represents pagination information from the API
type PaginationInfo struct {
	NextKey string `json:"next_key,omitempty"`
	Total   string `json:"total,omitempty"`
}

// ProposalData represents a unified proposal structure (internal use)
type ProposalData struct {
	ProposalID       string
	Title            string
	Description      string
	Status           string
	FinalTallyResult interface{}
	SubmitTime       string
	DepositEndTime   string
	TotalDeposit     []interface{}
	VotingStartTime  string
	VotingEndTime    string
}

// ProposalDataV1 represents a proposal from the v1 API
type ProposalDataV1 struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	Summary          string        `json:"summary"`
	Status           string        `json:"status"`
	FinalTallyResult interface{}   `json:"final_tally_result"`
	SubmitTime       string        `json:"submit_time"`
	DepositEndTime   string        `json:"deposit_end_time"`
	TotalDeposit     []interface{} `json:"total_deposit"`
	VotingStartTime  string        `json:"voting_start_time"`
	VotingEndTime    string        `json:"voting_end_time"`
}

// ProposalDataV1Beta1 represents a proposal from the v1beta1 API
type ProposalDataV1Beta1 struct {
	ProposalID       string        `json:"proposal_id"`
	Content          Content       `json:"content"`
	Status           string        `json:"status"`
	FinalTallyResult interface{}   `json:"final_tally_result"`
	SubmitTime       string        `json:"submit_time"`
	DepositEndTime   string        `json:"deposit_end_time"`
	TotalDeposit     []interface{} `json:"total_deposit"`
	VotingStartTime  string        `json:"voting_start_time"`
	VotingEndTime    string        `json:"voting_end_time"`
}

// Content represents the content of a proposal (v1beta1)
type Content struct {
	Type        string `json:"@type"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// GovernanceResponseV1 represents the v1 API response
type GovernanceResponseV1 struct {
	Proposals  []ProposalDataV1 `json:"proposals"`
	Pagination PaginationInfo   `json:"pagination,omitempty"`
}

// GovernanceResponseV1Beta1 represents the v1beta1 API response
type GovernanceResponseV1Beta1 struct {
	Proposals  []ProposalDataV1Beta1 `json:"proposals"`
	Pagination PaginationInfo        `json:"pagination,omitempty"`
}

// NewScanner creates a new proposal scanner
func NewScanner(db *gorm.DB, config *config.Config, logger *zap.Logger) *Scanner {
	return &Scanner{
		db:     db,
		config: config,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Start begins the scanning process for all configured chains
func (s *Scanner) Start(ctx context.Context) error {
	s.logger.Info("Starting proposal scanner", zap.Int("chains", len(s.config.Chains)))

	ticker := time.NewTicker(s.config.Scanning.Interval)
	defer ticker.Stop()

	// Initial scan
	s.scanAllChains(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Stopping proposal scanner")
			return ctx.Err()
		case <-ticker.C:
			s.scanAllChains(ctx)
		}
	}
}

// scanAllChains scans all configured chains for new proposals
func (s *Scanner) scanAllChains(ctx context.Context) {
	for _, chain := range s.config.Chains {
		select {
		case <-ctx.Done():
			return
		default:
			if err := s.scanChain(ctx, chain); err != nil {
				s.logger.Error("Failed to scan chain",
					zap.String("chain", chain.GetName()),
					zap.Error(err),
				)
			}
		}
	}
}

// scanChain scans a single chain for proposals
func (s *Scanner) scanChain(ctx context.Context, chain config.ChainConfig) error {
	s.logger.Debug("Scanning chain for proposals", zap.String("chain", chain.GetName()))

	// Try both API versions and use the one with better data
	var proposals []ProposalData

	// Try v1 first
	proposalsV1, errV1 := s.tryFetchProposalsV1(ctx, chain)

	// Try v1beta1 second
	proposalsV1Beta1, errV1Beta1 := s.tryFetchProposalsV1Beta1(ctx, chain)

	// If both failed, return error
	if errV1 != nil && errV1Beta1 != nil {
		return fmt.Errorf("both v1 and v1beta1 endpoints failed - v1: %v, v1beta1: %v", errV1, errV1Beta1)
	}

	// Choose the best API response based on data completeness
	if errV1 == nil && errV1Beta1 == nil {
		// Both succeeded - pick the one with better title data
		v1HasTitles := len(proposalsV1) > 0 && proposalsV1[0].Title != ""
		v1Beta1HasTitles := len(proposalsV1Beta1) > 0 && proposalsV1Beta1[0].Title != ""

		if v1HasTitles && !v1Beta1HasTitles {
			proposals = proposalsV1
			s.logger.Debug("Using v1 API (better metadata)", zap.String("chain", chain.GetName()))
		} else if v1Beta1HasTitles && !v1HasTitles {
			proposals = proposalsV1Beta1
			s.logger.Debug("Using v1beta1 API (better metadata)", zap.String("chain", chain.GetName()))
		} else {
			// Both have titles or both don't - prefer v1
			proposals = proposalsV1
			s.logger.Debug("Using v1 API (default choice)", zap.String("chain", chain.GetName()))
		}
	} else if errV1 == nil {
		// Only v1 succeeded
		proposals = proposalsV1
		s.logger.Debug("Using v1 API (v1beta1 failed)", zap.String("chain", chain.GetName()), zap.Error(errV1Beta1))
	} else {
		// Only v1beta1 succeeded
		proposals = proposalsV1Beta1
		s.logger.Debug("Using v1beta1 API (v1 failed)", zap.String("chain", chain.GetName()), zap.Error(errV1))
	}

	s.logger.Debug("Fetched proposals",
		zap.String("chain", chain.GetName()),
		zap.Int("proposal_count", len(proposals)),
	)

	return s.processProposals(chain, proposals)
}

// tryFetchProposalsV1Beta1 attempts to fetch proposals using the v1beta1 API
func (s *Scanner) tryFetchProposalsV1Beta1(ctx context.Context, chain config.ChainConfig) ([]ProposalData, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1beta1/proposals?pagination.limit=5&pagination.reverse=true", chain.REST)
	if s.config.AuthEndpoints.Enabled && s.config.AuthEndpoints.APIKey != "" {
		if strings.Contains(url, "?") {
			url = url + "&api_key=" + s.config.AuthEndpoints.APIKey
		} else {
			url = url + "?api_key=" + s.config.AuthEndpoints.APIKey
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch proposals: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var govResp GovernanceResponseV1Beta1
	if err := json.Unmarshal(body, &govResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal v1beta1 response: %w", err)
	}

	// Convert v1beta1 proposals to unified format
	var proposals []ProposalData
	for _, p := range govResp.Proposals {
		proposals = append(proposals, ProposalData{
			ProposalID:       p.ProposalID,
			Title:            p.Content.Title,
			Description:      p.Content.Description,
			Status:           p.Status,
			FinalTallyResult: p.FinalTallyResult,
			SubmitTime:       p.SubmitTime,
			DepositEndTime:   p.DepositEndTime,
			TotalDeposit:     p.TotalDeposit,
			VotingStartTime:  p.VotingStartTime,
			VotingEndTime:    p.VotingEndTime,
		})
	}

	return proposals, nil
}

// tryFetchProposalsV1 attempts to fetch proposals using the v1 API
func (s *Scanner) tryFetchProposalsV1(ctx context.Context, chain config.ChainConfig) ([]ProposalData, error) {
	url := fmt.Sprintf("%s/cosmos/gov/v1/proposals?pagination.limit=5&pagination.reverse=true", chain.REST)
	if s.config.AuthEndpoints.Enabled && s.config.AuthEndpoints.APIKey != "" {
		if strings.Contains(url, "?") {
			url = url + "&api_key=" + s.config.AuthEndpoints.APIKey
		} else {
			url = url + "?api_key=" + s.config.AuthEndpoints.APIKey
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch proposals: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var govResp GovernanceResponseV1
	if err := json.Unmarshal(body, &govResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal v1 response: %w", err)
	}

	// Convert v1 proposals to unified format
	var proposals []ProposalData
	for _, p := range govResp.Proposals {
		proposals = append(proposals, ProposalData{
			ProposalID:       p.ID,
			Title:            p.Title,
			Description:      p.Summary,
			Status:           p.Status,
			FinalTallyResult: p.FinalTallyResult,
			SubmitTime:       p.SubmitTime,
			DepositEndTime:   p.DepositEndTime,
			TotalDeposit:     p.TotalDeposit,
			VotingStartTime:  p.VotingStartTime,
			VotingEndTime:    p.VotingEndTime,
		})
	}

	return proposals, nil
}

// processProposals processes the proposals and stores new ones in the database
func (s *Scanner) processProposals(chain config.ChainConfig, proposals []ProposalData) error {
	// Check if this is the first scan for this chain (no proposals exist yet)
	var existingCount int64
	s.db.Model(&models.Proposal{}).Where("chain_id = ?", chain.GetChainID()).Count(&existingCount)
	isFirstScan := existingCount == 0

	// Filter proposals to focus on relevant ones
	relevantProposals := s.filterRelevantProposals(proposals)

	s.logger.Debug("Processing proposals",
		zap.String("chain", chain.Name),
		zap.Int("total_fetched", len(proposals)),
		zap.Int("relevant_proposals", len(relevantProposals)),
	)

	for _, proposal := range relevantProposals {
		// Check if proposal already exists
		var existing models.Proposal
		result := s.db.Where("chain_id = ? AND proposal_id = ?", chain.GetChainID(), proposal.ProposalID).First(&existing)

		if result.Error == gorm.ErrRecordNotFound {
			// New proposal, create it
			newProposal := s.convertToModel(chain, proposal)

			// Check if proposal is actively voting to determine notification behavior
			isActivelyVoting := strings.Contains(strings.ToUpper(proposal.Status), "VOTING")

			if isFirstScan && !isActivelyVoting {
				// Historical non-voting proposals: mark as already notified
				newProposal.NotificationSent = true
				s.logger.Debug("Historical proposal stored without notification",
					zap.String("chain", chain.GetName()),
					zap.String("proposal_id", proposal.ProposalID),
					zap.String("title", proposal.Title),
					zap.String("status", proposal.Status),
				)
			} else {
				// New proposals OR actively voting proposals: queue notification
				newProposal.NotificationSent = false
				if isActivelyVoting {
					s.logger.Info("Active voting proposal found - notification queued",
						zap.String("chain", chain.GetName()),
						zap.String("proposal_id", proposal.ProposalID),
						zap.String("title", proposal.Title),
						zap.String("status", proposal.Status),
					)
				} else {
					s.logger.Info("New proposal found - notification queued",
						zap.String("chain", chain.GetName()),
						zap.String("proposal_id", proposal.ProposalID),
						zap.String("title", proposal.Title),
						zap.String("status", proposal.Status),
					)
				}
			}

			if err := s.db.Create(&newProposal).Error; err != nil {
				s.logger.Error("Failed to create proposal",
					zap.String("chain", chain.GetName()),
					zap.String("proposal_id", proposal.ProposalID),
					zap.Error(err),
				)
				continue
			}
		} else if result.Error == nil {
			// Existing proposal, update if status changed
			if existing.Status != proposal.Status {
				existing.Status = proposal.Status
				if err := s.db.Save(&existing).Error; err != nil {
					s.logger.Error("Failed to update proposal",
						zap.String("chain", chain.GetName()),
						zap.String("proposal_id", proposal.ProposalID),
						zap.Error(err),
					)
				}
			}
		} else {
			s.logger.Error("Database error checking proposal",
				zap.String("chain", chain.GetName()),
				zap.String("proposal_id", proposal.ProposalID),
				zap.Error(result.Error),
			)
		}
	}

	return nil
}

// filterRelevantProposals filters proposals to focus on active and recent ones
func (s *Scanner) filterRelevantProposals(proposals []ProposalData) []ProposalData {
	var relevant []ProposalData

	for _, proposal := range proposals {
		// Include proposals that are:
		// 1. In voting period (PROPOSAL_STATUS_VOTING_PERIOD)
		// 2. In deposit period (PROPOSAL_STATUS_DEPOSIT_PERIOD)
		// 3. Recently passed/rejected (for final status tracking)
		switch proposal.Status {
		case "PROPOSAL_STATUS_VOTING_PERIOD",
			"PROPOSAL_STATUS_DEPOSIT_PERIOD",
			"PROPOSAL_STATUS_PASSED",
			"PROPOSAL_STATUS_REJECTED",
			"PROPOSAL_STATUS_FAILED":
			relevant = append(relevant, proposal)
		}
		// Skip historical proposals that are no longer relevant
	}

	return relevant
}

// convertToModel converts API proposal data to database model
func (s *Scanner) convertToModel(chain config.ChainConfig, proposal ProposalData) models.Proposal {
	model := models.Proposal{
		ChainID:     chain.GetChainID(),
		ProposalID:  proposal.ProposalID,
		Title:       proposal.Title,
		Description: proposal.Description,
		Status:      proposal.Status,
	}

	// Parse voting times if available
	if proposal.VotingStartTime != "" {
		if t, err := time.Parse(time.RFC3339, proposal.VotingStartTime); err == nil {
			model.VotingStart = &t
		}
	}

	if proposal.VotingEndTime != "" {
		if t, err := time.Parse(time.RFC3339, proposal.VotingEndTime); err == nil {
			model.VotingEnd = &t
		}
	}

	return model
}
