package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	configContent := `
discord:
  token: "test-token"
  channel_id: "123456789"
  allowed_user_id: "987654321"

database:
  path: "./test.db"

security:
  encryption_key: "test-encryption-key-32-characters"
  vote_secret: "test-secret"

scanning:
  interval: "10m"
  batch_size: 5

health:
  enabled: true
  port: 9090
  path: "/test-health"

chains:
  - name: "Test Chain"
    chain_id: "test-1"
    rpc: "http://localhost:26657"
    rest: "http://localhost:1317"
    denom: "utest"
    prefix: "test"
    cli_name: "testd"
    wallet_key: "test-key"
`

	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	// Test loading the config
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test Discord config
	if cfg.Discord.Token != "test-token" {
		t.Errorf("Expected Discord token 'test-token', got '%s'", cfg.Discord.Token)
	}
	if cfg.Discord.ChannelID != "123456789" {
		t.Errorf("Expected Discord channel ID '123456789', got '%s'", cfg.Discord.ChannelID)
	}
	if cfg.Discord.AllowedUser != "987654321" {
		t.Errorf("Expected Discord allowed user '987654321', got '%s'", cfg.Discord.AllowedUser)
	}

	// Test Database config
	if cfg.Database.Path != "./test.db" {
		t.Errorf("Expected database path './test.db', got '%s'", cfg.Database.Path)
	}

	// Test Security config
	if cfg.Security.EncryptionKey != "test-encryption-key-32-characters" {
		t.Errorf("Expected encryption key 'test-encryption-key-32-characters', got '%s'", cfg.Security.EncryptionKey)
	}
	if cfg.Security.VoteSecret != "test-secret" {
		t.Errorf("Expected vote secret 'test-secret', got '%s'", cfg.Security.VoteSecret)
	}

	// Test Scanning config
	expectedInterval := 10 * time.Minute
	if cfg.Scanning.Interval != expectedInterval {
		t.Errorf("Expected scanning interval %v, got %v", expectedInterval, cfg.Scanning.Interval)
	}
	if cfg.Scanning.BatchSize != 5 {
		t.Errorf("Expected scanning batch size 5, got %d", cfg.Scanning.BatchSize)
	}

	// Test Health config
	if !cfg.Health.Enabled {
		t.Error("Expected health to be enabled")
	}
	if cfg.Health.Port != 9090 {
		t.Errorf("Expected health port 9090, got %d", cfg.Health.Port)
	}
	if cfg.Health.Path != "/test-health" {
		t.Errorf("Expected health path '/test-health', got '%s'", cfg.Health.Path)
	}

	// Test Chains config
	if len(cfg.Chains) != 1 {
		t.Fatalf("Expected 1 chain, got %d", len(cfg.Chains))
	}

	chain := cfg.Chains[0]
	if chain.Name != "Test Chain" {
		t.Errorf("Expected chain name 'Test Chain', got '%s'", chain.Name)
	}
	if chain.ChainID != "test-1" {
		t.Errorf("Expected chain ID 'test-1', got '%s'", chain.ChainID)
	}
	if chain.RPC != "http://localhost:26657" {
		t.Errorf("Expected RPC 'http://localhost:26657', got '%s'", chain.RPC)
	}
	if chain.REST != "http://localhost:1317" {
		t.Errorf("Expected REST 'http://localhost:1317', got '%s'", chain.REST)
	}
	if chain.Denom != "utest" {
		t.Errorf("Expected denom 'utest', got '%s'", chain.Denom)
	}
	if chain.Prefix != "test" {
		t.Errorf("Expected prefix 'test', got '%s'", chain.Prefix)
	}
	if chain.CLIName != "testd" {
		t.Errorf("Expected CLI name 'testd', got '%s'", chain.CLIName)
	}
	if chain.WalletKey != "test-key" {
		t.Errorf("Expected wallet key 'test-key', got '%s'", chain.WalletKey)
	}

	// Test helper methods for legacy format
	if chain.UsesChainRegistry() {
		t.Error("Expected legacy chain not to use Chain Registry")
	}

	if chain.GetName() != "Test Chain" {
		t.Errorf("Expected GetName() to return 'Test Chain', got '%s'", chain.GetName())
	}

	if chain.GetChainID() != "test-1" {
		t.Errorf("Expected GetChainID() to return 'test-1', got '%s'", chain.GetChainID())
	}

	if chain.GetCLIName() != "testd" {
		t.Errorf("Expected GetCLIName() to return 'testd', got '%s'", chain.GetCLIName())
	}

	if chain.GetDenom() != "utest" {
		t.Errorf("Expected GetDenom() to return 'utest', got '%s'", chain.GetDenom())
	}

	if chain.GetPrefix() != "test" {
		t.Errorf("Expected GetPrefix() to return 'test', got '%s'", chain.GetPrefix())
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Create a minimal config file to test defaults
	configContent := `
discord:
  token: "test-token"
  channel_id: "123456789"
  allowed_user_id: "987654321"

security:
  encryption_key: "test-encryption-key-32-characters"
  vote_secret: "test-secret"

chains: []
`

	tmpFile, err := os.CreateTemp("", "test-config-defaults-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test defaults
	expectedInterval := 5 * time.Minute
	if cfg.Scanning.Interval != expectedInterval {
		t.Errorf("Expected default scanning interval %v, got %v", expectedInterval, cfg.Scanning.Interval)
	}
	if cfg.Scanning.BatchSize != 10 {
		t.Errorf("Expected default scanning batch size 10, got %d", cfg.Scanning.BatchSize)
	}
	if cfg.Database.Path != "./prop-voter.db" {
		t.Errorf("Expected default database path './prop-voter.db', got '%s'", cfg.Database.Path)
	}
	if !cfg.Health.Enabled {
		t.Error("Expected default health to be enabled")
	}
	if cfg.Health.Port != 8080 {
		t.Errorf("Expected default health port 8080, got %d", cfg.Health.Port)
	}
	if cfg.Health.Path != "/health" {
		t.Errorf("Expected default health path '/health', got '%s'", cfg.Health.Path)
	}
}

func TestLoadConfigError(t *testing.T) {
	// Test loading non-existent config file
	_, err := LoadConfig("non-existent-file.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	// Create an invalid YAML file
	invalidYAML := `
discord:
  token: "test-token"
  channel_id: 123456789  # This should be a string
  invalid: [unclosed array
`

	tmpFile, err := os.CreateTemp("", "test-invalid-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(invalidYAML); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	if err == nil {
		t.Error("Expected error when loading invalid YAML config file")
	}
}

func TestLoadConfigChainRegistry(t *testing.T) {
	// Create a config file with Chain Registry format
	configContent := `
discord:
  token: "test-token"
  channel_id: "123456789"
  allowed_user_id: "987654321"

database:
  path: "./test.db"

security:
  encryption_key: "test-encryption-key-32-characters"
  vote_secret: "test-secret"

chains:
  # Chain Registry format
  - chain_name: "osmosis"
    rpc: "https://rpc-osmosis.example.com"
    rest: "https://rest-osmosis.example.com"
    wallet_key: "osmosis-key"
  
  # Legacy format
  - name: "Custom Chain"
    chain_id: "custom-1"
    rpc: "http://localhost:26657"
    rest: "http://localhost:1317"
    denom: "ucustom"
    prefix: "custom"
    cli_name: "customd"
    wallet_key: "custom-key"
`

	tmpFile, err := os.CreateTemp("", "test-config-registry-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	// Test loading the config
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test that we have 2 chains
	if len(cfg.Chains) != 2 {
		t.Fatalf("Expected 2 chains, got %d", len(cfg.Chains))
	}

	// Test Chain Registry format chain
	registryChain := cfg.Chains[0]
	if !registryChain.UsesChainRegistry() {
		t.Error("Expected first chain to use Chain Registry")
	}

	if registryChain.ChainRegistryName != "osmosis" {
		t.Errorf("Expected chain registry name 'osmosis', got '%s'", registryChain.ChainRegistryName)
	}

	if registryChain.RPC != "https://rpc-osmosis.example.com" {
		t.Errorf("Expected RPC 'https://rpc-osmosis.example.com', got '%s'", registryChain.RPC)
	}

	if registryChain.REST != "https://rest-osmosis.example.com" {
		t.Errorf("Expected REST 'https://rest-osmosis.example.com', got '%s'", registryChain.REST)
	}

	if registryChain.WalletKey != "osmosis-key" {
		t.Errorf("Expected wallet key 'osmosis-key', got '%s'", registryChain.WalletKey)
	}

	// Test that legacy fields are empty for Chain Registry format
	if registryChain.Name != "" {
		t.Errorf("Expected empty name for Chain Registry format, got '%s'", registryChain.Name)
	}

	if registryChain.ChainID != "" {
		t.Errorf("Expected empty chain ID for Chain Registry format, got '%s'", registryChain.ChainID)
	}

	// Test legacy format chain
	legacyChain := cfg.Chains[1]
	if legacyChain.UsesChainRegistry() {
		t.Error("Expected second chain not to use Chain Registry")
	}

	if legacyChain.ChainRegistryName != "" {
		t.Errorf("Expected empty chain registry name for legacy format, got '%s'", legacyChain.ChainRegistryName)
	}

	if legacyChain.Name != "Custom Chain" {
		t.Errorf("Expected name 'Custom Chain', got '%s'", legacyChain.Name)
	}

	if legacyChain.ChainID != "custom-1" {
		t.Errorf("Expected chain ID 'custom-1', got '%s'", legacyChain.ChainID)
	}
}

func TestChainConfigHelperMethods(t *testing.T) {
	// Test Chain Registry format with populated registry info
	registryChain := ChainConfig{
		ChainRegistryName: "osmosis",
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "osmosis-key",
		RegistryInfo: &ChainRegistryInfo{
			PrettyName:   "Osmosis",
			ChainID:      "osmosis-1",
			Bech32Prefix: "osmo",
			DaemonName:   "osmosisd",
			Denom:        "uosmo",
			LogoURL:      "https://example.com/logo.png",
		},
	}

	if !registryChain.UsesChainRegistry() {
		t.Error("Expected chain to use Chain Registry")
	}

	if registryChain.GetName() != "Osmosis" {
		t.Errorf("Expected GetName() to return 'Osmosis', got '%s'", registryChain.GetName())
	}

	if registryChain.GetChainID() != "osmosis-1" {
		t.Errorf("Expected GetChainID() to return 'osmosis-1', got '%s'", registryChain.GetChainID())
	}

	if registryChain.GetCLIName() != "osmosisd" {
		t.Errorf("Expected GetCLIName() to return 'osmosisd', got '%s'", registryChain.GetCLIName())
	}

	if registryChain.GetDenom() != "uosmo" {
		t.Errorf("Expected GetDenom() to return 'uosmo', got '%s'", registryChain.GetDenom())
	}

	if registryChain.GetPrefix() != "osmo" {
		t.Errorf("Expected GetPrefix() to return 'osmo', got '%s'", registryChain.GetPrefix())
	}

	if registryChain.GetLogoURL() != "https://example.com/logo.png" {
		t.Errorf("Expected GetLogoURL() to return 'https://example.com/logo.png', got '%s'", registryChain.GetLogoURL())
	}

	// Test Chain Registry format without populated registry info (fallback to legacy)
	registryChainEmpty := ChainConfig{
		ChainRegistryName: "osmosis",
		Name:              "Fallback Name",
		ChainID:           "fallback-1",
		CLIName:           "fallbackd",
		Denom:             "ufallback",
		Prefix:            "fallback",
		LogoURL:           "https://example.com/fallback.png",
	}

	if registryChainEmpty.GetName() != "Fallback Name" {
		t.Errorf("Expected GetName() to fallback to 'Fallback Name', got '%s'", registryChainEmpty.GetName())
	}

	if registryChainEmpty.GetChainID() != "fallback-1" {
		t.Errorf("Expected GetChainID() to fallback to 'fallback-1', got '%s'", registryChainEmpty.GetChainID())
	}

	// Test legacy format
	legacyChain := ChainConfig{
		Name:      "Legacy Chain",
		ChainID:   "legacy-1",
		CLIName:   "legacyd",
		Denom:     "ulegacy",
		Prefix:    "legacy",
		WalletKey: "legacy-key",
		LogoURL:   "https://example.com/legacy.png",
	}

	if legacyChain.UsesChainRegistry() {
		t.Error("Expected legacy chain not to use Chain Registry")
	}

	if legacyChain.GetName() != "Legacy Chain" {
		t.Errorf("Expected GetName() to return 'Legacy Chain', got '%s'", legacyChain.GetName())
	}

	if legacyChain.GetChainID() != "legacy-1" {
		t.Errorf("Expected GetChainID() to return 'legacy-1', got '%s'", legacyChain.GetChainID())
	}

	if legacyChain.GetCLIName() != "legacyd" {
		t.Errorf("Expected GetCLIName() to return 'legacyd', got '%s'", legacyChain.GetCLIName())
	}

	if legacyChain.GetDenom() != "ulegacy" {
		t.Errorf("Expected GetDenom() to return 'ulegacy', got '%s'", legacyChain.GetDenom())
	}

	if legacyChain.GetPrefix() != "legacy" {
		t.Errorf("Expected GetPrefix() to return 'legacy', got '%s'", legacyChain.GetPrefix())
	}

	if legacyChain.GetLogoURL() != "https://example.com/legacy.png" {
		t.Errorf("Expected GetLogoURL() to return 'https://example.com/legacy.png', got '%s'", legacyChain.GetLogoURL())
	}
}

func TestPopulateFromRegistry(t *testing.T) {
	chain := ChainConfig{
		ChainRegistryName: "osmosis",
		RPC:               "https://rpc.example.com",
		REST:              "https://rest.example.com",
		WalletKey:         "osmosis-key",
	}

	registryInfo := &ChainRegistryInfo{
		PrettyName:   "Osmosis",
		ChainID:      "osmosis-1",
		Bech32Prefix: "osmo",
		DaemonName:   "osmosisd",
		Denom:        "uosmo",
		LogoURL:      "https://example.com/logo.png",
		GitRepo:      "https://github.com/osmosis-labs/osmosis/",
		Version:      "v15.0.0",
		BinaryURL:    "https://example.com/osmosisd",
	}

	chain.PopulateFromRegistry(registryInfo)

	if chain.RegistryInfo == nil {
		t.Fatal("Expected registry info to be populated")
	}

	if chain.RegistryInfo.PrettyName != "Osmosis" {
		t.Errorf("Expected pretty name 'Osmosis', got '%s'", chain.RegistryInfo.PrettyName)
	}

	if chain.RegistryInfo.ChainID != "osmosis-1" {
		t.Errorf("Expected chain ID 'osmosis-1', got '%s'", chain.RegistryInfo.ChainID)
	}

	// Test that helper methods now use registry info
	if chain.GetName() != "Osmosis" {
		t.Errorf("Expected GetName() to return 'Osmosis', got '%s'", chain.GetName())
	}

	if chain.GetChainID() != "osmosis-1" {
		t.Errorf("Expected GetChainID() to return 'osmosis-1', got '%s'", chain.GetChainID())
	}
}

func TestChainConfigAuthzHelperMethods(t *testing.T) {
	// Test authz enabled with granter address
	authzEnabledChain := ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		WalletKey: "grantee-key",
		Authz: AuthzConfig{
			Enabled:     true,
			GranterAddr: "test1granter123addr456",
			GranterName: "Test Validator",
		},
	}

	if !authzEnabledChain.IsAuthzEnabled() {
		t.Error("Expected authz to be enabled when Enabled=true and GranterAddr is set")
	}

	if authzEnabledChain.GetGranterAddr() != "test1granter123addr456" {
		t.Errorf("Expected GetGranterAddr() to return 'test1granter123addr456', got '%s'", authzEnabledChain.GetGranterAddr())
	}

	if authzEnabledChain.GetGranterName() != "Test Validator" {
		t.Errorf("Expected GetGranterName() to return 'Test Validator', got '%s'", authzEnabledChain.GetGranterName())
	}

	// Test authz enabled but no granter address (should be disabled)
	authzEnabledNoAddr := ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		WalletKey: "grantee-key",
		Authz: AuthzConfig{
			Enabled:     true,
			GranterAddr: "", // No granter address
			GranterName: "Test Validator",
		},
	}

	if authzEnabledNoAddr.IsAuthzEnabled() {
		t.Error("Expected authz to be disabled when GranterAddr is empty")
	}

	// Test authz disabled explicitly
	authzDisabled := ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		WalletKey: "grantee-key",
		Authz: AuthzConfig{
			Enabled:     false,
			GranterAddr: "test1granter123addr456",
			GranterName: "Test Validator",
		},
	}

	if authzDisabled.IsAuthzEnabled() {
		t.Error("Expected authz to be disabled when Enabled=false")
	}

	// Test authz not configured (default values)
	authzDefault := ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		WalletKey: "grantee-key",
		// Authz field is not set, uses zero values
	}

	if authzDefault.IsAuthzEnabled() {
		t.Error("Expected authz to be disabled by default")
	}

	if authzDefault.GetGranterAddr() != "" {
		t.Errorf("Expected GetGranterAddr() to return empty string by default, got '%s'", authzDefault.GetGranterAddr())
	}

	if authzDefault.GetGranterName() != "" {
		t.Errorf("Expected GetGranterName() to return empty string by default, got '%s'", authzDefault.GetGranterName())
	}

	// Test granter name fallback to address when name is not set
	authzNoName := ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		WalletKey: "grantee-key",
		Authz: AuthzConfig{
			Enabled:     true,
			GranterAddr: "test1granter123addr456",
			GranterName: "", // No friendly name
		},
	}

	if authzNoName.GetGranterName() != "test1granter123addr456" {
		t.Errorf("Expected GetGranterName() to fallback to address when name is empty, got '%s'", authzNoName.GetGranterName())
	}
}
