#!/usr/bin/env bash
set -euo pipefail

exec /opt/homelab-admin-node/scripts/adminctl mode "$@"
