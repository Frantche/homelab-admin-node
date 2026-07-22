---
title: Renovate
weight: 60
---

Renovate is configured through `renovate.json`.

The repository expects Renovate to run externally. It should open pull requests for dependency and image updates, while CI scenarios validate whether updated components still converge, validate, backup, and restore correctly.

`ghcr.io/frantche/gitea-backup-restore-process` is tracked with a Renovate regex
custom manager because the image appears in a script, an Ansible template, and
restore runbooks instead of a standard Compose file.

Recommended review flow:

1. Review the Renovate pull request.
2. Check changed images, Go modules, actions, or packages.
3. Run applicable validation or CI scenario.
4. Merge only after lifecycle checks pass.
