#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_REPO_DIR="${CONFIG_REPO_DIR:-/etc/admin-config/homelab-node-admin-config}"
SOPS_AGE_KEY_FILE="${SOPS_AGE_KEY_FILE:-/etc/sops/age/keys.txt}"
ADMIN_REPO_URL="${ADMIN_REPO_URL:-https://github.com/Frantche/homelab-admin-node.git}"

"$REPO_ROOT/ci/setup-ci-env.sh"

install -d -m 0755 "$CONFIG_REPO_DIR/hosts/group_vars"
cp "$REPO_ROOT/ansible/inventory.ini" "$CONFIG_REPO_DIR/hosts/inventory.ini"

python3 "$REPO_ROOT/ci/render-bootstrap-config-repo.py" "$REPO_ROOT" "$CONFIG_REPO_DIR" "$ADMIN_REPO_URL"

age_public="$(age-keygen -y "$SOPS_AGE_KEY_FILE")"
cat > "$CONFIG_REPO_DIR/.sops.yaml" <<SOPSEOF
creation_rules:
  - path_regex: hosts/group_vars/secrets\\.sops\\.yaml$
    age: ["${age_public}"]
SOPSEOF

(
  cd "$CONFIG_REPO_DIR"
  sops --encrypt \
    --input-type yaml \
    --output-type yaml \
    --filename-override hosts/group_vars/secrets.sops.yaml \
    hosts/group_vars/secrets.plain.yaml > hosts/group_vars/secrets.sops.yaml
  rm -f hosts/group_vars/secrets.plain.yaml
  SOPS_AGE_KEY_FILE="$SOPS_AGE_KEY_FILE" sops --decrypt hosts/group_vars/secrets.sops.yaml >/tmp/bootstrap-ci-secrets-decrypted.yaml
)

python3 "$REPO_ROOT/ci/validate-bootstrap-config-repo.py" \
  /tmp/bootstrap-ci-secrets-decrypted.yaml \
  "$CONFIG_REPO_DIR/hosts/group_vars/all.yml"
rm -f /tmp/bootstrap-ci-secrets-decrypted.yaml

(
  cd "$CONFIG_REPO_DIR"
  git init
  git checkout -B main
  git add .sops.yaml hosts/inventory.ini hosts/group_vars/all.yml hosts/group_vars/secrets.sops.yaml
  if ! git diff --cached --quiet; then
    git -c user.name="CI Admin" -c user.email="ci@example.com" commit -m "Initial CI admin config"
  fi
)

echo "Bootstrap CI config repo initialized at $CONFIG_REPO_DIR"
