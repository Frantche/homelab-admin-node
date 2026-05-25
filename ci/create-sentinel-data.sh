#!/usr/bin/env bash
set -euo pipefail
mkdir -p /srv/admin/data/sentinel
echo "sentinel-$(date +%s)" > /srv/admin/data/sentinel/value.txt
