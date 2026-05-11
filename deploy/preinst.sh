#!/bin/sh
# Maintainer script for rcmdd: pre-install.
# Creates the `rcmd` system user and group used to run the relay
# service. Idempotent: a no-op on upgrades.
set -e

if ! getent group rcmd >/dev/null 2>&1; then
    groupadd --system rcmd
fi

if ! getent passwd rcmd >/dev/null 2>&1; then
    useradd \
        --system \
        --gid rcmd \
        --home-dir /var/lib/rcmd \
        --shell /usr/sbin/nologin \
        --comment "rcmd relay service" \
        rcmd
fi

exit 0
