package rotation

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/internal/observability"
	"github.com/wbh1/latr/pkg/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// LinodeClient defines the interface for Linode API operations
type LinodeClient interface {
	CreateToken(ctx context.Context, label, scopes string, expiry time.Time) (*models.Token, error)
	FindTokenByLabel(ctx context.Context, label string) (*models.Token, error)
	RevokeToken(ctx context.Context, tokenID int) error
	ListTokens(ctx context.Context) ([]*models.Token, error)
}

// VaultClient defines the interface for Vault operations
type VaultClient interface {
	WriteToken(ctx context.Context, path, token string) error
	ReadToken(ctx context.Context, path string) (string, error)
	WriteTokenState(ctx context.Context, path string, state *models.TokenState) error
	ReadTokenState(ctx context.Context, path string) (*models.TokenState, error)
}

// Engine handles token rotation logic
type Engine struct {
	linodeClient LinodeClient
	vaultClient  VaultClient
	dryRun       bool
}

// NewEngine creates a new rotation engine
func NewEngine(linodeClient LinodeClient, vaultClient VaultClient, dryRun bool) *Engine {
	return &Engine{
		linodeClient: linodeClient,
		vaultClient:  vaultClient,
		dryRun:       dryRun,
	}
}

// ProcessToken processes a single token configuration
func (e *Engine) ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error {
	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "ProcessToken")
	defer span.End()

	span.SetAttributes(
		attribute.String("token.label", tokenConfig.Label),
		attribute.String("token.team", tokenConfig.Team),
	)

	log.Printf("Processing token: %s", tokenConfig.Label)

	// Parse validity duration
	validity, err := config.ParseValidityDuration(tokenConfig.Validity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid validity")
		return fmt.Errorf("invalid validity for token %s: %w", tokenConfig.Label, err)
	}

	// Check if token exists in Linode
	existingToken, err := e.linodeClient.FindTokenByLabel(ctx, tokenConfig.Label)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to find token")
		return fmt.Errorf("failed to find token %s: %w", tokenConfig.Label, err)
	}

	if existingToken == nil {
		// Token doesn't exist, create it
		return e.createNewToken(ctx, tokenConfig, validity)
	}

	// Token exists, check if it needs rotation
	existingToken.Validity = validity

	// Record token validity remaining metric
	validityRemaining := time.Until(existingToken.ExpiresAt).Seconds()
	observability.RecordTokenValidityRemaining(ctx, tokenConfig.Label, validityRemaining)
	span.SetAttributes(attribute.Float64("token.validity_remaining_seconds", validityRemaining))

	if existingToken.NeedsRotation(thresholdPercent) {
		log.Printf("Token %s needs rotation (%0.2f%% validity remaining)", tokenConfig.Label, existingToken.PercentValidityRemaining())
		return e.rotateToken(ctx, tokenConfig, existingToken, validity)
	}

	log.Printf("Token %s does not need rotation (%0.2f%% validity remaining)", tokenConfig.Label, existingToken.PercentValidityRemaining())
	span.SetStatus(codes.Ok, "no rotation needed")
	return nil
}

// createNewToken creates a new token that doesn't exist yet
func (e *Engine) createNewToken(ctx context.Context, tokenConfig config.TokenConfig, validity time.Duration) error {
	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "CreateNewToken")
	defer span.End()

	span.SetAttributes(attribute.String("token.label", tokenConfig.Label))

	log.Printf("Creating new token: %s", tokenConfig.Label)
	startTime := time.Now()

	if e.dryRun {
		log.Printf("[DRY RUN] Would create new token: %s", tokenConfig.Label)
		span.SetStatus(codes.Ok, "dry run")
		return nil
	}

	// Read existing state (if any)
	storagePath := tokenConfig.Storage[0].Path
	existingState, err := e.vaultClient.ReadTokenState(ctx, storagePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to read token state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		return fmt.Errorf("failed to read token state: %w", err)
	}

	// Calculate expiry
	expiry := time.Now().Add(validity)

	// Create token in Linode
	newToken, err := e.linodeClient.CreateToken(ctx, tokenConfig.Label, tokenConfig.Scopes, expiry)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to create token %s in Linode: %w", tokenConfig.Label, err)
	}

	log.Printf("Created token %s with ID %d, expires at %s", tokenConfig.Label, newToken.ID, newToken.ExpiresAt.Format(time.RFC3339))

	// Store token in all configured storage backends
	if err := e.storeTokenInBackends(ctx, tokenConfig.Storage, newToken.Token); err != nil {
		// Track state even if storage fails, so we can retry on next run
		_ = e.updateState(ctx, storagePath, newToken, existingState, expiry)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		observability.RecordVaultStorageError(ctx, storagePath)
		return fmt.Errorf("failed to store token in vault: %w", err)
	}

	// Update state
	if err := e.updateState(ctx, storagePath, newToken, existingState, expiry); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to update state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to update token state: %w", err)
	}

	// Record successful rotation
	span.SetStatus(codes.Ok, "token created successfully")
	observability.RecordRotation(ctx, tokenConfig.Label, true)
	observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))

	return nil
}

