#!/usr/bin/env bash
# Install or update fileshare on an existing server.
# Idempotent: existing .env is never overwritten and existing services are
# never restarted. Run as root: sudo bash deploy/install-server.sh
set -euo pipefail

# sudo's secure_path does not include the Nix profile. Prepend it so both the
# preflight checks and child commands can find nix.
export PATH="/nix/var/nix/profiles/default/bin:${PATH:-/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin}"

REPO_DIR=/srv/fileshare
DATA_DIR=/srv/fileshare-data
UNIT=fileshare
PORT=8087
SERVICE_USER=fileshare
REPO_URL=https://github.com/paleblue-2032/fileshare.git
ALLOW_LAN_HTTP=${ALLOW_LAN_HTTP:-0}

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root: sudo bash $0" >&2
  exit 1
fi

ADMIN_USER=${ADMIN_USER:-${SUDO_USER:-}}
if [ -z "$ADMIN_USER" ] || [ "$ADMIN_USER" = root ]; then
  echo "set ADMIN_USER to the unprivileged account that owns $REPO_DIR" >&2
  exit 1
fi
if ! id -u "$ADMIN_USER" >/dev/null 2>&1; then
  echo "unknown user: $ADMIN_USER" >&2
  exit 1
fi

for cmd in git nix curl systemctl systemd-analyze ss df useradd groupadd getent runuser openssl ip awk cmp install journalctl hostname; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing command: $cmd" >&2
    exit 1
  fi
done

admin_home=$(getent passwd "$ADMIN_USER" | cut -d: -f6)
run_as_admin() {
  runuser -u "$ADMIN_USER" -- env HOME="$admin_home" \
    PATH=/nix/var/nix/profiles/default/bin:/usr/local/bin:/usr/bin:/bin "$@"
}

echo "== 1. port check =="
if ss -H -ltn "sport = :$PORT" | grep -q .; then
  if systemctl is-active --quiet "$UNIT"; then
    echo "port $PORT is in use by $UNIT (expected on update)"
  else
    echo "port $PORT is already in use by another service; refusing to continue" >&2
    ss -ltnp "sport = :$PORT" >&2 || true
    exit 1
  fi
fi

echo "== 2. repository =="
if [ ! -d "$REPO_DIR/.git" ]; then
  install -d -o "$ADMIN_USER" -g "$ADMIN_USER" -m 0755 "$REPO_DIR"
  run_as_admin git clone "$REPO_URL" "$REPO_DIR"
else
  run_as_admin git -C "$REPO_DIR" pull --ff-only
fi

echo "== 3. build =="
run_as_admin nix build "$REPO_DIR#default" -o "$REPO_DIR/result" --print-build-logs

echo "== 4. service user and data directory =="
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin "$SERVICE_USER"
  echo "created system user $SERVICE_USER"
fi
if ! getent group "$SERVICE_USER" >/dev/null 2>&1; then
  groupadd --system "$SERVICE_USER"
  echo "created system group $SERVICE_USER"
fi
if [ ! -d "$DATA_DIR" ]; then
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0750 "$DATA_DIR"
  echo "created $DATA_DIR"
else
  owner=$(stat -c %U "$DATA_DIR")
  if [ "$owner" != "$SERVICE_USER" ]; then
    echo "$DATA_DIR is owned by $owner, not $SERVICE_USER; refusing to chown user data" >&2
    exit 1
  fi
  chmod 0750 "$DATA_DIR"
fi

