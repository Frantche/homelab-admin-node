---
title: Security And Secrets
weight: 30
---

Security expectations:

- Never commit the age private key.
- Never commit unencrypted production secrets.
- Keep the config repo private.
- Store `group_vars/secrets.sops.yaml` encrypted with SOPS.
- Keep cloud-init user-data free of production secrets.
- Review changes to hardening, firewall, backup, restore, and OIDC configuration carefully.

The repository includes public example secrets only for shape and documentation. Real values belong in the private config repo.
