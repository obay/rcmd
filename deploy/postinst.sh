#!/bin/sh
# Maintainer script for rcmdd: post-install.
# Creates state directories with correct ownership, reloads systemd so
# rcmdd.service is visible, and prints next-step instructions.
set -e

# State directories.
mkdir -p /etc/rcmd /var/lib/rcmd /var/lib/rcmd/autocert
chown rcmd:rcmd /etc/rcmd /var/lib/rcmd /var/lib/rcmd/autocert
chmod 0755 /etc/rcmd /var/lib/rcmd
chmod 0700 /var/lib/rcmd/autocert

# Make sure systemd sees the new unit. Best-effort: doesn't matter on
# non-systemd hosts.
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

# Helpful nudge for first-time installs only (when no state file exists).
if [ ! -f /etc/rcmd/rcmdd.json ]; then
    cat <<'EOF'

rcmdd installed. To finish setup:

    sudo rcmdd init --domain your.relay.example.com    # writes /etc/rcmd/rcmdd.json + prints join token
    sudo systemctl enable --now rcmdd                  # starts the service

For an IP-only test setup with no domain:

    sudo rcmdd init --insecure --public-url http://your-host:8080

EOF
fi

exit 0
