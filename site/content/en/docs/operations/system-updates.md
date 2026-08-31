---
title: System Updates
weight: 25
---

`admin-system-update.timer` keeps the Arch Linux host packages current. It runs
once per week by default and uses `Persistent=true`, so systemd catches up after
the node returns from downtime.

The associated service:

1. waits for the global admin-node operation lock;
2. records the installed package state;
3. runs a complete `pacman -Syu` transaction;
4. verifies whether installed package versions changed;
5. reboots the node when changes were installed and automatic reboot is enabled.

The full upgrade avoids partial Arch Linux upgrades. The operation lock prevents
the package transaction from overlapping convergence, backup, or restore. The
timer is independent of the lifecycle mode and therefore also protects a node
that remains in `locked` mode.

Inspect the schedule and the latest run with:

```bash
systemctl list-timers admin-system-update.timer
systemctl status admin-system-update.service
journalctl -u admin-system-update.service
```

Run the maintenance operation immediately with:

```bash
sudo systemctl start admin-system-update.service
```

Customize or disable the policy in the private config repository:

```yaml
system_updates:
  enabled: true
  on_calendar: "Sun *-*-* 02:00:00"
  randomized_delay_sec: "15m"
  operation_lock_timeout_sec: 1800
  service_timeout_sec: 7200
  auto_reboot: true
```

When `auto_reboot` is false, the upgrade still succeeds but the operator must
reboot manually to activate a new kernel and all updated userspace components.
Failures are visible through the service status and journal; systemd retries at
the next scheduled activation.
