package test

import (
	"context"
	"testing"
	"time"

	"prop-voter/config"
	"prop-voter/internal/registry"

	"go.uber.org/zap/zaptest"
)

// TestChainRegistryIntegration tests the real Chain Registry integration
func TestChainRegistryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	client := registry.NewClient(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test fetching real chains from Chain Registry
	testChains := []string{"osmosis", "juno", "akash"}

	for _, chainName := range testChains {
		t.Run("fetch_"+chainName, func(t *testing.T) {
			chainInfo, err := client.GetChainInfo(ctx, chainName)
			if err != nil {
				t.Fatalf("Failed to fetch %s chain info: %v", chainName, err)
			}

			// Validate required fields are populated
			if chainInfo.ChainName != chainName {
				t.Errorf("Expected chain name '%s', got '%s'", chainName, chainInfo.ChainName)
			}

			if chainInfo.PrettyName == "" {
				t.Error("Expected non-empty pretty name")
			}

			if chainInfo.ChainID == "" {
				t.Error("Expected non-empty chain ID")
			}

			if chainInfo.Bech32Prefix == "" {
				t.Error("Expected non-empty bech32 prefix")
			}

			if chainInfo.DaemonName == "" {
				t.Error("Expected non-empty daemon name")
			}

			if chainInfo.Denom == "" {
				t.Error("Expected non-empty denom")
			}

			if chainInfo.GitRepo == "" {
				t.Error("Expected non-empty git repo")
			}

			if chainInfo.Version == "" {
				t.Error("Expected non-empty version")
			}

			t.Logf("Successfully fetched %s: chain_id=%s, daemon=%s, version=%s",
				chainName, chainInfo.ChainID, chainInfo.DaemonName, chainInfo.Version)

			// Test binary info extraction if binary URL is available
			if chainInfo.BinaryURL != "" {
				binaryInfo, err := client.GetBinaryInfo(chainInfo)
				if err != nil {
					t.Logf("Binary info extraction failed for %s (not critical): %v", chainName, err)
				} else {
					if binaryInfo.Owner == "" {
						t.Error("Expected non-empty owner")
					}
					if binaryInfo.Repo == "" {
						t.Error("Expected non-empty repo")
					}
					if binaryInfo.FileName == "" {
						t.Error("Expected non-empty filename")
					}

					t.Logf("Binary info for %s: owner=%s, repo=%s, filename=%s",
						chainName, binaryInfo.Owner, binaryInfo.Repo, binaryInfo.FileName)
				}
			} else {
				t.Logf("No binary URL available for %s (may not support current platform)", chainName)
			}
		})
	}
}

// TestChainRegistryManager tests the manager with real Chain Registry
func TestChainRegistryManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	manager := registry.NewManager(logger)

	// Create test chain configurations
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
			// Legacy chain - should not be affected
			Name:      "Custom Chain",
			ChainID:   "custom-1",
			RPC:       "https://rpc-custom.example.com",
			REST:      "https://rest-custom.example.com",
			CLIName:   "customd",
			Denom:     "ucustom",
			Prefix:    "custom",
			WalletKey: "custom-key",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := manager.PopulateChainConfigs(ctx, chains)
	if err != nil {
		t.Fatalf("Failed to populate chain configs: %v", err)
	}

	// Verify osmosis chain was populated correctly
	osmosisChain := &chains[0]
	if !osmosisChain.UsesChainRegistry() {
		t.Error("Expected osmosis chain to use Chain Registry")
	}

	if osmosisChain.RegistryInfo == nil {
		t.Fatal("Expected osmosis registry info to be populated")
	}

	if osmosisChain.GetChainID() != "osmosis-1" {
		t.Errorf("Expected osmosis chain ID to be 'osmosis-1', got '%s'", osmosisChain.GetChainID())
	}

	if osmosisChain.GetCLIName() != "osmosisd" {
		t.Errorf("Expected osmosis CLI name to be 'osmosisd', got '%s'", osmosisChain.GetCLIName())
	}

	if osmosisChain.GetDenom() != "uosmo" {
		t.Errorf("Expected osmosis denom to be 'uosmo', got '%s'", osmosisChain.GetDenom())
	}

	if osmosisChain.GetPrefix() != "osmo" {
		t.Errorf("Expected osmosis prefix to be 'osmo', got '%s'", osmosisChain.GetPrefix())
	}

	// Verify juno chain was populated correctly
	junoChain := &chains[1]
	if !junoChain.UsesChainRegistry() {
		t.Error("Expected juno chain to use Chain Registry")
	}

	if junoChain.RegistryInfo == nil {
		t.Fatal("Expected juno registry info to be populated")
	}

	if junoChain.GetChainID() != "juno-1" {
		t.Errorf("Expected juno chain ID to be 'juno-1', got '%s'", junoChain.GetChainID())
	}

	if junoChain.GetCLIName() != "junod" {
		t.Errorf("Expected juno CLI name to be 'junod', got '%s'", junoChain.GetCLIName())
	}

	// Verify legacy chain was not modified
	customChain := &chains[2]
	if customChain.UsesChainRegistry() {
		t.Error("Expected custom chain not to use Chain Registry")
	}

	if customChain.RegistryInfo != nil {
		t.Error("Expected custom chain registry info to be nil")
	}

	if customChain.GetChainID() != "custom-1" {
		t.Errorf("Expected custom chain ID to remain 'custom-1', got '%s'", customChain.GetChainID())
	}

	if customChain.GetCLIName() != "customd" {
		t.Errorf("Expected custom CLI name to remain 'customd', got '%s'", customChain.GetCLIName())
	}

	t.Logf("Successfully populated %d Chain Registry chains", 2)
}

