package scanner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prop-voter/config"
	"prop-voter/internal/models"

	"go.uber.org/zap/zaptest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestScanner(t testing.TB) (*Scanner, *gorm.DB) {
	// Create in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Initialize tables
	if err := models.InitDB(db); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	cfg := &config.Config{
		Scanning: config.ScanConfig{
			Interval:  1 * time.Minute,
			BatchSize: 10,
		},
		Chains: []config.ChainConfig{
			{
				Name:    "Test Chain",
				ChainID: "test-1",
				RPC:     "http://localhost:26657",
				REST:    "http://localhost:1317",
			},
		},
	}

	logger := zaptest.NewLogger(t)
	scanner := NewScanner(db, cfg, logger)

	return scanner, db
}

func TestNewScanner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)

	scanner := NewScanner(db, cfg, logger)

	if scanner.db != db {
		t.Error("Expected scanner database to match provided database")
	}

	if scanner.config != cfg {
		t.Error("Expected scanner config to match provided config")
	}

	if scanner.logger != logger {
		t.Error("Expected scanner logger to match provided logger")
	}

	if scanner.client == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func TestConvertToModel(t *testing.T) {
	scanner, _ := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	proposalData := ProposalData{
		ProposalID: "123",
		Content: Content{
			Title:       "Test Proposal",
			Description: "This is a test proposal",
		},
		Status:          "PROPOSAL_STATUS_VOTING_PERIOD",
		VotingStartTime: "2023-01-01T00:00:00Z",
		VotingEndTime:   "2023-01-31T23:59:59Z",
	}

	model := scanner.convertToModel(chain, proposalData)

	if model.ChainID != "test-1" {
		t.Errorf("Expected chain ID 'test-1', got '%s'", model.ChainID)
	}

	if model.ProposalID != "123" {
		t.Errorf("Expected proposal ID '123', got '%s'", model.ProposalID)
	}

	if model.Title != "Test Proposal" {
		t.Errorf("Expected title 'Test Proposal', got '%s'", model.Title)
	}

	if model.Description != "This is a test proposal" {
		t.Errorf("Expected description 'This is a test proposal', got '%s'", model.Description)
	}

	if model.Status != "PROPOSAL_STATUS_VOTING_PERIOD" {
		t.Errorf("Expected status 'PROPOSAL_STATUS_VOTING_PERIOD', got '%s'", model.Status)
	}

	expectedStart, _ := time.Parse(time.RFC3339, "2023-01-01T00:00:00Z")
	if model.VotingStart == nil || !model.VotingStart.Equal(expectedStart) {
		t.Errorf("Expected voting start %v, got %v", expectedStart, model.VotingStart)
	}

	expectedEnd, _ := time.Parse(time.RFC3339, "2023-01-31T23:59:59Z")
	if model.VotingEnd == nil || !model.VotingEnd.Equal(expectedEnd) {
		t.Errorf("Expected voting end %v, got %v", expectedEnd, model.VotingEnd)
	}
}

func TestConvertToModelInvalidDates(t *testing.T) {
	scanner, _ := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	proposalData := ProposalData{
		ProposalID: "123",
		Content: Content{
			Title:       "Test Proposal",
			Description: "This is a test proposal",
		},
		Status:          "PROPOSAL_STATUS_VOTING_PERIOD",
		VotingStartTime: "invalid-date",
		VotingEndTime:   "also-invalid",
	}

	model := scanner.convertToModel(chain, proposalData)

	if model.VotingStart != nil {
		t.Error("Expected voting start to be nil for invalid date")
	}

	if model.VotingEnd != nil {
		t.Error("Expected voting end to be nil for invalid date")
	}
}

func TestProcessProposalsNewProposal(t *testing.T) {
	scanner, db := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	proposals := []ProposalData{
		{
			ProposalID: "123",
			Content: Content{
				Title:       "Test Proposal",
				Description: "This is a test proposal",
			},
			Status: "PROPOSAL_STATUS_VOTING_PERIOD",
		},
	}

	err := scanner.processProposals(chain, proposals)
	if err != nil {
		t.Fatalf("Failed to process proposals: %v", err)
	}

	// Verify proposal was stored
	var stored models.Proposal
	err = db.Where("chain_id = ? AND proposal_id = ?", "test-1", "123").First(&stored).Error
	if err != nil {
		t.Fatalf("Failed to retrieve stored proposal: %v", err)
	}

	if stored.Title != "Test Proposal" {
		t.Errorf("Expected title 'Test Proposal', got '%s'", stored.Title)
	}

	if stored.Status != "PROPOSAL_STATUS_VOTING_PERIOD" {
		t.Errorf("Expected status 'PROPOSAL_STATUS_VOTING_PERIOD', got '%s'", stored.Status)
	}

	if stored.NotificationSent {
		t.Error("Expected notification_sent to be false for new proposal")
	}
}