echo "== 5. environment =="
initial_password=""
if [ ! -f "$REPO_DIR/.env" ]; then
  initial_password=$(openssl rand -base64 32 | tr -d '\n=+/' | cut -c1-24)
  hash=$(printf '%s' "$initial_password" | "$REPO_DIR/result/bin/fileshare" hash-password)
  avail=$(df -B1 --output=avail "$DATA_DIR" 2>/dev/null | tail -1 | tr -d ' ')
  case "$avail" in ''|*[!0-9]*) avail=$(df -B1 "$DATA_DIR" | awk 'NR==2 {print $4}') ;; esac
  if ! [[ "${avail:-}" =~ ^[0-9]+$ ]]; then
    echo "could not determine free space for $DATA_DIR" >&2
    exit 1
  fi
  quota=$((avail * 60 / 100))
  if [ "$quota" -gt 128849018880 ]; then quota=128849018880; fi
  if [ "$quota" -lt 5368709120 ]; then quota=5368709120; fi
  umask 077
  listen_addr=127.0.0.1
  if [ "$ALLOW_LAN_HTTP" = 1 ]; then
    listen_addr=0.0.0.0
  fi
  cat >"$REPO_DIR/.env" <<ENV
FILESHARE_ADDR=$listen_addr:$PORT
FILESHARE_DATA_DIR=$DATA_DIR
FILESHARE_PASSWORD_HASH=$hash
FILESHARE_QUOTA_BYTES=$quota
FILESHARE_MAX_UPLOAD_BYTES=5368709120
FILESHARE_MIN_FREE_BYTES=3221225472
FILESHARE_RETENTION_DAYS=30
FILESHARE_TRASH_DAYS=30
FILESHARE_SWEEP_MINUTES=30
FILESHARE_WEBDAV=1
ENV
  chown root:root "$REPO_DIR/.env"
  chmod 600 "$REPO_DIR/.env"
  echo "created $REPO_DIR/.env"
else
  chmod 600 "$REPO_DIR/.env"
  echo "keeping existing $REPO_DIR/.env"
  if grep -q '^FILESHARE_ADDR=0.0.0.0' "$REPO_DIR/.env"; then
    echo "warning: the existing .env still binds 0.0.0.0 (plaintext LAN HTTP)" >&2
    echo "         run: sudo botctl harden fileshare" >&2
  fi
fi

echo "== 6. systemd unit =="
systemd-analyze verify "$REPO_DIR/deploy/fileshare.service"
install -o root -g root -m 0644 "$REPO_DIR/deploy/fileshare.service" "/etc/systemd/system/$UNIT.service"
systemctl daemon-reload
systemctl enable "$UNIT" >/dev/null
if ! systemctl restart "$UNIT"; then
  journalctl -u "$UNIT" -n 40 --no-pager >&2 || true
  exit 1
fi
if ! systemctl is-active --quiet "$UNIT"; then
  echo "$UNIT did not stay active; recent logs:" >&2
  journalctl -u "$UNIT" -n 40 --no-pager >&2 || true
  if [ -n "$initial_password" ]; then
    echo "initial password (service is not healthy): $initial_password" >&2
  fi
  exit 1
fi

echo "== 7. firewall (additive only) =="
LAN_CIDR=${LAN_CIDR:-}
if [ "$ALLOW_LAN_HTTP" = 1 ] && [ -z "$LAN_CIDR" ]; then
  iface=$(ip -4 route show default | awk '{print $5; exit}')
  if [ -n "${iface:-}" ]; then
    LAN_CIDR=$(ip -4 route show dev "$iface" scope link | awk '$1 ~ /\// {print $1; exit}')
  fi
  if [ -z "$LAN_CIDR" ]; then
    echo "could not detect the LAN CIDR; set LAN_CIDR (for example 192.168.1.0/24) and re-run" >&2
    exit 1
  fi