// TestChainRegistryValidation tests configuration validation
func TestChainRegistryValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := registry.NewManager(logger)

	validConfigs := []*config.ChainConfig{
		// Valid Chain Registry config
		{
			ChainRegistryName: "osmosis",
			RPC:               "https://rpc.example.com",
			REST:              "https://rest.example.com",
			WalletKey:         "osmosis-key",
		},
		// Valid legacy config
		{
			Name:      "Custom Chain",
			ChainID:   "custom-1",
			RPC:       "https://rpc.example.com",
			REST:      "https://rest.example.com",
			CLIName:   "customd",
			Denom:     "ucustom",
			Prefix:    "custom",
			WalletKey: "custom-key",
		},
	}

	for i, config := range validConfigs {
		err := manager.ValidateChainConfig(config)
		if err != nil {
			t.Errorf("Expected valid config %d to pass validation, got error: %v", i, err)
		}
	}

	invalidConfigs := []*config.ChainConfig{
		// Missing RPC
		{
			ChainRegistryName: "osmosis",
			REST:              "https://rest.example.com",
			WalletKey:         "key",
		},
		// Missing chain_name for Chain Registry
		{
			RPC:       "https://rpc.example.com",
			REST:      "https://rest.example.com",
			WalletKey: "key",
		},
		// Missing name for legacy
		{
			ChainID:   "custom-1",
			RPC:       "https://rpc.example.com",
			REST:      "https://rest.example.com",
			WalletKey: "key",
		},
	}

	for i, config := range invalidConfigs {
		err := manager.ValidateChainConfig(config)
		if err == nil {
			t.Errorf("Expected invalid config %d to fail validation", i)
		}
	}
}

// TestChainRegistryCache tests caching functionality
func TestChainRegistryCache(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	client := registry.NewClient(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chainName := "osmosis"

	// First request - should hit the network
	start := time.Now()
	chainInfo1, err := client.GetChainInfo(ctx, chainName)
	if err != nil {
		t.Fatalf("Failed to fetch chain info: %v", err)
	}
	duration1 := time.Since(start)

	// Second request - should use cache (much faster)
	start = time.Now()
	chainInfo2, err := client.GetChainInfo(ctx, chainName)
	if err != nil {
		t.Fatalf("Failed to fetch cached chain info: %v", err)
	}
	duration2 := time.Since(start)

	// Verify same data returned
	if chainInfo1.ChainID != chainInfo2.ChainID {
		t.Error("Expected same chain ID from cache")
	}

	if chainInfo1.DaemonName != chainInfo2.DaemonName {
		t.Error("Expected same daemon name from cache")
	}

	// Cache should be significantly faster (unless network is extremely fast)
	if duration2 > duration1/2 {
		t.Logf("Cache performance: network=%v, cache=%v (cache may not be significantly faster on very fast networks)", duration1, duration2)
	} else {
		t.Logf("Cache performance verified: network=%v, cache=%v", duration1, duration2)
	}

	// Test cache clearing
	client.ClearCache()

	// Third request - should hit network again
	start = time.Now()
	_, err = client.GetChainInfo(ctx, chainName)
	if err != nil {
		t.Fatalf("Failed to fetch chain info after cache clear: %v", err)
	}
	duration3 := time.Since(start)

	t.Logf("Performance after cache clear: %v", duration3)
}

// TestChainRegistryErrorHandling tests error handling
func TestChainRegistryErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	client := registry.NewClient(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test non-existent chain
	_, err := client.GetChainInfo(ctx, "nonexistent-chain-12345")
	if err == nil {
		t.Error("Expected error for non-existent chain")
	}

	t.Logf("Error for non-existent chain (expected): %v", err)

	// Test invalid chain name with special characters
	_, err = client.GetChainInfo(ctx, "invalid/chain@name")
	if err == nil {
		t.Error("Expected error for invalid chain name")
	}

	t.Logf("Error for invalid chain name (expected): %v", err)
}
