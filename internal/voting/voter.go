package voting

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

	// Build the CLI command with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := v.buildVoteCommandWithContext(ctx, chainConfig, proposalID, option)

	// Always log the full CLI command string for visibility without debug mode
	fullCmd := strings.Join(cmd.Args, " ")
	v.logger.Info("Executing vote command", zap.String("command", fullCmd))
	v.logger.Debug("Executing vote command (args)", zap.Strings("cmd", cmd.Args))

	// Execute the command with timeout
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			v.logger.Error("Vote command timed out after 60 seconds",
				zap.String("chain", chainConfig.GetName()),
				zap.String("proposal_id", proposalID),
			)
			return "", fmt.Errorf("vote command timed out after 60 seconds - the transaction may still be processing")
		}

		v.logger.Error("Vote command failed",
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return "", fmt.Errorf("vote command failed: %w - output: %s", err, string(output))
	}

	// Parse transaction response from CLI output
	txResponse, err := v.parseTxResponse(string(output))
	if err != nil {
		v.logger.Warn("Could not parse transaction response",
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return "UNKNOWN_HASH_CHECK_LOGS", nil
	}

	// Check if transaction was successful
	if txResponse.Code != 0 {
		v.logger.Error("Transaction failed",
			zap.Int("code", txResponse.Code),
			zap.String("codespace", txResponse.Codespace),
			zap.String("tx_hash", txResponse.TxHash),
		)
		return "", fmt.Errorf("transaction failed with code %d: %s", txResponse.Code, txResponse.Codespace)
	}

	v.logger.Info("Vote submitted successfully",
		zap.String("chain", chainConfig.GetName()),
		zap.String("proposal_id", proposalID),
		zap.String("tx_hash", txResponse.TxHash),
	)

	return txResponse.TxHash, nil
}

// VoteAuthz submits an authz vote for a proposal on the specified chain on behalf of a granter
func (v *Voter) VoteAuthz(chainID, proposalID, option string) (string, error) {
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

	// Check if authz is enabled for this chain
	if !chainConfig.IsAuthzEnabled() {
		return "", fmt.Errorf("authz voting is not enabled for chain %s", chainConfig.GetName())
	}

	v.logger.Info("Submitting authz vote",
		zap.String("chain", chainConfig.GetName()),
		zap.String("chain_id", chainID),
		zap.String("proposal_id", proposalID),
		zap.String("option", option),
		zap.String("granter", chainConfig.GetGranterAddr()),
	)

	// Build the authz CLI command with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := v.buildAuthzVoteCommandWithContext(ctx, chainConfig, proposalID, option)

	// Always log the full CLI command string for visibility without debug mode
	fullCmd := strings.Join(cmd.Args, " ")
	v.logger.Info("Executing authz vote command", zap.String("command", fullCmd))
	v.logger.Debug("Executing authz vote command (args)", zap.Strings("cmd", cmd.Args))

	// Execute the command with timeout
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			v.logger.Error("Authz vote command timed out after 60 seconds",
				zap.String("chain", chainConfig.GetName()),
				zap.String("proposal_id", proposalID),
			)
			return "", fmt.Errorf("authz vote command timed out after 60 seconds - the transaction may still be processing")
		}

		v.logger.Error("Authz vote command failed",
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return "", fmt.Errorf("authz vote command failed: %w - output: %s", err, string(output))
	}

	// Parse transaction response from CLI output
	txResponse, err := v.parseTxResponse(string(output))
	if err != nil {
		v.logger.Warn("Could not parse authz transaction response",
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return "UNKNOWN_HASH_CHECK_LOGS", nil
	}

	// Check if transaction was successful
	if txResponse.Code != 0 {
		v.logger.Error("Authz transaction failed",
			zap.Int("code", txResponse.Code),
			zap.String("codespace", txResponse.Codespace),
			zap.String("tx_hash", txResponse.TxHash),
		)
		return "", fmt.Errorf("authz transaction failed with code %d: %s", txResponse.Code, txResponse.Codespace)
	}

	v.logger.Info("Authz vote submitted successfully",
		zap.String("chain", chainConfig.GetName()),
		zap.String("proposal_id", proposalID),
		zap.String("tx_hash", txResponse.TxHash),
		zap.String("granter", chainConfig.GetGranterAddr()),
	)

	return txResponse.TxHash, nil
}

