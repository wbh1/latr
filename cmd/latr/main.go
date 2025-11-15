package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/internal/linode"
	"github.com/wbh1/latr/internal/rotation"
	"github.com/wbh1/latr/internal/scheduler"
	"github.com/wbh1/latr/internal/vault"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Parse CLI flags
	configPath := flag.String("config", "", "Path to configuration file or glob pattern (required)")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("latr version %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	if *configPath == "" {
		log.Fatal("Error: -config flag is required")
	}

	// Load Linode API token from environment
	linodeToken := os.Getenv("LINODE_TOKEN")
	if linodeToken == "" {
		log.Fatal("Error: LINODE_TOKEN environment variable is required")
	}

	// Load and validate configuration
	log.Printf("Loading configuration from: %s", *configPath)
	cfg, err := config.LoadAndValidate(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("Mode: %s", cfg.Daemon.Mode)
	log.Printf("Configured tokens: %d", len(cfg.Tokens))
	log.Printf("Rotation threshold: %d%%", cfg.Rotation.ThresholdPercent)
	log.Printf("Prune expired: %v", cfg.Rotation.PruneExpired)
	log.Printf("Dry run: %v", cfg.Daemon.DryRun)

	// Create Linode client
	linodeClient := linode.NewClient(linodeToken)
	log.Printf("Linode client initialized")

	// Create Vault client
	vaultConfig := &vault.Config{
		Address:   cfg.Vault.Address,
		RoleID:    cfg.Vault.RoleID,
		SecretID:  cfg.Vault.SecretID,
		MountPath: cfg.Vault.MountPath,
	}

	vaultClient, err := vault.NewClient(vaultConfig)
	if err != nil {
		log.Fatalf("Failed to create Vault client: %v", err)
	}
	log.Printf("Vault client initialized and authenticated")

	// Create rotation engine
	engine := rotation.NewEngine(linodeClient, vaultClient, cfg.Daemon.DryRun)

	// Create scheduler
	sched := scheduler.NewScheduler(cfg, engine)

	// Set up context with signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		log.Printf("Initiating graceful shutdown...")
		cancel()
	}()

	// Run scheduler
	log.Printf("Starting latr...")
	if err := sched.Run(ctx); err != nil {
		if err == context.Canceled {
			log.Printf("Shutdown complete")
			os.Exit(0)
		}
		log.Fatalf("Scheduler error: %v", err)
	}

	log.Printf("latr finished successfully")
}