func TestProcessProposalsUpdateExisting(t *testing.T) {
	scanner, db := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	// Store initial proposal
	initial := models.Proposal{
		ChainID:    "test-1",
		ProposalID: "123",
		Title:      "Test Proposal",
		Status:     "PROPOSAL_STATUS_DEPOSIT_PERIOD",
	}
	db.Create(&initial)

	// Process proposal with updated status
	proposals := []ProposalData{
		{
			ProposalID: "123",
			Content: Content{
				Title:       "Test Proposal",
				Description: "This is a test proposal",
			},
			Status: "PROPOSAL_STATUS_VOTING_PERIOD",
		},
	}

	err := scanner.processProposals(chain, proposals)
	if err != nil {
		t.Fatalf("Failed to process proposals: %v", err)
	}

	// Verify proposal was updated
	var updated models.Proposal
	err = db.Where("chain_id = ? AND proposal_id = ?", "test-1", "123").First(&updated).Error
	if err != nil {
		t.Fatalf("Failed to retrieve updated proposal: %v", err)
	}

	if updated.Status != "PROPOSAL_STATUS_VOTING_PERIOD" {
		t.Errorf("Expected updated status 'PROPOSAL_STATUS_VOTING_PERIOD', got '%s'", updated.Status)
	}

	// Verify only one proposal exists (update, not create)
	var count int64
	db.Model(&models.Proposal{}).Where("chain_id = ? AND proposal_id = ?", "test-1", "123").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 proposal, got %d", count)
	}
}

func TestProcessProposalsNoStatusChange(t *testing.T) {
	scanner, db := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	// Store initial proposal
	initial := models.Proposal{
		ChainID:    "test-1",
		ProposalID: "123",
		Title:      "Test Proposal",
		Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
	}
	db.Create(&initial)
	originalTime := initial.UpdatedAt

	// Process proposal with same status
	proposals := []ProposalData{
		{
			ProposalID: "123",
			Content: Content{
				Title:       "Test Proposal",
				Description: "This is a test proposal",
			},
			Status: "PROPOSAL_STATUS_VOTING_PERIOD",
		},
	}

	time.Sleep(10 * time.Millisecond) // Ensure time difference

	err := scanner.processProposals(chain, proposals)
	if err != nil {
		t.Fatalf("Failed to process proposals: %v", err)
	}

	// Verify proposal was not updated (same status)
	var unchanged models.Proposal
	err = db.Where("chain_id = ? AND proposal_id = ?", "test-1", "123").First(&unchanged).Error
	if err != nil {
		t.Fatalf("Failed to retrieve proposal: %v", err)
	}

	// The updated_at should not have changed significantly since no update occurred
	timeDiff := unchanged.UpdatedAt.Sub(originalTime)
	if timeDiff > 1*time.Second {
		t.Error("Expected proposal to not be updated when status is the same")
	}
}

func TestScanChainHTTPServer(t *testing.T) {
	// Create a test HTTP server
	testResponse := GovernanceResponse{
		Proposals: []ProposalData{
			{
				ProposalID: "456",
				Content: Content{
					Title:       "HTTP Test Proposal",
					Description: "This proposal came from HTTP test",
				},
				Status: "PROPOSAL_STATUS_VOTING_PERIOD",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cosmos/gov/v1beta1/proposals" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testResponse)
	}))
	defer server.Close()

	scanner, db := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
		REST:    server.URL,
	}

	ctx := context.Background()
	err := scanner.scanChain(ctx, chain)
	if err != nil {
		t.Fatalf("Failed to scan chain: %v", err)
	}

	// Verify proposal was stored
	var stored models.Proposal
	err = db.Where("chain_id = ? AND proposal_id = ?", "test-1", "456").First(&stored).Error
	if err != nil {
		t.Fatalf("Failed to retrieve stored proposal: %v", err)
	}

	if stored.Title != "HTTP Test Proposal" {
		t.Errorf("Expected title 'HTTP Test Proposal', got '%s'", stored.Title)
	}
}

