package voting

import (
	"context"
	"os"
	"strings"
	"testing"

	"prop-voter/config"

	"go.uber.org/zap/zaptest"
)

func TestNewVoter(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)

	voter := NewVoter(cfg, logger)

	if voter.config != cfg {
		t.Error("Expected voter config to match provided config")
	}

	if voter.logger != logger {
		t.Error("Expected voter logger to match provided logger")
	}
}

func TestBuildVoteCommand(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain",
				ChainID:   "test-1",
				RPC:       "http://localhost:26657",
				Denom:     "utest",
				CLIName:   "testd",
				WalletKey: "test-key",
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	chain := cfg.Chains[0]
	cmd := voter.buildVoteCommand(&chain, "123", "yes")

	expectedArgs := []string{
		"tx", "gov", "vote",
		"123",
		"yes",
		"--from", "test-key",
		"--chain-id", "test-1",
		"--node", "http://localhost:26657",
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", "5000utest",
		"--keyring-backend", "test",
		"--yes",
		"--output", "json",
	}

	if cmd.Args[0] != "testd" {
		t.Errorf("Expected command 'testd', got '%s'", cmd.Args[0])
	}

	for i, expectedArg := range expectedArgs {
		if i+1 >= len(cmd.Args) {
			t.Fatalf("Expected arg at index %d, but command has only %d args", i+1, len(cmd.Args))
		}
		if cmd.Args[i+1] != expectedArg {
			t.Errorf("Expected arg at index %d to be '%s', got '%s'", i+1, expectedArg, cmd.Args[i+1])
		}
	}
}

func TestCalculateFees(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	testCases := []struct {
		chain    config.ChainConfig
		expected string
	}{
		{
			chain:    config.ChainConfig{ChainID: "cosmoshub-4"},
			expected: "5000uatom",
		},
		{
			chain:    config.ChainConfig{ChainID: "osmosis-1"},
			expected: "5000uosmo",
		},
		{
			chain:    config.ChainConfig{ChainID: "juno-1"},
			expected: "5000ujuno",
		},
		{
			chain:    config.ChainConfig{ChainID: "unknown-chain", Denom: "ucustom"},
			expected: "5000ucustom",
		},
	}

	for _, tc := range testCases {
		result := voter.calculateFees(&tc.chain)
		if result != tc.expected {
			t.Errorf("For chain %s, expected fees '%s', got '%s'", tc.chain.ChainID, tc.expected, result)
		}
	}
}

func TestParseTxResponse(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	testCases := []struct {
		output       string
		expectedHash string
		expectedCode int
		expectError  bool
		name         string
	}{
		{
			name:         "Valid JSON response - success",
			output:       `{"height":"12345","txhash":"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890","code":0,"codespace":""}`,
			expectedHash: "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890",
			expectedCode: 0,
			expectError:  false,
		},
		{
			name:         "Valid JSON response - failure",
			output:       `{"height":"12345","txhash":"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890","code":5,"codespace":"sdk"}`,
			expectedHash: "ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890",
			expectedCode: 5,
			expectError:  false,
		},
		{
			name:         "Multiline output with JSON",
			output:       "gas estimate: 116065\n{\"height\":\"22853036\",\"txhash\":\"F07BDD31E6CF3D3BCF0C0BCCB0ECA10802548F8E4DB052ACA1BE4C074FE34295\",\"code\":0,\"codespace\":\"\"}",
			expectedHash: "F07BDD31E6CF3D3BCF0C0BCCB0ECA10802548F8E4DB052ACA1BE4C074FE34295",
			expectedCode: 0,
			expectError:  false,
		},
		{
			name:        "No valid JSON found",
			output:      "Some other output without valid JSON",
			expectError: true,
		},
		{
			name:        "Invalid JSON",
			output:      `{"txhash":"INVALID_JSON"`,
			expectError: true,
		},
		{
			name:        "JSON without txhash",
			output:      `{"code":0,"height":"12345"}`,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := voter.parseTxResponse(tc.output)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.TxHash != tc.expectedHash {
				t.Errorf("Expected hash '%s', got '%s'", tc.expectedHash, result.TxHash)
			}

			if result.Code != tc.expectedCode {
				t.Errorf("Expected code %d, got %d", tc.expectedCode, result.Code)
			}
		})
	}
}

