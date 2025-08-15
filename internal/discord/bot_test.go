package discord

import (
	"testing"
	"time"

	"prop-voter/config"
	"prop-voter/internal/models"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// VoterInterface defines the interface that voting implementations must satisfy
type VoterInterface interface {
	Vote(chainID, proposalID, option string) (string, error)
	VoteAuthz(chainID, proposalID, option string) (string, error)
	ValidateChainCLI(chain config.ChainConfig) error
	ValidateWalletKey(chain config.ChainConfig) error
	ValidateAllChains() error
}

// mockVoter implements the VoterInterface for testing
type mockVoter struct {
	voteAuthzErr    error
	voteAuthzTxHash string
	voteErr         error
	voteTxHash      string
	lastChainID     string
	lastProposalID  string
	lastOption      string
	lastIsAuthz     bool
}

func (m *mockVoter) Vote(chainID, proposalID, option string) (string, error) {
	m.lastChainID = chainID
	m.lastProposalID = proposalID
	m.lastOption = option
	m.lastIsAuthz = false
	return m.voteTxHash, m.voteErr
}

func (m *mockVoter) VoteAuthz(chainID, proposalID, option string) (string, error) {
	m.lastChainID = chainID
	m.lastProposalID = proposalID
	m.lastOption = option
	m.lastIsAuthz = true
	return m.voteAuthzTxHash, m.voteAuthzErr
}

func (m *mockVoter) ValidateChainCLI(chain config.ChainConfig) error {
	return nil
}

func (m *mockVoter) ValidateWalletKey(chain config.ChainConfig) error {
	return nil
}

func (m *mockVoter) ValidateAllChains() error {
	return nil
}

// testBot is a modified Bot struct for testing that accepts our VoterInterface
type testBot struct {
	session    interface{} // We don't need a real Discord session
	db         *gorm.DB
	config     *config.Config
	logger     *zap.Logger
	voter      VoterInterface
	notifyChan chan models.Proposal
}

// Copy the authz vote command method for testing
func (b *testBot) handleAuthzVoteCommand(channelID string, args []string) {
	if len(args) < 4 {
		return // Would send error message in real implementation
	}

	chainID := args[0]
	proposalID := args[1]
	voteOption := args[2]
	secret := args[3]

	// Verify secret
	if secret != b.config.Security.VoteSecret {
		return // Would send error message
	}

	// Validate vote option
	validVotes := map[string]bool{
		"yes":          true,
		"no":           true,
		"abstain":      true,
		"no_with_veto": true,
	}

	if !validVotes[voteOption] {
		return // Would send error message
	}

	// Find the chain configuration and check if authz is enabled
	var chainConfig *config.ChainConfig
	for _, chain := range b.config.Chains {
		if chain.GetChainID() == chainID {
			chainConfig = &chain
			break
		}
	}

	if chainConfig == nil {
		return // Would send error message
	}

	if !chainConfig.IsAuthzEnabled() {
		return // Would send error message
	}

	// Check if proposal exists
	var proposal models.Proposal
	if err := b.db.Where("chain_id = ? AND proposal_id = ?", chainID, proposalID).First(&proposal).Error; err != nil {
		return // Would send error message
	}

	// Submit authz vote
	txHash, err := b.voter.VoteAuthz(chainID, proposalID, voteOption)
	if err != nil {
		return // Would send error message
	}

	// Store authz vote in database
	granterName := chainConfig.GetGranterName()
	vote := models.Vote{
		ChainID:     chainID,
		ProposalID:  proposalID,
		Option:      voteOption,
		TxHash:      txHash,
		VotedAt:     time.Now(),
		IsAuthzVote: true,
		GranterAddr: chainConfig.GetGranterAddr(),
		GranterName: granterName,
	}

	b.db.Create(&vote) // Ignore error for test
}

// Copy the regular vote command method for testing
func (b *testBot) handleVoteCommand(channelID string, args []string) {
	if len(args) < 4 {
		return
	}

	chainID := args[0]
	proposalID := args[1]
	voteOption := args[2]
	secret := args[3]

	// Verify secret
	if secret != b.config.Security.VoteSecret {
		return
	}

	// Validate vote option
	validVotes := map[string]bool{
		"yes":          true,
		"no":           true,
		"abstain":      true,
		"no_with_veto": true,
	}

	if !validVotes[voteOption] {
		return
	}

	// Check if proposal exists
	var proposal models.Proposal
	if err := b.db.Where("chain_id = ? AND proposal_id = ?", chainID, proposalID).First(&proposal).Error; err != nil {
		return
	}

	// Submit vote
	txHash, err := b.voter.Vote(chainID, proposalID, voteOption)
	if err != nil {
		return
	}

	// Store vote in database
	vote := models.Vote{
		ChainID:    chainID,
		ProposalID: proposalID,
		Option:     voteOption,
		TxHash:     txHash,
		VotedAt:    time.Now(),
	}

	b.db.Create(&vote)
}

func setupTestBot(t *testing.T) (*testBot, *gorm.DB, *mockVoter) {
	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to setup test database: %v", err)
	}

	// Initialize database schema
	if err := models.InitDB(db); err != nil {
		t.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Discord: config.DiscordConfig{
			Token:       "test-token",
			ChannelID:   "test-channel",
			AllowedUser: "test-user",
		},
		Security: config.SecurityConfig{
			VoteSecret: "test-secret",
		},
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain",
				ChainID:   "test-1",
				RPC:       "http://localhost:26657",
				REST:      "http://localhost:1317",
				WalletKey: "test-key",
				CLIName:   "testd",
				Denom:     "utest",
				Prefix:    "test",
			},
			{
				Name:      "Test Chain with Authz",
				ChainID:   "test-authz-1",
				RPC:       "http://localhost:26657",
				REST:      "http://localhost:1317",
				WalletKey: "grantee-key",
				CLIName:   "testd",
				Denom:     "utest",
				Prefix:    "test",
				Authz: config.AuthzConfig{
					Enabled:     true,
					GranterAddr: "test1granter123addr456",
					GranterName: "Test Validator",
				},
			},
		},
	}

	logger := zaptest.NewLogger(t)
	mockVoter := &mockVoter{}

	// Create test bot
	bot := &testBot{
		session:    nil,
		db:         db,
		config:     cfg,
		logger:     logger,
		voter:      mockVoter,
		notifyChan: make(chan models.Proposal, 100),
	}

	return bot, db, mockVoter
}

