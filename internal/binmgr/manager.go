package binmgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"prop-voter/config"
	"prop-voter/internal/binmgr/modules"
	"prop-voter/internal/registry"

	"go.uber.org/zap"
)

// BinaryInfo holds information about a managed binary
type BinaryInfo struct {
	Name        string
	Version     string
	Path        string
	LastUpdated time.Time
}

// Manager handles binary downloads and updates from multiple sources
type Manager struct {
	config          *config.Config
	logger          *zap.Logger
	registryManager *registry.Manager

	// Modules
	platformDetector *modules.PlatformDetector
	sourceCompiler   *modules.SourceCompiler
	binaryDownloader *modules.BinaryDownloader
	binaryFinder     *modules.BinaryFinder
}

// NewManager creates a new binary manager with modular components
func NewManager(config *config.Config, logger *zap.Logger, registryManager *registry.Manager) *Manager {
	// Initialize modules
	platformDetector := modules.NewPlatformDetector(logger)
	binaryFinder := modules.NewBinaryFinder(logger)
	sourceCompiler := modules.NewSourceCompiler(logger, platformDetector, binaryFinder, config.BinaryManager.BinDir)
	binaryDownloader := modules.NewBinaryDownloader(logger, platformDetector, config.BinaryManager.BinDir)

	return &Manager{
		config:          config,
		logger:          logger,
		registryManager: registryManager,

		platformDetector: platformDetector,
		sourceCompiler:   sourceCompiler,
		binaryDownloader: binaryDownloader,
		binaryFinder:     binaryFinder,
	}
}

// Start begins the binary management process
func (m *Manager) Start(ctx context.Context) error {
	if !m.config.BinaryManager.Enabled {
		m.logger.Info("Binary manager disabled")
		return nil
	}

	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(m.config.BinaryManager.BinDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	m.logger.Info("Starting binary manager",
		zap.String("bin_dir", m.config.BinaryManager.BinDir),
		zap.Duration("check_interval", m.config.BinaryManager.CheckInterval),
		zap.Bool("auto_update", m.config.BinaryManager.AutoUpdate),
	)

	// Initial setup - download missing binaries
	if err := m.setupBinaries(ctx); err != nil {
		m.logger.Error("Failed to setup binaries", zap.Error(err))
	}

	// Start periodic update checker
	ticker := time.NewTicker(m.config.BinaryManager.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping binary manager")
			return ctx.Err()
		case <-ticker.C:
			if err := m.checkForUpdates(ctx); err != nil {
				m.logger.Error("Failed to check for updates", zap.Error(err))
			}
		}
	}
}

// setupBinaries downloads any missing binaries
func (m *Manager) setupBinaries(ctx context.Context) error {
	for _, chain := range m.config.Chains {
		// Skip if binary management not enabled for this chain
		if !m.shouldManageBinary(&chain) {
			continue
		}

		binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			m.logger.Info("Binary not found, attempting to acquire",
				zap.String("chain", chain.GetName()),
				zap.String("cli", chain.GetCLIName()),
				zap.String("source_type", chain.GetBinarySourceType()),
			)

			if err := m.acquireBinary(ctx, &chain); err != nil {
				m.logger.Error("Failed to acquire binary",
					zap.String("chain", chain.GetName()),
					zap.Error(err),
				)
				continue
			}
		}
	}

	return nil
}

// shouldManageBinary determines if a binary should be managed for the given chain
func (m *Manager) shouldManageBinary(chain *config.ChainConfig) bool {
	// Custom URL or source compilation is always managed
	if chain.HasCustomBinaryURL() || chain.ShouldCompileFromSource() {
		return true
	}

	// Standard registry/github management
	return chain.UsesChainRegistry() || chain.BinaryRepo.Enabled
}

// acquireBinary attempts to acquire a binary using the configured source type
func (m *Manager) acquireBinary(ctx context.Context, chain *config.ChainConfig) error {
	sourceType := chain.GetBinarySourceType()

	m.logger.Info("Acquiring binary",
		zap.String("chain", chain.GetName()),
		zap.String("source_type", sourceType),
	)

	switch sourceType {
	case "url":
		return m.binaryDownloader.DownloadFromCustomURL(ctx, chain)
	case "source":
		return m.sourceCompiler.CompileFromSource(ctx, chain)
	case "registry":
		return m.downloadFromRegistry(ctx, chain)
	case "github":
		return m.binaryDownloader.DownloadFromGitHub(ctx, chain)
	default:
		// Fallback: try registry first, then github, then source compilation
		m.logger.Info("Using automatic fallback strategy (registry → github → source compilation)")

		if err := m.downloadFromRegistry(ctx, chain); err != nil {
			m.logger.Warn("Registry download failed, trying GitHub", zap.Error(err))
			if err := m.binaryDownloader.DownloadFromGitHub(ctx, chain); err != nil {
				m.logger.Warn("GitHub download failed, trying source compilation", zap.Error(err))
				// Always try source compilation as final fallback if we have a repo
				if chain.ShouldCompileFromSource() || chain.GetSourceRepo() != "" {
					return m.sourceCompiler.CompileFromSource(ctx, chain)
				}
				// For Chain Registry chains, we always have a git repo, so try source compilation
				if chain.UsesChainRegistry() {
					m.logger.Info("Attempting automatic source compilation for Chain Registry chain")
					return m.sourceCompiler.CompileFromSource(ctx, chain)
				}
				return fmt.Errorf("all binary acquisition methods failed: %w", err)
			}
		}
		return nil
	}
}

