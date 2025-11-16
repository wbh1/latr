# E2E Testing with Docker Compose - Design Document

**Date:** 2025-11-16
**Status:** Approved

## Overview

End-to-end tests for latr using Docker Compose to validate the complete token rotation workflow with real Vault integration and mocked Linode API.

## Architecture

### Docker Compose Services

- **vault** - Official HashiCorp Vault container in dev mode with KV v2 secret engine pre-configured
- **mock-linode** - Lightweight HTTP server mocking Linode API endpoints (GET/POST /v4/profile/tokens)
- **Network** - All services on same Docker network for DNS resolution

### Project Structure

```
latr/
├── test/
│   ├── e2e/
│   │   ├── docker-compose.yml          # Vault + mock Linode services
│   │   ├── mock-linode/
│   │   │   ├── Dockerfile              # Simple Go HTTP server
│   │   │   └── main.go                 # Mock API implementation
│   │   ├── testdata/
│   │   │   ├── config-create.yaml      # Config for create test
│   │   │   ├── config-rotate.yaml      # Config for rotation test
│   │   │   └── config-dryrun.yaml      # Config for dry-run test
│   │   └── e2e_test.go                 # Main test file with //go:build e2e
```

### Test Execution Flow

1. `TestMain` starts Docker Compose and waits for service readiness (HTTP health checks)
2. Each test runs the `latr` binary with a specific config file
3. Tests validate results by querying Vault API directly
4. `TestMain` cleanup tears down Docker Compose with `docker compose down -v`

## Mock Linode API Design

### Implementation

Simple Go HTTP server that maintains in-memory state for tokens, running as a Docker container.

### Endpoints

- `GET /v4/profile/tokens` - Returns list of tokens from in-memory map
- `GET /v4/profile/tokens/{id}` - Returns specific token by ID
- `POST /v4/profile/tokens` - Creates new token, stores in memory, returns token with ID and secret value
- `DELETE /v4/profile/tokens/{id}` - Removes token from in-memory map
- `POST /reset` - Clears all in-memory state (for test isolation)

### State Management

- In-memory map: token ID → token data
- Generates realistic token values (random strings) and sequential IDs
- Tracks expiry times based on requested validity period
- Returns JSON matching actual Linode API structure

### Configuration

- Mock listens on port 8080 inside Docker
- Accessible to latr via `http://mock-linode:8080`
- Base URL passed to latr via `LINODE_API_URL` environment variable

## Test File Structure

### e2e_test.go

```go
//go:build e2e

package e2e

func TestMain(m *testing.M) {
    // 1. Build latr binary (go build -o ./latr ../../cmd/latr)
    // 2. Start Docker Compose (exec.Command("docker", "compose", "up", "-d"))
    // 3. Wait for readiness (HTTP GET vault:8200/v1/sys/health, mock-linode:8080/health)
    // 4. Initialize Vault (enable AppRole, create role/secret, enable KV v2)
    // 5. Run tests: code := m.Run()
    // 6. Cleanup: defer docker compose down -v
    // 7. os.Exit(code)
}

func TestE2E_CreateToken(t *testing.T)
func TestE2E_RotateToken(t *testing.T)
func TestE2E_DryRunMode(t *testing.T)
func TestE2E_DaemonMode(t *testing.T)
```

### Helper Functions

- `runLatr(t, configPath, env)` - Executes latr binary, returns stdout/stderr, fails test on error
- `getVaultSecret(t, path)` - Queries Vault KV v2 API, returns secret data
- `getVaultMetadata(t, path)` - Gets custom metadata from Vault
- `waitForFile(path, timeout)` - Waits for latr to complete (useful for daemon mode)
- `setupMockLinodeToken(label, expiry)` - Pre-populates mock with existing token

### Running Tests

```bash
go test -tags=e2e ./test/e2e -v
```

## Test Scenarios

### TestE2E_CreateToken (Happy Path)

**Setup:**
- Config: Single token that doesn't exist, validity 90d, team "test-team"
- Mock has no pre-existing tokens

**Execute:** Run latr in one-shot mode

**Validate:**
- Token created in mock Linode (verify via GET endpoint)
- Token value stored in Vault at configured path
- Vault metadata contains: token ID, creation timestamp, team, rotation_count = 0
- Exit code 0

### TestE2E_RotateToken

**Setup:**
- Pre-populate mock with existing token that's 95% expired (5% validity remaining)
- Token is below 10% rotation threshold