func TestVoteChainNotFound(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:    "Test Chain",
				ChainID: "test-1",
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	_, err := voter.Vote("non-existent-chain", "123", "yes")
	if err == nil {
		t.Error("Expected error for non-existent chain")
	}

	expectedError := "chain non-existent-chain not found in configuration"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedError, err.Error())
	}
}

// Note: Testing actual CLI execution would require the CLI tools to be installed
// and configured, so we'll test the command building logic instead

func TestValidateChainCLI(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	// Test with a CLI that should exist (assuming go is installed)
	chain := config.ChainConfig{
		CLIName: "go",
	}

	err := voter.ValidateChainCLI(chain)
	if err != nil {
		t.Errorf("Expected no error for 'go' command, got: %v", err)
	}

	// Test with a CLI that definitely doesn't exist
	chain = config.ChainConfig{
		CLIName: "this-cli-definitely-does-not-exist-12345",
	}

	err = voter.ValidateChainCLI(chain)
	if err == nil {
		t.Error("Expected error for non-existent CLI")
	}
}

func TestValidateWalletKey(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	// This test would require actual CLI tools and wallet keys to be set up
	// For now, we'll test that it properly constructs the command and handles errors

	chain := config.ChainConfig{
		CLIName:   "this-cli-does-not-exist",
		WalletKey: "test-key",
	}

	err := voter.ValidateWalletKey(chain)
	if err == nil {
		t.Error("Expected error for non-existent CLI")
	}
}

func TestValidateAllChains(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain 1",
				CLIName:   "this-cli-does-not-exist-1",
				WalletKey: "test-key-1",
				Prefix:    "test",
			},
			{
				Name:      "Test Chain 2",
				CLIName:   "this-cli-does-not-exist-2",
				WalletKey: "test-key-2",
				Prefix:    "test",
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	err := voter.ValidateAllChains()
	if err == nil {
		t.Error("Expected error due to non-existent CLI")
	}

	// Should fail on the first chain since both CLIs don't exist
	if !strings.Contains(err.Error(), "Test Chain 1") {
		t.Errorf("Expected error to mention 'Test Chain 1', got: %v", err)
	}
}

// Benchmark tests
func BenchmarkBuildVoteCommand(b *testing.B) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(b)
	voter := NewVoter(cfg, logger)

	chain := &config.ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		RPC:       "http://localhost:26657",
		Denom:     "utest",
		CLIName:   "testd",
		WalletKey: "test-key",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		voter.buildVoteCommand(chain, "123", "yes")
	}
}

// Tests for Authz functionality

func TestVoteAuthzChainNotFound(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:    "Test Chain",
				ChainID: "test-1",
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	_, err := voter.VoteAuthz("non-existent-chain", "123", "yes")
	if err == nil {
		t.Error("Expected error for non-existent chain")
	}

	expectedError := "chain non-existent-chain not found in configuration"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedError, err.Error())
	}
}

func TestVoteAuthzNotEnabled(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain",
				ChainID:   "test-1",
				WalletKey: "test-key",
				// Authz is not enabled - default values
				Authz: config.AuthzConfig{
					Enabled: false,
				},
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	_, err := voter.VoteAuthz("test-1", "123", "yes")
	if err == nil {
		t.Error("Expected error when authz is not enabled")
	}

	expectedError := "authz voting is not enabled for chain Test Chain"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedError, err.Error())
	}
}

func TestVoteAuthzAuthzEnabledButNoGranterAddr(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{
				Name:      "Test Chain",
				ChainID:   "test-1",
				WalletKey: "test-key",
				Authz: config.AuthzConfig{
					Enabled:     true,
					GranterAddr: "", // No granter address
				},
			},
		},
	}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	_, err := voter.VoteAuthz("test-1", "123", "yes")
	if err == nil {
		t.Error("Expected error when authz is enabled but no granter address")
	}

	expectedError := "authz voting is not enabled for chain Test Chain"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', got '%s'", expectedError, err.Error())
	}
}

