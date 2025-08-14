package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	Discord       DiscordConfig   `mapstructure:"discord"`
	Database      DatabaseConfig  `mapstructure:"database"`
	Security      SecurityConfig  `mapstructure:"security"`
	Chains        []ChainConfig   `mapstructure:"chains"`
	Scanning      ScanConfig      `mapstructure:"scanning"`
	Health        HealthConfig    `mapstructure:"health"`
	BinaryManager BinaryMgrConfig `mapstructure:"binary_manager"`
	KeyManager    KeyMgrConfig    `mapstructure:"key_manager"`
}

// DiscordConfig holds Discord bot configuration
type DiscordConfig struct {
	Token       string `mapstructure:"token"`
	ChannelID   string `mapstructure:"channel_id"`
	AllowedUser string `mapstructure:"allowed_user_id"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	EncryptionKey string `mapstructure:"encryption_key"`
	VoteSecret    string `mapstructure:"vote_secret"`
}

// ChainConfig represents a single Cosmos chain configuration
type ChainConfig struct {
	Name       string     `mapstructure:"name"`
	ChainID    string     `mapstructure:"chain_id"`
	RPC        string     `mapstructure:"rpc"`
	REST       string     `mapstructure:"rest"`
	Denom      string     `mapstructure:"denom"`
	Prefix     string     `mapstructure:"prefix"`
	CLIName    string     `mapstructure:"cli_name"`
	WalletKey  string     `mapstructure:"wallet_key"`
	LogoURL    string     `mapstructure:"logo_url"` // Chain logo URL for Discord embeds
	BinaryRepo BinaryRepo `mapstructure:"binary_repo"`
}

// BinaryRepo represents GitHub repository information for binary management
type BinaryRepo struct {
	Owner        string `mapstructure:"owner"`         // GitHub owner/org
	Repo         string `mapstructure:"repo"`          // Repository name
	AssetPattern string `mapstructure:"asset_pattern"` // Pattern to match release assets (e.g., "*linux-amd64*")
	Enabled      bool   `mapstructure:"enabled"`       // Whether to auto-update this binary
}

// ScanConfig holds scanning configuration
type ScanConfig struct {
	Interval  time.Duration `mapstructure:"interval"`
	BatchSize int           `mapstructure:"batch_size"`
}

// HealthConfig holds health endpoint configuration
type HealthConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// BinaryMgrConfig holds binary manager configuration
type BinaryMgrConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	BinDir        string        `mapstructure:"bin_dir"`
	CheckInterval time.Duration `mapstructure:"check_interval"`
	AutoUpdate    bool          `mapstructure:"auto_update"`
	BackupOld     bool          `mapstructure:"backup_old"`
}

// KeyMgrConfig holds key manager configuration
type KeyMgrConfig struct {
	AutoImport  bool   `mapstructure:"auto_import"`
	KeyDir      string `mapstructure:"key_dir"`
	BackupKeys  bool   `mapstructure:"backup_keys"`
	EncryptKeys bool   `mapstructure:"encrypt_keys"`
}

// LoadConfig loads configuration from file
func LoadConfig(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// Set defaults
	viper.SetDefault("scanning.interval", "5m")
	viper.SetDefault("scanning.batch_size", 10)
	viper.SetDefault("database.path", "./prop-voter.db")
	viper.SetDefault("health.enabled", true)
	viper.SetDefault("health.port", 8080)
	viper.SetDefault("health.path", "/health")
	viper.SetDefault("binary_manager.enabled", true)
	viper.SetDefault("binary_manager.bin_dir", "./bin")
	viper.SetDefault("binary_manager.check_interval", "24h")
	viper.SetDefault("binary_manager.auto_update", false)
	viper.SetDefault("binary_manager.backup_old", true)
	viper.SetDefault("key_manager.auto_import", false)
	viper.SetDefault("key_manager.key_dir", "./keys")
	viper.SetDefault("key_manager.backup_keys", true)
	viper.SetDefault("key_manager.encrypt_keys", true)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}