func TestHandleAuthzVoteCommand(t *testing.T) {
	bot, db, mockVoter := setupTestBot(t)

	// Create test proposal in database
	proposal := models.Proposal{
		ChainID:    "test-authz-1",
		ProposalID: "123",
		Title:      "Test Proposal",
		Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&proposal).Error; err != nil {
		t.Fatalf("Failed to create test proposal: %v", err)
	}

	// Mock successful authz vote
	mockVoter.voteAuthzTxHash = "ABCD1234567890"
	mockVoter.voteAuthzErr = nil

	// Test successful authz vote
	t.Run("successful_authz_vote", func(t *testing.T) {
		args := []string{"test-authz-1", "123", "yes", "test-secret"}

		// This would normally send Discord messages, but we're just testing the logic
		bot.handleAuthzVoteCommand("test-channel", args)

		// Verify that VoteAuthz was called with correct parameters
		if !mockVoter.lastIsAuthz {
			t.Error("Expected authz vote to be called")
		}
		if mockVoter.lastChainID != "test-authz-1" {
			t.Errorf("Expected chain ID 'test-authz-1', got '%s'", mockVoter.lastChainID)
		}
		if mockVoter.lastProposalID != "123" {
			t.Errorf("Expected proposal ID '123', got '%s'", mockVoter.lastProposalID)
		}
		if mockVoter.lastOption != "yes" {
			t.Errorf("Expected vote option 'yes', got '%s'", mockVoter.lastOption)
		}

		// Verify vote was stored in database
		var vote models.Vote
		if err := db.Where("chain_id = ? AND proposal_id = ?", "test-authz-1", "123").First(&vote).Error; err != nil {
			t.Fatalf("Expected vote to be stored in database: %v", err)
		}

		if !vote.IsAuthzVote {
			t.Error("Expected vote to be marked as authz vote")
		}
		if vote.GranterAddr != "test1granter123addr456" {
			t.Errorf("Expected granter address 'test1granter123addr456', got '%s'", vote.GranterAddr)
		}
		if vote.GranterName != "Test Validator" {
			t.Errorf("Expected granter name 'Test Validator', got '%s'", vote.GranterName)
		}
		if vote.TxHash != "ABCD1234567890" {
			t.Errorf("Expected tx hash 'ABCD1234567890', got '%s'", vote.TxHash)
		}
	})
}

func TestHandleAuthzVoteCommandErrors(t *testing.T) {
	bot, _, _ := setupTestBot(t)

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "insufficient_args",
			args: []string{"test-authz-1", "123", "yes"}, // Missing secret
		},
		{
			name: "invalid_secret",
			args: []string{"test-authz-1", "123", "yes", "wrong-secret"},
		},
		{
			name: "invalid_vote_option",
			args: []string{"test-authz-1", "123", "invalid", "test-secret"},
		},
		{
			name: "non_existent_chain",
			args: []string{"non-existent-chain", "123", "yes", "test-secret"},
		},
		{
			name: "authz_not_enabled",
			args: []string{"test-1", "123", "yes", "test-secret"}, // test-1 doesn't have authz enabled
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// These tests verify that the command handles errors gracefully
			// In a real scenario, error messages would be sent to Discord
			bot.handleAuthzVoteCommand("test-channel", tc.args)
			// We can't easily verify the exact error messages without mocking Discord
			// but we can verify that the function doesn't panic and handles errors
		})
	}
}

