package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"prop-voter/config"

	"go.uber.org/zap/zaptest"
)

func TestNewManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	if manager.client == nil {
		t.Error("Expected client to be initialized")
	}

	if manager.logger != logger {
		t.Error("Expected logger to be set")
	}
}

func TestPopulateChainConfigs_Success(t *testing.T) {
	// Create mock Chain Registry responses
	responses := map[string]ChainRegistryResponse{
		"osmosis": {
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
					"linux/amd64": "https://example.com/osmosisd",
				},
			},
			LogoURIs: struct {
				PNG string `json:"png"`
				SVG string `json:"svg"`
			}{
				PNG: "https://example.com/osmo.png",
			},
		},
		"juno": {
			ChainName:    "juno",
			PrettyName:   "Juno",
			ChainID:      "juno-1",
			Bech32Prefix: "juno",
			DaemonName:   "junod",
			Staking: struct {
				StakingTokens []struct {
					Denom string `json:"denom"`
				} `json:"staking_tokens"`
			}{
				StakingTokens: []struct {
					Denom string `json:"denom"`
				}{
					{Denom: "ujuno"},
				},
			},
			Codebase: struct {
				GitRepo            string            `json:"git_repo"`
				RecommendedVersion string            `json:"recommended_version"`
				Binaries           map[string]string `json:"binaries"`
			}{
				GitRepo:            "https://github.com/CosmosContracts/juno/",
				RecommendedVersion: "v12.0.0",
			},
		},
	}

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var chainName string
		switch r.URL.Path {
		case "/osmosis/chain.json":
			chainName = "osmosis"
		case "/juno/chain.json":
			chainName = "juno"
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses[chainName])
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)
	manager.client.baseURL = server.URL

	// Create test chains configuration
	chains := []config.ChainConfig{
		{
			ChainRegistryName: "osmosis",
			RPC:               "https://rpc-osmosis.example.com",
			REST:              "https://rest-osmosis.example.com",
			WalletKey:         "osmosis-key",
		},
		{
			ChainRegistryName: "juno",
			RPC:               "https://rpc-juno.example.com",
			REST:              "https://rest-juno.example.com",
			WalletKey:         "juno-key",
		},
		{
			// Legacy format chain - should be skipped
			Name:      "Custom Chain",
			ChainID:   "custom-1",
			RPC:       "https://rpc-custom.example.com",
			REST:      "https://rest-custom.example.com",
			WalletKey: "custom-key",
		},
	}

	ctx := context.Background()
	err := manager.PopulateChainConfigs(ctx, chains)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify osmosis chain was populated
	osmosisChain := &chains[0]
	if osmosisChain.RegistryInfo == nil {
		t.Fatal("Expected osmosis chain registry info to be populated")
	}

	if osmosisChain.GetChainID() != "osmosis-1" {
		t.Errorf("Expected chain ID 'osmosis-1', got '%s'", osmosisChain.GetChainID())
	}

	if osmosisChain.GetCLIName() != "osmosisd" {
		t.Errorf("Expected CLI name 'osmosisd', got '%s'", osmosisChain.GetCLIName())
	}

	if osmosisChain.GetDenom() != "uosmo" {
		t.Errorf("Expected denom 'uosmo', got '%s'", osmosisChain.GetDenom())
	}

	if osmosisChain.GetPrefix() != "osmo" {
		t.Errorf("Expected prefix 'osmo', got '%s'", osmosisChain.GetPrefix())
	}

	// Verify juno chain was populated
	junoChain := &chains[1]
	if junoChain.RegistryInfo == nil {
		t.Fatal("Expected juno chain registry info to be populated")
	}

	if junoChain.GetChainID() != "juno-1" {
		t.Errorf("Expected chain ID 'juno-1', got '%s'", junoChain.GetChainID())
	}

	// Verify legacy chain was not modified
	customChain := &chains[2]
	if customChain.RegistryInfo != nil {
		t.Error("Expected custom chain registry info to be nil")
	}

	if customChain.GetChainID() != "custom-1" {
		t.Errorf("Expected custom chain ID to remain 'custom-1', got '%s'", customChain.GetChainID())
	}
}