// rotateToken rotates an existing token
func (e *Engine) rotateToken(ctx context.Context, tokenConfig config.TokenConfig, existingToken *models.Token, validity time.Duration) error {
	// Start tracing span
	tracer := observability.GetTracer()
	ctx, span := tracer.Start(ctx, "RotateToken")
	defer span.End()

	span.SetAttributes(
		attribute.String("token.label", tokenConfig.Label),
		attribute.Int("token.existing_id", existingToken.ID),
	)

	log.Printf("Rotating token: %s", tokenConfig.Label)
	startTime := time.Now()

	if e.dryRun {
		log.Printf("[DRY RUN] Would rotate token: %s", tokenConfig.Label)
		span.SetStatus(codes.Ok, "dry run")
		return nil
	}

	// Read existing state
	storagePath := tokenConfig.Storage[0].Path
	existingState, err := e.vaultClient.ReadTokenState(ctx, storagePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to read token state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		return fmt.Errorf("failed to read token state: %w", err)
	}

	// Calculate new expiry
	newExpiry := time.Now().Add(validity)

	// Create new token in Linode
	newToken, err := e.linodeClient.CreateToken(ctx, tokenConfig.Label, tokenConfig.Scopes, newExpiry)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to create token %s in Linode: %w", tokenConfig.Label, err)
	}

	span.SetAttributes(attribute.Int("token.new_id", newToken.ID))

	log.Printf("Created new token %s with ID %d, expires at %s", tokenConfig.Label, newToken.ID, newToken.ExpiresAt.Format(time.RFC3339))
	log.Printf("Previous token ID %d will expire at %s", existingToken.ID, existingToken.ExpiresAt.Format(time.RFC3339))

	// Store new token in all configured storage backends
	if err := e.storeTokenInBackends(ctx, tokenConfig.Storage, newToken.Token); err != nil {
		// Track state even if storage fails
		_ = e.updateStateAfterRotation(ctx, storagePath, newToken, existingToken, existingState)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to store token")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		observability.RecordVaultStorageError(ctx, storagePath)
		return fmt.Errorf("failed to store token in vault: %w", err)
	}

	// Update state with previous token info
	if err := e.updateStateAfterRotation(ctx, storagePath, newToken, existingToken, existingState); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to update state")
		observability.RecordRotation(ctx, tokenConfig.Label, false)
		observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))
		return fmt.Errorf("failed to update token state: %w", err)
	}

	// Record successful rotation
	span.SetStatus(codes.Ok, "token rotated successfully")
	observability.RecordRotation(ctx, tokenConfig.Label, true)
	observability.RecordRotationDuration(ctx, tokenConfig.Label, time.Since(startTime))

	return nil
}

// storeTokenInBackends stores the token in all configured storage backends
func (e *Engine) storeTokenInBackends(ctx context.Context, storageConfigs []config.StorageConfig, token string) error {
	for _, storage := range storageConfigs {
		if storage.Type == "vault" {
			if err := e.vaultClient.WriteToken(ctx, storage.Path, token); err != nil {
				return err
			}
			log.Printf("Stored token in Vault at path: %s", storage.Path)
		}
	}
	return nil
}

// updateState updates the token state after creation
func (e *Engine) updateState(ctx context.Context, path string, newToken *models.Token, existingState *models.TokenState, expiry time.Time) error {
	rotationCount := 0
	if existingState != nil {
		rotationCount = existingState.RotationCount
	}

	state := &models.TokenState{
		Label:              newToken.Label,
		CurrentLinodeID:    newToken.ID,
		CurrentTokenValue:  newToken.Token,
		LastRotatedAt:      time.Now(),
		PreviousLinodeID:   0,
		PreviousExpiresAt:  time.Time{},
		RotationCount:      rotationCount,
	}

	return e.vaultClient.WriteTokenState(ctx, path, state)
}

// updateStateAfterRotation updates the token state after rotation
func (e *Engine) updateStateAfterRotation(ctx context.Context, path string, newToken, oldToken *models.Token, existingState *models.TokenState) error {
	rotationCount := 0
	if existingState != nil {
		rotationCount = existingState.RotationCount
	}

	state := &models.TokenState{
		Label:              newToken.Label,
		CurrentLinodeID:    newToken.ID,
		CurrentTokenValue:  newToken.Token,
		LastRotatedAt:      time.Now(),
		PreviousLinodeID:   oldToken.ID,
		PreviousExpiresAt:  oldToken.ExpiresAt,
		RotationCount:      rotationCount + 1,
	}

	return e.vaultClient.WriteTokenState(ctx, path, state)
}

// PruneExpiredTokens deletes expired tokens that are managed by this tool
func (e *Engine) PruneExpiredTokens(ctx context.Context, managedLabels []string) error {
	log.Printf("Pruning expired tokens...")

	// Get all tokens
	allTokens, err := e.linodeClient.ListTokens(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tokens: %w", err)
	}

	if e.dryRun {
		log.Printf("[DRY RUN] Would prune expired tokens")
		return nil
	}

	// Create a map of managed labels for fast lookup
	managedMap := make(map[string]bool)
	for _, label := range managedLabels {
		managedMap[label] = true
	}

	// Find and delete expired managed tokens
	for _, token := range allTokens {
		if !managedMap[token.Label] {
			continue // Skip unmanaged tokens
		}

		if token.IsExpired() {
			log.Printf("Pruning expired token %s (ID: %d)", token.Label, token.ID)
			if e.dryRun {
				log.Printf("[DRY RUN] Would revoke token ID %d", token.ID)
			} else {
				if err := e.linodeClient.RevokeToken(ctx, token.ID); err != nil {
					log.Printf("Failed to revoke token %s (ID: %d): %v", token.Label, token.ID, err)
					// Continue with other tokens
				} else {
					log.Printf("Revoked expired token %s (ID: %d)", token.Label, token.ID)
				}
			}
		}
	}

	return nil
}
