package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"prop-voter/config"
	"prop-voter/internal/binmgr"
	"prop-voter/internal/discord"
	"prop-voter/internal/health"
	"prop-voter/internal/keymgr"
	"prop-voter/internal/models"
	"prop-voter/internal/registry"
	"prop-voter/internal/scanner"
	"prop-voter/internal/voting"
	"prop-voter/internal/wallet"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func main() {
	var (
		configPath  = flag.String("config", "config.yaml", "Path to configuration file")
		validate    = flag.Bool("validate", false, "Validate configuration and chains then exit")
		debug       = flag.Bool("debug", false, "Enable debug logging")
		keyCmd      = flag.String("key", "", "Key management command (list, import, export, backup, validate)")
		binaryCmd   = flag.String("binary", "", "Binary management command (list, update, check)")
		registryCmd = flag.String("registry", "", "Chain Registry command (list, info, clear-cache)")
	)
	flag.Parse()

	args := flag.Args()

	// Initialize logger
	var logger *zap.Logger
	var err error

	if *debug {
		logger, err = zap.NewDevelopment()
	} else {
		logger, err = zap.NewProduction()
	}

	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting Prop-Voter", zap.String("config", *configPath))

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	logger.Info("Configuration loaded successfully",
		zap.Int("chains", len(cfg.Chains)),
		zap.Duration("scan_interval", cfg.Scanning.Interval),
	)

	// Initialize Chain Registry manager
	registryManager := registry.NewManager(logger)

	// Populate Chain Registry information for chains that use it
	ctx := context.Background()
	if err := registryManager.PopulateChainConfigs(ctx, cfg.Chains); err != nil {
		logger.Fatal("Failed to populate chain configurations from Chain Registry", zap.Error(err))
	}

	logger.Info("Chain Registry integration completed")

	// Handle CLI commands
	if *keyCmd != "" {
		cmdArgs := append([]string{*keyCmd}, args...)
		if err := handleKeyCommand(cmdArgs, cfg, logger); err != nil {
			logger.Fatal("Key command failed", zap.Error(err))
		}
		return
	}

	if *binaryCmd != "" {
		cmdArgs := append([]string{*binaryCmd}, args...)
		if err := handleBinaryCommand(cmdArgs, cfg, logger); err != nil {
			logger.Fatal("Binary command failed", zap.Error(err))
		}
		return
	}

	if *registryCmd != "" {
		cmdArgs := append([]string{*registryCmd}, args...)
		if err := handleRegistryCommand(cmdArgs, registryManager, logger); err != nil {
			logger.Fatal("Registry command failed", zap.Error(err))
		}
		return
	}

	// Initialize database
	db, err := initDatabase(cfg.Database.Path, logger)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Initialize wallet manager
	walletManager, err := wallet.NewManager(db, cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize wallet manager", zap.Error(err))
	}

	// Initialize binary manager with Chain Registry support
	binaryManager := binmgr.NewManager(cfg, logger, registryManager)

	// Initialize key manager
	keyManager := keymgr.NewManager(cfg, logger, walletManager)

	// Initialize voter (use local binaries if managed)
	voter := voting.NewVoter(cfg, logger)

	// Validate chains if requested
	if *validate {
		logger.Info("Validating chain configurations...")

		// Validate CLI tools
		if err := voter.ValidateAllChains(); err != nil {
			logger.Fatal("Chain validation failed", zap.Error(err))
		}

		// Validate keys
		if err := keyManager.ValidateKeys(); err != nil {
			logger.Warn("Key validation warning", zap.Error(err))
		}

		// Validate wallet manager
		for _, chain := range cfg.Chains {
			if err := walletManager.ValidateWalletExists(chain.GetChainID()); err != nil {
				logger.Warn("Wallet validation warning",
					zap.String("chain", chain.GetName()),
					zap.Error(err),
				)
			}
		}

		logger.Info("Validation completed successfully")
		return
	}

	// Initialize Discord bot
	bot, err := discord.NewBot(db, cfg, logger, voter)
	if err != nil {
		logger.Fatal("Failed to initialize Discord bot", zap.Error(err))
	}

	// Initialize proposal scanner
	proposalScanner := scanner.NewScanner(db, cfg, logger)

	// Initialize health server
	healthServer := health.NewServer(cfg, db, logger)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start services
	logger.Info("Starting services...")

	// Start health server
	if err := healthServer.Start(ctx); err != nil {
		logger.Fatal("Failed to start health server", zap.Error(err))
	}

	// Setup binaries first (synchronously)
	if err := binaryManager.SetupBinariesSync(ctx); err != nil {
		logger.Error("Failed to setup binaries", zap.Error(err))
	}

	// Setup keys after binaries are ready
	if err := keyManager.SetupKeys(ctx); err != nil {
		logger.Error("Failed to setup keys", zap.Error(err))
	}

	// Start binary manager background monitoring in a goroutine
	go func() {
		if err := binaryManager.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("Binary manager error", zap.Error(err))
		}
	}()

	// Start Discord bot
	if err := bot.Start(ctx); err != nil {
		logger.Fatal("Failed to start Discord bot", zap.Error(err))
	}
	defer bot.Stop()

	// Start proposal scanner in a goroutine
	go func() {
		if err := proposalScanner.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("Proposal scanner error", zap.Error(err))
		}
	}()

	logger.Info("All services started successfully")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	logger.Info("Shutdown signal received, stopping services...")

	cancel() // This will stop all services
	logger.Info("Prop-Voter stopped")
}

// initDatabase initializes the database connection and creates tables
func initDatabase(dbPath string, logger *zap.Logger) (*gorm.DB, error) {
	logger.Info("Initializing database", zap.String("path", dbPath))

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Initialize tables
	if err := models.InitDB(db); err != nil {
		return nil, fmt.Errorf("failed to initialize database tables: %w", err)
	}

	logger.Info("Database initialized successfully")
	return db, nil
}
