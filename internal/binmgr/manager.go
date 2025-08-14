package binmgr

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/registry"

	"go.uber.org/zap"
)

// Manager handles binary downloads and updates from GitHub releases
type Manager struct {
	config          *config.Config
	logger          *zap.Logger
	client          *http.Client
	registryManager *registry.Manager
}

// GitHubRelease represents a GitHub release
type GitHubRelease struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// BinaryInfo holds information about a managed binary
type BinaryInfo struct {
	Name        string
	Version     string
	Path        string
	LastUpdated time.Time
}

// NewManager creates a new binary manager
func NewManager(config *config.Config, logger *zap.Logger, registryManager *registry.Manager) *Manager {
	return &Manager{
		config:          config,
		logger:          logger,
		client:          &http.Client{Timeout: 30 * time.Second},
		registryManager: registryManager,
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
		if !chain.UsesChainRegistry() && !chain.BinaryRepo.Enabled {
			continue
		}

		binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			m.logger.Info("Binary not found, downloading",
				zap.String("chain", chain.GetName()),
				zap.String("cli", chain.GetCLIName()),
			)

			if err := m.downloadLatestBinary(ctx, &chain); err != nil {
				m.logger.Error("Failed to download binary",
					zap.String("chain", chain.GetName()),
					zap.Error(err),
				)
				continue
			}
		}
	}

	return nil
}

// checkForUpdates checks for and optionally downloads binary updates
func (m *Manager) checkForUpdates(ctx context.Context) error {
	m.logger.Debug("Checking for binary updates")

	for _, chain := range m.config.Chains {
		// Skip if binary management not enabled for this chain
		if !chain.UsesChainRegistry() && !chain.BinaryRepo.Enabled {
			continue
		}

		// Get binary info (works for both Chain Registry and legacy formats)
		binaryInfo, err := m.getBinaryInfoForChain(ctx, &chain)
		if err != nil {
			m.logger.Error("Failed to get binary info",
				zap.String("chain", chain.GetName()),
				zap.Error(err),
			)
			continue
		}

		// For Chain Registry chains
		if chain.UsesChainRegistry() {
			if binaryInfo.BinaryURL != "" {
				// Chain Registry provides direct binary URL
				binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())

				// Check if we need to update to a newer version
				needsUpdate := false
				if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
					needsUpdate = true
					m.logger.Info("Binary missing, downloading from Chain Registry",
						zap.String("chain", chain.GetName()),
						zap.String("version", binaryInfo.Version),
					)
				} else {
					// Check if current version differs from registry version
					currentVersion, err := m.getCurrentVersion(binaryPath)
					if err != nil || currentVersion != binaryInfo.Version {
						needsUpdate = true
						m.logger.Info("Version mismatch, updating from Chain Registry",
							zap.String("chain", chain.GetName()),
							zap.String("current", currentVersion),
							zap.String("registry", binaryInfo.Version),
						)
					}
				}

				if needsUpdate {
					if err := m.downloadBinaryFromURL(ctx, &chain, binaryInfo.BinaryURL, binaryInfo.Version); err != nil {
						m.logger.Error("Failed to download binary from Chain Registry",
							zap.String("chain", chain.GetName()),
							zap.Error(err),
						)
					}
				}
			} else {
				// Chain Registry chain but no binary URL - fall back to GitHub releases
				m.logger.Info("Chain Registry chain with no binary URL, falling back to GitHub releases",
					zap.String("chain", chain.GetName()),
					zap.String("repo", binaryInfo.Owner+"/"+binaryInfo.Repo),
					zap.String("registry_version", binaryInfo.Version),
				)

				// Convert to legacy-style repo config for GitHub API
				legacyRepo := config.BinaryRepo{
					Enabled:      true,
					Owner:        binaryInfo.Owner,
					Repo:         binaryInfo.Repo,
					AssetPattern: fmt.Sprintf("*%s_%s*", runtime.GOOS, runtime.GOARCH), // e.g., *linux_amd64*
				}

				// Use GitHub releases logic
				if err := m.handleGitHubRelease(ctx, &chain, legacyRepo, binaryInfo.Version); err != nil {
					m.logger.Error("Failed to handle GitHub release for Chain Registry chain",
						zap.String("chain", chain.GetName()),
						zap.Error(err),
					)
				}
			}
			continue
		}

		// Legacy format: check GitHub releases
		if err := m.handleGitHubRelease(ctx, &chain, chain.BinaryRepo, ""); err != nil {
			m.logger.Error("Failed to handle GitHub release for legacy chain",
				zap.String("chain", chain.GetName()),
				zap.Error(err),
			)
		}
	}

	return nil
}

