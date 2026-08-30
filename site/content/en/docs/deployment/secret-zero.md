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

The `bin/` directory is generated locally and is not stored in Git. After cloning the repository, build the CLI from the repository root:

```bash
cd /opt/homelab-admin-node
sudo make build-admin-node
```

The cloud-init convergence timer also runs this idempotent build, but running it explicitly makes the CLI available immediately for the secret-zero step.

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

OpenBao is different: its root token is generated during initialization. Before initialization, keep `openbao.root_token` and `openbao_config.root_token` empty or absent in the selected environment's secrets file, such as `pr/group_vars/secrets.sops.yaml` for production.

During initialization:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set init
sudo env INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/pr/inventory.ini \
  /opt/homelab-admin-node/bin/admin-node converge run
sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
```

After initialization, store only the encrypted result:

1. Confirm that initialization created
   `/opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml`. This local,
   SOPS-encrypted recovery file contains the unseal keysets and the generated
   root token.
2. Read the `openbao.root_token` scalar with the age key:

   ```bash
   sudo env SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
     sops --decrypt --extract '["openbao"]["root_token"]' \
     /opt/homelab-admin-node/secrets/openbao-unseal.sops.yaml
   ```

   Run this only in a private terminal: the command prints the token. The
   generated file also contains `openbao_config.root_token` with the same value
   for compatibility.
3. Edit the selected environment's encrypted file, for example production:

   ```bash
   cd /etc/admin-config/homelab-node-admin-config
   sudo env SOPS_AGE_KEY_FILE=/etc/sops/age/keys.txt \
     sops pr/group_vars/secrets.sops.yaml
   ```

4. Store the value once in the canonical location:

   ```yaml
   openbao:
     root_token: "paste-the-generated-value-here"
   ```

   `openbao.root_token` takes precedence. Do not duplicate it under
   `openbao_config.root_token`, which is only a fallback for older
   configurations.
5. Commit and push only the encrypted config-repo file. Do not add the local
   recovery file to this repository or the private config repo.

The OpenBao configuration role can consume the local generated recovery file
immediately after initialization, but copying the canonical token to the
encrypted config repo makes the environment configuration reproducible. Keep a
separate protected offline copy of the complete recovery file because the root
token alone cannot unseal OpenBao.

Never commit the age private key, decrypted SOPS files, raw OpenBao root token, unseal keys, Cloudflare credentials, Pi-hole tokens, or provider credentials.
