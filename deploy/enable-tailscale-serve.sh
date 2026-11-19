#!/usr/bin/env bash
# Expose fileshare over HTTPS on the tailnet after `tailscale up`.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root: sudo bash $0" >&2
  exit 1
fi

if ! command -v tailscale >/dev/null 2>&1; then
  echo "tailscale is not installed; run deploy/install-tailscale.sh first" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required to read the tailscale status" >&2
  exit 1
fi

status_json=$(tailscale status --json 2>/dev/null || true)
state=$(printf '%s' "$status_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("BackendState",""))' 2>/dev/null || true)
if [ "$state" != "Running" ]; then
  echo "tailscale is not connected yet (state: ${state:-unknown})." >&2
  echo "Run: sudo tailscale up --accept-dns=false --hostname=$(hostname)" >&2
  exit 1
fi

echo "== enabling tailscale serve =="
if ! tailscale serve --bg http://127.0.0.1:8087; then
  echo "retrying with the legacy port syntax" >&2
  tailscale serve --bg 8087
fi
tailscale serve status

dns=$(printf '%s' "$status_json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("Self",{}).get("DNSName","").rstrip("."))')
echo
echo "Tailscale Serve is configured."
if [ -n "$dns" ]; then
  echo "  Web UI:  https://$dns/"
  echo "  WebDAV:  https://$dns/dav/"
fi
echo
echo "If HTTPS certificates are not enabled for this tailnet yet, open the link"
echo "printed by 'tailscale serve status' and enable them in the admin console."