// handleGitHubRelease handles binary management using GitHub releases API
// Works for both Chain Registry chains (fallback) and legacy chains
func (m *Manager) handleGitHubRelease(ctx context.Context, chain *config.ChainConfig, repo config.BinaryRepo, registryVersion string) error {
	// Get latest release from GitHub
	release, err := m.getLatestRelease(ctx, repo)
	if err != nil {
		return fmt.Errorf("failed to get latest release: %w", err)
	}

	binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())

	// Determine target version
	targetVersion := release.TagName
	if registryVersion != "" {
		// For Chain Registry chains, prefer registry version if it matches a release
		targetVersion = registryVersion
		m.logger.Info("Using Chain Registry recommended version",
			zap.String("chain", chain.GetName()),
			zap.String("registry_version", registryVersion),
			zap.String("latest_release", release.TagName),
		)
	}

	// Check current version
	currentVersion, err := m.getCurrentVersion(binaryPath)
	needsUpdate := false

	if err != nil || !fileExists(binaryPath) {
		needsUpdate = true
		m.logger.Info("Binary missing, downloading from GitHub",
			zap.String("chain", chain.GetName()),
			zap.String("version", targetVersion),
		)
	} else if currentVersion != targetVersion {
		needsUpdate = true
		m.logger.Info("Update available from GitHub",
			zap.String("chain", chain.GetName()),
			zap.String("current", currentVersion),
			zap.String("target", targetVersion),
		)
	}

	// Download if needed
	if needsUpdate && (m.config.BinaryManager.AutoUpdate || !fileExists(binaryPath)) {
		// For Chain Registry chains with specific version, try to find that release
		if registryVersion != "" && registryVersion != release.TagName {
			specificRelease, err := m.getSpecificRelease(ctx, repo, registryVersion)
			if err != nil {
				m.logger.Warn("Registry version not found in releases, using latest",
					zap.String("chain", chain.GetName()),
					zap.String("registry_version", registryVersion),
					zap.String("latest", release.TagName),
				)
			} else {
				release = specificRelease
			}
		}

		return m.downloadBinaryFromRelease(ctx, chain, release)
	}

	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// downloadLatestBinary downloads the latest binary for a chain
func (m *Manager) downloadLatestBinary(ctx context.Context, chain *config.ChainConfig) error {
	if chain.UsesChainRegistry() {
		// Use Chain Registry binary URL
		binaryInfo, err := m.getBinaryInfoForChain(ctx, chain)
		if err != nil {
			return fmt.Errorf("failed to get binary info from registry: %w", err)
		}

		// Check if Chain Registry provides a direct binary URL
		if binaryInfo.BinaryURL != "" {
			return m.downloadBinaryFromURL(ctx, chain, binaryInfo.BinaryURL, binaryInfo.Version)
		}

		// Chain Registry doesn't provide binary URL - fall back to GitHub releases
		m.logger.Info("No binary URL in Chain Registry, falling back to GitHub releases",
			zap.String("chain", chain.GetName()),
			zap.String("repo", binaryInfo.Owner+"/"+binaryInfo.Repo),
			zap.String("version", binaryInfo.Version),
		)

		// Convert to legacy-style repo config for GitHub API
		legacyRepo := config.BinaryRepo{
			Enabled:      true,
			Owner:        binaryInfo.Owner,
			Repo:         binaryInfo.Repo,
			AssetPattern: fmt.Sprintf("*%s_%s*", runtime.GOOS, runtime.GOARCH), // e.g., *linux_amd64*
		}

		// Get release and download
		release, err := m.getLatestRelease(ctx, legacyRepo)
		if err != nil {
			return fmt.Errorf("failed to get latest release for Chain Registry fallback: %w", err)
		}

		return m.downloadBinaryFromRelease(ctx, chain, release)
	}

	// Legacy format: use GitHub releases
	release, err := m.getLatestRelease(ctx, chain.BinaryRepo)
	if err != nil {
		return fmt.Errorf("failed to get latest release: %w", err)
	}

	return m.downloadBinaryFromRelease(ctx, chain, release)
}

