#!/usr/bin/env bash
# Build & unggah PWA statis ke VPS (dilayani Caddy di BV_TENANT_DOMAIN / BV_ROOMS_DOMAIN dari $BV_PWA_DIR/<app>).
# Jalankan dari laptop (Git Bash): ./infra/scripts/deploy-pwa.sh [tenant|rooms|all] [ssh-host]
#   env: BV_ORG_SLUG (default buildingvision), TENANT_DIR (../buildingvision-mobile-tenant), ROOMS_DIR (../customer-booking-app)
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
app="${1:-all}"; host="${2:-bv-prod}"
ORG="${BV_ORG_SLUG:-buildingvision}"
TENANT_DIR="${TENANT_DIR:-$root/../buildingvision-mobile-tenant}"
ROOMS_DIR="${ROOMS_DIR:-$root/../customer-booking-app}"
build_push() { # <dir> <nama>
  local dir="$1" name="$2"
  echo "== build $name ($dir)"
  ( cd "$dir" && VITE_API_MODE=http VITE_ORG_SLUG="$ORG" VITE_API_BASE= npm run build >/dev/null )
  echo "== upload $name → $host:/opt/bv-apps/$name"
  tar czf - -C "$dir/dist" . | ssh "$host" "sudo mkdir -p /opt/bv-apps/$name.new && sudo tar xzf - -C /opt/bv-apps/$name.new && sudo rm -rf /opt/bv-apps/$name && sudo mv /opt/bv-apps/$name.new /opt/bv-apps/$name && sudo chmod -R a+rX /opt/bv-apps/$name && ls /opt/bv-apps/$name | head -3"
}
case "$app" in
  tenant) build_push "$TENANT_DIR" tenant;;
  rooms)  build_push "$ROOMS_DIR" rooms;;
  all)    build_push "$TENANT_DIR" tenant; build_push "$ROOMS_DIR" rooms;;
  *) echo "usage: $0 [tenant|rooms|all] [ssh-host]"; exit 2;;
esac
echo "selesai — cek https://tenant.<domain> / https://rooms.<domain>"
