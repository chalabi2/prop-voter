package modules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// GoVersionManager handles Go version detection and management
type GoVersionManager struct {
	logger *zap.Logger
}

// NewGoVersionManager creates a new Go version manager
func NewGoVersionManager(logger *zap.Logger) *GoVersionManager {
	return &GoVersionManager{
		logger: logger,
	}
}

// GoVersionInfo holds information about a Go installation
type GoVersionInfo struct {
	Version string
	Path    string
	Major   int
	Minor   int
	Patch   int
}

// GetCurrentGoVersion gets the version of the currently active Go installation
func (g *GoVersionManager) GetCurrentGoVersion() (*GoVersionInfo, error) {
	// Try to get go version
	cmd := exec.Command("go", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get go version: %w", err)
	}

	// Parse output like "go version go1.24.5 linux/amd64"
	versionStr := string(output)
	return g.parseGoVersion(versionStr, "go")
}

// GetGoVersionFromPath gets the version of a specific Go executable
func (g *GoVersionManager) GetGoVersionFromPath(goPath string) (*GoVersionInfo, error) {
	cmd := exec.Command(goPath, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get go version from %s: %w", goPath, err)
	}

	versionStr := string(output)
	return g.parseGoVersion(versionStr, goPath)
}

// parseGoVersion parses a Go version string
func (g *GoVersionManager) parseGoVersion(versionStr, goPath string) (*GoVersionInfo, error) {
	// Extract version using regex (e.g., "go1.20.1" or "go1.24.5")
	re := regexp.MustCompile(`go(\d+)\.(\d+)\.?(\d*)`)
	matches := re.FindStringSubmatch(versionStr)

	if len(matches) < 3 {
		return nil, fmt.Errorf("failed to parse go version from: %s", versionStr)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, fmt.Errorf("failed to parse major version: %w", err)
	}

	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse minor version: %w", err)
	}

	patch := 0
	if len(matches) > 3 && matches[3] != "" {
		patch, err = strconv.Atoi(matches[3])
		if err != nil {
			return nil, fmt.Errorf("failed to parse patch version: %w", err)
		}
	}

	version := fmt.Sprintf("go%d.%d.%d", major, minor, patch)
	if patch == 0 {
		version = fmt.Sprintf("go%d.%d", major, minor)
	}

	return &GoVersionInfo{
		Version: version,
		Path:    goPath,
		Major:   major,
		Minor:   minor,
		Patch:   patch,
	}, nil
}

// CheckVersionCompatibility checks if the current Go version is compatible with requirements
func (g *GoVersionManager) CheckVersionCompatibility(required string) (*GoVersionInfo, bool, error) {
	currentVersion, err := g.GetCurrentGoVersion()
	if err != nil {
		return nil, false, err
	}

	// Parse required version
	reqVersion, err := g.parseGoVersion("go version "+required+" linux/amd64", "required")
	if err != nil {
		return currentVersion, false, fmt.Errorf("failed to parse required version %s: %w", required, err)
	}

	// Check compatibility
	compatible := g.isVersionCompatible(currentVersion, reqVersion)

	g.logger.Info("Go version compatibility check",
		zap.String("current", currentVersion.Version),
		zap.String("required", reqVersion.Version),
		zap.Bool("compatible", compatible),
	)

	return currentVersion, compatible, nil
}

// isVersionCompatible checks if current version meets the requirement
func (g *GoVersionManager) isVersionCompatible(current, required *GoVersionInfo) bool {
	// For now, we'll be flexible and allow newer versions unless it's a major version difference
	// This might need to be adjusted based on specific chain requirements

	if current.Major > required.Major {
		// Major version is higher - might have breaking changes
		// For most Cosmos chains, this should still work, but log a warning
		g.logger.Warn("Using newer major Go version than required",
			zap.Int("current_major", current.Major),
			zap.Int("required_major", required.Major),
		)
		return true // Allow but warn
	}

	if current.Major == required.Major {
		if current.Minor >= required.Minor {
			return true // Same major, equal or newer minor
		}
	}

	return false // Current version is older than required
}

// FindCompatibleGoVersion searches for a Go installation that meets the requirements
func (g *GoVersionManager) FindCompatibleGoVersion(required string) (*GoVersionInfo, error) {
	// First check current Go
	if currentVersion, compatible, err := g.CheckVersionCompatibility(required); err == nil && compatible {
		return currentVersion, nil
	}

	// Search for other Go installations
	goPaths := g.findGoInstallations()

	for _, goPath := range goPaths {
		if version, err := g.GetGoVersionFromPath(goPath); err == nil {
			reqVersion, err := g.parseGoVersion("go version "+required+" linux/amd64", "required")
			if err != nil {
				continue
			}

			if g.isVersionCompatible(version, reqVersion) {
				g.logger.Info("Found compatible Go installation",
					zap.String("path", goPath),
					zap.String("version", version.Version),
					zap.String("required", required),
				)
				return version, nil
			}
		}
	}

	return nil, fmt.Errorf("no compatible Go version found for requirement: %s", required)
}

// findGoInstallations searches for Go installations in common locations
func (g *GoVersionManager) findGoInstallations() []string {
	var goPaths []string

	// Common Go installation paths
	commonPaths := []string{
		"/usr/local/go/bin/go",
		"/usr/bin/go",
		"/snap/go/current/bin/go",
		"/opt/go/bin/go",
		"/home/go/bin/go",
	}

	// Check user's home directory
	if home, err := os.UserHomeDir(); err == nil {
		commonPaths = append(commonPaths,
			filepath.Join(home, "go/bin/go"),
			filepath.Join(home, ".local/go/bin/go"),
			filepath.Join(home, "sdk/go*/bin/go"), // Common for manual installs
		)
	}

	// Check for multiple Go versions (common with version managers)
	versionPaths := []string{
		"/usr/local/go*/bin/go",
		"/opt/go*/bin/go",
		"/snap/go/*/bin/go",
	}

	for _, pattern := range versionPaths {
		if matches, err := filepath.Glob(pattern); err == nil {
			commonPaths = append(commonPaths, matches...)
		}
	}

	// Check which ones actually exist and are executable
	for _, path := range commonPaths {
		if stat, err := os.Stat(path); err == nil && stat.Mode()&0111 != 0 {
			goPaths = append(goPaths, path)
		}
	}

	return goPaths
}

// SuggestGoVersionInstallation suggests how to install a specific Go version
func (g *GoVersionManager) SuggestGoVersionInstallation(required string) string {
	suggestions := []string{
		fmt.Sprintf("To install Go %s, you can:", required),
		"",
		"1. Download from official site:",
		fmt.Sprintf("   wget https://go.dev/dl/%s.linux-amd64.tar.gz", required),
		fmt.Sprintf("   sudo tar -C /usr/local -xzf %s.linux-amd64.tar.gz", required),
		"   export PATH=/usr/local/go/bin:$PATH",
		"",
		"2. Or use a version manager like g:",
		"   curl -sSL https://git.io/g-install | sh -s",
		fmt.Sprintf("   g install %s", strings.TrimPrefix(required, "go")),
		"",
		"3. Or modify the chain config to allow version flexibility:",
		"   binary_source:",
		"     type: \"source\"",
		"     build_command: \"make build GO_VERSION=1.24\"  # Force newer Go",
	}

	return strings.Join(suggestions, "\n")
}
