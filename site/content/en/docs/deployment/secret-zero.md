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
  - path_regex: di/group_vars/secrets\.sops\.yaml$
    age: ["age1..."]
  - path_regex: pr/group_vars/secrets\.sops\.yaml$
    age: ["age1..."]
```

For the first deployment pass, both environments can use the admin/NAS age recipient. Do not create separate `di` and `pr` age keys until the access model is ready to enforce them.

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

## Secret lifecycle

Before the first convergence, the age private key is the only local secret that must already exist on the VM. The encrypted config repo must contain the application secrets needed to create databases, users, admin accounts, and provider credentials.

OpenBao is different: its root token is generated during initialization. Before initialization, keep `openbao.root_token` and `openbao_config.root_token` empty or absent in `di/group_vars/secrets.sops.yaml`.

During initialization:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set init
sudo /opt/homelab-admin-node/bin/admin-node converge run
sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
```

After initialization, store only the encrypted result:

1. Read the generated OpenBao root token from the local encrypted material.
2. Update `di/group_vars/secrets.sops.yaml` with SOPS.
3. Set the token values needed by the OpenBao configuration role.
4. Commit and push the encrypted config repo.

Never commit the age private key, decrypted SOPS files, raw OpenBao root token, unseal keys, Cloudflare credentials, Pi-hole tokens, or provider credentials.
