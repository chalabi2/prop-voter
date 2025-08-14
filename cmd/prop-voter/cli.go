package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/binmgr"
	"prop-voter/internal/keymgr"
	"prop-voter/internal/models"
	"prop-voter/internal/registry"
	"prop-voter/internal/wallet"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// CLI commands for key and binary management

func handleKeyCommand(args []string, cfg *config.Config, logger *zap.Logger) error {
	if len(args) < 1 {
		return fmt.Errorf("key command requires a subcommand (list, import, export, backup, validate)")
	}

	// Initialize database and wallet manager
	db, err := gorm.Open(sqlite.Open(cfg.Database.Path), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := models.InitDB(db); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	walletManager, err := wallet.NewManager(db, cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create wallet manager: %w", err)
	}

	keyManager := keymgr.NewManager(cfg, logger, walletManager)

	switch args[0] {
	case "list":
		return handleKeyList(keyManager)
	case "import":
		return handleKeyImport(args[1:], keyManager)
	case "export":
		return handleKeyExport(args[1:], keyManager)
	case "backup":
		return handleKeyBackup(args[1:], keyManager)
	case "validate":
		return handleKeyValidate(keyManager)
	default:
		return fmt.Errorf("unknown key command: %s", args[0])
	}
}

func handleBinaryCommand(args []string, cfg *config.Config, logger *zap.Logger) error {
	if len(args) < 1 {
		return fmt.Errorf("binary command requires a subcommand (list, update, check)")
	}

	// Initialize registry manager for Chain Registry support
	registryManager := registry.NewManager(logger)
	binManager := binmgr.NewManager(cfg, logger, registryManager)

	switch args[0] {
	case "list":
		return handleBinaryList(binManager)
	case "update":
		return handleBinaryUpdate(args[1:], binManager)
	case "check":
		return handleBinaryCheck(binManager)
	default:
		return fmt.Errorf("unknown binary command: %s", args[0])
	}
}

func handleRegistryCommand(args []string, registryManager *registry.Manager, logger *zap.Logger) error {
	if len(args) < 1 {
		return fmt.Errorf("registry command requires a subcommand (list, info, clear-cache)")
	}

	switch args[0] {
	case "list":
		return handleRegistryList(registryManager)
	case "info":
		return handleRegistryInfo(args[1:], registryManager, logger)
	case "clear-cache":
		return handleRegistryClearCache(registryManager)
	default:
		return fmt.Errorf("unknown registry command: %s", args[0])
	}
}

func handleKeyList(keyManager *keymgr.Manager) error {
	keys, err := keyManager.ListKeys()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	if len(keys) == 0 {
		fmt.Println("No keys found")
		return nil
	}

	fmt.Printf("%-20s %-15s %-50s %-10s\n", "NAME", "CHAIN", "ADDRESS", "TYPE")
	fmt.Println(strings.Repeat("-", 100))

	for _, key := range keys {
		fmt.Printf("%-20s %-15s %-50s %-10s\n", key.Name, key.Chain, key.Address, key.Type)
	}

	return nil
}

func handleKeyImport(args []string, keyManager *keymgr.Manager) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: key import <chain> <key-name> [mnemonic-file]")
	}

	chain := args[0]
	keyName := args[1]

	if len(args) >= 3 {
		// Import from file
		filePath := args[2]
		return keyManager.ImportKeyFromFile(chain, keyName, filePath)
	}

	// Interactive import
	fmt.Print("Enter mnemonic phrase: ")
	var mnemonic string
	fmt.Scanln(&mnemonic)

	return keyManager.ImportKey(chain, keyName, mnemonic)
}

func handleKeyExport(args []string, keyManager *keymgr.Manager) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: key export <chain> <key-name> [output-file]")
	}

	chain := args[0]
	keyName := args[1]

	outputFile := fmt.Sprintf("%s-%s.key", chain, keyName)
	if len(args) >= 3 {
		outputFile = args[2]
	}

	fmt.Printf("WARNING: This will export private key material to %s\n", outputFile)
	fmt.Print("Are you sure? (yes/no): ")
	var confirm string
	fmt.Scanln(&confirm)

	if confirm != "yes" {
		fmt.Println("Export cancelled")
		return nil
	}

	return keyManager.ExportKey(chain, keyName, outputFile)
}

func handleKeyBackup(args []string, keyManager *keymgr.Manager) error {
	backupDir := "./key-backups"
	if len(args) >= 1 {
		backupDir = args[0]
	}

	fmt.Printf("Creating backup in %s\n", backupDir)
	return keyManager.BackupKeys(backupDir)
}

func handleKeyValidate(keyManager *keymgr.Manager) error {
	err := keyManager.ValidateKeys()
	if err != nil {
		fmt.Printf("❌ Key validation failed: %v\n", err)
		return err
	}

	fmt.Println("✅ All required keys are present")
	return nil
}

