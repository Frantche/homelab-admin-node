SHELL := /usr/bin/env bash

.PHONY: \
	actionlint ansible-lint ansible-syntax build-admin-node check-ci-tools check-tools clean \
	ci-bootstrap ci-continuous ci-disaster-recovery ci-dr-action ci-full ci-quality \
	dev-deps docs docs-build docs-check docs-deps docs-serve govulncheck lint \
	python-lint python-test render scan-container-images shellcheck sops-check \
	test-build-admin-node-cache test-container-hardening test-disaster-recovery-actions \
	test-docker-api-isolation test-gitea-process-backup test-go test-grafana-dashboard-import \
	test-harbor-mirror-validation test-image-security-policy test-image-security-scanner \
	test-make-entrypoints test-offline-images test-oidc-contracts test-openbao-internal-tls \
	test-repo-permissions test-restic-config test-secret-rotation test-traefik-external-services \
	test-system-update test-traefik-security validate validate-apis validate-cloudflare-tunnel validate-compose \
	validate-dns validate-dr-promotion validate-grafana-dashboards validate-hardening \
	validate-observability validate-systemd

.NOTPARALLEL: ci-quality ci-continuous ci-disaster-recovery ci-full

build-admin-node:
	@./scripts/build-admin-node.sh

dev-deps:
	@./scripts/check-tools.sh python3
	@python3 -m venv .ci/quality-venv
	@.ci/quality-venv/bin/python -m pip install -r ci/requirements-dev.txt
	@ANSIBLE_GALAXY_CACHE_DIR=.ci/galaxy-cache \
	 .ci/quality-venv/bin/ansible-galaxy collection install \
	 -r ansible/requirements.yml -p .ci/ansible_collections
	@echo 'Run: export PATH="'"$$PWD"'/.ci/quality-venv/bin:$$PATH"'
	@echo 'Run: export ANSIBLE_COLLECTIONS_PATH="'"$$PWD"'/.ci/ansible_collections"'

check-tools:
	@./scripts/check-tools.sh go python3 shellcheck actionlint ansible-playbook ansible-lint

check-ci-tools: check-tools
	@./scripts/check-tools.sh curl docker govulncheck jq restic rg ssh sshd systemd-analyze

lint: go-vet shellcheck actionlint python-lint ansible-lint ansible-syntax sops-check

ci-quality: check-ci-tools test-go lint govulncheck python-test \
	test-build-admin-node-cache test-repo-permissions test-secret-rotation \
	test-system-update \
	test-docker-api-isolation test-gitea-process-backup test-openbao-internal-tls \
	test-restic-config test-offline-images test-image-security-scanner \
	test-disaster-recovery-actions test-make-entrypoints validate-compose \
	validate-systemd validate-grafana-dashboards

ci-continuous: ci-quality test-oidc-contracts

ci-full: ci-continuous ci-disaster-recovery

test-go:
	@./ci/check-go-coverage.sh

test-system-update:
	@./ci/test-system-update.sh

go-vet:
	@go vet ./...

govulncheck:
	@./scripts/check-tools.sh govulncheck
	@govulncheck ./...

ansible-lint:
	@./scripts/check-tools.sh ansible-lint
	@ansible-lint ansible/site.yml

ansible-syntax:
	@./scripts/check-tools.sh ansible-playbook
	@ansible-playbook -i ansible/inventory.ini ansible/site.yml --syntax-check

sops-check:
	@./scripts/validate-sops-files.sh

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

test-traefik-external-services: test-traefik-security
	@./ci/test-traefik-external-services.sh

test-traefik-security:
	@./ci/test-traefik-security-runtime.sh

test-docker-api-isolation: test-traefik-external-services
	@./ci/test-docker-api-isolation.sh

test-gitea-process-backup:
	@./ci/test-gitea-process-backup.sh

test-harbor-mirror-validation:
	@python3 ./ci/test_harbor_mirror_validation.py

test-build-admin-node-cache:
	@./ci/test-build-admin-node-cache.sh

test-container-hardening:
	@python3 ./ci/test_container_hardening.py

test-openbao-internal-tls: test-traefik-external-services
	@./ci/test-openbao-internal-tls.sh

test-repo-permissions:
	@./ci/test-repo-permissions.sh

test-secret-rotation:
	@./ci/test-secret-rotation.sh

test-restic-config:
	@./ci/test-restic-config.sh

test-offline-images:
	@./ci/test-offline-images.sh

test-image-security-policy:
	@python3 ./ci/test_image_security_policy.py

test-image-security-scanner:
	@./ci/test-scan-container-images.sh

scan-container-images:
	@./ci/scan-container-images.sh

ci-bootstrap:
	@./ci/scenarios/bootstrap-user-journey.sh

test-disaster-recovery-actions:
	@./ci/test-disaster-recovery-actions.sh

validate-dr-promotion:
	@if [[ -z "$(CANDIDATE_SHA)" ]]; then echo "CANDIDATE_SHA is required" >&2; exit 2; fi
	@./ci/validate-dr-promotion.sh "$(CANDIDATE_SHA)" "$(DR_EVIDENCE_DIR)"

test-make-entrypoints:
	@./ci/test-make-entrypoints.sh

ci-dr-action:
	@if [[ -z "$(DR_ACTION)" ]]; then echo "DR_ACTION is required" >&2; exit 2; fi
	@./ci/scenarios/main-to-candidate-disaster-recovery.sh "$(DR_ACTION)"

ci-disaster-recovery:
	@MAIN_SHA="$${MAIN_SHA:-$$(git rev-parse origin/main)}" \
	 CANDIDATE_SHA="$${CANDIDATE_SHA:-$$(git rev-parse HEAD)}" \
	 MAIN_REPO_URL="$${MAIN_REPO_URL:-https://github.com/Frantche/homelab-admin-node.git}" \
	 CANDIDATE_REPO_URL="$${CANDIDATE_REPO_URL:-https://github.com/Frantche/homelab-admin-node.git}" \
	 ./ci/run-disaster-recovery.sh $${DR_VARIANTS:-standard offline-images}

validate-compose:
	@./ci/validate-compose-configs.sh

validate-systemd:
	@./ci/validate-systemd-units.sh

validate-grafana-dashboards:
	@./ci/validate-grafana-dashboards.sh

test-grafana-dashboard-import:
	@./ci/test-grafana-dashboard-import.sh

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
		output="$${HUGO_DESTINATION:-$$(mktemp -d /tmp/admin-node-docs.XXXXXX)}"; \
		if [[ -z "$${HUGO_DESTINATION:-}" ]]; then trap 'rm -rf "$$output"' EXIT; fi; \
		args=(--minify --panicOnWarning --printPathWarnings --destination "$$output"); \
		if [[ -n "$${HUGO_BASEURL:-}" ]]; then args+=(--baseURL "$$HUGO_BASEURL/"); fi; \
		cd site && hugo "$${args[@]}"; \
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
	@./scripts/check-tools.sh shellcheck
	@shellcheck -e SC1091 scripts/*.sh ci/*.sh ci/lib/*.sh ci/scenarios/*.sh

actionlint:
	@./scripts/check-tools.sh actionlint
	@actionlint -shellcheck=

python-lint:
	@./scripts/check-tools.sh python3 ruff
	@ruff check scripts ci

python-test:
	@./scripts/check-tools.sh python3
	@python3 -m unittest discover -s ci -p 'test_*.py'

clean:
	rm -rf backups ci/tmp ci/.tmp .ci/vms
