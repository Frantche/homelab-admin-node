#!/usr/bin/env bash
set -euo pipefail

# Set up CI-specific prerequisites that the Ansible playbook cannot handle itself.
# This only does: TLS self-signed cert generation, /etc/hosts entries, mode directory.
# All actual deployment is done by the Ansible playbook.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Create required directories
mkdir -p /etc/admin-node
mkdir -p /srv/admin/certs

# --- Generate self-signed CA and wildcard cert for *.example.com (CI only) ---
echo "[ci-setup] Generating self-signed TLS certificate..."
_cert_dir="$(mktemp -d)"
trap 'rm -rf "$_cert_dir"' EXIT

openssl genrsa -out "$_cert_dir/ca.key" 4096 2>/dev/null
openssl req -new -x509 -days 3650 -key "$_cert_dir/ca.key" -out "$_cert_dir/ca.crt" \
  -subj "/CN=Admin Node CI CA" 2>/dev/null

openssl genrsa -out "$_cert_dir/server.key" 2048 2>/dev/null
openssl req -new -key "$_cert_dir/server.key" -out "$_cert_dir/server.csr" \
  -subj "/CN=*.example.com" 2>/dev/null

cat > "$_cert_dir/san.ext" <<'EOT'
[SAN]
subjectAltName=DNS:*.example.com,DNS:localhost,IP:127.0.0.1
EOT

openssl x509 -req -days 3650 \
  -in "$_cert_dir/server.csr" \
  -CA "$_cert_dir/ca.crt" -CAkey "$_cert_dir/ca.key" -CAcreateserial \
  -out "$_cert_dir/server.crt" \
  -extfile "$_cert_dir/san.ext" -extensions SAN 2>/dev/null

cp "$_cert_dir/server.crt" /srv/admin/certs/cert.pem
cp "$_cert_dir/server.key" /srv/admin/certs/key.pem
cp "$_cert_dir/ca.crt" /srv/admin/certs/ca.pem
chmod 0600 /srv/admin/certs/key.pem

# Add CA to the system trust store (Arch Linux)
if command -v update-ca-trust &>/dev/null; then
  cp "$_cert_dir/ca.crt" /etc/ca-certificates/trust-source/anchors/ci-admin-ca.crt
  update-ca-trust
elif command -v update-ca-certificates &>/dev/null; then
  cp "$_cert_dir/ca.crt" /usr/local/share/ca-certificates/ci-admin-ca.crt
  update-ca-certificates
fi

echo "[ci-setup] TLS certificate generated and CA trusted"

# --- Add /etc/hosts entries for service domains ---
# "keycloak" (without TLD) must also resolve to localhost so that tokens
# obtained from http://keycloak:8080 carry the same issuer URL that OpenBao
# uses when it contacts Keycloak directly on the Docker network.
for domain in keycloak.example.com bao.example.com harbor.example.com traefik.example.com keycloak; do
  if ! grep -qF "$domain" /etc/hosts; then
    echo "127.0.0.1 $domain" >> /etc/hosts
  fi
done

# --- Install required Ansible collections ---
if [[ -f "$REPO_ROOT/ansible/requirements.yml" ]]; then
  echo "[ci-setup] Installing required Ansible collections..."
  ansible-galaxy collection install -r "$REPO_ROOT/ansible/requirements.yml" --force 2>/dev/null || true
fi

echo "[ci-setup] CI prerequisites ready"
