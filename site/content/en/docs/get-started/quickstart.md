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
   di/inventory.ini
   di/group_vars/all.yml
   di/group_vars/secrets.sops.yaml
   pr/inventory.ini
   pr/group_vars/all.yml
   pr/group_vars/secrets.sops.yaml
   .sops.yaml
   ```

6. Build the CLI, which is generated locally and is not stored in Git:

   ```bash
   cd /opt/homelab-admin-node
   sudo make build-admin-node
   ```

7. Generate an age key and install the private key on the node:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node secret install-age-key /path/to/age-key.txt
   ```

8. Copy or clone the config repo into:

   ```text
   /etc/admin-config/homelab-node-admin-config
   ```

9. Select the inventory for this VM. The example below uses `di`; use `pr` instead for the production environment:

   ```bash
   sudo systemctl edit admin-converge.service
   ```

   ```ini
   [Service]
   Environment=INVENTORY_PATH=/etc/admin-config/homelab-node-admin-config/di/inventory.ini
   Environment=HARBOR_DOMAIN=harbor.example.com
   Environment=OPENBAO_DOMAIN=bao.example.com
   Environment=KEYCLOAK_DOMAIN=keycloak.example.com
   Environment=GITEA_DOMAIN=git.example.com
   Environment=TRAEFIK_DOMAIN=traefik.example.com
   Environment=ADMIN_NODE_LAN_IP=192.0.2.10
   ```

   ```bash
   sudo systemctl daemon-reload
   ```

10. Confirm the initial mode is locked:

   ```bash
   cat /etc/admin-node/mode
   ```

11. Switch to init mode and converge:

   ```bash
   sudo /opt/homelab-admin-node/bin/admin-node mode set init
   sudo systemctl start admin-converge.service
   ```

   Starting the service uses the `INVENTORY_PATH` configured in its systemd drop-in. A direct `sudo ... admin-node converge run` command does not inherit that service environment.

12. Initialize OpenBao if required by your deployment:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
    ```

13. Update the selected environment's `group_vars/secrets.sops.yaml` with the generated OpenBao token (`pr/group_vars/secrets.sops.yaml` for production), then commit and push only the encrypted config repo.
14. Switch to normal mode and converge:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node mode set normal
    sudo systemctl start admin-converge.service
    ```

15. Validate the node:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node validate all
    ```

16. Run a first backup after validation succeeds:

    ```bash
    sudo /opt/homelab-admin-node/bin/admin-node backup run
    ```

For the full guided path, follow the deployment guide from Proxmox through validation.
