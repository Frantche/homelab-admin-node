---
title: Local Development
weight: 10
---

Useful commands:

```bash
make build-admin-node
make lint
make ansible-syntax
make shellcheck
make validate
make test-ci-fast
```

Some commands require local tools such as Ansible, ShellCheck, SOPS, Docker, QEMU, or Hugo.

Keep changes scoped:

- Prefer existing Ansible roles and repository patterns.
- Keep secrets out of this repository.
- Add or update docs when behavior changes.
- Add tests for CLI behavior, validation logic, backup/restore behavior, or lifecycle changes.

The documentation site can be served locally with:

```bash
make docs-serve
```
