SHELL := /usr/bin/env bash

build-admin-node:
	@./scripts/build-admin-node.sh

lint: shellcheck ansible-syntax sops-check

ansible-syntax:
	@if command -v ansible-playbook >/dev/null 2>&1; then \
		ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check; \
	else \
		echo "ansible-playbook not installed"; \
	fi

sops-check:
	@if command -v sops >/dev/null 2>&1; then \
		echo "sops binary present"; \
	else \
		echo "sops not installed"; \
	fi

validate: validate-apis validate-dns validate-cloudflare-tunnel validate-hardening

validate-apis:
	@./scripts/build-admin-node.sh >/dev/null
	@./bin/admin-node validate apis

validate-dns:
	@./scripts/build-admin-node.sh >/dev/null
	@./bin/admin-node validate dns

validate-cloudflare-tunnel:
	@./scripts/build-admin-node.sh >/dev/null
	@./bin/admin-node validate tunnel

validate-hardening:
	@./scripts/build-admin-node.sh >/dev/null
	@./bin/admin-node validate hardening

test-oidc-contracts:
	@./ci/test-oidc-contracts.sh

test-restic-config:
	@./ci/test-restic-config.sh

test-offline-images:
	@./ci/test-offline-images.sh

test-ci-fast:
	@./ci/run-admin-lifecycle.sh fresh-branch

test-ci-full:
	@./ci/run-admin-lifecycle.sh fresh-branch && \
	 ./ci/run-admin-lifecycle.sh upgrade-main-to-branch && \
	 ./ci/run-admin-lifecycle.sh restore-main-backup-with-branch

render:
	@echo "Render is managed by Ansible templates/tasks"

docs:
	@echo "Documentation is in README.md and docs/*.md"

shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck -e SC1091 scripts/*.sh ci/*.sh ci/scenarios/*.sh; \
	else \
		echo "shellcheck not installed"; \
	fi

clean:
	rm -rf backups ci/tmp ci/.tmp
