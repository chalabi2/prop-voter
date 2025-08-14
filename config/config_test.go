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
