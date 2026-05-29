#!/usr/bin/env bash
# One-time TLS bootstrap for the DigitalOcean deploy.
#
# nginx's :443 server can't start until a certificate exists, and Let's Encrypt
# can't issue one until nginx is up to answer the ACME http-01 challenge. This
# script breaks that chicken-and-egg: it drops in a throwaway self-signed cert,
# brings the stack up, then swaps in a real Let's Encrypt cert and reloads nginx.
# After this runs once, the long-running `certbot` container (docker-compose.prod.yml)
# renews automatically and nginx reloads every 6h to pick renewals up.
#
# Run once, on the Droplet, from /opt/shortener, after .env is filled in:
#   bash init-letsencrypt.sh
set -euo pipefail
cd "$(dirname "$0")"

[ -f .env ] || { echo "ERROR: .env not found — copy .env.example to .env and fill it in." >&2; exit 1; }

# Pull only the keys we need (avoids executing arbitrary .env values via sourcing).
read_env() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- | tr -d '"'; }
DOMAIN="$(read_env DOMAIN)"
EMAIL="$(read_env LETSENCRYPT_EMAIL)"
STAGING="$(read_env STAGING)"

[ -n "$DOMAIN" ] || { echo "ERROR: set DOMAIN in .env (e.g. 203-0-113-5.nip.io)." >&2; exit 1; }

CERT_NAME="shortener"
LIVE="/etc/letsencrypt/live/${CERT_NAME}"
COMPOSE="docker compose -f docker-compose.yml -f docker-compose.prod.yml"

staging_arg=""; [ "${STAGING:-0}" = "1" ] && staging_arg="--staging"
if [ -n "$EMAIL" ]; then email_arg="--email $EMAIL"; else email_arg="--register-unsafely-without-email"; fi

echo "==> Domain: $DOMAIN  (staging=${STAGING:-0})"

echo "==> [1/4] Creating a temporary self-signed certificate so nginx can start"
$COMPOSE run --rm --entrypoint /bin/sh certbot -c "\
  mkdir -p '$LIVE' && \
  openssl req -x509 -nodes -newkey rsa:2048 -days 1 \
    -keyout '$LIVE/privkey.pem' -out '$LIVE/fullchain.pem' -subj '/CN=localhost'"

echo "==> [2/4] Building and starting the full stack"
$COMPOSE up -d --build --remove-orphans

echo "==> Waiting for nginx to answer on :80"
for _ in $(seq 1 30); do
  curl -fsS http://localhost/nginx-health >/dev/null 2>&1 && break
  sleep 2
done

echo "==> [3/4] Requesting the real Let's Encrypt certificate"
# Drop the throwaway cert so certbot creates a clean lineage at the fixed path.
$COMPOSE run --rm --entrypoint /bin/sh certbot -c "\
  rm -rf '$LIVE' '/etc/letsencrypt/archive/$CERT_NAME' '/etc/letsencrypt/renewal/$CERT_NAME.conf'"
$COMPOSE run --rm --entrypoint certbot certbot \
  certonly --webroot -w /var/www/certbot \
  --cert-name "$CERT_NAME" -d "$DOMAIN" \
  $email_arg $staging_arg \
  --rsa-key-size 4096 --agree-tos --no-eff-email --non-interactive --force-renewal

echo "==> [4/4] Reloading nginx with the real certificate"
$COMPOSE exec nginx nginx -s reload

echo
echo "==> TLS bootstrap complete. Open: https://$DOMAIN/app/"
