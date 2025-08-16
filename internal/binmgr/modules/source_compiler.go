package modules

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"prop-voter/config"

	"go.uber.org/zap"
)

// SourceCompiler handles compilation of binaries from source code
type SourceCompiler struct {
	logger           *zap.Logger
	platformDetector *PlatformDetector
	binaryFinder     *BinaryFinder
	goVersionManager *GoVersionManager
	binDir           string
}

// NewSourceCompiler creates a new source compiler
func NewSourceCompiler(logger *zap.Logger, platformDetector *PlatformDetector, binaryFinder *BinaryFinder, binDir string) *SourceCompiler {
	return &SourceCompiler{
		logger:           logger,
		platformDetector: platformDetector,
		binaryFinder:     binaryFinder,
		goVersionManager: NewGoVersionManager(logger),
		binDir:           binDir,
	}
}

// CompileFromSource compiles a binary from source code
func (s *SourceCompiler) CompileFromSource(ctx context.Context, chain *config.ChainConfig) error {
	sourceRepo := chain.GetSourceRepo()
	if sourceRepo == "" {
		return fmt.Errorf("no source repository configured for chain %s", chain.GetName())
	}

	s.logger.Info("Compiling binary from source",
		zap.String("chain", chain.GetName()),
		zap.String("repo", sourceRepo),
		zap.String("branch", chain.GetSourceBranch()),
	)

	// Create temporary directory for cloning
	tempDir, err := os.MkdirTemp("", "prop-voter-build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Clone the repository
	cloneDir := filepath.Join(tempDir, "source")
	branch := chain.GetSourceBranch()

	if err := s.cloneRepository(ctx, sourceRepo, branch, cloneDir); err != nil {
		return err
	}

	// Build the binary
	buildCmd := chain.GetBuildCommand()
	buildTarget := chain.GetBuildTarget()

	if err := s.buildBinary(ctx, chain, cloneDir, buildCmd, buildTarget); err != nil {
		return err
	}

	return nil
}

// cloneRepository clones a git repository
func (s *SourceCompiler) cloneRepository(ctx context.Context, sourceRepo, branch, cloneDir string) error {
	s.logger.Info("Cloning repository",
		zap.String("repo", sourceRepo),
		zap.String("branch", branch),
		zap.String("dir", cloneDir),
	)

	cloneCmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", branch, sourceRepo, cloneDir)
	cloneCmd.Env = os.Environ() // Inherit environment for git as well
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone repository: %w\nOutput: %s", err, output)
	}

	return nil
}

// buildBinary builds the binary using the specified build command
func (s *SourceCompiler) buildBinary(ctx context.Context, chain *config.ChainConfig, cloneDir, buildCmd, buildTarget string) error {
	// If ignoring Go version, use a build command that bypasses version checks
	if chain.BinarySource.IgnoreGoVersion {
		buildCmd = s.getBuildCommandForIgnoreGoVersion(cloneDir, buildTarget, buildCmd)
		s.logger.Info("Using Go version bypass build command",
			zap.String("chain", chain.GetName()),
			zap.String("original_command", chain.GetBuildCommand()),
			zap.String("bypass_command", buildCmd),
		)
	}

	s.logger.Info("Building binary",
		zap.String("command", buildCmd),
		zap.String("target", buildTarget),
		zap.String("dir", cloneDir),
	)

	// Parse command and environment variables
	envVars, cleanCmd := s.parseEnvVarsFromCommand(buildCmd)

	// Execute build command in the cloned directory
	cmdParts := strings.Fields(cleanCmd)
	if len(cmdParts) == 0 {
		return fmt.Errorf("empty build command")
	}

	buildExecCmd := exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
	buildExecCmd.Dir = cloneDir

	// Setup environment for build
	if err := s.setupBuildEnvironment(buildExecCmd); err != nil {
		return err
	}

	// Add any command-specific environment variables
	for key, value := range envVars {
		buildExecCmd.Env = s.platformDetector.UpdateEnvVar(buildExecCmd.Env, key, value)
		s.logger.Debug("Added environment variable for build",
			zap.String("key", key),
			zap.String("value", value),
		)
	}

	// Execute build and capture output
	output, err := buildExecCmd.CombinedOutput()

	if err != nil {
		s.logger.Error("Build failed",
			zap.String("command", buildCmd),
			zap.Error(err),
			zap.String("output", string(output)),
		)

		// Check if this is a Go version compatibility issue
		outputStr := string(output)
		if s.isGoVersionError(outputStr) {
			if chain.BinarySource.IgnoreGoVersion {
				s.logger.Info("Go version incompatibility detected but ignored due to configuration",
					zap.String("chain", chain.GetName()),
					zap.Bool("ignore_go_version", chain.BinarySource.IgnoreGoVersion),
				)
				// Still fail the build, but with a different message suggesting a custom build command
				return fmt.Errorf("build failed due to Go version incompatibility (ignored). Consider using a custom build_command to bypass version checks: %s", outputStr)
			} else {
				s.logger.Warn("Build failed due to Go version incompatibility")
				if suggestion := s.handleGoVersionError(outputStr, chain); suggestion != "" {
					return fmt.Errorf("Go version incompatibility detected:\n%s\n\nSuggestions:\n%s", outputStr, suggestion)
				}
			}
		}

		return fmt.Errorf("failed to build binary: %w\nOutput: %s", err, output)
	}

	s.logger.Info("Build completed successfully",
		zap.String("command", buildCmd),
		zap.Int("output_length", len(output)),
	)

	// Log a portion of the build output for debugging
	outputStr := string(output)
	if len(outputStr) > 500 {
		s.logger.Debug("Build output (truncated)", zap.String("output", outputStr[:500]+"..."))
	} else {
		s.logger.Debug("Build output", zap.String("output", outputStr))
	}

	// Find and install the built binary
	return s.findAndInstallBinary(chain, cloneDir, buildTarget)
}

