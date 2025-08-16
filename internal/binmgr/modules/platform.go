package modules

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

// PlatformInfo holds platform-specific information
type PlatformInfo struct {
	OS           string
	Arch         string
	OSVariants   []string // Alternative OS names (e.g., ["linux", "Linux"])
	ArchVariants []string // Alternative arch names (e.g., ["amd64", "x86_64"])
}

// PlatformDetector handles platform detection and Go executable finding
type PlatformDetector struct {
	logger *zap.Logger
}

// NewPlatformDetector creates a new platform detector
func NewPlatformDetector(logger *zap.Logger) *PlatformDetector {
	return &PlatformDetector{
		logger: logger,
	}
}

// GetCurrentPlatform returns detailed platform information with variants
func (p *PlatformDetector) GetCurrentPlatform() *PlatformInfo {
	os := runtime.GOOS
	arch := runtime.GOARCH

	osVariants := []string{os}
	archVariants := []string{arch}

	// Add common OS variants
	switch os {
	case "linux":
		osVariants = append(osVariants, "Linux", "linux")
	case "darwin":
		osVariants = append(osVariants, "Darwin", "macOS", "macos", "osx")
	case "windows":
		osVariants = append(osVariants, "Windows", "win", "Win")
	}

	// Add common architecture variants
	switch arch {
	case "amd64":
		archVariants = append(archVariants, "x86_64", "x64", "64")
	case "arm64":
		archVariants = append(archVariants, "aarch64", "arm")
	case "386":
		archVariants = append(archVariants, "i386", "x86", "32")
	}

	return &PlatformInfo{
		OS:           os,
		Arch:         arch,
		OSVariants:   osVariants,
		ArchVariants: archVariants,
	}
}

// FindGoExecutable searches for Go executable in common locations
func (p *PlatformDetector) FindGoExecutable() string {
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
		)
	}

	// Check current PATH first
	if goPath, err := exec.LookPath("go"); err == nil {
		return goPath
	}

	// Check common installation paths
	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// UpdateEnvVar updates or adds an environment variable in the environment slice
func (p *PlatformDetector) UpdateEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, envVar := range env {
		if strings.HasPrefix(envVar, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	// If not found, append it
	return append(env, prefix+value)
}

// GetEnvVar gets an environment variable value from the environment slice
func (p *PlatformDetector) GetEnvVar(env []string, key string) string {
	prefix := key + "="
	for _, envVar := range env {
		if strings.HasPrefix(envVar, prefix) {
			return strings.TrimPrefix(envVar, prefix)
		}
	}
	return ""
}
