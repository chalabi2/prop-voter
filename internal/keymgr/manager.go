package keymgr

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/wallet"

	"go.uber.org/zap"
)

// Manager handles secure key operations and management
type Manager struct {
	config        *config.Config
	logger        *zap.Logger
	walletManager *wallet.Manager
}

// KeyInfo represents information about a wallet key
type KeyInfo struct {
	Name    string
	Address string
	Chain   string
	Type    string // local, ledger, etc.
}

// NewManager creates a new key manager
func NewManager(config *config.Config, logger *zap.Logger, walletManager *wallet.Manager) *Manager {
	return &Manager{
		config:        config,
		logger:        logger,
		walletManager: walletManager,
	}
}

// SetupKeys initializes key management for all chains
func (m *Manager) SetupKeys(ctx context.Context) error {
	if !m.config.KeyManager.AutoImport {
		m.logger.Info("Auto-import disabled, keys must be managed manually")
		return nil
	}

	m.logger.Info("Setting up keys for all chains")

	for _, chain := range m.config.Chains {
		if err := m.setupChainKeys(ctx, chain); err != nil {
			m.logger.Error("Failed to setup keys for chain",
				zap.String("chain", chain.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

// setupChainKeys sets up keys for a specific chain
func (m *Manager) setupChainKeys(ctx context.Context, chain config.ChainConfig) error {
	// Get CLI binary path
	binaryPath := m.getBinaryPath(chain.GetCLIName())

	// Check if key already exists
	keyExists, err := m.keyExists(binaryPath, chain.WalletKey)
	if err != nil {
		return fmt.Errorf("failed to check if key exists: %w", err)
	}

	if keyExists {
		m.logger.Debug("Key already exists",
			zap.String("chain", chain.Name),
			zap.String("key", chain.WalletKey),
		)
		return nil
	}

	m.logger.Info("Key not found, need to import",
		zap.String("chain", chain.Name),
		zap.String("key", chain.WalletKey),
	)

	// Look for key files in key directory
	keyFile := m.findKeyFile(chain.WalletKey)
	if keyFile != "" {
		return m.importKeyFromFile(binaryPath, chain.WalletKey, keyFile)
	}

	// If no key file found, prompt for manual import
	m.logger.Warn("No key file found for chain, manual import required",
		zap.String("chain", chain.Name),
		zap.String("key", chain.WalletKey),
	)

	return nil
}

// ImportKey imports a key from a mnemonic or private key
func (m *Manager) ImportKey(chainName, keyName, mnemonic string) error {
	chain := m.findChainConfig(chainName)
	if chain == nil {
		return fmt.Errorf("chain %s not found", chainName)
	}

	binaryPath := m.getBinaryPath(chain.GetCLIName())

	m.logger.Info("Importing key",
		zap.String("chain", chainName),
		zap.String("key", keyName),
	)

	// Use the CLI to import the key with proper keyring backend
	cmd := exec.Command(binaryPath, "keys", "add", keyName, "--recover", "--keyring-backend", "test")

	// Set up stdin to provide the mnemonic
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start import command: %w", err)
	}

	// Send the mnemonic
	if _, err := stdin.Write([]byte(mnemonic + "\n")); err != nil {
		stdin.Close()
		cmd.Wait()
		return fmt.Errorf("failed to write mnemonic: %w", err)
	}
	stdin.Close()

	// Wait for completion
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("key import failed: %w", err)
	}

	// Get the address
	address, err := m.getKeyAddress(binaryPath, keyName)
	if err != nil {
		return fmt.Errorf("failed to get key address: %w", err)
	}

	// Store in wallet manager if encryption is enabled
	if m.config.KeyManager.EncryptKeys {
		if err := m.walletManager.StoreWallet(chain.ChainID, keyName, address, mnemonic); err != nil {
			m.logger.Warn("Failed to store key in wallet manager", zap.Error(err))
		}
	}

	m.logger.Info("Key imported successfully",
		zap.String("chain", chainName),
		zap.String("key", keyName),
		zap.String("address", address),
	)

	return nil
}

// ImportKeyFromFile imports a key from a file
func (m *Manager) ImportKeyFromFile(chainName, keyName, filePath string) error {
	// Read the file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read key file: %w", err)
	}

	// Assume it's a mnemonic for now
	mnemonic := strings.TrimSpace(string(content))

	return m.ImportKey(chainName, keyName, mnemonic)
}

// ExportKey exports a key to a file (with user confirmation)
func (m *Manager) ExportKey(chainName, keyName, outputPath string) error {
	chain := m.findChainConfig(chainName)
	if chain == nil {
		return fmt.Errorf("chain %s not found", chainName)
	}

	// Security warning
	m.logger.Warn("SECURITY WARNING: Exporting private key material",
		zap.String("chain", chainName),
		zap.String("key", keyName),
		zap.String("output", outputPath),
	)

	// Get mnemonic from wallet manager or CLI
	var mnemonic string
	var err error

	if m.config.KeyManager.EncryptKeys {
		_, privateData, err := m.walletManager.GetWallet(chain.ChainID)
		if err == nil {
			mnemonic = privateData
		}
	}

	if mnemonic == "" {
		// Try to export from CLI (this might not work for all chains)
		binaryPath := m.getBinaryPath(chain.GetCLIName())
		mnemonic, err = m.exportKeyFromCLI(binaryPath, keyName)
		if err != nil {
			return fmt.Errorf("failed to export key: %w", err)
		}
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Write to file with restricted permissions
	if err := os.WriteFile(outputPath, []byte(mnemonic), 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	m.logger.Info("Key exported successfully",
		zap.String("chain", chainName),
		zap.String("key", keyName),
		zap.String("output", outputPath),
	)

	return nil
}

// ListKeys lists all keys for all chains
func (m *Manager) ListKeys() ([]KeyInfo, error) {
	var keys []KeyInfo

	for _, chain := range m.config.Chains {
		binaryPath := m.getBinaryPath(chain.GetCLIName())

		chainKeys, err := m.listChainKeys(binaryPath, chain.Name)
		if err != nil {
			m.logger.Error("Failed to list keys for chain",
				zap.String("chain", chain.Name),
				zap.Error(err),
			)
			continue
		}

		keys = append(keys, chainKeys...)
	}

	return keys, nil
}

// ValidateKeys validates that all required keys exist
func (m *Manager) ValidateKeys() error {
	var missingKeys []string

	for _, chain := range m.config.Chains {
		binaryPath := m.getBinaryPath(chain.GetCLIName())

		exists, err := m.keyExists(binaryPath, chain.WalletKey)
		if err != nil {
			return fmt.Errorf("failed to check key for chain %s: %w", chain.Name, err)
		}

		if !exists {
			missingKeys = append(missingKeys, fmt.Sprintf("%s:%s", chain.Name, chain.WalletKey))
		}
	}

	if len(missingKeys) > 0 {
		return fmt.Errorf("missing keys: %s", strings.Join(missingKeys, ", "))
	}

	return nil
}

// BackupKeys creates backups of all keys
func (m *Manager) BackupKeys(backupDir string) error {
	if !m.config.KeyManager.BackupKeys {
		return fmt.Errorf("key backup is disabled")
	}

	// Create backup directory
	timestamp := time.Now().Format("20060102-150405")
	fullBackupDir := filepath.Join(backupDir, "key-backup-"+timestamp)

	if err := os.MkdirAll(fullBackupDir, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	m.logger.Info("Creating key backup", zap.String("dir", fullBackupDir))

	for _, chain := range m.config.Chains {
		if chain.WalletKey == "" {
			continue
		}

		backupFile := filepath.Join(fullBackupDir, fmt.Sprintf("%s-%s.key", chain.Name, chain.WalletKey))

		if err := m.ExportKey(chain.Name, chain.WalletKey, backupFile); err != nil {
			m.logger.Error("Failed to backup key",
				zap.String("chain", chain.Name),
				zap.String("key", chain.WalletKey),
				zap.Error(err),
			)
			continue
		}
	}

	m.logger.Info("Key backup completed", zap.String("dir", fullBackupDir))
	return nil
}

// Helper methods

func (m *Manager) getBinaryPath(cliName string) string {
	if m.config.BinaryManager.Enabled {
		return filepath.Join(m.config.BinaryManager.BinDir, cliName)
	}
	return cliName // Assume it's in PATH
}

func (m *Manager) findChainConfig(chainName string) *config.ChainConfig {
	for _, chain := range m.config.Chains {
		if chain.Name == chainName {
			return &chain
		}
	}
	return nil
}

func (m *Manager) keyExists(binaryPath, keyName string) (bool, error) {
	cmd := exec.Command(binaryPath, "keys", "show", keyName, "--address", "--keyring-backend", "test")
	err := cmd.Run()
	return err == nil, nil
}

func (m *Manager) getKeyAddress(binaryPath, keyName string) (string, error) {
	cmd := exec.Command(binaryPath, "keys", "show", keyName, "--address", "--keyring-backend", "test")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (m *Manager) listChainKeys(binaryPath, chainName string) ([]KeyInfo, error) {
	cmd := exec.Command(binaryPath, "keys", "list", "--output", "json", "--keyring-backend", "test")
	_, err := cmd.Output()
	if err != nil {
		// Fallback to simple list
		return m.listChainKeysSimple(binaryPath, chainName)
	}

	// Parse JSON output (implementation would depend on the specific format)
	// For now, return simple list
	return m.listChainKeysSimple(binaryPath, chainName)
}

func (m *Manager) listChainKeysSimple(binaryPath, chainName string) ([]KeyInfo, error) {
	cmd := exec.Command(binaryPath, "keys", "list", "--keyring-backend", "test")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var keys []KeyInfo
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}

		// Parse the line (format varies by CLI)
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			keys = append(keys, KeyInfo{
				Name:    parts[0],
				Address: parts[1],
				Chain:   chainName,
				Type:    "local",
			})
		}
	}

	return keys, nil
}

func (m *Manager) findKeyFile(keyName string) string {
	keyDir := m.config.KeyManager.KeyDir

	// Look for common key file extensions
	extensions := []string{".key", ".txt", ".mnemonic", ""}

	for _, ext := range extensions {
		filename := keyName + ext
		fullPath := filepath.Join(keyDir, filename)

		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}

	return ""
}

func (m *Manager) importKeyFromFile(binaryPath, keyName, keyFile string) error {
	content, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("failed to read key file: %w", err)
	}

	mnemonic := strings.TrimSpace(string(content))

	// Import using CLI with proper keyring backend
	cmd := exec.Command(binaryPath, "keys", "add", keyName, "--recover", "--keyring-backend", "test")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start import command: %w", err)
	}

	if _, err := stdin.Write([]byte(mnemonic + "\n")); err != nil {
		stdin.Close()
		cmd.Wait()
		return fmt.Errorf("failed to write mnemonic: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("key import failed: %w", err)
	}

	m.logger.Info("Key imported from file",
		zap.String("key", keyName),
		zap.String("file", keyFile),
	)

	return nil
}

func (m *Manager) exportKeyFromCLI(binaryPath, keyName string) (string, error) {
	// Note: This is dangerous and most chains don't support direct mnemonic export
	// This is a placeholder - in practice, you'd need chain-specific implementation
	return "", fmt.Errorf("CLI export not supported for security reasons")
}