func TestPopulateChainConfigs_ChainNotFound(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)
	manager.client.baseURL = server.URL

	chains := []config.ChainConfig{
		{
			ChainRegistryName: "nonexistent",
			RPC:               "https://rpc.example.com",
			REST:              "https://rest.example.com",
			WalletKey:         "key",
		},
	}

	ctx := context.Background()
	err := manager.PopulateChainConfigs(ctx, chains)

	if err == nil {
		t.Error("Expected error for non-existent chain")
	}

	expectedErrorMsg := "failed to fetch chain info for nonexistent"
	if !contains(err.Error(), expectedErrorMsg) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedErrorMsg, err.Error())
	}
}

func TestGetBinaryInfoForChain_ChainRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	// Create chain config with registry info
	chain := &config.ChainConfig{
		ChainRegistryName: "osmosis",
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "osmosis-key",
		RegistryInfo: &config.ChainRegistryInfo{
			PrettyName:   "Osmosis",
			ChainID:      "osmosis-1",
			Bech32Prefix: "osmo",
			DaemonName:   "osmosisd",
			Denom:        "uosmo",
			GitRepo:      "https://github.com/osmosis-labs/osmosis/",
			Version:      "v15.0.0",
			BinaryURL:    "https://example.com/osmosisd",
		},
	}

	ctx := context.Background()
	binaryInfo, err := manager.GetBinaryInfoForChain(ctx, chain)

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
}

func TestGetBinaryInfoForChain_Legacy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	// Create legacy chain config
	chain := &config.ChainConfig{
		Name:      "Custom Chain",
		ChainID:   "custom-1",
		RPC:       "https://rpc.example.com",
		REST:      "https://rest.example.com",
		CLIName:   "customd",
		WalletKey: "custom-key",
		BinaryRepo: config.BinaryRepo{
			Enabled:      true,
			Owner:        "custom-org",
			Repo:         "custom-chain",
			AssetPattern: "*linux-amd64*",
		},
	}

	ctx := context.Background()
	binaryInfo, err := manager.GetBinaryInfoForChain(ctx, chain)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if binaryInfo.Owner != "custom-org" {
		t.Errorf("Expected owner 'custom-org', got '%s'", binaryInfo.Owner)
	}

	if binaryInfo.Repo != "custom-chain" {
		t.Errorf("Expected repo 'custom-chain', got '%s'", binaryInfo.Repo)
	}

	if binaryInfo.FileName != "customd" {
		t.Errorf("Expected filename 'customd', got '%s'", binaryInfo.FileName)
	}
}

func TestGetBinaryInfoForChain_LegacyDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	chain := &config.ChainConfig{
		Name:      "Custom Chain",
		ChainID:   "custom-1",
		CLIName:   "customd",
		WalletKey: "custom-key",
		BinaryRepo: config.BinaryRepo{
			Enabled: false, // Disabled
		},
	}

	ctx := context.Background()
	_, err := manager.GetBinaryInfoForChain(ctx, chain)

	if err == nil {
		t.Error("Expected error for disabled binary repository")
	}

	expectedError := "binary repository not enabled for chain Custom Chain"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestGetBinaryInfoForChain_ChainRegistryNoInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	chain := &config.ChainConfig{
		ChainRegistryName: "osmosis",
		// No RegistryInfo populated
	}

	ctx := context.Background()
	_, err := manager.GetBinaryInfoForChain(ctx, chain)

	if err == nil {
		t.Error("Expected error for missing registry info")
	}

	expectedError := "chain registry info not populated for osmosis"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestValidateChainConfig_ChainRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	// Valid Chain Registry config
	validConfig := &config.ChainConfig{
		ChainRegistryName: "osmosis",
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "osmosis-key",
	}

	err := manager.ValidateChainConfig(validConfig)
	if err != nil {
		t.Errorf("Expected no error for valid config, got: %v", err)
	}

	// Test config with no chain_name and no legacy fields (should require legacy fields)
	invalidConfig := &config.ChainConfig{
		ChainRegistryName: "", // Missing - this will be treated as legacy format
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "key",
		// Missing: Name, ChainID, CLIName, etc. for legacy format
	}

	err = manager.ValidateChainConfig(invalidConfig)
	if err == nil {
		t.Error("Expected error for missing legacy fields")
	}

	expectedError := "name is required when not using Chain Registry"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}

	// Test Chain Registry config with empty chain_name (should be treated as legacy and fail)
	invalidRegistryConfig := &config.ChainConfig{
		ChainRegistryName: "", // Empty - treated as legacy
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "key",
		// No legacy fields either
	}

	err = manager.ValidateChainConfig(invalidRegistryConfig)
	if err == nil {
		t.Error("Expected error for config with no chain_name and no legacy fields")
	}

	// Should require legacy fields since chain_name is empty
	if !strings.Contains(err.Error(), "name is required when not using Chain Registry") {
		t.Errorf("Expected error about missing name for legacy format, got '%s'", err.Error())
	}
}

