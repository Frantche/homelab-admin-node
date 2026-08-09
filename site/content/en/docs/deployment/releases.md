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

This command performs remote checksum and evidence verification. The validator's
`--structure-only` mode is only for offline authoring feedback and must never be
used by a promotion path.

The manifest binds the tag and full commit to the configuration schema, dated
Arch cloud image and checksum, `pacman -Q` package-state artifact, Python, Go
and Ansible dependencies, container digests,
and evidence artifacts. The validator downloads the image and package inventory
and verifies their SHA-256 values. Each evidence entry also has an
`artifact_sha256`, the producing GitHub Actions run ID/API URL, and the trusted
workflow path and source SHA, plus the GitHub Actions artifact ID/API URL.
Promotion verifies that the workflow source belongs to trusted `main` history,
that the run used the expected workflow, was triggered by `workflow_dispatch`,
and completed successfully. It then downloads the authenticated Actions ZIP and
requires the staged evidence bytes to be present in that exact run artifact;
the evidence inside that trusted artifact binds the tested candidate SHA.
Bootstrap and upgrade evidence reference the actual component
inventory artifact, which is downloaded and hashed. The evidence JSON contains
`kind` and the same commit, result, run identity, and variant-specific claims as
the manifest. Evidence and
the package inventory must be hosted below the trusted repository prefix passed
by the promotion workflow. Qualification is
valid only when two fresh nodes built from the same release have identical
component-inventory checksums, and bootstrap, disaster recovery, upgrade, and
rollback all passed. Disaster-recovery evidence must include both the `standard`
and `offline-images` variants exactly once. Evidence must test the exact release commit.
Publish qualification inputs first in an immutable prerelease named
`qualification-vMAJOR.MINOR.PATCH`. This staging name is rejected by the node
installer and avoids referring to assets of a production release that does not
exist yet. Retain these artifacts for the entire support period. Publish notable
changes, migrations, known limitations, and the previous
supported release in the GitHub release notes.

The upgraded node must produce the same component-inventory checksum as both
fresh nodes. A green playbook with a different OS, collection or component set
does not qualify an in-place upgrade.

The validator also compares `container_images` with every literal image used by
`stacks/*/compose.yaml*`; copy the complete digest-pinned set into the manifest.

Production publication uses the `bootstrap-user-journey` workflow on the
default branch with `scope=disaster-recovery`, `promote=true`, the immutable
candidate SHA, `release_tag=vMAJOR.MINOR.PATCH`, and the HTTPS URL plus SHA-256
of the completed manifest. Promotion reruns both DR variants, verifies the
downloaded manifest against the candidate and local annotated tag, then creates
a draft GitHub release containing the manifest. It pushes the tag only after the
draft exists and publishes the draft last. An interrupted publication therefore
leaves either no tag or a non-public qualification URL that the installer
refuses. Do not
create production tags manually or reuse the older
`homelab-production-<sha>` marker convention.

## Supported upgrade

Before the first convergence after adopting this release policy, create
`/etc/admin-node/release-ref`. Its absence is a hard failure; there is no legacy
fallback to a moving branch. Production uses a qualified tag plus its immutable
commit pin and downloaded manifest. Use the literal value `main` only when
deliberately selecting the development channel; direct SHA selection is reserved
for CI.
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
   curl --fail --location --proto '=https' --tlsv1.2 \
     --output /tmp/qualification.json \
     https://github.com/Frantche/homelab-admin-node/releases/download/v1.2.0/qualification.json
   curl --fail --location --proto '=https' --tlsv1.2 \
     --output /tmp/release-metadata.json \
     https://api.github.com/repos/Frantche/homelab-admin-node/releases/tags/v1.2.0
   printf '%s  %s\n' '<SHA256_FROM_RELEASE_NOTES>' /tmp/qualification.json | sha256sum --check --strict
   (cd /opt/homelab-admin-node && python3 scripts/verify-installed-release.py \
     /tmp/qualification.json --release-metadata /tmp/release-metadata.json \
     --tag v1.2.0 --commit "$target" --manifest-sha256 '<SHA256_FROM_RELEASE_NOTES>')
   sudo install -m 0644 /tmp/qualification.json /etc/admin-node/qualification.json
   printf '%s\n' production | sudo tee /etc/admin-node/release-channel
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
lifecycle logic and the playbook come from the same release. It then installs
the exact `ansible/requirements.yml` collection set into a directory addressed
by the requirements checksum and runs Ansible with only that collection root.

Do not upgrade system packages or configuration independently while evaluating
a release; that would no longer match its qualified component inventory.
`admin-node version` and convergence reject repository drift.

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
