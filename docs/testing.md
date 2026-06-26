# Testing
- `make build-admin-node`
- `go test ./...`
- `make lint`
- `make ansible-syntax`
- `make shellcheck`
- `make validate`
- `make test-oidc-contracts`
- `make test-restic-config`
- `make test-offline-images`
- `make test-ci-fast`
- `make test-ci-full`

`make test-ci-fast` execute le scenario `fresh-branch`.

`make test-ci-full` execute `fresh-branch`, `upgrade-main-to-branch` et `restore-main-backup-with-branch`.

Les tests CI appellent directement `bin/admin-node`, donc le binaire doit exister. `make build-admin-node` et le role Ansible `base` appellent tous les deux `scripts/build-admin-node.sh`.

Ce build est idempotent: sans changement dans les sources Go, il conserve `bin/admin-node` et sort `changed=false`. Si le fingerprint change, il compile un binaire temporaire, le verifie, puis remplace `bin/admin-node`.