func TestValidateChainConfig_Legacy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	// Valid legacy config
	validConfig := &config.ChainConfig{
		Name:      "Custom Chain",
		ChainID:   "custom-1",
		RPC:       "https://rpc.example.com",
		REST:      "https://rest.example.com",
		CLIName:   "customd",
		Denom:     "ucustom",
		Prefix:    "custom",
		WalletKey: "custom-key",
	}

	err := manager.ValidateChainConfig(validConfig)
	if err != nil {
		t.Errorf("Expected no error for valid legacy config, got: %v", err)
	}

	// Test missing name
	invalidConfig := &config.ChainConfig{
		Name:      "", // Missing
		ChainID:   "custom-1",
		RPC:       "https://rpc.example.com",
		REST:      "https://rest.example.com",
		WalletKey: "key",
	}

	err = manager.ValidateChainConfig(invalidConfig)
	if err == nil {
		t.Error("Expected error for missing name")
	}

	expectedError := "name is required when not using Chain Registry"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestValidateChainConfig_CommonFields(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	testCases := []struct {
		name        string
		config      *config.ChainConfig
		expectedErr string
	}{
		{
			name: "missing RPC",
			config: &config.ChainConfig{
				ChainRegistryName: "osmosis",
				RPC:               "", // Missing
				REST:              "https://rest.example.com",
				WalletKey:         "key",
			},
			expectedErr: "RPC endpoint is required",
		},
		{
			name: "missing REST",
			config: &config.ChainConfig{
				ChainRegistryName: "osmosis",
				RPC:               "https://rpc.example.com",
				REST:              "", // Missing
				WalletKey:         "key",
			},
			expectedErr: "REST endpoint is required",
		},
		{
			name: "missing wallet key",
			config: &config.ChainConfig{
				ChainRegistryName: "osmosis",
				RPC:               "https://rpc.example.com",
				REST:              "https://rest.example.com",
				WalletKey:         "", // Missing
			},
			expectedErr: "wallet key is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := manager.ValidateChainConfig(tc.config)
			if err == nil {
				t.Errorf("Expected error for %s", tc.name)
			}

			if err.Error() != tc.expectedErr {
				t.Errorf("Expected error '%s', got '%s'", tc.expectedErr, err.Error())
			}
		})
	}
}

func TestManagerListSupportedChains(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	chains := manager.ListSupportedChains()

	if len(chains) == 0 {
		t.Error("Expected non-empty list of supported chains")
	}

	// Check for some well-known chains
	expectedChains := []string{"cosmoshub", "osmosis", "juno"}

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

func TestManagerClearCache(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewManager(logger)

	// Add something to the client cache through manager
	manager.client.cache["test"] = &ChainInfo{ChainName: "test"}

	if len(manager.client.cache) != 1 {
		t.Errorf("Expected cache to have 1 item, got %d", len(manager.client.cache))
	}

	manager.ClearCache()

	if len(manager.client.cache) != 0 {
		t.Errorf("Expected cache to be empty after clear, got %d items", len(manager.client.cache))
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					func() bool {
						for i := 1; i < len(s)-len(substr)+1; i++ {
							if s[i:i+len(substr)] == substr {
								return true
							}
						}
						return false
					}())))
}
