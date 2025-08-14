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

// GovernanceResponse represents the REST API response for governance proposals
type GovernanceResponse struct {
	Proposals  []ProposalData `json:"proposals"`
	Pagination PaginationInfo `json:"pagination,omitempty"`
}

// PaginationInfo represents pagination information from the API
type PaginationInfo struct {
	NextKey string `json:"next_key,omitempty"`
	Total   string `json:"total,omitempty"`
}

// ProposalData represents a single proposal from the REST API
type ProposalData struct {
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

// Content represents the content of a proposal
type Content struct {
	Type        string `json:"@type"`
	Title       string `json:"title"`
	Description string `json:"description"`
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
					zap.String("chain", chain.Name),
					zap.Error(err),
				)
			}
		}
	}
}

// scanChain scans a single chain for proposals
func (s *Scanner) scanChain(ctx context.Context, chain config.ChainConfig) error {
	s.logger.Debug("Scanning chain for proposals", zap.String("chain", chain.Name))

	// Try v1beta1 with very small pagination first to avoid complex proposals
	url := fmt.Sprintf("%s/cosmos/gov/v1beta1/proposals?pagination.limit=5&pagination.reverse=true", chain.REST)

	proposals, err := s.fetchProposals(ctx, url)
	if err != nil {
		// If v1beta1 fails due to conversion issues, try v1 endpoint
		s.logger.Debug("v1beta1 failed, trying v1 endpoint", zap.String("chain", chain.Name), zap.Error(err))
		urlV1 := fmt.Sprintf("%s/cosmos/gov/v1/proposals?pagination.limit=5&pagination.reverse=true", chain.REST)
		proposals, err = s.fetchProposals(ctx, urlV1)
		if err != nil {
			return fmt.Errorf("both v1beta1 and v1 endpoints failed: %w", err)
		}
	}

	s.logger.Debug("Fetched proposals with pagination",
		zap.String("chain", chain.Name),
		zap.Int("proposal_count", len(proposals)),
	)

	return s.processProposals(chain, proposals)
}

// fetchProposals fetches proposals from a given URL
func (s *Scanner) fetchProposals(ctx context.Context, url string) ([]ProposalData, error) {
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

	var govResp GovernanceResponse
	if err := json.Unmarshal(body, &govResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return govResp.Proposals, nil
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
					zap.String("chain", chain.Name),
					zap.String("proposal_id", proposal.ProposalID),
					zap.String("title", proposal.Content.Title),
					zap.String("status", proposal.Status),
				)
			} else {
				// New proposals OR actively voting proposals: queue notification
				newProposal.NotificationSent = false
				if isActivelyVoting {
					s.logger.Info("Active voting proposal found - notification queued",
						zap.String("chain", chain.Name),
						zap.String("proposal_id", proposal.ProposalID),
						zap.String("title", proposal.Content.Title),
						zap.String("status", proposal.Status),
					)
				} else {
					s.logger.Info("New proposal found - notification queued",
						zap.String("chain", chain.Name),
						zap.String("proposal_id", proposal.ProposalID),
						zap.String("title", proposal.Content.Title),
						zap.String("status", proposal.Status),
					)
				}
			}

			if err := s.db.Create(&newProposal).Error; err != nil {
				s.logger.Error("Failed to create proposal",
					zap.String("chain", chain.Name),
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
						zap.String("chain", chain.Name),
						zap.String("proposal_id", proposal.ProposalID),
						zap.Error(err),
					)
				}
			}
		} else {
			s.logger.Error("Database error checking proposal",
				zap.String("chain", chain.Name),
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
		Title:       proposal.Content.Title,
		Description: proposal.Content.Description,
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