// buildVoteCommandWithContext builds the CLI command for voting with timeout context
func (v *Voter) buildVoteCommandWithContext(ctx context.Context, chain *config.ChainConfig, proposalID, option string) *exec.Cmd {
	args := []string{
		"tx", "gov", "vote",
		proposalID,
		option,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", v.calculateFees(chain),
		"--keyring-backend", "test",
		"--yes",
		"--output", "json",
	}

	// Use managed binary path if available
	cliPath := v.getBinaryPath(chain.GetCLIName())
	return exec.CommandContext(ctx, cliPath, args...)
}

// buildVoteCommand builds the CLI command for voting
func (v *Voter) buildVoteCommand(chain *config.ChainConfig, proposalID, option string) *exec.Cmd {
	args := []string{
		"tx", "gov", "vote",
		proposalID,
		option,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
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

// buildAuthzVoteCommandWithContext builds the CLI command for authz voting with timeout context
func (v *Voter) buildAuthzVoteCommandWithContext(ctx context.Context, chain *config.ChainConfig, proposalID, option string) *exec.Cmd {
	// Create temporary message file for authz exec
	msgFile := fmt.Sprintf("/tmp/vote_msg_%s_%s.json", chain.GetChainID(), proposalID)

	// Create the governance vote message JSON
	govVoteMsg := fmt.Sprintf(`{
  "body": {
    "messages": [
      {
        "@type": "/cosmos.gov.v1beta1.MsgVote",
        "proposal_id": "%s",
        "voter": "%s",
        "option": "%s"
      }
    ]
  }
}`, proposalID, chain.GetGranterAddr(), v.mapVoteOption(option))

	// Write message to temporary file
	if err := os.WriteFile(msgFile, []byte(govVoteMsg), 0644); err != nil {
		v.logger.Error("Failed to write authz message file", zap.Error(err))
	}

	args := []string{
		"tx", "authz", "exec",
		msgFile,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", v.calculateFees(chain),
		"--keyring-backend", "test",
		"--yes",
		"--output", "json",
	}

	// Use managed binary path if available
	cliPath := v.getBinaryPath(chain.GetCLIName())
	cmd := exec.CommandContext(ctx, cliPath, args...)

	// Set up cleanup of the temporary file after command execution
	go func() {
		<-ctx.Done()
		if err := os.Remove(msgFile); err != nil {
			v.logger.Debug("Failed to remove temporary message file", zap.Error(err))
		}
	}()

	return cmd
}

// appendAPIKeyIfEnabled appends the api_key query parameter to a base URL if configured
func (v *Voter) appendAPIKeyIfEnabled(base string) string {
	if v.config.AuthEndpoints.Enabled && v.config.AuthEndpoints.APIKey != "" {
		if strings.Contains(base, "?") {
			return base + "&api_key=" + v.config.AuthEndpoints.APIKey
		}
		return base + "?api_key=" + v.config.AuthEndpoints.APIKey
	}
	return base
}

// mapVoteOption maps user-friendly vote options to the format expected by governance
func (v *Voter) mapVoteOption(option string) string {
	switch strings.ToLower(option) {
	case "yes":
		return "VOTE_OPTION_YES"
	case "no":
		return "VOTE_OPTION_NO"
	case "abstain":
		return "VOTE_OPTION_ABSTAIN"
	case "no_with_veto":
		return "VOTE_OPTION_NO_WITH_VETO"
	default:
		return "VOTE_OPTION_UNSPECIFIED"
	}
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

// TxResponse represents the essential fields from CLI transaction output
type TxResponse struct {
	TxHash    string `json:"txhash"`
	Code      int    `json:"code"`
	Codespace string `json:"codespace"`
}

// parseTxResponse parses the CLI JSON output to extract transaction details
func (v *Voter) parseTxResponse(output string) (*TxResponse, error) {
	// Try to find JSON in the output (might have other text before/after)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			var txResp TxResponse
			if err := json.Unmarshal([]byte(line), &txResp); err == nil {
				// Validate we got the essential fields
				if txResp.TxHash != "" {
					return &txResp, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no valid JSON transaction response found in output")
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