// getBinaryInfoForChain gets binary information for a chain
func (m *Manager) getBinaryInfoForChain(ctx context.Context, chain *config.ChainConfig) (*registry.BinaryInfo, error) {
	return m.registryManager.GetBinaryInfoForChain(ctx, chain)
}

// downloadBinaryFromURL downloads a binary from a direct URL
func (m *Manager) downloadBinaryFromURL(ctx context.Context, chain *config.ChainConfig, binaryURL, version string) error {
	m.logger.Info("Downloading binary from URL",
		zap.String("chain", chain.GetName()),
		zap.String("version", version),
		zap.String("url", binaryURL),
	)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", binaryURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Download the file
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d when downloading binary", resp.StatusCode)
	}

	// Determine file extension and extraction method
	binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())

	// Handle different archive formats
	if strings.HasSuffix(binaryURL, ".zip") {
		return m.extractZipBinary(resp.Body, binaryPath, chain.GetCLIName())
	} else if strings.HasSuffix(binaryURL, ".tar.gz") {
		return m.extractTarGzBinary(resp.Body, binaryPath, chain.GetCLIName())
	} else {
		// Direct binary download
		return m.saveBinary(resp.Body, binaryPath)
	}
}

// downloadBinaryFromRelease downloads a binary from a specific release
func (m *Manager) downloadBinaryFromRelease(ctx context.Context, chain *config.ChainConfig, release *GitHubRelease) error {
	// Find the appropriate asset for our platform
	asset, err := m.findAssetForPlatform(release.Assets, chain.BinaryRepo.AssetPattern)
	if err != nil {
		return fmt.Errorf("failed to find asset: %w", err)
	}

	m.logger.Info("Downloading binary",
		zap.String("chain", chain.GetName()),
		zap.String("version", release.TagName),
		zap.String("asset", asset.Name),
		zap.Int64("size", asset.Size),
	)

	// Download the asset
	req, err := http.NewRequestWithContext(ctx, "GET", asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "binary-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download to temp file
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	tmpFile.Close()

	// Extract and install the binary
	binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.GetCLIName())

	// Backup old binary if it exists and backup is enabled
	if m.config.BinaryManager.BackupOld {
		if _, err := os.Stat(binaryPath); err == nil {
			backupPath := binaryPath + ".backup"
			if err := os.Rename(binaryPath, backupPath); err != nil {
				m.logger.Warn("Failed to backup old binary", zap.Error(err))
			} else {
				m.logger.Info("Backed up old binary", zap.String("path", backupPath))
			}
		}
	}

	if err := m.extractBinary(tmpFile.Name(), binaryPath, chain.GetCLIName(), asset.Name); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	m.logger.Info("Binary updated successfully",
		zap.String("chain", chain.GetName()),
		zap.String("version", release.TagName),
		zap.String("path", binaryPath),
	)

	return nil
}

// getLatestRelease gets the latest release from GitHub
func (m *Manager) getLatestRelease(ctx context.Context, repo config.BinaryRepo) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repo.Owner, repo.Repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &release, nil
}

// getSpecificRelease gets a specific release version from GitHub
func (m *Manager) getSpecificRelease(ctx context.Context, repo config.BinaryRepo, version string) (*GitHubRelease, error) {
	// Ensure version starts with 'v' if it doesn't already
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", repo.Owner, repo.Repo, version)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release %s: %w", version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release %s not found", version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &release, nil
}

