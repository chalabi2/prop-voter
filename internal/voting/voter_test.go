package voting

import (
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

func BenchmarkParseTxResponse(b *testing.B) {
	cfg := &config.Config{}
	logger := zaptest.NewLogger(b)
	voter := NewVoter(cfg, logger)

	output := `{"height":"12345","txhash":"ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890ABCDEF1234567890","code":0,"codespace":""}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		voter.parseTxResponse(output)
	}
}
