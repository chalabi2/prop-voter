package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ChainInfo represents the essential chain information from Chain Registry
type ChainInfo struct {
	ChainName    string `json:"chain_name"`
	PrettyName   string `json:"pretty_name"`
	ChainID      string `json:"chain_id"`
	Bech32Prefix string `json:"bech32_prefix"`
	DaemonName   string `json:"daemon_name"`
	Denom        string `json:"-"` // Extracted from staking tokens
	Decimals     int    `json:"-"` // Token decimal precision
	LogoURL      string `json:"-"` // Extracted from logo_URIs
	GitRepo      string `json:"-"` // Extracted from codebase
	Version      string `json:"-"` // Extracted from codebase
	BinaryURL    string `json:"-"` // Extracted from codebase binaries
}

// ChainRegistryResponse represents the full Chain Registry response
type ChainRegistryResponse struct {
	ChainName    string `json:"chain_name"`
	PrettyName   string `json:"pretty_name"`
	ChainID      string `json:"chain_id"`
	Bech32Prefix string `json:"bech32_prefix"`
	DaemonName   string `json:"daemon_name"`
	Staking      struct {
		StakingTokens []struct {
			Denom string `json:"denom"`
		} `json:"staking_tokens"`
	} `json:"staking"`
	Codebase struct {
		GitRepo            string            `json:"git_repo"`
		RecommendedVersion string            `json:"recommended_version"`
		Binaries           map[string]string `json:"binaries"`
	} `json:"codebase"`
	LogoURIs struct {
		PNG string `json:"png"`
		SVG string `json:"svg"`
	} `json:"logo_URIs"`
}

// Client handles Chain Registry API interactions
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *zap.Logger
	cache      map[string]*ChainInfo
	cacheTTL   time.Duration
}

// NewClient creates a new Chain Registry client
func NewClient(logger *zap.Logger) *Client {
	return &Client{
		baseURL: "https://raw.githubusercontent.com/cosmos/chain-registry/master",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:   logger,
		cache:    make(map[string]*ChainInfo),
		cacheTTL: 1 * time.Hour, // Cache for 1 hour
	}
}

// GetChainInfo fetches chain information from the Chain Registry
func (c *Client) GetChainInfo(ctx context.Context, chainName string) (*ChainInfo, error) {
	// Check cache first
	if cachedInfo, exists := c.cache[chainName]; exists {
		c.logger.Debug("Using cached chain info", zap.String("chain", chainName))
		return cachedInfo, nil
	}

	c.logger.Info("Fetching chain info from Chain Registry",
		zap.String("chain", chainName))

	// Construct URL for chain.json
	url := fmt.Sprintf("%s/%s/chain.json", c.baseURL, chainName)

	// Make HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chain registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chain registry returned status %d for chain %s", resp.StatusCode, chainName)
	}

	// Parse response
	var registryResp ChainRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode chain registry response: %w", err)
	}

	// Extract and convert to our format
	chainInfo := &ChainInfo{
		ChainName:    registryResp.ChainName,
		PrettyName:   registryResp.PrettyName,
		ChainID:      registryResp.ChainID,
		Bech32Prefix: registryResp.Bech32Prefix,
		DaemonName:   registryResp.DaemonName,
		GitRepo:      registryResp.Codebase.GitRepo,
		Version:      registryResp.Codebase.RecommendedVersion,
		Decimals:     6, // Default to 6 decimals, will be updated from assetlist
	}

	// Extract staking denom
	if len(registryResp.Staking.StakingTokens) > 0 {
		chainInfo.Denom = registryResp.Staking.StakingTokens[0].Denom
	}

	// Extract logo URL (prefer PNG)
	if registryResp.LogoURIs.PNG != "" {
		chainInfo.LogoURL = registryResp.LogoURIs.PNG
	} else if registryResp.LogoURIs.SVG != "" {
		chainInfo.LogoURL = registryResp.LogoURIs.SVG
	}

	// Extract binary URL for current platform
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if binaryURL, exists := registryResp.Codebase.Binaries[platform]; exists {
		chainInfo.BinaryURL = binaryURL
	}

	// Fetch decimal precision from assetlist
	if err := c.fetchAssetInfo(ctx, chainName, chainInfo); err != nil {
		c.logger.Warn("Failed to fetch asset info, using default decimals",
			zap.String("chain", chainName),
			zap.Error(err),
		)
	}

	// Cache the result
	c.cache[chainName] = chainInfo

	c.logger.Info("Successfully fetched chain info",
		zap.String("chain", chainName),
		zap.String("chain_id", chainInfo.ChainID),
		zap.String("daemon", chainInfo.DaemonName),
		zap.String("version", chainInfo.Version),
		zap.String("binary_url", chainInfo.BinaryURL),
		zap.Int("decimals", chainInfo.Decimals),
	)

	return chainInfo, nil
}