**Execute:** Run latr in one-shot mode

**Validate:**
- New token created in mock (different ID from original)
- New token value stored in Vault secret
- Vault metadata updated: rotation_count = 1, previous_token_id set, rotation timestamp updated
- Old token still exists in mock (not revoked)

### TestE2E_DryRunMode

**Setup:**
- Config: Token doesn't exist, dry_run: true

**Execute:** Run latr in one-shot mode

**Validate:**
- NO token created in mock Linode
- NO data written to Vault
- Logs indicate "dry-run mode, would create token" (check stdout)
- Exit code 0

### TestE2E_DaemonMode

**Setup:**
- Config: daemon mode, check_interval: 5s, token needs rotation

**Execute:** Run latr in background

**Validate:**
- Wait for first check cycle to complete (parse logs or poll Vault)
- Token rotated and stored in Vault
- Send SIGTERM, verify graceful shutdown
- Logs show "starting daemon" and "shutting down gracefully"

## Configuration & Data Management

### Test Configuration Files

Each test has a dedicated config file in `test/e2e/testdata/` to avoid conflicts.

**Example: config-create.yaml**
```yaml
daemon:
  mode: "one-shot"
  dry_run: false

vault:
  address: "http://vault:8200"
  role_id: "${VAULT_ROLE_ID}"     # Set by test
  secret_id: "${VAULT_SECRET_ID}" # Set by test
  mount_path: "secret"

tokens:
  - label: "e2e-test-token"
    team: "test-team"
    validity: "90d"
    scopes: "*"
    storage:
      - type: "vault"
        path: "secret/data/e2e/test-token"
```

### Environment Variables

Tests pass these to latr:
- `LINODE_TOKEN` = "test-token-from-mock" (mock ignores auth)
- `LINODE_API_URL` = "http://mock-linode:8080" (override base URL)
- `VAULT_ROLE_ID` = generated during TestMain Vault initialization
- `VAULT_SECRET_ID` = generated during TestMain Vault initialization

### Vault Initialization

TestMain performs these steps:
1. Enable AppRole auth: `vault auth enable approle`
2. Create policy allowing `secret/data/e2e/*` access
3. Create role: `vault write auth/approle/role/latr-test policies=latr-test`
4. Get role_id: `vault read auth/approle/role/latr-test/role-id`
5. Generate secret_id: `vault write -f auth/approle/role/latr-test/secret-id`
6. Store credentials for test use

### Mock State Reset

Between tests, call `POST /reset` endpoint to clear in-memory token state.

## Cleanup & Error Handling

### Docker Compose Cleanup

- TestMain uses `defer` to ensure cleanup runs even on test failures
- Command: `docker compose down -v` (removes containers AND volumes)
- Log errors but don't fail test run if cleanup fails (already have exit code)

### Per-Test Cleanup

- Each test uses `t.Cleanup()` to reset mock state via POST /reset
- No Vault cleanup needed - each test uses unique paths under `secret/data/e2e/`
- Timeout contexts: 30s for one-shot tests, 20s for daemon tests

### Error Handling

- Docker Compose fails to start → log output, skip all tests with clear message
- Vault initialization fails → retry once (Vault can be slow), then fail
- Mock health check fails → fail fast with diagnostic info
- Capture latr stdout/stderr on failure and include in test output

### CI Considerations

- Check `docker compose` version in TestMain (fail if not installed)
- Set `-timeout=5m` for e2e test runs to prevent hanging in CI
- Separate `make test-e2e` target that ensures Docker is running

### Local Development

```bash
# Run e2e tests
go test -tags=e2e ./test/e2e -v

# Debug: leave containers running
docker compose -f test/e2e/docker-compose.yml up

# Manual cleanup if tests crash
docker compose -f test/e2e/docker-compose.yml down -v
```

## Implementation Approach

Following TDD principles:
1. Create mock Linode API service first
2. Create Docker Compose configuration
3. Implement TestMain with Docker orchestration
4. Implement helper functions
5. Implement each test scenario (starting with happy path)
6. Add error handling and cleanup logic
7. Verify all tests pass and clean up properly

## Technical Notes

- Uses build tag `//go:build e2e` to separate from unit tests
- Direct exec of `docker compose` commands (no testcontainers-go dependency)
- Mock API returns realistic Linode API JSON responses
- Tests validate Vault integration with real Vault instance
- Each test is independent and can run in isolation
