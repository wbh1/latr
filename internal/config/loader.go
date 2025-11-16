package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Load reads and parses a single configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	return cfg, nil
}

// LoadGlob loads and merges multiple configuration files matching a glob pattern
func LoadGlob(pattern string) (*Config, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no config files found matching pattern: %s", pattern)
	}

	var merged *Config
	for _, path := range matches {
		cfg, err := Load(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file %s: %w", path, err)
		}

		if merged == nil {
			merged = cfg
		} else {
			merged = MergeConfigs(merged, cfg)
		}
	}

	return merged, nil
}

// MergeConfigs merges two configurations, with the second config overriding the first
// for non-empty values. Tokens are appended rather than replaced.
func MergeConfigs(base, override *Config) *Config {
	merged := &Config{}

	// Merge Daemon config
	merged.Daemon = base.Daemon
	if override.Daemon.Mode != "" {
		merged.Daemon.Mode = override.Daemon.Mode
	}
	if override.Daemon.CheckInterval != "" {
		merged.Daemon.CheckInterval = override.Daemon.CheckInterval
	}
	if override.Daemon.DryRun {
		merged.Daemon.DryRun = override.Daemon.DryRun
	}

	// Merge Rotation config
	merged.Rotation = base.Rotation
	if override.Rotation.ThresholdPercent != 0 {
		merged.Rotation.ThresholdPercent = override.Rotation.ThresholdPercent
	}
	if override.Rotation.PruneExpired {
		merged.Rotation.PruneExpired = override.Rotation.PruneExpired
	}

	// Merge Vault config
	merged.Vault = base.Vault
	if override.Vault.Address != "" {
		merged.Vault.Address = override.Vault.Address
	}
	if override.Vault.RoleID != "" {
		merged.Vault.RoleID = override.Vault.RoleID
	}
	if override.Vault.SecretID != "" {
		merged.Vault.SecretID = override.Vault.SecretID
	}
	if override.Vault.MountPath != "" {
		merged.Vault.MountPath = override.Vault.MountPath
	}

	// Merge Observability config
	merged.Observability = base.Observability
	if override.Observability.OTelEndpoint != "" {
		merged.Observability.OTelEndpoint = override.Observability.OTelEndpoint
	}
	if override.Observability.LogLevel != "" {
		merged.Observability.LogLevel = override.Observability.LogLevel
	}

	// Merge tokens (append, don't replace)
	merged.Tokens = append([]TokenConfig{}, base.Tokens...)
	merged.Tokens = append(merged.Tokens, override.Tokens...)

	return merged
}

// LoadAndValidate loads a configuration file (or glob pattern), applies defaults,
// and validates it
func LoadAndValidate(pathOrPattern string) (*Config, error) {
	var cfg *Config
	var err error

	// Check if it's a glob pattern (contains * or ?)
	if containsGlobChar(pathOrPattern) {
		cfg, err = LoadGlob(pathOrPattern)
	} else {
		cfg, err = Load(pathOrPattern)
	}

	if err != nil {
		return nil, err
	}

	// Apply defaults
	cfg.ApplyDefaults()

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// containsGlobChar checks if a path contains glob characters
func containsGlobChar(path string) bool {
	for _, ch := range path {
		if ch == '*' || ch == '?' || ch == '[' {
			return true
		}
	}
	return false
}
