package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prop-voter/config"

	"go.uber.org/zap/zaptest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestServer(t *testing.T) (*Server, *gorm.DB) {
	// Create in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg := &config.Config{
		Health: config.HealthConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/health",
		},
		Discord: config.DiscordConfig{
			Token: "test-token",
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
	server := NewServer(cfg, db, logger)

	return server, db
}

func TestHealthHandler(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	if response.Services["database"] != "healthy" {
		t.Errorf("Expected database service to be healthy, got '%s'", response.Services["database"])
	}

	if response.Services["discord"] != "configured" {
		t.Errorf("Expected discord service to be configured, got '%s'", response.Services["discord"])
	}

	if response.Metrics.TotalChains != 1 {
		t.Errorf("Expected 1 total chain, got %d", response.Metrics.TotalChains)
	}

	if response.Metrics.ActiveChains != 1 {
		t.Errorf("Expected 1 active chain, got %d", response.Metrics.ActiveChains)
	}
}

func TestHealthHandlerNoDiscordToken(t *testing.T) {
	server, _ := setupTestServer(t)
	server.config.Discord.Token = "" // Remove token

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	if w.Code != http.StatusPartialContent {
		t.Errorf("Expected status %d, got %d", http.StatusPartialContent, w.Code)
	}

	var response HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "degraded" {
		t.Errorf("Expected status 'degraded', got '%s'", response.Status)
	}

	if response.Services["discord"] != "not configured" {
		t.Errorf("Expected discord service to be 'not configured', got '%s'", response.Services["discord"])
	}
}

func TestHealthHandlerNoChains(t *testing.T) {
	server, _ := setupTestServer(t)
	server.config.Chains = []config.ChainConfig{} // Remove chains

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.healthHandler(w, req)

	if w.Code != http.StatusPartialContent {
		t.Errorf("Expected status %d, got %d", http.StatusPartialContent, w.Code)
	}

	var response HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "degraded" {
		t.Errorf("Expected status 'degraded', got '%s'", response.Status)
	}

	if response.Services["chains"] != "no active chains" {
		t.Errorf("Expected chains service to be 'no active chains', got '%s'", response.Services["chains"])
	}

	if response.Metrics.TotalChains != 0 {
		t.Errorf("Expected 0 total chains, got %d", response.Metrics.TotalChains)
	}

	if response.Metrics.ActiveChains != 0 {
		t.Errorf("Expected 0 active chains, got %d", response.Metrics.ActiveChains)
	}
}

func TestMetricsHandler(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	server.metricsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Expected Content-Type 'text/plain', got '%s'", contentType)
	}

	body := w.Body.String()
	expectedMetrics := []string{
		"prop_voter_uptime_seconds",
		"prop_voter_goroutines",
		"prop_voter_memory_bytes",
		"prop_voter_scan_errors_total",
		"prop_voter_chains_configured",
	}

	for _, metric := range expectedMetrics {
		if !containsString(body, metric) {
			t.Errorf("Expected metric '%s' to be present in response", metric)
		}
	}
}

func TestReadinessHandler(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	server.readinessHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	ready, ok := response["ready"].(bool)
	if !ok || !ready {
		t.Error("Expected ready to be true")
	}

	checks, ok := response["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected checks to be a map")
	}

	if checks["database"] != true {
		t.Error("Expected database check to be true")
	}

	if checks["configuration"] != true {
		t.Error("Expected configuration check to be true")
	}
}

func TestReadinessHandlerNotReady(t *testing.T) {
	server, _ := setupTestServer(t)
	server.config.Chains = []config.ChainConfig{} // Remove chains to make it not ready

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	server.readinessHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	ready, ok := response["ready"].(bool)
	if !ok || ready {
		t.Error("Expected ready to be false")
	}

	checks, ok := response["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected checks to be a map")
	}

	if checks["configuration"] != false {
		t.Error("Expected configuration check to be false")
	}
}

func TestUpdateScanMetrics(t *testing.T) {
	server, _ := setupTestServer(t)

	now := time.Now()
	server.UpdateScanMetrics(now, 5)

	if !server.lastScan.Equal(now) {
		t.Errorf("Expected last scan time %v, got %v", now, server.lastScan)
	}

	if server.scanErrors != 5 {
		t.Errorf("Expected scan errors 5, got %d", server.scanErrors)
	}
}

func TestIncrementScanErrors(t *testing.T) {
	server, _ := setupTestServer(t)

	server.IncrementScanErrors()
	if server.scanErrors != 1 {
		t.Errorf("Expected scan errors 1, got %d", server.scanErrors)
	}

	server.IncrementScanErrors()
	if server.scanErrors != 2 {
		t.Errorf("Expected scan errors 2, got %d", server.scanErrors)
	}
}

func TestServerStartAndStop(t *testing.T) {
	server, _ := setupTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	err := server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test health endpoint
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		t.Fatalf("Failed to make health request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Stop server
	cancel()

	// Give server time to shutdown
	time.Sleep(100 * time.Millisecond)
}

func TestServerDisabled(t *testing.T) {
	server, _ := setupTestServer(t)
	server.config.Health.Enabled = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Server should not be listening since it's disabled
	_, err = http.Get("http://localhost:8080/health")
	if err == nil {
		t.Error("Expected connection error since server should be disabled")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
