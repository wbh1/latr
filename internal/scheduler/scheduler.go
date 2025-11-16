package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/wbh1/latr/internal/config"
)

// Engine defines the interface for the rotation engine
type Engine interface {
	ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error
	PruneExpiredTokens(ctx context.Context, managedLabels []string) error
}

// Scheduler manages the execution schedule for token rotation
type Scheduler struct {
	config *config.Config
	engine Engine
}

// NewScheduler creates a new scheduler
func NewScheduler(cfg *config.Config, engine Engine) *Scheduler {
	return &Scheduler{
		config: cfg,
		engine: engine,
	}
}

// Run starts the scheduler based on the configured mode
func (s *Scheduler) Run(ctx context.Context) error {
	if s.config.Daemon.Mode == "one-shot" {
		return s.runOnce(ctx)
	}
	return s.runDaemon(ctx)
}

// runOnce executes a single rotation cycle
func (s *Scheduler) runOnce(ctx context.Context) error {
	log.Printf("Running in one-shot mode")
	return s.executeCycle(ctx)
}

// runDaemon runs the rotation cycle at regular intervals
func (s *Scheduler) runDaemon(ctx context.Context) error {
	log.Printf("Running in daemon mode with interval: %s", s.config.Daemon.CheckInterval)

	interval, err := time.ParseDuration(s.config.Daemon.CheckInterval)
	if err != nil {
		return fmt.Errorf("invalid check interval: %w", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	if err := s.executeCycle(ctx); err != nil {
		log.Printf("Error in rotation cycle: %v", err)
	}

	// Then run at intervals
	for {
		select {
		case <-ctx.Done():
			log.Printf("Shutting down scheduler: %v", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			if err := s.executeCycle(ctx); err != nil {
				log.Printf("Error in rotation cycle: %v", err)
				// Continue running even if there's an error
			}
		}
	}
}

// executeCycle processes all configured tokens
func (s *Scheduler) executeCycle(ctx context.Context) error {
	log.Printf("Starting rotation cycle for %d token(s)", len(s.config.Tokens))

	if len(s.config.Tokens) == 0 {
		log.Printf("No tokens configured")
		return nil
	}

	// Process each token
	for _, tokenConfig := range s.config.Tokens {
		// Determine threshold (use token-specific if set, otherwise global)
		threshold := s.config.Rotation.ThresholdPercent
		if tokenConfig.RotationThreshold > 0 {
			threshold = tokenConfig.RotationThreshold
		}

		if err := s.engine.ProcessToken(ctx, tokenConfig, threshold); err != nil {
			log.Printf("Failed to process token %s: %v", tokenConfig.Label, err)
			// Continue processing other tokens
		}
	}

	// Prune expired tokens if configured
	if s.config.Rotation.PruneExpired {
		managedLabels := make([]string, len(s.config.Tokens))
		for i, token := range s.config.Tokens {
			managedLabels[i] = token.Label
		}

		if err := s.engine.PruneExpiredTokens(ctx, managedLabels); err != nil {
			log.Printf("Failed to prune expired tokens: %v", err)
		}
	}

	log.Printf("Rotation cycle completed")
	return nil
}
