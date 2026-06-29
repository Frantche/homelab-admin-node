---
title: Quickstart
weight: 40
---

Use this path when you already know Proxmox cloud-init, Git, SOPS, and Ansible.

1. Create an Arch Linux cloud-image template in Proxmox.
2. Clone the template into `admin-01`.
3. Attach `cloud-init/admin-01.user-data.yaml` as the VM user-data snippet.
4. Boot the VM and wait for cloud-init to finish.
5. Create a private config repository with:

   ```text
   hosts/inventory.ini
   group_vars/all.yml
   group_vars/secrets.sops.yaml
   .sops.yaml
   ```

6. Generate an age key and install the private key on the node:

   ```bash
   sudo ./bin/admin-node secret install-age-key /path/to/age-key.txt
   ```

7. Copy or clone the config repo into:

   ```text
   /etc/admin-config/homelab-node-admin-config
   ```

8. Confirm the initial mode is locked:

   ```bash
   cat /etc/admin-node/mode
   ```

9. Switch to init mode and converge:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set init
   sudo /opt/homelab-admin-node/bin/admin-node converge run
   ```

10. Initialize OpenBao if required by your deployment:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
    ```

11. Commit the generated or updated encrypted secrets to the private config repo.
12. Switch to normal mode and converge:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node mode set normal
    sudo /opt/homelab-admin-node/bin/admin-node converge run
    ```

13. Validate the node:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node validate all
    ```

For the full guided path, follow the deployment guide from Proxmox through validation.
