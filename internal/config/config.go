package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/viper"
)

// Config represents the complete configuration for the token rotator
type Config struct {
	Daemon        DaemonConfig        `yaml:"daemon" mapstructure:"daemon"`
	Rotation      RotationConfig      `yaml:"rotation" mapstructure:"rotation"`
	Vault         VaultConfig         `yaml:"vault" mapstructure:"vault"`
	Observability ObservabilityConfig `yaml:"observability" mapstructure:"observability"`
	Tokens        []TokenConfig       `yaml:"tokens" mapstructure:"tokens"`
}

// DaemonConfig contains settings for daemon behavior
type DaemonConfig struct {
	Mode          string `yaml:"mode" mapstructure:"mode"`
	CheckInterval string `yaml:"check_interval" mapstructure:"check_interval"`
	DryRun        bool   `yaml:"dry_run" mapstructure:"dry_run"`
}

// RotationConfig contains settings for token rotation
type RotationConfig struct {
	ThresholdPercent int  `yaml:"threshold_percent" mapstructure:"threshold_percent"`
	PruneExpired     bool `yaml:"prune_expired" mapstructure:"prune_expired"`
}

// VaultConfig contains Vault connection and authentication settings
type VaultConfig struct {
	Address   string `yaml:"address" mapstructure:"address"`
	RoleID    string `yaml:"role_id" mapstructure:"role_id"`
	SecretID  string `yaml:"secret_id" mapstructure:"secret_id"`
	MountPath string `yaml:"mount_path" mapstructure:"mount_path"`
}

// ObservabilityConfig contains settings for telemetry and logging
type ObservabilityConfig struct {
	OTelEndpoint string `yaml:"otel_endpoint" mapstructure:"otel_endpoint"`
	LogLevel     string `yaml:"log_level" mapstructure:"log_level"`
}

// TokenConfig represents a single token to manage
type TokenConfig struct {
	Label             string          `yaml:"label" mapstructure:"label"`
	Team              string          `yaml:"team" mapstructure:"team"`
	Validity          string          `yaml:"validity" mapstructure:"validity"`
	Scopes            string          `yaml:"scopes" mapstructure:"scopes"`
	RotationThreshold int             `yaml:"rotation_threshold" mapstructure:"rotation_threshold"`
	Storage           []StorageConfig `yaml:"storage" mapstructure:"storage"`
}

// StorageConfig represents where to store the rotated token
type StorageConfig struct {
	Type string `yaml:"type" mapstructure:"type"`
	Path string `yaml:"path" mapstructure:"path"`
}

// Parse parses YAML configuration data into a Config struct
// Environment variables in the format ${VAR_NAME} or $VAR_NAME are automatically expanded
func Parse(data []byte) (*Config, error) {
	// Expand environment variables in the YAML content
	expandedData := []byte(os.ExpandEnv(string(data)))

	v := viper.New()
	v.SetConfigType("yaml")

	// Read config from byte slice
	if err := v.ReadConfig(bytes.NewReader(expandedData)); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// ApplyDefaults sets default values for optional configuration fields
func (c *Config) ApplyDefaults() {
	if c.Daemon.Mode == "" {
		c.Daemon.Mode = "daemon"
	}
	if c.Daemon.CheckInterval == "" {
		c.Daemon.CheckInterval = "30m"
	}
	if c.Rotation.ThresholdPercent == 0 {
		c.Rotation.ThresholdPercent = 10
	}
	if c.Vault.MountPath == "" {
		c.Vault.MountPath = "secret"
	}
	if c.Observability.LogLevel == "" {
		c.Observability.LogLevel = "info"
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Validate Vault config
	if c.Vault.Address == "" {
		return fmt.Errorf("vault address is required")
	}
	if c.Vault.RoleID == "" {
		return fmt.Errorf("vault role_id is required")
	}
	if c.Vault.SecretID == "" {
		return fmt.Errorf("vault secret_id is required")
	}

	// Validate tokens
	if len(c.Tokens) == 0 {
		return fmt.Errorf("at least one token must be configured")
	}

	for i, token := range c.Tokens {
		if err := c.validateToken(&token, i); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) validateToken(token *TokenConfig, index int) error {
	if token.Label == "" {
		return fmt.Errorf("token[%d]: token label is required", index)
	}
	if token.Validity == "" {
		return fmt.Errorf("token[%d]: token validity is required", index)
	}
	if token.Scopes == "" {
		return fmt.Errorf("token[%d]: token scopes is required", index)
	}
	if len(token.Storage) == 0 {
		return fmt.Errorf("token[%d]: at least one storage backend is required", index)
	}

	// Validate validity period
	duration, err := ParseValidityDuration(token.Validity)
	if err != nil {
		return fmt.Errorf("token[%d]: invalid validity period: %w", index, err)
	}

	// Check that validity is <= 6 months (180 days)
	maxValidity := 180 * 24 * time.Hour
	if duration > maxValidity {
		return fmt.Errorf("token[%d]: validity period must be <= 6 months (180d), got %s", index, token.Validity)
	}

	return nil
}

// ParseValidityDuration parses a validity string (e.g., "90d", "6mo") into a time.Duration
func ParseValidityDuration(validity string) (time.Duration, error) {
	// Support formats: 90d, 6mo, 1h, 30m
	re := regexp.MustCompile(`^(\d+)(mo|d|h|m)$`)
	matches := re.FindStringSubmatch(validity)
	if matches == nil {
		return 0, fmt.Errorf("invalid validity format: %s (expected format: <number><unit>, e.g., 90d, 6mo)", validity)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value in validity: %s", validity)
	}

	unit := matches[2]
	switch unit {
	case "mo":
		// Treat 1 month as 30 days
		return time.Duration(value) * 30 * 24 * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	default:
		return 0, fmt.Errorf("unsupported time unit: %s", unit)
	}
}
