package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestNewClient(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	if client.baseURL != "https://raw.githubusercontent.com/cosmos/chain-registry/master" {
		t.Errorf("Expected default base URL, got %s", client.baseURL)
	}

	if client.logger != logger {
		t.Error("Expected logger to be set")
	}

	if client.cache == nil {
		t.Error("Expected cache to be initialized")
	}

	if client.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func TestGetChainInfo_Success(t *testing.T) {
	// Create a mock Chain Registry response
	mockResponse := ChainRegistryResponse{
		ChainName:    "osmosis",
		PrettyName:   "Osmosis",
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		DaemonName:   "osmosisd",
		Staking: struct {
			StakingTokens []struct {
				Denom string `json:"denom"`
			} `json:"staking_tokens"`
		}{
			StakingTokens: []struct {
				Denom string `json:"denom"`
			}{
				{Denom: "uosmo"},
			},
		},
		Codebase: struct {
			GitRepo            string            `json:"git_repo"`
			RecommendedVersion string            `json:"recommended_version"`
			Binaries           map[string]string `json:"binaries"`
		}{
			GitRepo:            "https://github.com/osmosis-labs/osmosis/",
			RecommendedVersion: "v15.0.0",
			Binaries: map[string]string{
				"linux/amd64":  "https://github.com/osmosis-labs/osmosis/releases/download/v15.0.0/osmosisd-15.0.0-linux-amd64",
				"darwin/arm64": "https://github.com/osmosis-labs/osmosis/releases/download/v15.0.0/osmosisd-15.0.0-darwin-arm64",
			},
		},
		LogoURIs: struct {
			PNG string `json:"png"`
			SVG string `json:"svg"`
		}{
			PNG: "https://raw.githubusercontent.com/cosmos/chain-registry/master/osmosis/images/osmo.png",
		},
	}

	// Mock assetlist response
	mockAssetlist := struct {
		Assets []struct {
			Base       string `json:"base"`
			DenomUnits []struct {
				Denom    string `json:"denom"`
				Exponent int    `json:"exponent"`
			} `json:"denom_units"`
		} `json:"assets"`
	}{
		Assets: []struct {
			Base       string `json:"base"`
			DenomUnits []struct {
				Denom    string `json:"denom"`
				Exponent int    `json:"exponent"`
			} `json:"denom_units"`
		}{
			{
				Base: "uosmo",
				DenomUnits: []struct {
					Denom    string `json:"denom"`
					Exponent int    `json:"exponent"`
				}{
					{Denom: "uosmo", Exponent: 0},
					{Denom: "osmo", Exponent: 6},
				},
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/osmosis/chain.json":
			json.NewEncoder(w).Encode(mockResponse)
		case "/osmosis/assetlist.json":
			json.NewEncoder(w).Encode(mockAssetlist)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)
	client.baseURL = server.URL // Override with test server URL

	ctx := context.Background()
	chainInfo, err := client.GetChainInfo(ctx, "osmosis")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify parsed chain info
	if chainInfo.ChainName != "osmosis" {
		t.Errorf("Expected chain name 'osmosis', got '%s'", chainInfo.ChainName)
	}

	if chainInfo.PrettyName != "Osmosis" {
		t.Errorf("Expected pretty name 'Osmosis', got '%s'", chainInfo.PrettyName)
	}

	if chainInfo.ChainID != "osmosis-1" {
		t.Errorf("Expected chain ID 'osmosis-1', got '%s'", chainInfo.ChainID)
	}

	if chainInfo.Bech32Prefix != "osmo" {
		t.Errorf("Expected bech32 prefix 'osmo', got '%s'", chainInfo.Bech32Prefix)
	}

	if chainInfo.DaemonName != "osmosisd" {
		t.Errorf("Expected daemon name 'osmosisd', got '%s'", chainInfo.DaemonName)
	}

	if chainInfo.Denom != "uosmo" {
		t.Errorf("Expected denom 'uosmo', got '%s'", chainInfo.Denom)
	}

	if chainInfo.Version != "v15.0.0" {
		t.Errorf("Expected version 'v15.0.0', got '%s'", chainInfo.Version)
	}

	if chainInfo.GitRepo != "https://github.com/osmosis-labs/osmosis/" {
		t.Errorf("Expected git repo URL, got '%s'", chainInfo.GitRepo)
	}

	if chainInfo.LogoURL != "https://raw.githubusercontent.com/cosmos/chain-registry/master/osmosis/images/osmo.png" {
		t.Errorf("Expected logo URL, got '%s'", chainInfo.LogoURL)
	}

	// Binary URL should be extracted from the binaries map for current platform
	if chainInfo.BinaryURL == "" {
		t.Error("Expected binary URL to be extracted from binaries map")
	}

	// Verify it contains the expected base URL structure
	if !strings.Contains(chainInfo.BinaryURL, "https://github.com/osmosis-labs/osmosis/releases/download/v15.0.0/osmosisd-15.0.0") {
		t.Errorf("Expected binary URL to contain release download path, got '%s'", chainInfo.BinaryURL)
	}
}

func TestGetChainInfo_NotFound(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)
	client.baseURL = server.URL

	ctx := context.Background()
	_, err := client.GetChainInfo(ctx, "nonexistent")

	if err == nil {
		t.Error("Expected error for non-existent chain")
	}

	expectedError := "chain registry returned status 404 for chain nonexistent"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestGetChainInfo_Cache(t *testing.T) {
	requestCount := 0

	// Mock assetlist response
	mockAssetlist := struct {
		Assets []struct {
			Base       string `json:"base"`
			DenomUnits []struct {
				Denom    string `json:"denom"`
				Exponent int    `json:"exponent"`
			} `json:"denom_units"`
		} `json:"assets"`
	}{
		Assets: []struct {
			Base       string `json:"base"`
			DenomUnits []struct {
				Denom    string `json:"denom"`
				Exponent int    `json:"exponent"`
			} `json:"denom_units"`
		}{
			{
				Base: "uosmo",
				DenomUnits: []struct {
					Denom    string `json:"denom"`
					Exponent int    `json:"exponent"`
				}{
					{Denom: "uosmo", Exponent: 0},
					{Denom: "osmo", Exponent: 6},
				},
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/osmosis/chain.json":
			mockResponse := ChainRegistryResponse{
				ChainName:    "osmosis",
				PrettyName:   "Osmosis",
				ChainID:      "osmosis-1",
				Bech32Prefix: "osmo",
				DaemonName:   "osmosisd",
			}
			json.NewEncoder(w).Encode(mockResponse)
		case "/osmosis/assetlist.json":
			json.NewEncoder(w).Encode(mockAssetlist)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)
	client.baseURL = server.URL

	ctx := context.Background()

	// First request should hit the server (2 requests: chain.json + assetlist.json)
	_, err := client.GetChainInfo(ctx, "osmosis")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("Expected 2 requests, got %d", requestCount)
	}

	// Second request should use cache (no additional requests)
	_, err = client.GetChainInfo(ctx, "osmosis")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("Expected 2 requests (cached), got %d", requestCount)
	}
}

func TestGetBinaryInfo_Success(t *testing.T) {
	chainInfo := &ChainInfo{
		ChainName:  "osmosis",
		GitRepo:    "https://github.com/osmosis-labs/osmosis/",
		Version:    "v15.0.0",
		BinaryURL:  "https://github.com/osmosis-labs/osmosis/releases/download/v15.0.0/osmosisd-15.0.0-linux-amd64",
		DaemonName: "osmosisd",
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	binaryInfo, err := client.GetBinaryInfo(chainInfo)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if binaryInfo.Owner != "osmosis-labs" {
		t.Errorf("Expected owner 'osmosis-labs', got '%s'", binaryInfo.Owner)
	}

	if binaryInfo.Repo != "osmosis" {
		t.Errorf("Expected repo 'osmosis', got '%s'", binaryInfo.Repo)
	}

	if binaryInfo.Version != "v15.0.0" {
		t.Errorf("Expected version 'v15.0.0', got '%s'", binaryInfo.Version)
	}

	if binaryInfo.FileName != "osmosisd" {
		t.Errorf("Expected filename 'osmosisd', got '%s'", binaryInfo.FileName)
	}

	expectedBinaryURL := "https://github.com/osmosis-labs/osmosis/releases/download/v15.0.0/osmosisd-15.0.0-linux-amd64"
	if binaryInfo.BinaryURL != expectedBinaryURL {
		t.Errorf("Expected binary URL '%s', got '%s'", expectedBinaryURL, binaryInfo.BinaryURL)
	}
}

func TestGetBinaryInfo_NoBinaryURL(t *testing.T) {
	chainInfo := &ChainInfo{
		ChainName:  "osmosis",
		GitRepo:    "https://github.com/osmosis-labs/osmosis/",
		Version:    "v15.0.0",
		BinaryURL:  "", // No binary URL - should trigger GitHub fallback
		DaemonName: "osmosisd",
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	binaryInfo, err := client.GetBinaryInfo(chainInfo)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return BinaryInfo with empty BinaryURL for GitHub fallback
	if binaryInfo.BinaryURL != "" {
		t.Errorf("Expected empty binary URL for GitHub fallback, got '%s'", binaryInfo.BinaryURL)
	}

	if binaryInfo.Owner != "osmosis-labs" {
		t.Errorf("Expected owner 'osmosis-labs', got '%s'", binaryInfo.Owner)
	}

	if binaryInfo.Repo != "osmosis" {
		t.Errorf("Expected repo 'osmosis', got '%s'", binaryInfo.Repo)
	}

	if binaryInfo.FileName != "osmosisd" {
		t.Errorf("Expected filename 'osmosisd', got '%s'", binaryInfo.FileName)
	}

	if binaryInfo.Version != "v15.0.0" {
		t.Errorf("Expected version 'v15.0.0', got '%s'", binaryInfo.Version)
	}
}

func TestGetBinaryInfo_InvalidGitRepo(t *testing.T) {
	chainInfo := &ChainInfo{
		ChainName: "osmosis",
		GitRepo:   "invalid-url",
		Version:   "v15.0.0",
		BinaryURL: "https://example.com/binary",
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	_, err := client.GetBinaryInfo(chainInfo)

	if err == nil {
		t.Error("Expected error for invalid git repo URL")
	}

	expectedError := "invalid git repository URL: invalid-url"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestClientClearCache(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	// Add something to cache
	client.cache["test"] = &ChainInfo{ChainName: "test"}

	if len(client.cache) != 1 {
		t.Errorf("Expected cache to have 1 item, got %d", len(client.cache))
	}

	client.ClearCache()

	if len(client.cache) != 0 {
		t.Errorf("Expected cache to be empty after clear, got %d items", len(client.cache))
	}
}

func TestClientListSupportedChains(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := NewClient(logger)

	chains := client.ListSupportedChains()

	if len(chains) == 0 {
		t.Error("Expected non-empty list of supported chains")
	}

	// Check for some well-known chains
	expectedChains := []string{"cosmoshub", "osmosis", "juno", "akash"}

	chainMap := make(map[string]bool)
	for _, chain := range chains {
		chainMap[chain] = true
	}

	for _, expected := range expectedChains {
		if !chainMap[expected] {
			t.Errorf("Expected chain '%s' to be in supported list", expected)
		}
	}
}

// Test SVG logo fallback
func TestGetChainInfo_SVGLogoFallback(t *testing.T) {
	mockResponse := ChainRegistryResponse{
		ChainName:    "testchain",
		PrettyName:   "Test Chain",
		ChainID:      "test-1",
		Bech32Prefix: "test",
		DaemonName:   "testd",
		LogoURIs: struct {
			PNG string `json:"png"`
			SVG string `json:"svg"`
		}{
			PNG: "", // No PNG
			SVG: "https://example.com/logo.svg",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)
	client.baseURL = server.URL

	ctx := context.Background()
	chainInfo, err := client.GetChainInfo(ctx, "testchain")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if chainInfo.LogoURL != "https://example.com/logo.svg" {
		t.Errorf("Expected SVG logo URL, got '%s'", chainInfo.LogoURL)
	}
}

// Test empty staking tokens
func TestGetChainInfo_NoStakingTokens(t *testing.T) {
	mockResponse := ChainRegistryResponse{
		ChainName:    "testchain",
		PrettyName:   "Test Chain",
		ChainID:      "test-1",
		Bech32Prefix: "test",
		DaemonName:   "testd",
		Staking: struct {
			StakingTokens []struct {
				Denom string `json:"denom"`
			} `json:"staking_tokens"`
		}{
			StakingTokens: []struct {
				Denom string `json:"denom"`
			}{}, // Empty staking tokens
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	client := NewClient(logger)
	client.baseURL = server.URL

	ctx := context.Background()
	chainInfo, err := client.GetChainInfo(ctx, "testchain")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if chainInfo.Denom != "" {
		t.Errorf("Expected empty denom, got '%s'", chainInfo.Denom)
	}
}
