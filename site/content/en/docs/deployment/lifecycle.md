---
title: First Convergence
weight: 50
---

After cloud-init, secret zero, and the config repo are ready, use the lifecycle modes to bring the node up safely.

## Locked mode

The node should start in `locked` mode:

```bash
cat /etc/admin-node/mode
```

This mode keeps the system safe before production secrets are present.

## Init mode

Switch to `init` mode:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set init
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Init mode deploys the first service state needed to bootstrap the node.

Initialize OpenBao if needed:

```bash
sudo /opt/homelab-admin-node/bin/admin-node openbao init-if-needed
```

When OpenBao generates or updates encrypted material, commit the encrypted result to the private config repo.

## Normal mode

Switch to steady-state operation:

```bash
sudo /opt/homelab-admin-node/bin/admin-node mode set normal
sudo /opt/homelab-admin-node/bin/admin-node converge run
```

Normal mode deploys and validates the operational service set, then runs backup tasks according to configuration.

## Validate

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate all
```

If a specific subsystem fails, run the narrower validator:

```bash
sudo /opt/homelab-admin-node/bin/admin-node validate apis
sudo /opt/homelab-admin-node/bin/admin-node validate dns
sudo /opt/homelab-admin-node/bin/admin-node validate tunnel
sudo /opt/homelab-admin-node/bin/admin-node validate hardening
sudo /opt/homelab-admin-node/bin/admin-node validate observability
```
