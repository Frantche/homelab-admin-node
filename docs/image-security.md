# Container image security

`security/container-images.txt` is the exhaustive inventory of runtime stack images and production utility images. Every entry is pinned by tag and SHA-256 digest. The inventory contract checks that every image declared under `stacks/*/compose.yaml` or `stacks/*/compose.yaml.j2` remains listed.

The `image-security` workflow runs every Monday, on demand, and for every pull request that changes one of its monitored image-security paths. It scans each digest with Trivy and publishes:

- the complete vulnerability report in Trivy JSON format;
- a CycloneDX JSON SBOM for each image.

A fixable `CRITICAL` vulnerability blocks the scan. A critical vulnerability without an upstream fixed version remains visible in the report but does not block an otherwise unactionable deployment.

## Temporary exceptions

Exceptions belong in `security/image-vulnerability-policy.json` and identify one image repository and vulnerability:

```json
{
  "id": "CVE-2099-0001-temporary",
  "repository": "registry.example/app",
  "vulnerability": "CVE-2099-0001",
  "owner": "platform@example.com",
  "justification": "Upgrade is being validated against the restore scenario.",
  "expires_at": "2099-02-01"
}
```

The exception applies to the vulnerability across every tag and digest published in that repository. Other repositories and other vulnerabilities remain blocked. This repository-wide scope lets Renovate update images without copying exceptions into its pull requests, but it deliberately carries the accepted risk across version upgrades until the exception expires.

An exception without an owner or justification is invalid. Repository and vulnerability pairs must be unique. Expired exceptions fail before the scan begins. Remove an exception as soon as the fixed image is deployed.

## Local checks

Run the policy contract without downloading images:

```bash
make test-image-security-policy
```

With Trivy installed, run the complete scan and SBOM generation:

```bash
make scan-container-images
```