func TestBuildAuthzVoteCommand(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	chain := &config.ChainConfig{
		Name:      "Test Chain",
		ChainID:   "test-1",
		RPC:       "http://localhost:26657",
		Denom:     "utest",
		CLIName:   "testd",
		WalletKey: "grantee-key",
		Authz: config.AuthzConfig{
			Enabled:     true,
			GranterAddr: "test1granter123addr456",
			GranterName: "Test Granter",
		},
	}

	ctx := context.Background()
	cmd := voter.buildAuthzVoteCommandWithContext(ctx, chain, "123", "yes")

	expectedCommand := "testd"
	if cmd.Args[0] != expectedCommand {
		t.Errorf("Expected command '%s', got '%s'", expectedCommand, cmd.Args[0])
	}

	expectedArgs := []string{
		"tx", "authz", "exec",
		"/tmp/vote_msg_test-1_123.json", // message file path
		"--from", "grantee-key",
		"--chain-id", "test-1",
		"--node", "http://localhost:26657",
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--fees", "5000utest",
		"--keyring-backend", "test",
		"--yes",
		"--output", "json",
	}

	if len(cmd.Args) != len(expectedArgs)+1 {
		t.Fatalf("Expected %d args, got %d", len(expectedArgs)+1, len(cmd.Args))
	}

	for i, expectedArg := range expectedArgs {
		if cmd.Args[i+1] != expectedArg {
			t.Errorf("Expected arg at index %d to be '%s', got '%s'", i+1, expectedArg, cmd.Args[i+1])
		}
	}

	// Clean up the temporary file that might have been created
	msgFile := "/tmp/vote_msg_test-1_123.json"
	_ = os.Remove(msgFile) // Ignore error in test cleanup
}

func TestMapVoteOption(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	testCases := []struct {
		input    string
		expected string
	}{
		{"yes", "VOTE_OPTION_YES"},
		{"YES", "VOTE_OPTION_YES"},
		{"Yes", "VOTE_OPTION_YES"},
		{"no", "VOTE_OPTION_NO"},
		{"NO", "VOTE_OPTION_NO"},
		{"abstain", "VOTE_OPTION_ABSTAIN"},
		{"ABSTAIN", "VOTE_OPTION_ABSTAIN"},
		{"no_with_veto", "VOTE_OPTION_NO_WITH_VETO"},
		{"NO_WITH_VETO", "VOTE_OPTION_NO_WITH_VETO"},
		{"invalid", "VOTE_OPTION_UNSPECIFIED"},
		{"", "VOTE_OPTION_UNSPECIFIED"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := voter.mapVoteOption(tc.input)
			if result != tc.expected {
				t.Errorf("Expected '%s' for input '%s', got '%s'", tc.expected, tc.input, result)
			}
		})
	}
}

func TestAuthzVoteMessageGeneration(t *testing.T) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(t)
	voter := NewVoter(cfg, logger)

	chain := &config.ChainConfig{
		ChainID: "test-1",
		CLIName: "testd",
		Authz: config.AuthzConfig{
			Enabled:     true,
			GranterAddr: "test1granter123addr456",
		},
	}

	ctx := context.Background()
	proposalID := "123"
	option := "yes"

	// Build the command which should create the message file
	cmd := voter.buildAuthzVoteCommandWithContext(ctx, chain, proposalID, option)

	// Check that the temporary file was created and has correct content
	msgFile := "/tmp/vote_msg_test-1_123.json"

	// The file should exist after building the command
	if _, err := os.Stat(msgFile); os.IsNotExist(err) {
		t.Fatalf("Expected message file to be created at %s", msgFile)
	}

	// Read the message file content
	content, err := os.ReadFile(msgFile)
	if err != nil {
		t.Fatalf("Failed to read message file: %v", err)
	}

	// Verify the JSON structure contains expected fields
	expectedContent := []string{
		`"@type": "/cosmos.gov.v1beta1.MsgVote"`,
		`"proposal_id": "123"`,
		`"voter": "test1granter123addr456"`,
		`"option": "VOTE_OPTION_YES"`,
	}

	contentStr := string(content)
	for _, expected := range expectedContent {
		if !strings.Contains(contentStr, expected) {
			t.Errorf("Expected message file to contain '%s', but content was: %s", expected, contentStr)
		}
	}

	// Clean up
	_ = os.Remove(msgFile) // Ignore error in test cleanup

	// Verify command args
	if cmd.Args[0] != "testd" { // Default CLI name from getBinaryPath
		t.Errorf("Expected command to use CLI tool, got: %s", cmd.Args[0])
	}
}

func BenchmarkParseTxResponse(b *testing.B) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(b)
	voter := NewVoter(cfg, logger)

	output := `{"height":"12345","txhash":"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890","code":0,"codespace":""}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = voter.parseTxResponse(output) // Benchmark doesn't need error checking
	}
}
