#!/bin/sh
# Maintainer script for rcmdd: pre-removal.
# Stop and disable the service before its unit file is removed.
set -e

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl stop rcmdd.service 2>/dev/null || true
    systemctl disable rcmdd.service 2>/dev/null || true
fi

exit 0
