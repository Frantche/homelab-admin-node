#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d /tmp/admin-node-secret-rotation-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

cat >"$tmp_dir/secrets.yaml" <<'EOF'
vault_oidc_harbor_client_secret: oidc-harbor-old
vault_oidc_openbao_client_secret: oidc-openbao-old
vault_oidc_gitea_client_secret: oidc-gitea-old
keycloak:
  db_password: keycloak-db-old
  admin_password: keycloak-admin-old
keycloak_config:
  users:
    - username: human-user
      password: human-password-must-not-change
harbor:
  db_password: harbor-db-old
  admin_password: harbor-admin-old
gitea:
  db_password: gitea-db-old
  admin_password: gitea-admin-old
EOF

python3 "$REPO_ROOT/ci/rotate-bootstrap-secrets.py" \
  prepare "$tmp_dir/secrets.yaml" --audit-file "$tmp_dir/audit.json"

python3 - "$tmp_dir/secrets.yaml" "$tmp_dir/audit.json" <<'PY'
import json
import sys
import yaml

secrets = yaml.safe_load(open(sys.argv[1]))
audit = json.load(open(sys.argv[2]))
assert secrets["keycloak_config"]["users"][0]["password"] == "human-password-must-not-change"
assert secrets["harbor"]["previous_admin_password"] == "harbor-admin-old"
assert secrets["harbor"]["rotate_admin_password"] is True
assert secrets["gitea"]["rotate_admin_password"] is True
assert audit["old"]["keycloak.admin_password"] != audit["new"]["keycloak.admin_password"]
assert "human-password-must-not-change" not in json.dumps(audit)
assert len(audit["oidc_user_password_sha256"]["human-user"]) == 64
PY

python3 "$REPO_ROOT/ci/rotate-bootstrap-secrets.py" \
  finalize "$tmp_dir/secrets.yaml" --audit-file "$tmp_dir/audit.json"

python3 - "$tmp_dir/secrets.yaml" <<'PY'
import sys
import yaml

secrets = yaml.safe_load(open(sys.argv[1]))
assert secrets["keycloak_config"]["users"][0]["password"] == "human-password-must-not-change"
assert "previous_admin_password" not in secrets["harbor"]
assert "rotate_admin_password" not in secrets["harbor"]
assert "rotate_admin_password" not in secrets["gitea"]
PY

echo "Secret rotation preserves OIDC user passwords"
