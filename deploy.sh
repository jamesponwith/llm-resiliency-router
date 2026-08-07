#!/bin/sh
# Deploy a released router binary onto this host.
#   ./deploy.sh            # latest release
#   ./deploy.sh v0.1.0     # specific tag
#   DEPLOY_DIR=~/bin ./deploy.sh   # install dir (default /usr/local/bin)
# If a systemd unit named llm-resiliency-router is enabled, it is restarted.
# Example unit (/etc/systemd/system/llm-resiliency-router.service):
#   [Service]
#   ExecStart=/usr/local/bin/llm-resiliency-router -config /etc/llm-resiliency-router/config.yaml
#   Restart=on-failure
#   [Install]
#   WantedBy=multi-user.target
set -eu
REPO=jamesponwith/llm-resiliency-router
BIN=llm-resiliency-router
DIR=${DEPLOY_DIR:-/usr/local/bin}
VER=${1:-latest}
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64 | arm64) ARCH=arm64 ;; esac

if [ "$VER" = latest ]; then
	API="https://api.github.com/repos/$REPO/releases/latest"
else
	API="https://api.github.com/repos/$REPO/releases/tags/$VER"
fi
URL=$(curl -fsSL "$API" | grep -o "https://[^\"]*_${OS}_${ARCH}\.tar\.gz" | head -1)
[ -n "$URL" ] || { echo "no ${OS}_${ARCH} asset for $VER" >&2; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$URL" | tar -xz -C "$TMP"
install "$TMP/$BIN" "$DIR/$BIN"
echo "installed $URL -> $DIR/$BIN"

if command -v systemctl >/dev/null 2>&1 && systemctl is-enabled --quiet "$BIN" 2>/dev/null; then
	sudo systemctl restart "$BIN"
	echo "restarted systemd unit $BIN"
fi
