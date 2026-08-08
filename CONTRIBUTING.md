# Contributing

Keep changes small, preserve the lifecycle modes described in `AGENTS.md`, and
never commit real credentials.

Before opening a pull request, run:

```bash
make ci-quality
make docs-check
```

Run `make test-oidc-contracts` for identity changes and `make ci-full` for
backup, restore, cloud-init, systemd, or lifecycle changes. If a required tool
or environment is unavailable, list the skipped validation and its reason in
the pull request.

User-facing documentation belongs in `site/content/en/docs/`. Files under
`docs/` are retained for technical design notes that are not published.
