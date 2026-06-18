#!/usr/bin/env bash
set -euo pipefail

# Set up CI-specific prerequisites that the Ansible playbook cannot handle itself.
# This only does: TLS self-signed cert generation, CI age key setup, /etc/hosts entries, mode directory.
# All actual deployment is done by the Ansible playbook.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Create required directories
mkdir -p /etc/admin-node
mkdir -p /srv/admin/certs

# --- Install CI-only age key for SOPS secrets ---
if [[ ! -f /etc/sops/age/keys.txt ]]; then
  echo "[ci-setup] Generating CI-only age key..."
  install -d -m 0700 /etc/sops/age
  tmp_age_dir="$(mktemp -d /tmp/ci-age-key.XXXXXX)"
  tmp_age_key="$tmp_age_dir/keys.txt"
  age-keygen -o "$tmp_age_key" >/dev/null
  install -m 0400 "$tmp_age_key" /etc/sops/age/keys.txt
  rm -rf "$tmp_age_dir"
fi

mapfile -t service_domains < <("$REPO_ROOT/ci/service-domains.py" list)
if [[ ${#service_domains[@]} -eq 0 ]]; then
  service_domains=(keycloak.example.com bao.example.com harbor.example.com traefik.example.com)
fi
primary_domain="${service_domains[0]}"

# --- Generate self-signed CA and cert for configured CI service domains ---
echo "[ci-setup] Generating self-signed TLS certificate..."
_cert_dir="$(mktemp -d)"
trap 'rm -rf "$_cert_dir"' EXIT

openssl genrsa -out "$_cert_dir/ca.key" 4096 2>/dev/null
openssl req -new -key "$_cert_dir/ca.key" -out "$_cert_dir/ca.csr"   -subj "/CN=Admin Node CI CA" 2>/dev/null
cat > "$_cert_dir/ca.ext" <<'EOT'
[v3_ca]
basicConstraints=critical,CA:TRUE
keyUsage=critical,keyCertSign,cRLSign
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid,issuer
EOT
openssl x509 -req -days 3650   -in "$_cert_dir/ca.csr"   -signkey "$_cert_dir/ca.key"   -out "$_cert_dir/ca.crt"   -extfile "$_cert_dir/ca.ext" -extensions v3_ca 2>/dev/null

openssl genrsa -out "$_cert_dir/server.key" 2048 2>/dev/null
openssl req -new -key "$_cert_dir/server.key" -out "$_cert_dir/server.csr"   -subj "/CN=${primary_domain}" 2>/dev/null

{
  printf '[SAN]
'
  printf 'basicConstraints=critical,CA:FALSE
'
  printf 'keyUsage=critical,digitalSignature,keyEncipherment
'
  printf 'extendedKeyUsage=serverAuth
'
  printf 'subjectAltName='
  first=true
  for domain in "${service_domains[@]}" localhost; do
    if [[ "$first" == true ]]; then
      printf 'DNS:%s' "$domain"
      first=false
    else
      printf ',DNS:%s' "$domain"
    fi
  done
  printf ',IP:127.0.0.1
'
} > "$_cert_dir/san.ext"

openssl x509 -req -days 3650   -in "$_cert_dir/server.csr"   -CA "$_cert_dir/ca.crt" -CAkey "$_cert_dir/ca.key" -CAcreateserial   -out "$_cert_dir/server.crt"   -extfile "$_cert_dir/san.ext" -extensions SAN 2>/dev/null

cp "$_cert_dir/server.crt" /srv/admin/certs/cert.pem
cp "$_cert_dir/server.key" /srv/admin/certs/key.pem
cp "$_cert_dir/ca.crt" /srv/admin/certs/ca.pem
chmod 0600 /srv/admin/certs/key.pem

# Add CA to the system trust store
if command -v update-ca-trust &>/dev/null; then
  cp "$_cert_dir/ca.crt" /etc/ca-certificates/trust-source/anchors/ci-admin-ca.crt
  update-ca-trust
elif command -v update-ca-certificates &>/dev/null; then
  cp "$_cert_dir/ca.crt" /usr/local/share/ca-certificates/ci-admin-ca.crt
  update-ca-certificates
fi

echo "[ci-setup] TLS certificate generated and CA trusted"

# --- Add /etc/hosts entries for service domains ---
for domain in "${service_domains[@]}"; do
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
