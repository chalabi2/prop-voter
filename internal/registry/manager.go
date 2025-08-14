package registry

import (
	"context"
	"fmt"

	"prop-voter/config"

	"go.uber.org/zap"
)

// Manager handles Chain Registry integration with application config
type Manager struct {
	client *Client
	logger *zap.Logger
}

// NewManager creates a new registry manager
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		client: NewClient(logger),
		logger: logger,
	}
}

// PopulateChainConfigs populates Chain Registry information for all chains
func (m *Manager) PopulateChainConfigs(ctx context.Context, chains []config.ChainConfig) error {
	for i := range chains {
		chain := &chains[i]

		// Skip chains that don't use Chain Registry
		if !chain.UsesChainRegistry() {
			m.logger.Debug("Skipping chain (not using Chain Registry)",
				zap.String("chain", chain.GetName()))
			continue
		}

		m.logger.Info("Populating chain info from Chain Registry",
			zap.String("chain_name", chain.ChainRegistryName))

		// Fetch chain info from registry
		chainInfo, err := m.client.GetChainInfo(ctx, chain.ChainRegistryName)
		if err != nil {
			return fmt.Errorf("failed to fetch chain info for %s: %w",
				chain.ChainRegistryName, err)
		}

		// Convert to config format
		registryInfo := &config.ChainRegistryInfo{
			PrettyName:   chainInfo.PrettyName,
			ChainID:      chainInfo.ChainID,
			Bech32Prefix: chainInfo.Bech32Prefix,
			DaemonName:   chainInfo.DaemonName,
			Denom:        chainInfo.Denom,
			Decimals:     chainInfo.Decimals,
			LogoURL:      chainInfo.LogoURL,
			GitRepo:      chainInfo.GitRepo,
			Version:      chainInfo.Version,
			BinaryURL:    chainInfo.BinaryURL,
		}

		// Populate the chain config
		chain.PopulateFromRegistry(registryInfo)

		m.logger.Info("Successfully populated chain info",
			zap.String("chain_name", chain.ChainRegistryName),
			zap.String("pretty_name", registryInfo.PrettyName),
			zap.String("chain_id", registryInfo.ChainID),
			zap.String("daemon", registryInfo.DaemonName),
			zap.String("version", registryInfo.Version),
		)
	}

	return nil
}

// GetBinaryInfoForChain returns binary download information for a chain
func (m *Manager) GetBinaryInfoForChain(ctx context.Context, chain *config.ChainConfig) (*BinaryInfo, error) {
	if !chain.UsesChainRegistry() {
		// For legacy format, convert to BinaryInfo
		if !chain.BinaryRepo.Enabled {
			return nil, fmt.Errorf("binary repository not enabled for chain %s", chain.GetName())
		}

		return &BinaryInfo{
			Owner:     chain.BinaryRepo.Owner,
			Repo:      chain.BinaryRepo.Repo,
			Version:   "", // Will be fetched from GitHub releases
			BinaryURL: "", // Will be constructed from asset pattern
			FileName:  chain.GetCLIName(),
		}, nil
	}

	// For Chain Registry format, get from registry info
	if chain.RegistryInfo == nil {
		return nil, fmt.Errorf("chain registry info not populated for %s", chain.ChainRegistryName)
	}

	// Create a temporary ChainInfo for conversion
	chainInfo := &ChainInfo{
		ChainName:    chain.ChainRegistryName,
		PrettyName:   chain.RegistryInfo.PrettyName,
		ChainID:      chain.RegistryInfo.ChainID,
		Bech32Prefix: chain.RegistryInfo.Bech32Prefix,
		DaemonName:   chain.RegistryInfo.DaemonName,
		Denom:        chain.RegistryInfo.Denom,
		Decimals:     chain.RegistryInfo.Decimals,
		LogoURL:      chain.RegistryInfo.LogoURL,
		GitRepo:      chain.RegistryInfo.GitRepo,
		Version:      chain.RegistryInfo.Version,
		BinaryURL:    chain.RegistryInfo.BinaryURL,
	}

	return m.client.GetBinaryInfo(chainInfo)
}

// ValidateChainConfig validates a chain configuration
func (m *Manager) ValidateChainConfig(chain *config.ChainConfig) error {
	// Check required fields
	if chain.RPC == "" {
		return fmt.Errorf("RPC endpoint is required")
	}
	if chain.REST == "" {
		return fmt.Errorf("REST endpoint is required")
	}
	if chain.WalletKey == "" {
		return fmt.Errorf("wallet key is required")
	}

	// Validate format-specific requirements
	if chain.UsesChainRegistry() {
		if chain.ChainRegistryName == "" {
			return fmt.Errorf("chain_name is required when using Chain Registry")
		}
	} else {
		// Legacy format validation
		if chain.Name == "" {
			return fmt.Errorf("name is required when not using Chain Registry")
		}
		if chain.ChainID == "" {
			return fmt.Errorf("chain_id is required when not using Chain Registry")
		}
		if chain.CLIName == "" {
			return fmt.Errorf("cli_name is required when not using Chain Registry")
		}
		if chain.Denom == "" {
			return fmt.Errorf("denom is required when not using Chain Registry")
		}
		if chain.Prefix == "" {
			return fmt.Errorf("prefix is required when not using Chain Registry")
		}
	}

	return nil
}

// ListSupportedChains returns chains supported by Chain Registry
func (m *Manager) ListSupportedChains() []string {
	return m.client.ListSupportedChains()
}

// ClearCache clears the Chain Registry cache
func (m *Manager) ClearCache() {
	m.client.ClearCache()
}
