package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"prop-voter/config"
	"prop-voter/internal/health"
	"prop-voter/internal/models"
	"prop-voter/internal/scanner"
	"prop-voter/internal/voting"
	"prop-voter/internal/wallet"

	"go.uber.org/zap/zaptest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// IntegrationTest represents a full integration test suite
type IntegrationTest struct {
	db            *gorm.DB
	config        *config.Config
	healthServer  *health.Server
	walletManager *wallet.Manager
	voter         *voting.Voter
	scanner       *scanner.Scanner
}

func setupIntegrationTest(t testing.TB) *IntegrationTest {
	// Create temporary config file
	configContent := `
discord:
  token: "test-token-for-integration"
  channel_id: "123456789"
  allowed_user_id: "987654321"

database:
  path: ":memory:"

security:
  encryption_key: "test-encryption-key-32-characters"
  vote_secret: "test-secret-for-voting"

scanning:
  interval: "1s"
  batch_size: 5

health:
  enabled: true
  port: 8081
  path: "/health"

chains:
  - name: "Test Chain"
    chain_id: "test-1"
    rpc: "http://localhost:26657"
    rest: "http://localhost:1317"
    denom: "utest"
    prefix: "test"
    cli_name: "echo"  # Use echo for testing (should exist on all systems)
    wallet_key: "test-key"
`

	tmpFile, err := os.CreateTemp("", "integration-test-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	// Load configuration
	cfg, err := config.LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Create in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Initialize database
	if err := models.InitDB(db); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	logger := zaptest.NewLogger(t)

	// Initialize components
	walletManager, err := wallet.NewManager(db, cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create wallet manager: %v", err)
	}

	voter := voting.NewVoter(cfg, logger)
	healthServer := health.NewServer(cfg, db, logger)
	proposalScanner := scanner.NewScanner(db, cfg, logger)

	return &IntegrationTest{
		db:            db,
		config:        cfg,
		healthServer:  healthServer,
		walletManager: walletManager,
		voter:         voter,
		scanner:       proposalScanner,
	}
}

func TestIntegrationHealthEndpoint(t *testing.T) {
	it := setupIntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start health server
	err := it.healthServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start health server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Fatalf("Failed to request health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var healthResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if healthResp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", healthResp["status"])
	}

	// Test metrics endpoint
	resp, err = http.Get("http://localhost:8081/metrics")
	if err != nil {
		t.Fatalf("Failed to request metrics endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for metrics, got %d", resp.StatusCode)
	}

	// Test readiness endpoint
	resp, err = http.Get("http://localhost:8081/ready")
	if err != nil {
		t.Fatalf("Failed to request readiness endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for readiness, got %d", resp.StatusCode)
	}
}

func TestIntegrationWalletManager(t *testing.T) {
	it := setupIntegrationTest(t)

	// Test storing wallet
	err := it.walletManager.StoreWallet("test-1", "test-key", "test1abc123", "sensitive-data")
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Test retrieving wallet
	wallet, privateData, err := it.walletManager.GetWallet("test-1")
	if err != nil {
		t.Fatalf("Failed to get wallet: %v", err)
	}

	if wallet.ChainID != "test-1" {
		t.Errorf("Expected chain ID 'test-1', got '%s'", wallet.ChainID)
	}

	if privateData != "sensitive-data" {
		t.Errorf("Expected private data 'sensitive-data', got '%s'", privateData)
	}

	// Test listing wallets
	wallets, err := it.walletManager.ListWallets()
	if err != nil {
		t.Fatalf("Failed to list wallets: %v", err)
	}

	if len(wallets) != 1 {
		t.Errorf("Expected 1 wallet, got %d", len(wallets))
	}

	// Test wallet validation
	err = it.walletManager.ValidateWalletExists("test-1")
	if err != nil {
		t.Errorf("Expected wallet to exist: %v", err)
	}
}

func TestIntegrationVoter(t *testing.T) {
	it := setupIntegrationTest(t)

	// Test CLI validation (using echo which should exist)
	chain := it.config.Chains[0]
	err := it.voter.ValidateChainCLI(chain)
	if err != nil {
		t.Errorf("Expected no error for echo command: %v", err)
	}

	// Test with non-existent CLI
	badChain := chain
	badChain.CLIName = "definitely-does-not-exist-12345"
	err = it.voter.ValidateChainCLI(badChain)
	if err == nil {
		t.Error("Expected error for non-existent CLI")
	}

	// Test vote command building (using echo actually succeeds but produces wrong output)
	// We can test that it returns something (even if not a valid tx hash)
	_, err = it.voter.Vote("test-1", "123", "yes")
	if err != nil {
		t.Logf("Expected error or success with echo command: %v", err)
	}
}

