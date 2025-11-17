#!/bin/sh
# ABOUTME: Initializes Vault with approle for local development
# ABOUTME: Creates latr-dev role with fixed credentials for predictable dev environment

set -e

echo "Waiting for Vault to be ready..."
until vault status >/dev/null 2>&1; do
  echo "Vault not ready, retrying in 1s..."
  sleep 1
done
echo "Vault is ready!"

echo "Enabling AppRole auth method..."
vault auth enable approle 2>/dev/null || echo "AppRole already enabled"

echo "Creating latr-dev policy..."
vault policy write latr-dev - <<EOF
path "secret/data/latr-dev/*" {
  capabilities = ["create", "read", "update", "delete"]
}
path "secret/metadata/latr-dev/*" {
  capabilities = ["create", "read", "update", "list", "delete"]
}
EOF

echo "Creating latr-dev approle..."
vault write auth/approle/role/latr-dev \
  token_policies=latr-dev \
  token_ttl=1h \
  token_max_ttl=4h

echo "Setting fixed role_id..."
vault write auth/approle/role/latr-dev/role-id \
  role_id=dev-role-id-12345678

echo "Setting fixed secret_id..."
vault write -f auth/approle/role/latr-dev/custom-secret-id \
  secret_id=dev-secret-id-87654321

echo "Vault initialization complete!"
echo "role_id: dev-role-id-12345678"
echo "secret_id: dev-secret-id-87654321"
