package voting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	// Build, sign, encode, and broadcast via REST
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	txHash, err := v.buildSignAndBroadcastGovVoteREST(ctx, chainConfig, proposalID, option)
	if err != nil {
		return "", err
	}

	v.logger.Info("Vote submitted successfully",
		zap.String("chain", chainConfig.GetName()),
		zap.String("proposal_id", proposalID),
		zap.String("tx_hash", txHash),
	)

	return txHash, nil
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

	// Build, sign, encode, and broadcast via REST
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	txHash, err := v.buildSignAndBroadcastAuthzVoteREST(ctx, chainConfig, proposalID, option)
	if err != nil {
		return "", err
	}

	v.logger.Info("Authz vote submitted successfully",
		zap.String("chain", chainConfig.GetName()),
		zap.String("proposal_id", proposalID),
		zap.String("tx_hash", txHash),
		zap.String("granter", chainConfig.GetGranterAddr()),
	)

	return txHash, nil
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

// buildSignAndBroadcastGovVoteREST constructs, signs, encodes and broadcasts a gov vote via REST
func (v *Voter) buildSignAndBroadcastGovVoteREST(ctx context.Context, chain *config.ChainConfig, proposalID, option string) (string, error) {
	// 1) Build unsigned tx to temp file
	unsignedFile := fmt.Sprintf("/tmp/unsigned_vote_%s_%s.json", chain.GetChainID(), proposalID)
	buildArgs := []string{
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
		"--generate-only",
		"--output", "json",
	}

	if err := v.execToFileWithContext(ctx, chain.GetCLIName(), buildArgs, unsignedFile); err != nil {
		return "", fmt.Errorf("failed to build unsigned tx: %w", err)
	}

	// 2) Sign the tx (online, queries via RPC are OK)
	signedFile := fmt.Sprintf("/tmp/signed_vote_%s_%s.json", chain.GetChainID(), proposalID)
	signArgs := []string{
		"tx", "sign", unsignedFile,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
		"--keyring-backend", "test",
		"--output", "json",
	}
	if err := v.execToFileWithContext(ctx, chain.GetCLIName(), signArgs, signedFile); err != nil {
		return "", fmt.Errorf("failed to sign tx: %w", err)
	}

	// 3) Encode to base64 (tx_bytes)
	txBytes, err := v.encodeTxFileToBase64WithContext(ctx, chain.GetCLIName(), signedFile)
	if err != nil {
		return "", fmt.Errorf("failed to encode tx to base64: %w", err)
	}

	// 4) Broadcast via REST
	txResp, err := v.broadcastTxBytesREST(ctx, chain, txBytes)
	if err != nil {
		return "", err
	}
	if txResp.Code != 0 {
		return "", fmt.Errorf("transaction failed with code %d: %s", txResp.Code, txResp.Codespace)
	}
	return txResp.TxHash, nil
}