// AssetListResponse represents the assetlist.json response
type AssetListResponse struct {
	Assets []struct {
		Base       string `json:"base"`
		DenomUnits []struct {
			Denom    string `json:"denom"`
			Exponent int    `json:"exponent"`
		} `json:"denom_units"`
	} `json:"assets"`
}

// fetchAssetInfo fetches asset information including decimal precision
func (c *Client) fetchAssetInfo(ctx context.Context, chainName string, chainInfo *ChainInfo) error {
	url := fmt.Sprintf("%s/%s/assetlist.json", c.baseURL, chainName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch assetlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assetlist returned status %d", resp.StatusCode)
	}

	var assetResp AssetListResponse
	if err := json.NewDecoder(resp.Body).Decode(&assetResp); err != nil {
		return fmt.Errorf("failed to decode assetlist response: %w", err)
	}

	// Find the main staking token and extract its decimal precision
	for _, asset := range assetResp.Assets {
		// Look for the asset that matches our staking denom
		if asset.Base == chainInfo.Denom {
			// Find the display denomination with highest exponent
			maxExponent := 0
			for _, denomUnit := range asset.DenomUnits {
				if denomUnit.Exponent > maxExponent {
					maxExponent = denomUnit.Exponent
				}
			}
			if maxExponent > 0 {
				chainInfo.Decimals = maxExponent
				c.logger.Debug("Found decimal precision from assetlist",
					zap.String("chain", chainName),
					zap.String("denom", chainInfo.Denom),
					zap.Int("decimals", maxExponent),
				)
				return nil
			}
		}
	}

	c.logger.Debug("Asset not found in assetlist, using default decimals",
		zap.String("chain", chainName),
		zap.String("denom", chainInfo.Denom),
	)
	return nil
}

// GetBinaryInfo extracts binary download information
func (c *Client) GetBinaryInfo(chainInfo *ChainInfo) (*BinaryInfo, error) {
	// Extract repository info from git URL
	gitRepo := chainInfo.GitRepo
	if gitRepo == "" {
		return nil, fmt.Errorf("no git repository specified for %s", chainInfo.ChainName)
	}

	// Parse GitHub repository from URL (e.g., "https://github.com/osmosis-labs/osmosis/")
	parts := strings.Split(strings.TrimSuffix(gitRepo, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid git repository URL: %s", gitRepo)
	}

	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]

	binaryInfo := &BinaryInfo{
		Owner:     owner,
		Repo:      repo,
		Version:   chainInfo.Version,
		BinaryURL: chainInfo.BinaryURL,
		FileName:  chainInfo.DaemonName,
	}

	// If no binary URL provided by Chain Registry, mark for GitHub fallback
	if chainInfo.BinaryURL == "" {
		c.logger.Debug("No binary URL in Chain Registry, will use GitHub releases fallback",
			zap.String("chain", chainInfo.ChainName),
			zap.String("repo", owner+"/"+repo),
			zap.String("version", chainInfo.Version),
		)
		binaryInfo.BinaryURL = "" // Will trigger GitHub releases lookup in binary manager
	}

	return binaryInfo, nil
}

// BinaryInfo contains binary download information
type BinaryInfo struct {
	Owner     string
	Repo      string
	Version   string
	BinaryURL string
	FileName  string
}

// ClearCache removes all cached chain information
func (c *Client) ClearCache() {
	c.cache = make(map[string]*ChainInfo)
	c.logger.Debug("Chain registry cache cleared")
}

// ListSupportedChains returns a list of commonly supported chains
func (c *Client) ListSupportedChains() []string {
	return []string{
		"cosmoshub",
		"osmosis",
		"juno",
		"akash",
		"kujira",
		"stargaze",
		"injective",
		"stride",
		"evmos",
		"kava",
		"secret",
		"terra",
		"terra2",
		"persistence",
		"sommelier",
		"gravity-bridge",
		"crescent",
		"chihuahua",
		"bitsong",
		"lumnetwork",
		"comdex",
		"cerberus",
		"bostrom",
		"cheqd",
		"lum-network",
		"vidulum",
		"desmos",
		"dig",
		"rizon",
		"sif",
		"bandchain",
		"emoney",
		"ixo",
		"regen",
		"sentinel",
		"starname",
		"cyber",
		"iris",
		"cryptocom",
		"shentu",
		"likecoin",
		"kichain",
		"panacea",
		"bitcanna",
		"konstellation",
		"omniflixhub",
		"galaxy",
		"nyx",
		"pylons",
		"jackal",
		"passage",
		"cudos",
		"fetchai",
		"assetmantle",
		"kyve",
		"archway",
		"neutron",
		"noble",
		"composable",
		"saga",
		"dymension",
		"celestia",
	}
}