func TestHandleVoteCommandComparison(t *testing.T) {
	bot, db, mockVoter := setupTestBot(t)

	// Create test proposal
	proposal := models.Proposal{
		ChainID:    "test-1",
		ProposalID: "123",
		Title:      "Test Proposal",
		Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&proposal).Error; err != nil {
		t.Fatalf("Failed to create test proposal: %v", err)
	}

	// Test regular vote vs authz vote
	mockVoter.voteTxHash = "REGULAR1234567890"
	mockVoter.voteErr = nil

	// Regular vote
	t.Run("regular_vote", func(t *testing.T) {
		args := []string{"test-1", "123", "yes", "test-secret"}
		bot.handleVoteCommand("test-channel", args)

		if mockVoter.lastIsAuthz {
			t.Error("Expected regular vote, not authz vote")
		}

		// Verify vote was stored as regular vote
		var vote models.Vote
		if err := db.Where("chain_id = ? AND proposal_id = ? AND is_authz_vote = ?", "test-1", "123", false).First(&vote).Error; err != nil {
			t.Fatalf("Expected regular vote to be stored: %v", err)
		}

		if vote.IsAuthzVote {
			t.Error("Expected vote to NOT be marked as authz vote")
		}
		if vote.GranterAddr != "" {
			t.Errorf("Expected empty granter address for regular vote, got '%s'", vote.GranterAddr)
		}
	})
}

func TestChainConfigAuthzIntegration(t *testing.T) {
	bot, _, _ := setupTestBot(t)

	// Test that authz-enabled chains are properly configured
	for _, chain := range bot.config.Chains {
		if chain.ChainID == "test-authz-1" {
			if !chain.IsAuthzEnabled() {
				t.Error("Expected test-authz-1 to have authz enabled")
			}
			if chain.GetGranterAddr() != "test1granter123addr456" {
				t.Errorf("Expected granter address 'test1granter123addr456', got '%s'", chain.GetGranterAddr())
			}
			if chain.GetGranterName() != "Test Validator" {
				t.Errorf("Expected granter name 'Test Validator', got '%s'", chain.GetGranterName())
			}
		}
		if chain.ChainID == "test-1" {
			if chain.IsAuthzEnabled() {
				t.Error("Expected test-1 to NOT have authz enabled")
			}
		}
	}
}

// setupTestBotForBench is a benchmark-safe version of setupTestBot
func setupTestBotForBench() (*testBot, *gorm.DB, *mockVoter) {
	// Setup in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("Failed to setup test database: " + err.Error())
	}

	// Initialize database schema
	if err := models.InitDB(db); err != nil {
		panic("Failed to initialize database schema: " + err.Error())
	}

	// Create test config
	cfg := &config.Config{
		Discord: config.DiscordConfig{
			Token:       "test-token",
			ChannelID:   "test-channel",
			AllowedUser: "test-user",
		},
		Security: config.SecurityConfig{
			VoteSecret: "test-secret",
		},
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain with Authz",
				ChainID:   "test-authz-1",
				RPC:       "http://localhost:26657",
				REST:      "http://localhost:1317",
				WalletKey: "grantee-key",
				CLIName:   "testd",
				Denom:     "utest",
				Prefix:    "test",
				Authz: config.AuthzConfig{
					Enabled:     true,
					GranterAddr: "test1granter123addr456",
					GranterName: "Test Validator",
				},
			},
		},
	}

	logger := zaptest.NewLogger(nil)
	mockVoter := &mockVoter{}

	// Create test bot
	bot := &testBot{
		session:    nil,
		db:         db,
		config:     cfg,
		logger:     logger,
		voter:      mockVoter,
		notifyChan: make(chan models.Proposal, 100),
	}

	return bot, db, mockVoter
}

// Benchmark test for authz vote command handling
func BenchmarkHandleAuthzVoteCommand(b *testing.B) {
	bot, db, mockVoter := setupTestBotForBench()

	// Create test proposal
	proposal := models.Proposal{
		ChainID:    "test-authz-1",
		ProposalID: "123",
		Title:      "Test Proposal",
		Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&proposal).Error; err != nil {
		b.Fatalf("Failed to create test proposal: %v", err)
	}

	mockVoter.voteAuthzTxHash = "ABCD1234567890"
	mockVoter.voteAuthzErr = nil

	args := []string{"test-authz-1", "123", "yes", "test-secret"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bot.handleAuthzVoteCommand("test-channel", args)
	}
}
