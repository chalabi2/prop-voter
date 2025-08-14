package voting

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"prop-voter/config"

	"go.uber.org/zap"
)

// Voter handles voting operations across different Cosmos chains
type Voter struct {
	config *config.Config
	logger *zap.Logger
}

// NewVoter creates a new voter instance
func NewVoter(config *config.Config, logger *zap.Logger) *Voter {
	return &Voter{
		config: config,
		logger: logger,
	}
}

// Vote submits a vote for a proposal on the specified chain
func (v *Voter) Vote(chainID, proposalID, option string) (string, error) {
	// Find the chain configuration
	var chainConfig *config.ChainConfig
	for _, chain := range v.config.Chains {
		if chain.GetChainID() == chainID {
			chainConfig = &chain
			break
		}
	}

	if chainConfig == nil {
		return "", fmt.Errorf("chain %s not found in configuration", chainID)
	}

	v.logger.Info("Submitting vote",
		zap.String("chain", chainConfig.GetName()),
		zap.String("chain_id", chainID),
		zap.String("proposal_id", proposalID),
		zap.String("option", option),
	)

	// Build the CLI command
	cmd := v.buildVoteCommand(chainConfig, proposalID, option)

	v.logger.Debug("Executing vote command", zap.Strings("cmd", cmd.Args))

	// Execute the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		v.logger.Error("Vote command failed",
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return "", fmt.Errorf("vote command failed: %w - output: %s", err, string(output))
	}

	// Parse transaction hash from output
	txHash := v.parseTxHash(string(output))
	if txHash == "" {
		v.logger.Warn("Could not parse transaction hash from output",
			zap.String("output", string(output)),
		)
		return string(output), nil // Return raw output if we can't parse tx hash
	}

	v.logger.Info("Vote submitted successfully",
		zap.String("chain", chainConfig.GetName()),
		zap.String("proposal_id", proposalID),
		zap.String("tx_hash", txHash),
	)

	return txHash, nil
}

// buildVoteCommand builds the CLI command for voting
func (v *Voter) buildVoteCommand(chain *config.ChainConfig, proposalID, option string) *exec.Cmd {
	args := []string{
		"tx", "gov", "vote",
		proposalID,
		option,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", chain.RPC,
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", v.calculateFees(chain),
		"--keyring-backend", "test",
		"--yes",
		"--output", "json",
	}

	// Use managed binary path if available
	cliPath := v.getBinaryPath(chain.GetCLIName())
	return exec.Command(cliPath, args...)
}

// getBinaryPath returns the path to the CLI binary (managed or system)
func (v *Voter) getBinaryPath(cliName string) string {
	// Check if we have a managed binary
	managedPath := filepath.Join("./bin", cliName)
	if _, err := os.Stat(managedPath); err == nil {
		return managedPath
	}

	// Fall back to system PATH
	return cliName
}

// calculateFees calculates appropriate fees for the transaction
func (v *Voter) calculateFees(chain *config.ChainConfig) string {
	// Default fee amounts for different chains
	feeMap := map[string]string{
		"cosmoshub-4": "5000uatom",
		"osmosis-1":   "5000uosmo",
		"juno-1":      "5000ujuno",
	}

	if fee, exists := feeMap[chain.GetChainID()]; exists {
		return fee
	}

	// Default fallback
	return fmt.Sprintf("5000%s", chain.GetDenom())
}

// parseTxHash extracts the transaction hash from CLI output
func (v *Voter) parseTxHash(output string) string {
	// Look for txhash in JSON output
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "txhash") {
			// Extract hash from JSON-like output - handle both "txhash":"hash" and "txhash": "hash"
			// Find the colon and extract everything after it
			colonIndex := strings.Index(line, ":")
			if colonIndex >= 0 {
				hashPart := line[colonIndex+1:]
				// Remove quotes, commas, spaces, and braces
				hash := strings.Trim(hashPart, " \"',}{")
				if len(hash) == 64 { // SHA256 hash length
					return hash
				}
			}
		}

		// Alternative: look for "Transaction Hash:" format
		if strings.Contains(strings.ToLower(line), "transaction hash") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				hash := strings.TrimSpace(parts[1])
				if len(hash) == 64 {
					return hash
				}
			}
		}
	}

	return ""
}

// ValidateChainCLI validates that the CLI tool for a chain is available
func (v *Voter) ValidateChainCLI(chain config.ChainConfig) error {
	cliPath := v.getBinaryPath(chain.GetCLIName())
	cmd := exec.Command(cliPath, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("CLI tool %s not found or not working: %w - output: %s",
			cliPath, err, string(output))
	}

	v.logger.Debug("CLI validation successful",
		zap.String("cli", cliPath),
		zap.String("output", string(output)),
	)

	return nil
}

// ValidateWalletKey validates that the wallet key exists for a chain
func (v *Voter) ValidateWalletKey(chain config.ChainConfig) error {
	cliPath := v.getBinaryPath(chain.GetCLIName())
	cmd := exec.Command(cliPath, "keys", "show", chain.WalletKey, "--address", "--keyring-backend", "test")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wallet key %s not found for chain %s: %w - output: %s",
			chain.WalletKey, chain.GetName(), err, string(output))
	}

	address := strings.TrimSpace(string(output))
	if !strings.HasPrefix(address, chain.GetPrefix()) {
		return fmt.Errorf("wallet address %s does not match expected prefix %s for chain %s",
			address, chain.GetPrefix(), chain.GetName())
	}

	v.logger.Info("Wallet validation successful",
		zap.String("chain", chain.GetName()),
		zap.String("key", chain.WalletKey),
		zap.String("address", address),
	)

	return nil
}

// ValidateAllChains validates CLI tools and wallet keys for all configured chains
func (v *Voter) ValidateAllChains() error {
	for _, chain := range v.config.Chains {
		if err := v.ValidateChainCLI(chain); err != nil {
			return fmt.Errorf("validation failed for chain %s: %w", chain.GetName(), err)
		}

		if err := v.ValidateWalletKey(chain); err != nil {
			return fmt.Errorf("wallet validation failed for chain %s: %w", chain.GetName(), err)
		}
	}

	v.logger.Info("All chains validated successfully")
	return nil
}
