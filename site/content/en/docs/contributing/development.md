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

The site uses Docsy 0.16 as the `github.com/google/docsy/theme` Hugo module.
Use Hugo Extended 0.160.1 or later; CI validates the officially supported
0.164.0 release. After changing the Docsy version, refresh its npm workspace
before committing the generated dependency metadata:

```bash
cd site
hugo mod tidy
hugo mod npm pack
npm install
```
