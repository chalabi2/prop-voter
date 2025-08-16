package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// BinaryFinder handles finding built binaries in various locations
type BinaryFinder struct {
	logger *zap.Logger
}

// NewBinaryFinder creates a new binary finder
func NewBinaryFinder(logger *zap.Logger) *BinaryFinder {
	return &BinaryFinder{
		logger: logger,
	}
}

// FindBuiltBinary finds a built binary in common locations
func (b *BinaryFinder) FindBuiltBinary(cloneDir, buildTarget string) (string, error) {
	// For 'make install', check if binary is now in Go bin or local bin
	goBinPath := os.Getenv("GOBIN")
	if goBinPath == "" {
		if goPath := os.Getenv("GOPATH"); goPath != "" {
			goBinPath = filepath.Join(goPath, "bin")
		} else if home, err := os.UserHomeDir(); err == nil {
			goBinPath = filepath.Join(home, "go", "bin")
		}
	}

	// Common locations where binaries might be built or installed
	possiblePaths := []string{
		// Installed locations (for make install)
		filepath.Join(goBinPath, buildTarget),
		filepath.Join("/usr/local/bin", buildTarget),
		filepath.Join(os.Getenv("HOME"), "go/bin", buildTarget),
		// Build locations (for make build)
		filepath.Join(cloneDir, "build", buildTarget),
		filepath.Join(cloneDir, "bin", buildTarget),
		filepath.Join(cloneDir, buildTarget),
		filepath.Join(cloneDir, "cmd", buildTarget, buildTarget),
		// Cosmos SDK common patterns
		filepath.Join(cloneDir, "build", "bin", buildTarget),
		filepath.Join(cloneDir, "cmd", "cosmosd", buildTarget),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			b.logger.Debug("Found built binary",
				zap.String("path", path),
				zap.String("target", buildTarget),
			)
			return path, nil
		}
	}

	// If not found, do a more thorough search
	b.logger.Warn("Could not find built binary, scanning for files",
		zap.String("target", buildTarget),
		zap.Int("locations_checked", len(possiblePaths)),
	)

	// Show what files/directories actually exist
	for _, path := range possiblePaths {
		if stat, err := os.Stat(path); err == nil {
			b.logger.Info("Found file/directory",
				zap.String("path", path),
				zap.Bool("is_dir", stat.IsDir()),
				zap.Int64("size", stat.Size()),
			)
		} else {
			b.logger.Debug("Path does not exist", zap.String("path", path))
		}
	}

	// Try to find any files with the target name
	b.logger.Info("Searching for any files matching target name", zap.String("target", buildTarget))
	foundBinary := ""
	if err := b.findFilesInDirectory(cloneDir, buildTarget, &foundBinary); err != nil {
		b.logger.Warn("Failed to search directory", zap.Error(err))
	}

	if foundBinary != "" {
		return foundBinary, nil
	}

	return "", fmt.Errorf("could not find built binary %s in any expected location. Tried: %v", buildTarget, possiblePaths)
}

// findFilesInDirectory recursively searches for files matching the target name
func (b *BinaryFinder) findFilesInDirectory(dir, target string, foundBinary *string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking even if there's an error
		}

		if !info.IsDir() && (strings.Contains(info.Name(), target) || info.Name() == target) {
			b.logger.Info("Found potential binary",
				zap.String("path", path),
				zap.String("name", info.Name()),
				zap.Int64("size", info.Size()),
				zap.Bool("executable", info.Mode()&0111 != 0),
			)

			// If this is an exact match and executable, use it
			if info.Name() == target && info.Mode()&0111 != 0 && *foundBinary == "" {
				*foundBinary = path
			}
		}
		return nil
	})
}
