package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfigFileWithEnvVars verifies that environment variables are properly
// expanded when loading a config file, simulating the e2e test scenario
func TestLoadConfigFileWithEnvVars(t *testing.T) {
	// Create a temporary config file with env var references
	// This simulates what the e2e tests do
	configContent := `daemon:
  mode: "one-shot"
  dry_run: true

rotation:
  threshold_percent: 10
  prune_expired: false

vault:
  address: "http://localhost:8200"
  role_id: "${VAULT_ROLE_ID}"
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "secret"

observability:
  log_level: "info"

tokens:
  - label: "e2e-test-dryrun"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "e2e/test-dryrun"
`

	// Write config file to tmp
	tmpFile := filepath.Join(os.TempDir(), "test-latr-config.yaml")
	err := os.WriteFile(tmpFile, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Set environment variables (simulating what runLatr() does in e2e tests)
	os.Setenv("VAULT_ROLE_ID", "test-role-id-123")
	os.Setenv("VAULT_SECRET_ID", "test-secret-id-456")
	defer func() {
		os.Unsetenv("VAULT_ROLE_ID")
		os.Unsetenv("VAULT_SECRET_ID")
	}()

	// Load the config file (this calls os.Expand internally)
	cfg, err := Load(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify environment variables were expanded correctly
	assert.Equal(t, "test-role-id-123", cfg.Vault.RoleID, "VAULT_ROLE_ID should be expanded")
	assert.Equal(t, "test-secret-id-456", cfg.Vault.SecretID, "VAULT_SECRET_ID should be expanded")
	assert.Equal(t, "http://localhost:8200", cfg.Vault.Address)
	assert.Equal(t, "one-shot", cfg.Daemon.Mode)
	assert.True(t, cfg.Daemon.DryRun)
}

// TestLoadConfigFileWithComplexEnvVarValues verifies that env vars with special
// characters are handled correctly
func TestLoadConfigFileWithComplexEnvVarValues(t *testing.T) {
	configContent := `vault:
  address: "http://localhost:8200"
  role_id: "${VAULT_ROLE_ID}"
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "secret"

tokens:
  - label: "test-token"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "test/path"
`

	tmpFile := filepath.Join(os.TempDir(), "test-complex-config.yaml")
	err := os.WriteFile(tmpFile, []byte(configContent), 0644)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	// Test with values that contain hyphens, underscores, and numbers
	testCases := []struct {
		name      string
		roleID    string
		secretID  string
	}{
		{
			name:     "Simple alphanumeric",
			roleID:   "abc123",
			secretID: "xyz789",
		},
		{
			name:     "With hyphens",
			roleID:   "role-id-with-hyphens",
			secretID: "secret-id-with-hyphens",
		},
		{
			name:     "With underscores",
			roleID:   "role_id_with_underscores",
			secretID: "secret_id_with_underscores",
		},
		{
			name:     "UUID-like values",
			roleID:   "550e8400-e29b-41d4-a716-446655440000",
			secretID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("VAULT_ROLE_ID", tc.roleID)
			os.Setenv("VAULT_SECRET_ID", tc.secretID)
			defer func() {
				os.Unsetenv("VAULT_ROLE_ID")
				os.Unsetenv("VAULT_SECRET_ID")
			}()

			cfg, err := Load(tmpFile)
			require.NoError(t, err)
			require.NotNil(t, cfg)

			assert.Equal(t, tc.roleID, cfg.Vault.RoleID)
			assert.Equal(t, tc.secretID, cfg.Vault.SecretID)
		})
	}
}