// findAssetForPlatform finds the appropriate asset for the current platform
func (m *Manager) findAssetForPlatform(assets []Asset, pattern string) (*Asset, error) {
	// Check if release has no assets at all
	if len(assets) == 0 {
		return nil, fmt.Errorf("no binary assets found in release - this chain may not provide pre-compiled binaries")
	}

	platformParts := []string{runtime.GOOS, runtime.GOARCH}

	// Common platform mappings
	platformMap := map[string]string{
		"darwin":  "darwin",
		"linux":   "linux",
		"windows": "windows",
		"amd64":   "amd64",
		"arm64":   "arm64",
		"386":     "386",
	}

	for _, asset := range assets {
		name := strings.ToLower(asset.Name)

		// Check if asset matches the pattern (if specified)
		if pattern != "" && !matchesPattern(name, pattern) {
			continue
		}

		// If pattern is specific (no wildcards) and matches exactly, use it
		if pattern != "" && !strings.Contains(pattern, "*") && strings.EqualFold(name, pattern) {
			return &asset, nil
		}

		// Check if asset matches our platform
		osMatch := false
		archMatch := false

		for _, part := range platformParts {
			mapped, exists := platformMap[part]
			if !exists {
				mapped = part
			}

			if strings.Contains(name, mapped) {
				if part == runtime.GOOS {
					osMatch = true
				} else if part == runtime.GOARCH {
					archMatch = true
				}
			}
		}

		if osMatch && archMatch {
			return &asset, nil
		}
	}

	// Fallback: if pattern is specified, try to find any asset that matches the pattern
	// (even without platform matching - useful for assets like "junod" that don't have platform suffixes)
	if pattern != "" {
		for _, asset := range assets {
			name := strings.ToLower(asset.Name)
			if matchesPattern(name, pattern) {
				m.logger.Debug("Using pattern-matched asset without platform check",
					zap.String("asset", asset.Name),
					zap.String("pattern", pattern),
				)
				return &asset, nil
			}
		}
	}

	// Final fallback: try to find any asset that contains the OS
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, runtime.GOOS) {
			return &asset, nil
		}
	}

	// Collect available asset names for better error reporting
	var assetNames []string
	for _, asset := range assets {
		assetNames = append(assetNames, asset.Name)
	}

	if pattern != "" {
		return nil, fmt.Errorf("no suitable asset found for platform %s/%s with pattern '%s'. Available assets: %v",
			runtime.GOOS, runtime.GOARCH, pattern, assetNames)
	}

	return nil, fmt.Errorf("no suitable asset found for platform %s/%s. Available assets: %v",
		runtime.GOOS, runtime.GOARCH, assetNames)
}

// matchesPattern checks if a string matches a simple pattern (supports * wildcards)
func matchesPattern(s, pattern string) bool {
	pattern = strings.ToLower(pattern)

	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		pos := 0
		for i, part := range parts {
			if part == "" {
				continue
			}

			idx := strings.Index(s[pos:], part)
			if idx == -1 {
				return false
			}

			pos += idx + len(part)

			// For the first part, it should match from the beginning
			if i == 0 && idx != 0 {
				return false
			}
		}

		// For the last part, it should match to the end
		lastPart := parts[len(parts)-1]
		if lastPart != "" && !strings.HasSuffix(s, lastPart) {
			return false
		}

		return true
	}

	return strings.Contains(s, pattern)
}

// extractBinary extracts a binary from an archive or copies it if it's not archived
func (m *Manager) extractBinary(srcPath, destPath, binaryName, assetName string) error {
	if strings.HasSuffix(assetName, ".tar.gz") || strings.HasSuffix(assetName, ".tgz") {
		return m.extractFromTarGz(srcPath, destPath, binaryName)
	} else if strings.HasSuffix(assetName, ".zip") {
		return m.extractFromZip(srcPath, destPath, binaryName)
	} else {
		// Assume it's a raw binary
		return m.copyFile(srcPath, destPath)
	}
}

// extractFromTarGz extracts a binary from a tar.gz archive
func (m *Manager) extractFromTarGz(srcPath, destPath, binaryName string) error {
	file, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for the binary (it might be in a subdirectory)
		if strings.HasSuffix(header.Name, binaryName) || strings.HasSuffix(header.Name, binaryName+".exe") {
			outFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, tr)
			return err
		}
	}

	return fmt.Errorf("binary %s not found in archive", binaryName)
}

