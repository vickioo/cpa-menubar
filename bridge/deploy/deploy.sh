#!/bin/zsh
set -euo pipefail

ROOT="${0:A:h:h}"
REMOTE="${1:?Usage: $0 <ssh-host>}"
BUILD_DIR="$(mktemp -d /tmp/cpa-desktop-bridge.XXXXXX)"
trap 'rm -rf "$BUILD_DIR"' EXIT

cd "$ROOT"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/cpa-desktop-bridge" ./cmd/cpa-desktop-bridge
scp "$BUILD_DIR/cpa-desktop-bridge" "$ROOT/deploy/cpa-desktop-bridge.service" "$ROOT/deploy/nginx-location.conf" "$REMOTE:/tmp/"

ssh "$REMOTE" 'bash -s' <<'REMOTE_SCRIPT'
set -euo pipefail
install -d -m 0755 /opt/cpa-desktop-bridge /etc/nginx/snippets /etc/cpa-desktop-bridge
install -m 0755 /tmp/cpa-desktop-bridge /opt/cpa-desktop-bridge/cpa-desktop-bridge
install -m 0644 /tmp/cpa-desktop-bridge.service /etc/systemd/system/cpa-desktop-bridge.service
install -m 0644 /tmp/nginx-location.conf /etc/nginx/snippets/cpa-desktop-bridge.conf

if [ ! -s /etc/cpa-desktop-bridge.token ]; then
    umask 077
    openssl rand -base64 48 | tr -d '\n' > /etc/cpa-desktop-bridge.token
fi
chmod 0600 /etc/cpa-desktop-bridge.token

systemctl daemon-reload
systemctl enable cpa-desktop-bridge.service
systemctl restart cpa-desktop-bridge.service
curl --fail --silent http://127.0.0.1:8330/healthz >/dev/null
systemctl is-active --quiet cpa-desktop-bridge.service
rm -f /tmp/cpa-desktop-bridge /tmp/cpa-desktop-bridge.service /tmp/nginx-location.conf
REMOTE_SCRIPT

echo "CPA Desktop Bridge deployed to $REMOTE"
echo "Add 'include /etc/nginx/snippets/cpa-desktop-bridge.conf;' to your HTTPS server block, then validate and reload nginx."