func TestIntegrationDatabase(t *testing.T) {
	it := setupIntegrationTest(t)

	// Test storing and retrieving proposals
	proposal := models.Proposal{
		ChainID:     "test-1",
		ProposalID:  "123",
		Title:       "Test Proposal",
		Description: "Integration test proposal",
		Status:      "PROPOSAL_STATUS_VOTING_PERIOD",
	}

	err := it.db.Create(&proposal).Error
	if err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Retrieve proposal
	var retrieved models.Proposal
	err = it.db.Where("chain_id = ? AND proposal_id = ?", "test-1", "123").First(&retrieved).Error
	if err != nil {
		t.Fatalf("Failed to retrieve proposal: %v", err)
	}

	if retrieved.Title != "Test Proposal" {
		t.Errorf("Expected title 'Test Proposal', got '%s'", retrieved.Title)
	}

	// Test storing vote
	vote := models.Vote{
		ChainID:    "test-1",
		ProposalID: "123",
		Option:     "yes",
		TxHash:     "abc123def456",
		VotedAt:    time.Now(),
	}

	err = it.db.Create(&vote).Error
	if err != nil {
		t.Fatalf("Failed to create vote: %v", err)
	}

	// Test relationship
	err = it.db.Preload("Vote").Where("id = ?", proposal.ID).First(&retrieved).Error
	if err != nil {
		t.Fatalf("Failed to retrieve proposal with vote: %v", err)
	}

	if retrieved.Vote == nil {
		t.Error("Expected vote to be loaded")
	} else if retrieved.Vote.Option != "yes" {
		t.Errorf("Expected vote option 'yes', got '%s'", retrieved.Vote.Option)
	}
}

func TestIntegrationFullSystemWithoutDiscord(t *testing.T) {
	it := setupIntegrationTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start health server
	err := it.healthServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start health server: %v", err)
	}

	// Store a test wallet
	err = it.walletManager.StoreWallet("test-1", "test-key", "test1abc123", "private-key-data")
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Store a test proposal
	proposal := models.Proposal{
		ChainID:          "test-1",
		ProposalID:       "456",
		Title:            "Integration Test Proposal",
		Description:      "Full system integration test",
		Status:           "PROPOSAL_STATUS_VOTING_PERIOD",
		NotificationSent: false,
	}
	err = it.db.Create(&proposal).Error
	if err != nil {
		t.Fatalf("Failed to create proposal: %v", err)
	}

	// Wait a moment for services to initialize
	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Fatalf("Failed to request health endpoint: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected health status 200, got %d", resp.StatusCode)
	}

	// Test that wallet exists
	err = it.walletManager.ValidateWalletExists("test-1")
	if err != nil {
		t.Errorf("Expected wallet to exist: %v", err)
	}

	// Test that proposal exists
	var count int64
	it.db.Model(&models.Proposal{}).Where("chain_id = ? AND proposal_id = ?", "test-1", "456").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 proposal, got %d", count)
	}

	t.Log("Full system integration test completed successfully")
}

func TestIntegrationConfigValidation(t *testing.T) {
	it := setupIntegrationTest(t)

	// Test that all expected configuration is loaded
	if it.config.Discord.Token != "test-token-for-integration" {
		t.Errorf("Expected Discord token to be loaded correctly")
	}

	if it.config.Security.VoteSecret != "test-secret-for-voting" {
		t.Errorf("Expected vote secret to be loaded correctly")
	}

	if len(it.config.Chains) != 1 {
		t.Errorf("Expected 1 chain, got %d", len(it.config.Chains))
	}

	if it.config.Health.Port != 8081 {
		t.Errorf("Expected health port 8081, got %d", it.config.Health.Port)
	}

	if it.config.Scanning.Interval != 1*time.Second {
		t.Errorf("Expected scanning interval 1s, got %v", it.config.Scanning.Interval)
	}
}

// Test that would require Discord bot (skipped in normal tests)
func TestIntegrationDiscordBotSkipped(t *testing.T) {
	t.Skip("Discord bot integration test requires real Discord token and setup")
	
	// This is what a Discord integration test might look like:
	/*
	it := setupIntegrationTest(t)
	
	// Would need real Discord token and configured bot
	bot, err := discord.NewBot(it.db, it.config, zaptest.NewLogger(t), it.voter)
	if err != nil {
		t.Fatalf("Failed to create Discord bot: %v", err)
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = bot.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start Discord bot: %v", err)
	}
	defer bot.Stop()
	
	// Test bot functionality...
	*/
}

// Benchmark integration test
func BenchmarkIntegrationHealthEndpoint(b *testing.B) {
	it := setupIntegrationTest(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := it.healthServer.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start health server: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Let server start

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := http.Get("http://localhost:8081/health")
		if err != nil {
			b.Fatalf("Failed to request health endpoint: %v", err)
		}
		resp.Body.Close()
	}
}