// setupBuildEnvironment sets up the build environment with proper Go paths
func (s *SourceCompiler) setupBuildEnvironment(buildExecCmd *exec.Cmd) error {
	// Start with inherited environment
	buildExecCmd.Env = os.Environ()

	// Detect and add Go to PATH explicitly
	goPath := s.platformDetector.FindGoExecutable()
	if goPath != "" {
		s.logger.Info("Found Go executable", zap.String("path", goPath))
		// Update PATH to include Go
		currentPath := os.Getenv("PATH")
		goBinDir := filepath.Dir(goPath)
		newPath := goBinDir + ":" + currentPath
		buildExecCmd.Env = s.platformDetector.UpdateEnvVar(buildExecCmd.Env, "PATH", newPath)
	} else {
		s.logger.Warn("Go executable not found in common locations, build may fail")
	}

	// Set Go environment variables
	if goPathEnv := os.Getenv("GOPATH"); goPathEnv != "" {
		buildExecCmd.Env = s.platformDetector.UpdateEnvVar(buildExecCmd.Env, "GOPATH", goPathEnv)
	}
	if goRoot := os.Getenv("GOROOT"); goRoot != "" {
		buildExecCmd.Env = s.platformDetector.UpdateEnvVar(buildExecCmd.Env, "GOROOT", goRoot)
	}

	// Log the environment for debugging
	s.logger.Info("Build environment prepared",
		zap.String("PATH", s.platformDetector.GetEnvVar(buildExecCmd.Env, "PATH")),
		zap.String("GOPATH", s.platformDetector.GetEnvVar(buildExecCmd.Env, "GOPATH")),
		zap.String("GOROOT", s.platformDetector.GetEnvVar(buildExecCmd.Env, "GOROOT")),
	)

	s.logger.Info("Starting build process",
		zap.String("command", strings.Join(buildExecCmd.Args, " ")),
		zap.String("working_dir", buildExecCmd.Dir),
	)

	return nil
}

