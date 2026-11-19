#!/usr/bin/env bash
# Install Tailscale on Ubuntu and add the fileshare firewall rules.
# This does not log in to a tailnet; run `tailscale up` yourself afterwards.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root: sudo bash $0" >&2
  exit 1
fi

if ! command -v apt-get >/dev/null 2>&1; then
  echo "this script supports apt-based systems (Ubuntu/Debian)" >&2
  exit 1
fi

if ! command -v tailscale >/dev/null 2>&1; then
  . /etc/os-release
  codename=${VERSION_CODENAME:-noble}
  keyring=/usr/share/keyrings/tailscale-archive-keyring.gpg
  sources=/etc/apt/sources.list.d/tailscale.list

  install -d -m 0755 /usr/share/keyrings
  tmp=$(mktemp)
  curl -fsSL "https://pkgs.tailscale.com/stable/ubuntu/${codename}.noarmor.gpg" -o "$tmp"
  if [ -f "$keyring" ] && ! cmp -s "$tmp" "$keyring"; then
    cp -p "$keyring" "/root/tailscale-keyring.bak.$(date +%Y%m%d%H%M%S)"
  fi
  install -o root -g root -m 0644 "$tmp" "$keyring"
  rm -f "$tmp"

  if [ -f "$sources" ]; then
    cp -p "$sources" "/root/tailscale.list.bak.$(date +%Y%m%d%H%M%S)"
  fi
  printf 'deb [signed-by=%s] https://pkgs.tailscale.com/stable/ubuntu %s main\n' \
    "$keyring" "$codename" >"$sources"
  chmod 0644 "$sources"

  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y tailscale
else
  echo "tailscale is already installed"
fi

systemctl enable --now tailscaled
systemctl is-active tailscaled
tailscale version

if command -v ufw >/dev/null 2>&1; then
  if ! ufw show added 2>/dev/null | grep -Fq "on tailscale0 to any port 8087"; then
    ufw allow in on tailscale0 to any port 8087 proto tcp comment 'fileshare tailnet'
  fi
  if ! ufw show added 2>/dev/null | grep -Fq "on tailscale0 to any port 443"; then
    ufw allow in on tailscale0 to any port 443 proto tcp comment 'fileshare tailscale serve'
  fi
fi

echo
echo "Tailscale is installed. Log in to your tailnet:"
echo "  sudo tailscale up --accept-dns=false --hostname=$(hostname)"
echo
echo "Open the URL printed by that command, approve the machine, then disable"
echo "key expiry for this node in the Tailscale admin console."
