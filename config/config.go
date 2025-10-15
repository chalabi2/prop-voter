package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	Discord       DiscordConfig       `mapstructure:"discord"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Security      SecurityConfig      `mapstructure:"security"`
	AuthEndpoints AuthEndpointsConfig `mapstructure:"auth_endpoints"`
	Chains        []ChainConfig       `mapstructure:"chains"`
	Scanning      ScanConfig          `mapstructure:"scanning"`
	Health        HealthConfig        `mapstructure:"health"`
	BinaryManager BinaryMgrConfig     `mapstructure:"binary_manager"`
	KeyManager    KeyMgrConfig        `mapstructure:"key_manager"`
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

// AuthEndpointsConfig controls optional API key query param on RPC/REST endpoints
type AuthEndpointsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	APIKey  string `mapstructure:"api_key"`
}

// ChainConfig represents a single Cosmos chain configuration
type ChainConfig struct {
	// New simplified format using Chain Registry
	ChainRegistryName string `mapstructure:"chain_name"` // Chain Registry identifier (e.g., "osmosis")

	// Required fields for both formats
	RPC       string `mapstructure:"rpc"`
	REST      string `mapstructure:"rest"`
	WalletKey string `mapstructure:"wallet_key"`

	// Authz configuration for voting on behalf of other wallets
	Authz AuthzConfig `mapstructure:"authz"`

	// Legacy format fields (optional when using Chain Registry)
	Name       string     `mapstructure:"name"`
	ChainID    string     `mapstructure:"chain_id"`
	Denom      string     `mapstructure:"denom"`
	Prefix     string     `mapstructure:"prefix"`
	CLIName    string     `mapstructure:"cli_name"`
	LogoURL    string     `mapstructure:"logo_url"`
	BinaryRepo BinaryRepo `mapstructure:"binary_repo"`

	// Binary source configuration (works for both formats)
	BinarySource BinarySource `mapstructure:"binary_source"`

	// Runtime fields populated from Chain Registry (not in config file)
	RegistryInfo *ChainRegistryInfo `mapstructure:"-"`
}

// ChainRegistryInfo holds information fetched from Chain Registry
type ChainRegistryInfo struct {
	PrettyName   string
	ChainID      string
	Bech32Prefix string
	DaemonName   string
	Denom        string
	Decimals     int
	LogoURL      string
	GitRepo      string
	Version      string
	BinaryURL    string
}

// AuthzConfig holds authorization configuration for voting on behalf of other wallets
type AuthzConfig struct {
	Enabled     bool   `mapstructure:"enabled"`      // Whether authz voting is enabled for this chain
	GranterAddr string `mapstructure:"granter_addr"` // Address of the wallet we vote on behalf of
	GranterName string `mapstructure:"granter_name"` // Optional friendly name for the granter
}

// BinaryRepo represents GitHub repository information for binary management
type BinaryRepo struct {
	Owner        string `mapstructure:"owner"`         // GitHub owner/org
	Repo         string `mapstructure:"repo"`          // Repository name
	AssetPattern string `mapstructure:"asset_pattern"` // Pattern to match release assets (e.g., "*linux-amd64*")
	Enabled      bool   `mapstructure:"enabled"`       // Whether to auto-update this binary
}

// BinarySource represents different ways to obtain binaries
type BinarySource struct {
	Type              string `mapstructure:"type"`                // "registry", "github", "url", "source"
	CustomURL         string `mapstructure:"custom_url"`          // Direct URL to binary (for type "url")
	SourceRepo        string `mapstructure:"source_repo"`         // Git repository URL (for type "source")
	SourceBranch      string `mapstructure:"source_branch"`       // Git branch/tag to build (for type "source")
	BuildCommand      string `mapstructure:"build_command"`       // Custom build command (for type "source")
	BuildTarget       string `mapstructure:"build_target"`        // Build target binary name (for type "source")
	CompileFromSource bool   `mapstructure:"compile_from_source"` // Whether to compile from source as fallback
	IgnoreGoVersion   bool   `mapstructure:"ignore_go_version"`   // Whether to ignore Go version requirements
	RequiredGoVersion string `mapstructure:"required_go_version"` // Override required Go version (e.g., "go1.20")
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
	viper.SetDefault("auth_endpoints.enabled", false)
	viper.SetDefault("auth_endpoints.api_key", "")
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

// Helper methods for ChainConfig

// UsesChainRegistry returns true if this chain uses Chain Registry format
func (c *ChainConfig) UsesChainRegistry() bool {
	return c.ChainRegistryName != ""
}

// GetName returns the effective chain name
func (c *ChainConfig) GetName() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.PrettyName
	}
	return c.Name
}

// GetChainID returns the effective chain ID
func (c *ChainConfig) GetChainID() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.ChainID
	}
	return c.ChainID
}

// GetCLIName returns the effective CLI/daemon name
func (c *ChainConfig) GetCLIName() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.DaemonName
	}
	return c.CLIName
}

// GetDenom returns the effective staking denom
func (c *ChainConfig) GetDenom() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.Denom
	}
	return c.Denom
}

// GetPrefix returns the effective bech32 prefix
func (c *ChainConfig) GetPrefix() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.Bech32Prefix
	}
	return c.Prefix
}

// GetLogoURL returns the effective logo URL
func (c *ChainConfig) GetLogoURL() string {
	if c.UsesChainRegistry() && c.RegistryInfo != nil {
		return c.RegistryInfo.LogoURL
	}
	return c.LogoURL
}

// PopulateFromRegistry sets registry info for this chain
func (c *ChainConfig) PopulateFromRegistry(registryInfo *ChainRegistryInfo) {
	c.RegistryInfo = registryInfo
}

// IsAuthzEnabled returns true if authz voting is enabled for this chain
func (c *ChainConfig) IsAuthzEnabled() bool {
	return c.Authz.Enabled && c.Authz.GranterAddr != ""
}

// GetGranterAddr returns the granter address for authz voting
func (c *ChainConfig) GetGranterAddr() string {
	return c.Authz.GranterAddr
}

// GetGranterName returns the friendly name for the granter, or the address if no name is set
func (c *ChainConfig) GetGranterName() string {
	if c.Authz.GranterName != "" {
		return c.Authz.GranterName
	}
	return c.Authz.GranterAddr
}

// GetBinarySourceType returns the preferred binary source type for this chain
func (c *ChainConfig) GetBinarySourceType() string {
	if c.BinarySource.Type != "" {
		return c.BinarySource.Type
	}

	// Default behavior based on current config
	if c.UsesChainRegistry() {
		return "registry"
	} else if c.BinaryRepo.Enabled {
		return "github"
	}

	return "registry" // fallback
}

// HasCustomBinaryURL returns true if chain has a custom binary URL configured
func (c *ChainConfig) HasCustomBinaryURL() bool {
	return c.BinarySource.CustomURL != ""
}

// GetCustomBinaryURL returns the custom binary URL if configured
func (c *ChainConfig) GetCustomBinaryURL() string {
	return c.BinarySource.CustomURL
}

// ShouldCompileFromSource returns true if this chain should compile from source
func (c *ChainConfig) ShouldCompileFromSource() bool {
	return c.BinarySource.Type == "source" || c.BinarySource.CompileFromSource
}

// GetSourceRepo returns the source repository URL for compilation
func (c *ChainConfig) GetSourceRepo() string {
	if c.BinarySource.SourceRepo != "" {
		return c.BinarySource.SourceRepo
	}

	// Fallback to Chain Registry git repo if available
	if c.RegistryInfo != nil && c.RegistryInfo.GitRepo != "" {
		return c.RegistryInfo.GitRepo
	}

	return ""
}

// GetSourceBranch returns the source branch/tag to build
func (c *ChainConfig) GetSourceBranch() string {
	if c.BinarySource.SourceBranch != "" {
		return c.BinarySource.SourceBranch
	}

	// Fallback to Chain Registry version if available
	if c.RegistryInfo != nil && c.RegistryInfo.Version != "" {
		return c.RegistryInfo.Version
	}

	return "main" // default branch
}

// GetBuildCommand returns the build command for source compilation
func (c *ChainConfig) GetBuildCommand() string {
	if c.BinarySource.BuildCommand != "" {
		return c.BinarySource.BuildCommand
	}

	// Default build command - try install first (more common), then build
	return "make install"
}

// GetBuildTarget returns the build target binary name
func (c *ChainConfig) GetBuildTarget() string {
	if c.BinarySource.BuildTarget != "" {
		return c.BinarySource.BuildTarget
	}

	// Default to CLI name
	return c.GetCLIName()
}