fi
if command -v ufw >/dev/null 2>&1; then
  if [ "$ALLOW_LAN_HTTP" = 1 ]; then
    echo "warning: ALLOW_LAN_HTTP=1 exposes plaintext HTTP and WebDAV to the LAN" >&2
    if ! ufw show added 2>/dev/null | grep -Fq "from $LAN_CIDR to any port $PORT"; then
      ufw allow from "$LAN_CIDR" to any port "$PORT" proto tcp comment 'fileshare LAN'
    else
      echo "LAN rule already present"
    fi
    if ! ufw show added 2>/dev/null | grep -Fq "on tailscale0 to any port $PORT"; then
      ufw allow in on tailscale0 to any port "$PORT" proto tcp comment 'fileshare tailnet'
    else
      echo "tailnet rule already present"
    fi
  fi
  if ! ufw show added 2>/dev/null | grep -Fq "on tailscale0 to any port 443"; then
    ufw allow in on tailscale0 to any port 443 proto tcp comment 'fileshare tailscale serve'
  else
    echo "tailscale serve rule already present"
  fi
  if ufw status 2>/dev/null | grep -qi inactive; then
    echo "warning: ufw is inactive; rules were recorded but are not enforced" >&2
  fi
else
  echo "warning: ufw not found; no firewall rules were added" >&2
fi

echo "== 8. botctl =="
if [ -f "$REPO_DIR/deploy/botctl" ]; then
  bash -n "$REPO_DIR/deploy/botctl"
  if [ ! -f /usr/local/sbin/botctl ] || ! cmp -s "$REPO_DIR/deploy/botctl" /usr/local/sbin/botctl; then
    if [ -f /usr/local/sbin/botctl ]; then
      backup="/root/botctl.bak.$(date +%Y%m%d%H%M%S)"
      cp -p /usr/local/sbin/botctl "$backup"
      echo "backed up the previous botctl to $backup"
    fi
    install -o root -g root -m 0755 "$REPO_DIR/deploy/botctl" /usr/local/sbin/botctl
    echo "installed /usr/local/sbin/botctl"
  else
    echo "botctl already up to date"
  fi
fi
if ! grep -rqs '/usr/local/sbin/botctl' /etc/sudoers /etc/sudoers.d 2>/dev/null; then
  echo "warning: no sudoers rule for /usr/local/sbin/botctl was found" >&2
fi

echo "== 9. health check =="
healthy=0
for _ in $(seq 1 20); do
  if curl -fsS --max-time 2 "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
    healthy=1
    break
  fi
  sleep 1
done
if [ "$healthy" -ne 1 ]; then
  echo "fileshare did not become healthy; recent logs:" >&2
  journalctl -u "$UNIT" -n 40 --no-pager >&2 || true
  if [ -n "$initial_password" ]; then
    echo "initial password (service is not healthy): $initial_password" >&2
  fi
  exit 1
fi

server_ip=$(hostname -I | awk '{print $1}')
echo
echo "fileshare is running."
if [ "$ALLOW_LAN_HTTP" = 1 ]; then
  echo "  URL:      http://${server_ip:-<server-ip>}:$PORT  (plaintext, LAN only)"
  echo "  warning:  prefer the Tailscale HTTPS URL for credentials and files"
else
  echo "  URL:      use the Tailscale HTTPS URL (tailscale serve status)"
  echo "            http://127.0.0.1:$PORT is loopback-only"
fi
echo "  data:     $DATA_DIR"
echo "  service:  systemctl status $UNIT"
echo "  manage:   sudo botctl status fileshare"
if [ -n "$initial_password" ]; then
  echo
  echo "  initial password: $initial_password"
  echo "  change it: printf '%s' 'new-password' | $REPO_DIR/result/bin/fileshare hash-password"
  echo "             then edit $REPO_DIR/.env and run: sudo botctl restart fileshare"
fi
echo
echo "Next, for access outside the LAN:"
echo "  sudo bash $REPO_DIR/deploy/install-tailscale.sh"
echo "  sudo tailscale up --accept-dns=false --hostname=$(hostname)"
echo "  sudo bash $REPO_DIR/deploy/enable-tailscale-serve.sh"
if [ "$ALLOW_LAN_HTTP" != 1 ] && command -v tailscale >/dev/null 2>&1; then
  if ! tailscale serve status 2>/dev/null | grep -Fq 'proxy http://127.0.0.1:8087'; then
    echo "warning: Tailscale Serve is not configured yet; the loopback-only service is not reachable until it is" >&2
  fi
fi