// buildSignAndBroadcastAuthzVoteREST constructs, signs, encodes and broadcasts an authz vote via REST
func (v *Voter) buildSignAndBroadcastAuthzVoteREST(ctx context.Context, chain *config.ChainConfig, proposalID, option string) (string, error) {
	// Prepare the authz exec message file
	msgFile := fmt.Sprintf("/tmp/vote_msg_%s_%s.json", chain.GetChainID(), proposalID)
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
	if err := os.WriteFile(msgFile, []byte(govVoteMsg), 0644); err != nil {
		return "", fmt.Errorf("failed to write authz msg file: %w", err)
	}
	defer func() { _ = os.Remove(msgFile) }()

	// 1) Build unsigned tx to temp file
	unsignedFile := fmt.Sprintf("/tmp/unsigned_authz_vote_%s_%s.json", chain.GetChainID(), proposalID)
	buildArgs := []string{
		"tx", "authz", "exec",
		msgFile,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", v.calculateFees(chain),
		"--keyring-backend", "test",
		"--generate-only",
		"--output", "json",
	}
	if err := v.execToFileWithContext(ctx, chain.GetCLIName(), buildArgs, unsignedFile); err != nil {
		return "", fmt.Errorf("failed to build unsigned authz tx: %w", err)
	}

	// 2) Sign the tx (online)
	signedFile := fmt.Sprintf("/tmp/signed_authz_vote_%s_%s.json", chain.GetChainID(), proposalID)
	signArgs := []string{
		"tx", "sign", unsignedFile,
		"--from", chain.WalletKey,
		"--chain-id", chain.GetChainID(),
		"--node", v.appendAPIKeyIfEnabled(chain.RPC),
		"--keyring-backend", "test",
		"--output", "json",
	}
	if err := v.execToFileWithContext(ctx, chain.GetCLIName(), signArgs, signedFile); err != nil {
		return "", fmt.Errorf("failed to sign authz tx: %w", err)
	}

	// 3) Encode to base64 (tx_bytes)
	txBytes, err := v.encodeTxFileToBase64WithContext(ctx, chain.GetCLIName(), signedFile)
	if err != nil {
		return "", fmt.Errorf("failed to encode authz tx to base64: %w", err)
	}

	// 4) Broadcast via REST
	txResp, err := v.broadcastTxBytesREST(ctx, chain, txBytes)
	if err != nil {
		return "", err
	}
	if txResp.Code != 0 {
		return "", fmt.Errorf("authz transaction failed with code %d: %s", txResp.Code, txResp.Codespace)
	}
	return txResp.TxHash, nil
}

// execToFileWithContext runs a CLI command and writes stdout to a file
func (v *Voter) execToFileWithContext(ctx context.Context, cli string, args []string, outPath string) error {
	cliPath := v.getBinaryPath(cli)
	cmd := exec.CommandContext(ctx, cliPath, args...)
	fullCmd := strings.Join(cmd.Args, " ")
	v.logger.Info("Executing CLI command", zap.String("command", fullCmd))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %w - output: %s", err, string(output))
	}
	if err := os.WriteFile(outPath, output, 0644); err != nil {
		return fmt.Errorf("failed writing output file: %w", err)
	}
	return nil
}

// encodeTxFileToBase64WithContext encodes a signed tx JSON file to base64 using CLI
func (v *Voter) encodeTxFileToBase64WithContext(ctx context.Context, cli string, signedFile string) (string, error) {
	cliPath := v.getBinaryPath(cli)
	cmd := exec.CommandContext(ctx, cliPath, "tx", "encode", signedFile)
	fullCmd := strings.Join(cmd.Args, " ")
	v.logger.Info("Encoding tx to base64", zap.String("command", fullCmd))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("encode failed: %w - output: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

// broadcastTxBytesREST posts tx_bytes to the REST txs endpoint
func (v *Voter) broadcastTxBytesREST(ctx context.Context, chain *config.ChainConfig, txBytesBase64 string) (*TxResponse, error) {
	type broadcastRequest struct {
		TxBytes string `json:"tx_bytes"`
		Mode    string `json:"mode"`
	}
	type broadcastResponse struct {
		TxResponse TxResponse `json:"tx_response"`
	}

	urlBase := strings.TrimRight(v.appendAPIKeyIfEnabled(chain.REST), "/")
	url := urlBase + "/cosmos/tx/v1beta1/txs"
	reqBody := broadcastRequest{TxBytes: txBytesBase64, Mode: "BROADCAST_MODE_SYNC"}
	data, _ := json.Marshal(reqBody)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func(body io.ReadCloser) { _ = body.Close() }(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("REST broadcast failed: status %d - body: %s", resp.StatusCode, string(body))
	}

	var br broadcastResponse
	if err := json.Unmarshal(body, &br); err != nil {
		return nil, fmt.Errorf("failed to parse broadcast response: %w - body: %s", err, string(body))
	}
	return &br.TxResponse, nil
}

// defaultGasLimit returns a conservative gas limit for simple messages
func (v *Voter) defaultGasLimit(chain *config.ChainConfig) string {
	// Conservative default; adjust per-chain here if needed
	return "200000"
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
