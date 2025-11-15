# latr - Linode API Token Rotator

A Go application for automatically managing and rotating Linode API tokens with configurable validity periods and secure storage in HashiCorp Vault.

## Features

- **Automatic Token Rotation**: Automatically rotates tokens based on configurable thresholds (default: 10% validity remaining)
- **Secure Storage**: Stores rotated tokens in HashiCorp Vault (KV v2)
- **State Tracking**: Tracks token rotation history and state via Vault metadata
- **Graceful Token Management**: Keeps old tokens until expiration (configurable pruning)
- **Multiple Tokens**: Manage multiple API tokens with different configurations
- **Team Metadata**: Associate tokens with owning teams for organization
- **Flexible Configuration**: Single YAML file or glob pattern support
- **Daemon or One-Shot**: Run as a long-running daemon or one-time execution
- **Dry-Run Mode**: Test configuration without making changes
- **OpenTelemetry Support**: Observability via traces, metrics, and logs

## Requirements

- Go 1.21+
- Linode account with API token (for creating/managing tokens)
- HashiCorp Vault with AppRole authentication
- Valid Vault role and secret IDs

## Installation

```bash
# Clone the repository
git clone https://github.com/wbh1/latr.git
cd latr

# Build the binary
go build -o latr ./cmd/latr

# Or install directly
go install ./cmd/latr
```

## Configuration

### Environment Variables

- `LINODE_TOKEN`: Your Linode API token (required)
- `VAULT_ROLE_ID`: Vault AppRole role ID (optional if in config)
- `VAULT_SECRET_ID`: Vault AppRole secret ID (optional if in config)

### Configuration File

Create a YAML configuration file (see `examples/config.yaml`):

```yaml
# Global daemon settings
daemon:
  mode: "daemon" # "daemon" or "one-shot"
  check_interval: "30m" # How often to check tokens (daemon mode only)
  dry_run: false # If true, no actual changes are made

# Rotation behavior
rotation:
  threshold_percent: 10 # Rotate when <=10% of validity remains
  prune_expired: false # Delete expired tokens from Linode API

# Vault configuration
vault:
  address: "https://vault.example.com"
  role_id: "${VAULT_ROLE_ID}" # Can use env vars
  secret_id: "${VAULT_SECRET_ID}"
  mount_path: "secret" # KV v2 mount path

# Observability settings
observability:
  otel_endpoint: "localhost:4317" # OpenTelemetry collector endpoint (optional)
  log_level: "info" # debug, info, warn, error

# Token definitions
tokens:
  - label: "my-api-token"
    team: "platform-team"
    validity: "90d" # Must be <= 6 months (180d)
    scopes: "*" # "*" for all scopes, or comma-separated list
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/my-api-token"

  - label: "backup-token"
    team: "sre-team"
    validity: "180d" # Maximum allowed
    scopes: "linodes:read_only,domains:read_only"
    rotation_threshold: 15 # Override global threshold for this token
    storage:
      - type: "vault"
        path: "secret/data/linode/tokens/backup"
```

## Usage

### One-Shot Mode

Run once and exit:

```bash
export LINODE_TOKEN="your-linode-token"
./latr -config config.yaml
```

### Daemon Mode

Run continuously with periodic checks:

```yaml
# In config.yaml
daemon:
  mode: "daemon"
  check_interval: "30m"
```

```bash
export LINODE_TOKEN="your-linode-token"
./latr -config config.yaml
```

### Dry-Run Mode

Test configuration without making changes:

```yaml
daemon:
  dry_run: true
```

### Multiple Configuration Files

Use a glob pattern to load multiple config files:

```bash
./latr -config "configs/*.yaml"
```

### Version Information

```bash
./latr -version
```

## How It Works

1. **Token Discovery**: latr checks if each configured token exists in Linode
2. **Rotation Check**: Calculates percentage of validity remaining
3. **Token Creation/Rotation**:
   - If token doesn't exist: Creates it
   - If rotation needed: Creates new token with same label
4. **Storage**: Stores new token value in configured Vault paths
5. **State Tracking**: Updates Vault metadata with:
   - Current token ID and value
   - Previous token ID and expiry
   - Rotation count and timestamp
6. **Cleanup** (optional): Prunes expired tokens if `prune_expired: true`

### Important Behaviors

- **Old tokens are kept**: Previous tokens are NOT deleted until their expiration date
- **Only manages configured tokens**: Only rotates/deletes tokens in the configuration
- **Vault retry on failure**: If Linode succeeds but Vault fails, state is tracked for retry on next run
- **Graceful shutdown**: Handles SIGTERM/SIGINT for clean daemon shutdown

## Token Validity

- Maximum validity: **6 months (180 days)**
- Supported formats: `90d`, `180d`, `6mo`, `30m`, `1h`
- Default rotation threshold: **10% of validity remaining**
- Token-specific thresholds can override global setting

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Run specific package tests
go test ./internal/rotation -v
```

### Project Structure

```
latr/
├── cmd/latr/           # Main application entrypoint
├── internal/
│   ├── config/        # Configuration parsing and validation
│   ├── linode/        # Linode API client wrapper
│   ├── vault/         # Vault client with AppRole auth
│   ├── rotation/      # Core rotation engine logic
│   ├── scheduler/     # Daemon/one-shot scheduler
│   └── observability/ # OpenTelemetry setup
├── pkg/models/        # Shared domain models
└── examples/          # Example configurations
```

### Testing Approach

This project was built using **Test-Driven Development (TDD)**:

- All features have comprehensive unit tests
- Mock implementations for external dependencies (Linode, Vault)
- Table-driven tests for validation logic
- Integration test support via Docker Compose (future enhancement)

## Observability

latr supports OpenTelemetry for observability:

### Metrics

- `latr_tokens_total` - Total configured tokens
- `latr_rotations_total{status="success|failure"}` - Rotation attempts
- `latr_rotation_duration_seconds` - Rotation operation duration
- `latr_token_validity_remaining_seconds` - Time until rotation needed
- `latr_vault_storage_errors_total` - Vault write failures

### Traces

Distributed tracing for rotation operations (when OTel endpoint configured)

### Logs

Structured logging with rotation events, errors, and state changes

## Security Considerations

- Store `LINODE_TOKEN` securely (env var, secrets manager)
- Use Vault AppRole with minimal required permissions
- Enable TLS for Vault communication in production
- Rotate Vault AppRole secret IDs regularly
- Review token scopes - use least privilege principle
- Consider enabling `prune_expired` to reduce token sprawl

## Roadmap

- [ ] Additional storage backends
- [ ] Prometheus metrics exporter
- [ ] Integration tests with Docker Compose

## Contributing

Contributions welcome! Please:

1. Write tests for new features
2. Follow existing code structure
3. Update documentation
4. Run `go test ./...` before submitting