// downloadFromRegistry downloads a binary using Chain Registry information
func (m *Manager) downloadFromRegistry(ctx context.Context, chain *config.ChainConfig) error {
	if !chain.UsesChainRegistry() {
		return fmt.Errorf("chain %s does not use Chain Registry", chain.GetName())
	}

	binaryInfo, err := m.getBinaryInfoForChain(ctx, chain)
	if err != nil {
		return fmt.Errorf("failed to get binary info from registry: %w", err)
	}

	// Check if Chain Registry provides a direct binary URL
	if binaryInfo.BinaryURL != "" {
		m.logger.Info("Using binary URL from Chain Registry",
			zap.String("chain", chain.GetName()),
			zap.String("binary_url", binaryInfo.BinaryURL),
		)
		return m.downloadBinaryFromURL(ctx, chain, binaryInfo.BinaryURL, binaryInfo.Version)
	}

	// Chain Registry doesn't provide binary URL - fall back to GitHub releases
	m.logger.Info("No binary URL in Chain Registry, falling back to GitHub releases",
		zap.String("chain", chain.GetName()),
		zap.String("repo", binaryInfo.Owner+"/"+binaryInfo.Repo),
		zap.String("version", binaryInfo.Version),
	)

	if err := m.binaryDownloader.DownloadFromGitHubWithInfo(ctx, chain, binaryInfo); err != nil {
		// GitHub releases failed - try automatic source compilation as final fallback
		m.logger.Warn("GitHub releases failed for Chain Registry chain, attempting automatic source compilation",
			zap.String("chain", chain.GetName()),
			zap.Error(err),
		)

		// Check if user explicitly disabled source compilation
		if chain.ShouldCompileFromSource() || chain.GetSourceRepo() != "" || (!chain.HasCustomBinaryURL() && chain.BinarySource.Type == "") {
			m.logger.Info("Attempting automatic source compilation for Chain Registry chain",
				zap.String("chain", chain.GetName()),
				zap.String("repo", binaryInfo.Owner+"/"+binaryInfo.Repo),
			)
			return m.sourceCompiler.CompileFromSource(ctx, chain)
		}

		return fmt.Errorf("all binary acquisition methods failed for Chain Registry chain: %w", err)
	}

	return nil
}

// checkForUpdates checks for and optionally downloads binary updates
func (m *Manager) checkForUpdates(ctx context.Context) error {
	m.logger.Debug("Checking for binary updates")

	// For now, keep update logic simple - just check if binaries exist
	return m.setupBinaries(ctx)
}

// getBinaryInfoForChain gets binary information for a chain
func (m *Manager) getBinaryInfoForChain(ctx context.Context, chain *config.ChainConfig) (*registry.BinaryInfo, error) {
	return m.registryManager.GetBinaryInfoForChain(ctx, chain)
}

// GetManagedBinaries returns information about all managed binaries
func (m *Manager) GetManagedBinaries() ([]BinaryInfo, error) {
	var binaries []BinaryInfo

	for _, chain := range m.config.Chains {
		if !m.shouldManageBinary(&chain) {
			continue
		}

		binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())

		var info BinaryInfo
		info.Name = chain.GetCLIName()
		info.Path = binaryPath

		if stat, err := os.Stat(binaryPath); err == nil {
			info.LastUpdated = stat.ModTime()
		}

		// Try to get version (simplified for now)
		info.Version = "unknown"

		binaries = append(binaries, info)
	}

	return binaries, nil
}

// UpdateBinary manually updates a specific binary
func (m *Manager) UpdateBinary(ctx context.Context, chainName string) error {
	for _, chain := range m.config.Chains {
		if (chain.GetName() == chainName || chain.ChainRegistryName == chainName) &&
			m.shouldManageBinary(&chain) {
			return m.acquireBinary(ctx, &chain)
		}
	}

	return fmt.Errorf("chain %s not found or binary management not enabled", chainName)
}

// downloadBinaryFromURL downloads a binary from a direct URL (helper method)
func (m *Manager) downloadBinaryFromURL(ctx context.Context, chain *config.ChainConfig, binaryURL, version string) error {
	return m.binaryDownloader.DownloadBinaryFromURL(ctx, chain, binaryURL, version)
}
