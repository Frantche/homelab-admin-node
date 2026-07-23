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

validate: validate-apis validate-dns validate-cloudflare-tunnel validate-hardening validate-observability

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

validate-observability:
	@./scripts/build-admin-node.sh >/dev/null
	@./bin/admin-node validate observability

test-oidc-contracts:
	@./ci/test-oidc-contracts.sh

test-traefik-external-services:
	@./ci/test-traefik-external-services.sh

test-restic-config:
	@./ci/test-restic-config.sh

test-offline-images:
	@./ci/test-offline-images.sh

test-ci-fast:
	@./ci/scenarios/bootstrap-user-journey.sh

test-ci-full:
	@./ci/setup-garage.sh
	@MAIN_SHA="$${MAIN_SHA:-$$(git rev-parse origin/main)}" \
	 CANDIDATE_SHA="$${CANDIDATE_SHA:-$$(git rev-parse HEAD)}" \
	 MAIN_REPO_URL="$${MAIN_REPO_URL:-https://github.com/Frantche/homelab-admin-node.git}" \
	 CANDIDATE_REPO_URL="$${CANDIDATE_REPO_URL:-https://github.com/Frantche/homelab-admin-node.git}" \
	 ./ci/scenarios/main-to-candidate-disaster-recovery.sh

render:
	@echo "Render is managed by Ansible templates/tasks"

docs: docs-build

docs-deps:
	@if command -v npm >/dev/null 2>&1; then \
		cd site && npm ci; \
	else \
		echo "npm not installed"; \
		exit 1; \
	fi

docs-build: docs-deps
	@if command -v hugo >/dev/null 2>&1; then \
		cd site && hugo --minify; \
	else \
		echo "hugo not installed"; \
		exit 1; \
	fi

docs-check: docs-deps
	@if command -v hugo >/dev/null 2>&1; then \
		cd site && hugo --minify --panicOnWarning --printPathWarnings; \
	else \
		echo "hugo not installed"; \
		exit 1; \
	fi

docs-serve: docs-deps
	@if command -v hugo >/dev/null 2>&1; then \
		cd site && hugo server --bind 127.0.0.1 --baseURL http://127.0.0.1:1313/; \
	else \
		echo "hugo not installed"; \
		exit 1; \
	fi

shellcheck:
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck -e SC1091 scripts/*.sh ci/*.sh ci/lib/*.sh ci/scenarios/*.sh; \
	else \
		echo "shellcheck not installed"; \
	fi

clean:
	rm -rf backups ci/tmp ci/.tmp .ci/vms