// extractFromZip extracts a binary from a zip archive
func (m *Manager) extractFromZip(srcPath, destPath, binaryName string) error {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Look for the binary (it might be in a subdirectory)
		if strings.HasSuffix(f.Name, binaryName) || strings.HasSuffix(f.Name, binaryName+".exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			outFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, rc)
			return err
		}
	}

	return fmt.Errorf("binary %s not found in archive", binaryName)
}

// copyFile copies a file from src to dest
func (m *Manager) copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// getCurrentVersion attempts to get the current version of a binary
func (m *Manager) getCurrentVersion(binaryPath string) (string, error) {
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary not found")
	}

	// Try common version commands
	versionCommands := [][]string{
		{"version"},
		{"--version"},
		{"-v"},
		{"-version"},
	}

	for _, args := range versionCommands {
		if version := m.tryGetVersion(binaryPath, args); version != "" {
			return version, nil
		}
	}

	return "unknown", nil
}

// tryGetVersion tries to get version using specific arguments
func (m *Manager) tryGetVersion(binaryPath string, args []string) string {
	// This would need os/exec but keeping it simple for now
	// In a real implementation, you'd use exec.CommandContext
	return ""
}

// GetManagedBinaries returns information about all managed binaries
func (m *Manager) GetManagedBinaries() ([]BinaryInfo, error) {
	var binaries []BinaryInfo

	for _, chain := range m.config.Chains {
		if !chain.BinaryRepo.Enabled {
			continue
		}

		binaryPath := filepath.Join(m.config.BinaryManager.BinDir, chain.CLIName)

		var info BinaryInfo
		info.Name = chain.CLIName
		info.Path = binaryPath

		if stat, err := os.Stat(binaryPath); err == nil {
			info.LastUpdated = stat.ModTime()
		}

		if version, err := m.getCurrentVersion(binaryPath); err == nil {
			info.Version = version
		}

		binaries = append(binaries, info)
	}

	return binaries, nil
}

// UpdateBinary manually updates a specific binary
func (m *Manager) UpdateBinary(ctx context.Context, chainName string) error {
	for _, chain := range m.config.Chains {
		if (chain.GetName() == chainName || chain.ChainRegistryName == chainName) &&
			(chain.UsesChainRegistry() || chain.BinaryRepo.Enabled) {
			return m.downloadLatestBinary(ctx, &chain)
		}
	}

	return fmt.Errorf("chain %s not found or binary management not enabled", chainName)
}

// saveBinary saves a binary directly from an io.Reader
func (m *Manager) saveBinary(reader io.Reader, binaryPath string) error {
	// Create temporary file first
	tempPath := binaryPath + ".tmp"

	outFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer outFile.Close()

	// Copy the binary data
	size, err := io.Copy(outFile, reader)
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to write binary: %w", err)
	}

	// Close the file before moving
	outFile.Close()

	// Make it executable
	if err := os.Chmod(tempPath, 0755); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Move temp file to final location
	if err := os.Rename(tempPath, binaryPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to move binary to final location: %w", err)
	}

	m.logger.Info("Binary saved successfully",
		zap.String("path", binaryPath),
		zap.Int64("size", size),
	)

	return nil
}

// extractZipBinary extracts a binary from a ZIP archive stream
func (m *Manager) extractZipBinary(reader io.Reader, binaryPath, binaryName string) error {
	// Save to temp file first
	tempPath := binaryPath + ".zip.tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, reader); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to save archive: %w", err)
	}
	tempFile.Close()

	// Extract from temp file
	return m.extractFromZip(tempPath, binaryPath, binaryName)
}

// extractTarGzBinary extracts a binary from a tar.gz archive stream
func (m *Manager) extractTarGzBinary(reader io.Reader, binaryPath, binaryName string) error {
	// Save to temp file first
	tempPath := binaryPath + ".tar.gz.tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, reader); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to save archive: %w", err)
	}
	tempFile.Close()

	// Extract from temp file
	return m.extractFromTarGz(tempPath, binaryPath, binaryName)
}
