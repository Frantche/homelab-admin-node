---
title: Production Releases and Rollback
weight: 60
---

Production nodes must run a qualified immutable release. A release is an
annotated `vMAJOR.MINOR.PATCH` Git tag whose GitHub release contains release
notes and a completed qualification manifest. Tags are never moved or reused;
a correction receives a new patch tag. Protect release tags in the repository
settings and retain release artifacts for as long as the release is supported.

`main` is the development channel. It is useful for disposable test nodes, but
it is a moving branch and is not a production release.

## Qualification contract

Copy `release/qualification.example.json`, replace every example value, and
attach the completed file to the GitHub release. Validate it from the exact tag:

```bash
git checkout --detach v1.2.0
make validate-release MANIFEST=/path/to/qualification.json RELEASE_COMMIT=v1.2.0
```

The manifest binds the tag and full commit to the configuration schema, dated
Arch cloud image and checksum, Arch Linux Archive package snapshot, `pacman -Q` package-state artifact, Python and
Ansible dependencies, container digests, and evidence URLs. The package-state
artifact has its own SHA-256 so its contents cannot be replaced silently. Qualification is
valid only when two fresh nodes built from the same release have identical
component-inventory checksums, and bootstrap, disaster recovery, upgrade, and
rollback all passed. Disaster-recovery evidence must include both the `standard`
and `offline-images` variants exactly once. Evidence must test the exact release commit. Publish these
artifacts and notable changes, migrations, known limitations, and the previous
supported release in the GitHub release notes.

The validator also compares `container_images` with every literal image used by
`stacks/*/compose.yaml*`; copy the complete digest-pinned set into the manifest.

Production publication uses the `bootstrap-user-journey` workflow on the
default branch with `scope=disaster-recovery`, `promote=true`, the immutable
candidate SHA, `release_tag=vMAJOR.MINOR.PATCH`, and the HTTPS URL plus SHA-256
of the completed manifest. Promotion reruns both DR variants, verifies the
downloaded manifest against the candidate and local annotated tag, then pushes
that tag and creates the GitHub release with the manifest attached. Do not
create production tags manually or reuse the older
`homelab-production-<sha>` marker convention.

## Supported upgrade

Before the first convergence after adopting this release policy, create
`/etc/admin-node/release-ref`. Its absence is a hard failure; there is no legacy
fallback to a moving branch. Use a qualified full commit for production, or the
literal value `main` only when deliberately selecting the development channel.
When adopting the policy from an older binary, check out the first qualified
release and run `scripts/build-admin-node.sh` once before using this procedure.
Perform that one-time handoff after the backup in step 2 and after resolving
`target` in step 3:

```bash
sudo git -C /opt/homelab-admin-node checkout --detach "$target"
sudo /opt/homelab-admin-node/scripts/build-admin-node.sh
```

Do not repeat this manual checkout for later release-to-release upgrades; the
qualified CLI then performs and verifies its own rebuild/re-exec handoff.

1. Read the target release notes and confirm that the current release is listed
   as a supported upgrade source. Apply documented config-schema migrations in
   the private config repository and commit its encrypted state.
2. Run and verify a fresh remote backup before changing code:

   ```bash
   sudo admin-node backup run
   sudo admin-node backup list
   sudo admin-node validate all
   ```

3. Fetch the target tag, verify that it resolves to the commit stated in its
   qualification manifest, then pin that full commit. Keeping the tag name makes
   the operator-facing version clear:

   ```bash
   git -C /opt/homelab-admin-node fetch origin tag v1.2.0
   target="$(git -C /opt/homelab-admin-node rev-parse 'v1.2.0^{commit}')"
   printf '%s\n' v1.2.0 | sudo tee /etc/admin-node/release-name
   printf '%s\n' "$target" | sudo tee /etc/admin-node/release-ref
   sudo admin-node converge run
   sudo admin-node version
   sudo admin-node validate all
   ```

4. Confirm that `release_ref` and `revision` are identical and retain the
   pre-upgrade backup until the release has completed its observation period.

Convergence checks out the selected commit, rebuilds `admin-node`, and replaces
the running process before Ansible starts. This handoff ensures that both the Go
lifecycle logic and the playbook come from the same release.

Do not upgrade system packages or configuration independently while evaluating
a release; that would no longer match its qualified component inventory.

## Failure recovery and rollback

If convergence fails before data migration, restore the previous full commit to
`release-ref`, run convergence, and validate. Never move the old tag:

```bash
printf '%s\n' PREVIOUS_40_CHARACTER_COMMIT | sudo tee /etc/admin-node/release-ref
printf '%s\n' v1.1.0 | sudo tee /etc/admin-node/release-name
sudo admin-node converge run
sudo admin-node validate all
```

If the upgrade changed persistent data or the configuration schema, code-only
rollback is unsafe. Follow the normal disaster-recovery procedure using the
pre-upgrade backup and the previous release commit, then validate restored
services before returning to `normal`. Preserve the failed node and logs until
recovery evidence has been collected.

## Support window

The latest qualified release and its immediate predecessor are supported for
upgrade and rollback. Older versions require a staged upgrade through a
supported predecessor or a fresh restore rehearsal. A schema change increments
`release/config-schema-version` and release notes must describe its migration
and backward-compatibility boundary.
