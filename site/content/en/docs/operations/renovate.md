---
title: Renovate
weight: 60
---

Renovate is configured through `renovate.json`.

The repository expects Renovate to run externally. It should open pull requests for dependency and image updates, while CI scenarios validate whether updated components still converge, validate, backup, and restore correctly.

Renovate must not ignore tests (`ignoreTests: false`). Native GitHub automerge is
disabled (`platformAutomerge: false`): Renovate checks CI results on a subsequent
run and only merges a passing, up-to-date branch. Failed, pending, or absent
checks must prevent automatic merging. The central self-hosted configuration
also forces these settings so repository rules cannot bypass them.

Keep `main` protected with required `quality`, `oidc-contracts`, `bootstrap`, and
CodeQL checks, including enforcement for administrators when the bot uses an
administrator's token. Do not configure `requiredStatusChecks: null`: that legacy
option disables test verification. A successful Renovate workflow means the bot
ran successfully; it does not mean a dependency PR passed its lifecycle tests.

The quality job validates `renovate.json` with the Renovate configuration
validator. CrowdSec image references in Ansible defaults, Jinja fallback values,
and CI fixtures are tracked together with `security/container-images.txt`; the
image policy tests check both consistency and extraction coverage.

Minor, patch, and digest updates remain grouped in the Monday window. Major
updates still require approval in the Dependency Dashboard and manual merging.
After approving updates, allow another bot run to merge passing non-major PRs.

Go patch releases remain in the automatically merged `weekly non-major dependency updates` group. Go minor releases use the separate `weekly Go minor updates` group and require manual review because they can need source, tooling, or CI changes.

`ghcr.io/frantche/gitea-backup-restore-process` is tracked with a Renovate regex
custom manager because the image appears in the CLI fallback, a script, an
Ansible template, and restore runbooks instead of a standard Compose file.

Recommended review flow:

1. Review the Renovate pull request.
2. Check changed images, Go modules, actions, or packages.
3. Run applicable validation or CI scenario.
4. Merge only after lifecycle checks pass.
