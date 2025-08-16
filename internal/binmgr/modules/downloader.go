package modules

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
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/registry"

	"go.uber.org/zap"
)

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

// BinaryDownloader handles downloading binaries from various sources
type BinaryDownloader struct {
	logger           *zap.Logger
	client           *http.Client
	platformDetector *PlatformDetector
	binDir           string
}

// NewBinaryDownloader creates a new binary downloader
func NewBinaryDownloader(logger *zap.Logger, platformDetector *PlatformDetector, binDir string) *BinaryDownloader {
	return &BinaryDownloader{
		logger:           logger,
		client:           &http.Client{Timeout: 30 * time.Second},
		platformDetector: platformDetector,
		binDir:           binDir,
	}
}

// DownloadFromCustomURL downloads a binary from a custom URL
func (d *BinaryDownloader) DownloadFromCustomURL(ctx context.Context, chain *config.ChainConfig) error {
	if !chain.HasCustomBinaryURL() {
		return fmt.Errorf("no custom binary URL configured for chain %s", chain.GetName())
	}

	customURL := chain.GetCustomBinaryURL()
	d.logger.Info("Downloading binary from custom URL",
		zap.String("chain", chain.GetName()),
		zap.String("url", customURL),
	)

	return d.downloadBinaryFromURL(ctx, chain, customURL, "custom")
}

// DownloadFromGitHubWithInfo downloads from GitHub using registry binary info
func (d *BinaryDownloader) DownloadFromGitHubWithInfo(ctx context.Context, chain *config.ChainConfig, binaryInfo *registry.BinaryInfo) error {
	// Convert to legacy-style repo config for GitHub API
	platform := d.platformDetector.GetCurrentPlatform()
	assetPattern := fmt.Sprintf("*%s*%s*", platform.OS, platform.Arch)

	legacyRepo := config.BinaryRepo{
		Enabled:      true,
		Owner:        binaryInfo.Owner,
		Repo:         binaryInfo.Repo,
		AssetPattern: assetPattern,
	}

	// Get release and download
	release, err := d.getLatestRelease(ctx, legacyRepo)
	if err != nil {
		return fmt.Errorf("failed to get latest release for Chain Registry fallback: %w", err)
	}

	return d.downloadBinaryFromRelease(ctx, chain, release)
}

// DownloadFromGitHub downloads a binary from GitHub releases using legacy config
func (d *BinaryDownloader) DownloadFromGitHub(ctx context.Context, chain *config.ChainConfig) error {
	if !chain.BinaryRepo.Enabled {
		return fmt.Errorf("GitHub binary repo not enabled for chain %s", chain.GetName())
	}

	release, err := d.getLatestRelease(ctx, chain.BinaryRepo)
	if err != nil {
		return fmt.Errorf("failed to get latest release: %w", err)
	}

	return d.downloadBinaryFromRelease(ctx, chain, release)
}

// downloadBinaryFromURL downloads a binary from a direct URL
func (d *BinaryDownloader) downloadBinaryFromURL(ctx context.Context, chain *config.ChainConfig, binaryURL, version string) error {
	d.logger.Info("Downloading binary from URL",
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
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d when downloading binary", resp.StatusCode)
	}

	// Determine file extension and extraction method
	binaryPath := filepath.Join(d.binDir, chain.GetCLIName())

	// Handle different archive formats
	if strings.HasSuffix(binaryURL, ".zip") {
		return d.extractZipBinary(resp.Body, binaryPath, chain.GetCLIName())
	} else if strings.HasSuffix(binaryURL, ".tar.gz") {
		return d.extractTarGzBinary(resp.Body, binaryPath, chain.GetCLIName())
	} else {
		// Direct binary download
		return d.saveBinary(resp.Body, binaryPath)
	}
}

