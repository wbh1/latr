// ABOUTME: End-to-end tests for latr using Docker Compose
// ABOUTME: Tests complete workflows with real Vault and mock Linode API
//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	latrBinary  string
	vaultAddr   = "http://localhost:8200"
	vaultToken  = "root"
	roleID      string
	secretID    string
	mockLinode  = "http://localhost:8080"
	composeFile string
)

func TestMain(m *testing.M) {
	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	// Get absolute path to project root
	projectRoot, err := getProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get project root: %v\n", err)
		exitCode = 1
		return
	}

	// Set compose file path
	composeFile = filepath.Join(projectRoot, "test", "e2e", "docker-compose.yml")

	// Build latr binary
	fmt.Println("Building latr binary...")
	latrBinary = filepath.Join(projectRoot, "latr-e2e")
	buildCmd := exec.Command("go", "build", "-o", latrBinary, "./cmd/latr")
	buildCmd.Dir = projectRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build latr: %v\n%s\n", err, output)
		exitCode = 1
		return
	}
	defer os.Remove(latrBinary)

	// Start Docker Compose
	fmt.Println("Starting Docker Compose...")
	upCmd := exec.Command("docker", "compose", "-f", composeFile, "up", "-d", "--build")
	if output, err := upCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start Docker Compose: %v\n%s\n", err, output)
		exitCode = 1
		return
	}

	// Ensure cleanup happens
	defer func() {
		fmt.Println("Cleaning up Docker Compose...")
		downCmd := exec.Command("docker", "compose", "-f", composeFile, "down", "-v")
		if output, err := downCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clean up Docker Compose: %v\n%s\n", err, output)
		}
	}()

	// Wait for services to be ready
	fmt.Println("Waiting for services to be ready...")
	if err := waitForService(vaultAddr+"/v1/sys/health", 30*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Vault failed to become ready: %v\n", err)
		exitCode = 1
		return
	}
	if err := waitForService(mockLinode+"/health", 30*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Mock Linode failed to become ready: %v\n", err)
		exitCode = 1
		return
	}

	// Initialize Vault
	fmt.Println("Initializing Vault...")
	if err := initializeVault(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize Vault: %v\n", err)
		exitCode = 1
		return
	}

	// Run tests
	fmt.Println("Running tests...")
	exitCode = m.Run()
}

func getProjectRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(output)), nil
}

func waitForService(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for service at %s", url)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil && resp.StatusCode < 500 {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

func initializeVault() error {
	// Enable KV v2 secrets engine (already enabled in dev mode at "secret/")
	// Enable AppRole auth
	if err := vaultExec("auth", "enable", "approle"); err != nil {
		return fmt.Errorf("failed to enable approle: %w", err)
	}

	// Create policy
	policy := `
path "secret/data/e2e/*" {
  capabilities = ["create", "read", "update", "delete"]
}
path "secret/metadata/e2e/*" {
  capabilities = ["read", "list", "delete"]
}
`
	policyFile := "/tmp/latr-e2e-policy.hcl"
	if err := os.WriteFile(policyFile, []byte(policy), 0644); err != nil {
		return fmt.Errorf("failed to write policy file: %w", err)
	}
	defer os.Remove(policyFile)

	if err := vaultExec("policy", "write", "latr-e2e", policyFile); err != nil {
		return fmt.Errorf("failed to write policy: %w", err)
	}

	// Create AppRole
	if err := vaultExec("write", "auth/approle/role/latr-e2e",
		"token_policies=latr-e2e",
		"token_ttl=1h",
		"token_max_ttl=4h"); err != nil {
		return fmt.Errorf("failed to create approle: %w", err)
	}

	// Get role ID
	output, err := vaultExecOutput("read", "-field=role_id", "auth/approle/role/latr-e2e/role-id")
	if err != nil {
		return fmt.Errorf("failed to get role_id: %w", err)
	}
	roleID = string(bytes.TrimSpace(output))

	// Generate secret ID
	output, err = vaultExecOutput("write", "-field=secret_id", "-f", "auth/approle/role/latr-e2e/secret-id")
	if err != nil {
		return fmt.Errorf("failed to generate secret_id: %w", err)
	}
	secretID = string(bytes.TrimSpace(output))

	return nil
}

func vaultExec(args ...string) error {
	_, err := vaultExecOutput(args...)
	return err
}

func vaultExecOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("vault", args...)
	cmd.Env = append(os.Environ(),
		"VAULT_ADDR="+vaultAddr,
		"VAULT_TOKEN="+vaultToken,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("vault command failed: %w\n%s", err, output)
	}
	return output, nil
}

// Helper function to run latr with config
func runLatr(t *testing.T, configPath string) (stdout, stderr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, latrBinary, "-config", configPath)
	cmd.Env = append(os.Environ(),
		"LINODE_TOKEN=test-token",
		"LINODE_API_URL="+mockLinode,
		"VAULT_ROLE_ID="+roleID,
		"VAULT_SECRET_ID="+secretID,
	)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("latr command timed out. stdout: %s\nstderr: %s", stdout, stderr)
	}

	if err != nil {
		t.Logf("latr failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	return stdout, stderr
}

// Helper function to get Vault secret
func getVaultSecret(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	url := fmt.Sprintf("%s/v1/%s", vaultAddr, path)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", vaultToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil
	}
	require.Equal(t, 200, resp.StatusCode, "unexpected status code from Vault")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &result))

	return result.Data.Data
}

// Helper function to get Vault metadata
func getVaultMetadata(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	// Convert data path to metadata path
	metadataPath := path
	if bytes.Contains([]byte(path), []byte("/data/")) {
		metadataPath = string(bytes.ReplaceAll([]byte(path), []byte("/data/"), []byte("/metadata/")))
	}

	url := fmt.Sprintf("%s/v1/%s", vaultAddr, metadataPath)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", vaultToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil
	}
	require.Equal(t, 200, resp.StatusCode, "unexpected status code from Vault")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result struct {
		Data struct {
			CustomMetadata map[string]interface{} `json:"custom_metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &result))

	return result.Data.CustomMetadata
}

// Helper function to reset mock Linode state
func resetMockLinode(t *testing.T) {
	t.Helper()

	resp, err := http.Post(mockLinode+"/reset", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
}

// Helper function to setup mock Linode token
func setupMockLinodeToken(t *testing.T, label string, expiry time.Time) int {
	t.Helper()

	reqBody := map[string]string{
		"label":  label,
		"scopes": "*",
		"expiry": expiry.Format(time.RFC3339),
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp, err := http.Post(mockLinode+"/v4/profile/tokens", "application/json", bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var result struct {
		ID int `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	return result.ID
}

// Helper function to get mock Linode tokens
func getMockLinodeTokens(t *testing.T) []map[string]interface{} {
	t.Helper()

	resp, err := http.Get(mockLinode + "/v4/profile/tokens")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	return result.Data
}
