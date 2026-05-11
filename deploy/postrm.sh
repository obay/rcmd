#!/bin/sh
# Maintainer script for rcmdd: post-removal.
# On 'purge' (full uninstall + config removal), clean up the state
# directories and the system user. On 'remove' (keep config), leave
# state in place so re-installing recovers cleanly.
set -e

case "$1" in
    purge)
        rm -rf /etc/rcmd /var/lib/rcmd
        if getent passwd rcmd >/dev/null 2>&1; then
            userdel rcmd 2>/dev/null || true
        fi
        if getent group rcmd >/dev/null 2>&1; then
            groupdel rcmd 2>/dev/null || true
        fi
        ;;
    *)
        # remove, upgrade, etc — no-op.
        ;;
esac

if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

exit 0