func handleBinaryList(binManager *binmgr.Manager) error {
	binaries, err := binManager.GetManagedBinaries()
	if err != nil {
		return fmt.Errorf("failed to list binaries: %w", err)
	}

	if len(binaries) == 0 {
		fmt.Println("No managed binaries found")
		return nil
	}

	fmt.Printf("%-15s %-15s %-60s %-20s\n", "NAME", "VERSION", "PATH", "LAST UPDATED")
	fmt.Println(strings.Repeat("-", 120))

	for _, binary := range binaries {
		lastUpdated := "unknown"
		if !binary.LastUpdated.IsZero() {
			lastUpdated = binary.LastUpdated.Format("2006-01-02 15:04")
		}

		fmt.Printf("%-15s %-15s %-60s %-20s\n",
			binary.Name,
			binary.Version,
			binary.Path,
			lastUpdated,
		)
	}

	return nil
}

func handleBinaryUpdate(args []string, binManager *binmgr.Manager) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: binary update <chain-name>")
	}

	chainName := args[0]

	fmt.Printf("Updating binary for %s...\n", chainName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := binManager.UpdateBinary(ctx, chainName); err != nil {
		return fmt.Errorf("failed to update binary: %w", err)
	}

	fmt.Printf("✅ Binary for %s updated successfully\n", chainName)
	return nil
}

func handleBinaryCheck(binManager *binmgr.Manager) error {
	fmt.Println("Checking for binary updates...")

	// This would normally be handled by the main service
	// For CLI, we just show current status
	binaries, err := binManager.GetManagedBinaries()
	if err != nil {
		return fmt.Errorf("failed to get binary status: %w", err)
	}

	for _, binary := range binaries {
		exists := ""
		if _, err := os.Stat(binary.Path); err == nil {
			exists = "✅"
		} else {
			exists = "❌"
		}

		fmt.Printf("%s %s (version: %s)\n", exists, binary.Name, binary.Version)
	}

	return nil
}

// Registry command handlers

func handleRegistryList(registryManager *registry.Manager) error {
	chains := registryManager.ListSupportedChains()

	fmt.Printf("Supported Chain Registry chains (%d total):\n\n", len(chains))

	// Group chains for better display
	const perColumn = 4
	for i := 0; i < len(chains); i += perColumn {
		for j := 0; j < perColumn && i+j < len(chains); j++ {
			fmt.Printf("%-20s", chains[i+j])
		}
		fmt.Println()
	}

	fmt.Println("\nUsage in config.yaml:")
	fmt.Println("chains:")
	fmt.Println("  - chain_name: \"osmosis\"")
	fmt.Println("    rpc: \"https://rpc-osmosis.example.com\"")
	fmt.Println("    rest: \"https://rest-osmosis.example.com\"")
	fmt.Println("    wallet_key: \"my-osmosis-key\"")
	fmt.Println("    # All other fields auto-discovered!")

	return nil
}

func handleRegistryInfo(args []string, registryManager *registry.Manager, logger *zap.Logger) error {
	if len(args) < 1 {
		return fmt.Errorf("info command requires a chain name")
	}

	chainName := args[0]
	client := registry.NewClient(logger)

	ctx := context.Background()
	chainInfo, err := client.GetChainInfo(ctx, chainName)
	if err != nil {
		return fmt.Errorf("failed to fetch chain info for %s: %w", chainName, err)
	}

	fmt.Printf("Chain Registry Information for '%s':\n\n", chainName)
	fmt.Printf("  Pretty Name:     %s\n", chainInfo.PrettyName)
	fmt.Printf("  Chain ID:        %s\n", chainInfo.ChainID)
	fmt.Printf("  Bech32 Prefix:   %s\n", chainInfo.Bech32Prefix)
	fmt.Printf("  Daemon Name:     %s\n", chainInfo.DaemonName)
	fmt.Printf("  Staking Denom:   %s\n", chainInfo.Denom)
	fmt.Printf("  Version:         %s\n", chainInfo.Version)
	fmt.Printf("  Git Repository:  %s\n", chainInfo.GitRepo)

	if chainInfo.LogoURL != "" {
		fmt.Printf("  Logo URL:        %s\n", chainInfo.LogoURL)
	}

	if chainInfo.BinaryURL != "" {
		fmt.Printf("  Binary URL:      %s\n", chainInfo.BinaryURL)

		// Show binary info if available
		binaryInfo, err := client.GetBinaryInfo(chainInfo)
		if err == nil {
			fmt.Printf("\n  Binary Information:\n")
			fmt.Printf("    Owner:         %s\n", binaryInfo.Owner)
			fmt.Printf("    Repository:    %s\n", binaryInfo.Repo)
			fmt.Printf("    Filename:      %s\n", binaryInfo.FileName)
		}
	} else {
		fmt.Printf("  Binary URL:      Not available for current platform (%s/%s)\n",
			"linux", "amd64") // Could detect runtime.GOOS/GOARCH
	}

	fmt.Printf("\nExample config.yaml entry:\n")
	fmt.Printf("- chain_name: \"%s\"\n", chainName)
	fmt.Printf("  rpc: \"https://rpc-%s.example.com\"\n", chainName)
	fmt.Printf("  rest: \"https://rest-%s.example.com\"\n", chainName)
	fmt.Printf("  wallet_key: \"my-%s-key\"\n", chainName)

	return nil
}

func handleRegistryClearCache(registryManager *registry.Manager) error {
	registryManager.ClearCache()
	fmt.Println("Chain Registry cache cleared successfully.")
	return nil
}