// downloadBinaryFromRelease downloads a binary from a specific release
func (d *BinaryDownloader) downloadBinaryFromRelease(ctx context.Context, chain *config.ChainConfig, release *GitHubRelease) error {
	// Find the appropriate asset for our platform
	asset, err := d.findAssetForPlatform(release.Assets, chain.BinaryRepo.AssetPattern)
	if err != nil {
		return fmt.Errorf("failed to find asset: %w", err)
	}

	d.logger.Info("Downloading binary",
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

	resp, err := d.client.Do(req)
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
	binaryPath := filepath.Join(d.binDir, chain.GetCLIName())

	if err := d.extractBinary(tmpFile.Name(), binaryPath, chain.GetCLIName(), asset.Name); err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	d.logger.Info("Binary updated successfully",
		zap.String("chain", chain.GetName()),
		zap.String("version", release.TagName),
		zap.String("path", binaryPath),
	)

	return nil
}

// getLatestRelease gets the latest release from GitHub
func (d *BinaryDownloader) getLatestRelease(ctx context.Context, repo config.BinaryRepo) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repo.Owner, repo.Repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
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

// findAssetForPlatform finds the appropriate asset for the current platform
func (d *BinaryDownloader) findAssetForPlatform(assets []Asset, pattern string) (*Asset, error) {
	// Check if release has no assets at all
	if len(assets) == 0 {
		return nil, fmt.Errorf("no binary assets found in release - this chain may not provide pre-compiled binaries")
	}

	platform := d.platformDetector.GetCurrentPlatform()

	for _, asset := range assets {
		name := strings.ToLower(asset.Name)

		// Check if asset matches the pattern (if specified)
		if pattern != "" && !d.matchesPattern(name, pattern) {
			continue
		}

		// If pattern is specific (no wildcards) and matches exactly, use it
		if pattern != "" && !strings.Contains(pattern, "*") && strings.EqualFold(name, pattern) {
			return &asset, nil
		}

		// Check if asset matches our platform using variants
		osMatch := false
		archMatch := false

		// Check all OS variants
		for _, osVariant := range platform.OSVariants {
			if strings.Contains(name, strings.ToLower(osVariant)) {
				osMatch = true
				break
			}
		}

		// Check all architecture variants
		for _, archVariant := range platform.ArchVariants {
			if strings.Contains(name, strings.ToLower(archVariant)) {
				archMatch = true
				break
			}
		}

		if osMatch && archMatch {
			d.logger.Debug("Found platform-specific asset",
				zap.String("asset", asset.Name),
				zap.String("os", platform.OS),
				zap.String("arch", platform.Arch),
			)
			return &asset, nil
		}
	}

	// Fallback logic for pattern matching and OS-only matching
	if pattern != "" {
		for _, asset := range assets {
			name := strings.ToLower(asset.Name)
			if d.matchesPattern(name, pattern) {
				d.logger.Debug("Using pattern-matched asset without platform check",
					zap.String("asset", asset.Name),
					zap.String("pattern", pattern),
				)
				return &asset, nil
			}
		}
	}

	// Final fallback: try to find any asset that contains any OS variant
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		for _, osVariant := range platform.OSVariants {
			if strings.Contains(name, strings.ToLower(osVariant)) {
				d.logger.Debug("Using OS-matched asset without architecture check",
					zap.String("asset", asset.Name),
					zap.String("os_variant", osVariant),
				)
				return &asset, nil
			}
		}
	}

	// Collect available asset names for better error reporting
	var assetNames []string
	for _, asset := range assets {
		assetNames = append(assetNames, asset.Name)
	}

	if pattern != "" {
		return nil, fmt.Errorf("no suitable asset found for platform %s/%s with pattern '%s'. Available assets: %v. Consider using source compilation as fallback",
			platform.OS, platform.Arch, pattern, assetNames)
	}

	return nil, fmt.Errorf("no suitable asset found for platform %s/%s. Available assets: %v. Consider using source compilation as fallback",
		platform.OS, platform.Arch, assetNames)
}

// matchesPattern checks if a string matches a simple pattern (supports * wildcards)
func (d *BinaryDownloader) matchesPattern(s, pattern string) bool {
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

// Helper methods for file extraction and handling

// extractBinary extracts a binary from an archive or copies it if it's not archived
func (d *BinaryDownloader) extractBinary(srcPath, destPath, binaryName, assetName string) error {
	if strings.HasSuffix(assetName, ".tar.gz") || strings.HasSuffix(assetName, ".tgz") {
		return d.extractFromTarGz(srcPath, destPath, binaryName)
	} else if strings.HasSuffix(assetName, ".zip") {
		return d.extractFromZip(srcPath, destPath, binaryName)
	} else {
		// Assume it's a raw binary
		return d.copyFile(srcPath, destPath)
	}
}

// extractFromTarGz extracts a binary from a tar.gz archive
func (d *BinaryDownloader) extractFromTarGz(srcPath, destPath, binaryName string) error {
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
func (d *BinaryDownloader) extractFromZip(srcPath, destPath, binaryName string) error {
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
func (d *BinaryDownloader) copyFile(src, dest string) error {
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

// saveBinary saves a binary directly from an io.Reader
func (d *BinaryDownloader) saveBinary(reader io.Reader, binaryPath string) error {
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

	d.logger.Info("Binary saved successfully",
		zap.String("path", binaryPath),
		zap.Int64("size", size),
	)

	return nil
}

// extractZipBinary extracts a binary from a ZIP archive stream
func (d *BinaryDownloader) extractZipBinary(reader io.Reader, binaryPath, binaryName string) error {
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
	return d.extractFromZip(tempPath, binaryPath, binaryName)
}

// extractTarGzBinary extracts a binary from a tar.gz archive stream
func (d *BinaryDownloader) extractTarGzBinary(reader io.Reader, binaryPath, binaryName string) error {
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
	return d.extractFromTarGz(tempPath, binaryPath, binaryName)
}