func TestScanChainHTTPError(t *testing.T) {
	scanner, _ := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
		REST:    "http://non-existent-server:99999",
	}

	ctx := context.Background()
	err := scanner.scanChain(ctx, chain)
	if err == nil {
		t.Error("Expected error when scanning non-existent server")
	}
}

func TestScanChainHTTP404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	scanner, _ := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
		REST:    server.URL,
	}

	ctx := context.Background()
	err := scanner.scanChain(ctx, chain)
	if err == nil {
		t.Error("Expected error when server returns 404")
	}
}

func TestScanChainInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	scanner, _ := setupTestScanner(t)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
		REST:    server.URL,
	}

	ctx := context.Background()
	err := scanner.scanChain(ctx, chain)
	if err == nil {
		t.Error("Expected error when server returns invalid JSON")
	}
}

func TestScanAllChains(t *testing.T) {
	// Create test servers for multiple chains
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GovernanceResponse{
			Proposals: []ProposalData{
				{
					ProposalID: "111",
					Content:    Content{Title: "Chain 1 Proposal"},
					Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GovernanceResponse{
			Proposals: []ProposalData{
				{
					ProposalID: "222",
					Content:    Content{Title: "Chain 2 Proposal"},
					Status:     "PROPOSAL_STATUS_VOTING_PERIOD",
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server2.Close()

	cfg := &config.Config{
		Scanning: config.ScanConfig{
			Interval:  1 * time.Minute,
			BatchSize: 10,
		},
		Chains: []config.ChainConfig{
			{
				Name:    "Chain 1",
				ChainID: "chain-1",
				REST:    server1.URL,
			},
			{
				Name:    "Chain 2",
				ChainID: "chain-2",
				REST:    server2.URL,
			},
		},
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	models.InitDB(db)

	logger := zaptest.NewLogger(t)
	scanner := NewScanner(db, cfg, logger)

	scanner.scanAllChains(context.Background())

	// Verify both proposals were stored
	var proposals []models.Proposal
	db.Find(&proposals)

	if len(proposals) != 2 {
		t.Errorf("Expected 2 proposals, got %d", len(proposals))
	}

	chainProposals := make(map[string]string)
	for _, p := range proposals {
		chainProposals[p.ChainID] = p.Title
	}

	if chainProposals["chain-1"] != "Chain 1 Proposal" {
		t.Errorf("Expected 'Chain 1 Proposal', got '%s'", chainProposals["chain-1"])
	}

	if chainProposals["chain-2"] != "Chain 2 Proposal" {
		t.Errorf("Expected 'Chain 2 Proposal', got '%s'", chainProposals["chain-2"])
	}
}

func TestStartAndStop(t *testing.T) {
	scanner, _ := setupTestScanner(t)
	scanner.config.Scanning.Interval = 10 * time.Millisecond // Fast for testing

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start should block, so run in goroutine
	done := make(chan error, 1)
	go func() {
		done <- scanner.Start(ctx)
	}()

	// Wait for context to be cancelled
	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Errorf("Expected context.DeadlineExceeded, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Scanner did not stop within expected time")
	}
}

// Benchmark tests
func BenchmarkConvertToModel(b *testing.B) {
	scanner, _ := setupTestScanner(b)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	proposalData := ProposalData{
		ProposalID: "123",
		Content: Content{
			Title:       "Test Proposal",
			Description: "This is a test proposal",
		},
		Status:          "PROPOSAL_STATUS_VOTING_PERIOD",
		VotingStartTime: "2023-01-01T00:00:00Z",
		VotingEndTime:   "2023-01-31T23:59:59Z",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.convertToModel(chain, proposalData)
	}
}

func BenchmarkProcessProposals(b *testing.B) {
	scanner, _ := setupTestScanner(b)

	chain := config.ChainConfig{
		Name:    "Test Chain",
		ChainID: "test-1",
	}

	proposals := []ProposalData{
		{
			ProposalID: "123",
			Content: Content{
				Title:       "Test Proposal",
				Description: "This is a test proposal",
			},
			Status: "PROPOSAL_STATUS_VOTING_PERIOD",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.processProposals(chain, proposals)
	}
}
