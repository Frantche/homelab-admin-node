---
title: Secret Zero
weight: 30
---

The secret zero is the age private key used by SOPS to decrypt the private config repository secrets.

Generate a key pair:

```bash
age-keygen -o age-key.txt
```

Record the public key printed by `age-keygen` in the config repo `.sops.yaml`:

```yaml
creation_rules:
  - path_regex: group_vars/secrets\.sops\.yaml$
    age: ["age1..."]
```

Install the private key on the admin node:

```bash
sudo /opt/homelab-admin-node/bin/admin-node secret install-age-key ./age-key.txt
```

The command installs the key at:

```text
/etc/sops/age/keys.txt
```

Expected permissions are `0400 root:root`.

Keep an offline copy of the private key in a password manager or another secure recovery location. Without this key, the node cannot decrypt the config repo secrets during convergence.