// findAndInstallBinary finds the built binary and installs it to the bin directory
func (s *SourceCompiler) findAndInstallBinary(chain *config.ChainConfig, cloneDir, buildTarget string) error {
	builtBinaryPath, err := s.binaryFinder.FindBuiltBinary(cloneDir, buildTarget)
	if err != nil {
		return err
	}

	// Copy the built binary to the bin directory
	finalBinaryPath := filepath.Join(s.binDir, chain.GetCLIName())
	if err := s.copyFile(builtBinaryPath, finalBinaryPath); err != nil {
		return fmt.Errorf("failed to copy built binary: %w", err)
	}

	// Make it executable
	if err := os.Chmod(finalBinaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	s.logger.Info("Successfully compiled and installed binary from source",
		zap.String("chain", chain.GetName()),
		zap.String("source", builtBinaryPath),
		zap.String("dest", finalBinaryPath),
	)

	return nil
}

// copyFile copies a file from src to dest
func (s *SourceCompiler) copyFile(src, dest string) error {
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

// isGoVersionError checks if the build error is related to Go version incompatibility
func (s *SourceCompiler) isGoVersionError(output string) bool {
	goVersionKeywords := []string{
		"Go version",
		"go version",
		"required for compiling",
		"check-go-version",
		"golang version",
		"unsupported Go version",
	}

	outputLower := strings.ToLower(output)
	for _, keyword := range goVersionKeywords {
		if strings.Contains(outputLower, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

// handleGoVersionError handles Go version compatibility errors
func (s *SourceCompiler) handleGoVersionError(output string, chain *config.ChainConfig) string {
	// Extract required version from error message
	// Look for patterns like "Go version 1.20" or "go1.20"
	requiredVersion := s.extractRequiredGoVersion(output)
	if requiredVersion == "" {
		requiredVersion = "go1.20" // Default fallback
	}

	// Check for user override
	if chain.BinarySource.RequiredGoVersion != "" {
		requiredVersion = chain.BinarySource.RequiredGoVersion
		s.logger.Info("Using user-specified Go version requirement",
			zap.String("version", requiredVersion),
			zap.String("chain", chain.GetName()),
		)
	}

	suggestions := []string{}

	// Check if we have a compatible version installed
	if compatibleVersion, err := s.goVersionManager.FindCompatibleGoVersion(requiredVersion); err == nil {
		suggestion := fmt.Sprintf("Found compatible Go version: %s at %s", compatibleVersion.Version, compatibleVersion.Path)
		suggestion += "\nYou can set this as default or modify your PATH to use it:"
		suggestion += fmt.Sprintf("\nexport PATH=%s:$PATH", filepath.Dir(compatibleVersion.Path))
		suggestions = append(suggestions, suggestion)
	}

	// Add configuration-based solutions
	configSuggestion := fmt.Sprintf(`Configure chain to ignore Go version requirements:
  - chain_name: "%s"
    binary_source:
      type: "source"
      ignore_go_version: true  # Bypass Go version checks
      build_command: "make build"  # Or use custom build command`, chain.GetName())
	suggestions = append(suggestions, configSuggestion)

	// Add installation suggestion
	installSuggestion := s.goVersionManager.SuggestGoVersionInstallation(requiredVersion)
	suggestions = append(suggestions, installSuggestion)

	return strings.Join(suggestions, "\n\n"+strings.Repeat("-", 50)+"\n\n")
}

// extractRequiredGoVersion extracts the required Go version from error message
func (s *SourceCompiler) extractRequiredGoVersion(output string) string {
	// Look for patterns like:
	// "Go version 1.20 is required"
	// "go1.20 is required"
	// "golang 1.20+"

	patterns := []string{
		`[Gg]o version (\d+\.\d+)`,
		`go(\d+\.\d+)`,
		`golang (\d+\.\d+)`,
		`version (\d+\.\d+) is required`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(output); len(matches) > 1 {
			version := matches[1]
			// Ensure it has "go" prefix
			if !strings.HasPrefix(version, "go") {
				version = "go" + version
			}
			s.logger.Debug("Extracted required Go version", zap.String("version", version))
			return version
		}
	}

	return ""
}

// getBuildCommandForIgnoreGoVersion returns a build command that bypasses Go version checks
func (s *SourceCompiler) getBuildCommandForIgnoreGoVersion(cloneDir, buildTarget, originalCmd string) string {
	// Strategy 1: Try direct go build/install bypassing make
	// Most Cosmos chains follow standard patterns, so we can build directly

	// Check if there's a cmd directory structure (common in Cosmos projects)
	cmdDirs := []string{
		filepath.Join(cloneDir, "cmd", buildTarget),
		filepath.Join(cloneDir, "cmd", "daemon"),
		filepath.Join(cloneDir, "cmd"),
	}

	for _, cmdDir := range cmdDirs {
		if _, err := os.Stat(cmdDir); err == nil {
			relPath, _ := filepath.Rel(cloneDir, cmdDir)
			return fmt.Sprintf("go install -mod=readonly -ldflags '-w -s' ./%s", relPath)
		}
	}

	// Strategy 2: Try make with version check override
	// Many Cosmos chains support environment variables to bypass checks
	if strings.Contains(originalCmd, "make") {
		// Common environment variables that bypass version checks
		return "SKIP_GO_VERSION_CHECK=1 " + originalCmd
	}

	// Strategy 3: Direct go install (fallback)
	return "go install -mod=readonly -ldflags '-w -s' ./cmd/..."
}

// parseEnvVarsFromCommand separates environment variables from the command
func (s *SourceCompiler) parseEnvVarsFromCommand(cmd string) (map[string]string, string) {
	envVars := make(map[string]string)
	parts := strings.Fields(cmd)

	var cmdStart int
	for i, part := range parts {
		if strings.Contains(part, "=") && !strings.HasPrefix(part, "-") {
			// This looks like an environment variable
			envParts := strings.SplitN(part, "=", 2)
			if len(envParts) == 2 {
				envVars[envParts[0]] = envParts[1]
				continue
			}
		}
		// First non-env-var part is the start of the actual command
		cmdStart = i
		break
	}

	if cmdStart < len(parts) {
		cleanCmd := strings.Join(parts[cmdStart:], " ")
		return envVars, cleanCmd
	}

	return envVars, cmd
}
