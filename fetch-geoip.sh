#!/usr/bin/env bash
# Fetch the MaxMind GeoLite2-Country database into backend/shortener/data/.
#
# The .mmdb is licensed and git-ignored (not redistributable), so it must be
# pulled on every machine that BUILDS the go-api image — the Dockerfile bakes it
# in via `COPY data/`, and the redirect handler reads it (GEOIP_DB_PATH) to label
# click countries. GeoIP degrades gracefully if it's missing (country stays blank),
# so this step is optional — but the Geo dashboard/analytics need it.
#
# Run on the Droplet from /opt/shortener BEFORE building the image:
#   - first deploy:  run this, THEN `bash init-letsencrypt.sh`
#   - later refresh: run this, THEN rebuild go-api (see the hint printed at the end)
#
# Provide a free MaxMind license key (https://www.maxmind.com/en/geolite2/signup →
# Account → "Manage License Keys") one of three ways:
#   MAXMIND_LICENSE_KEY=xxxxx bash fetch-geoip.sh
#   bash fetch-geoip.sh xxxxx
#   put MAXMIND_LICENSE_KEY=xxxxx in .env, then: bash fetch-geoip.sh
set -euo pipefail
cd "$(dirname "$0")"

EDITION="GeoLite2-Country"
DEST_DIR="backend/shortener/data"
DEST_FILE="${DEST_DIR}/${EDITION}.mmdb"

# License key precedence: positional arg > environment > .env file.
KEY="${1:-${MAXMIND_LICENSE_KEY:-}}"
if [ -z "$KEY" ] && [ -f .env ]; then
  KEY="$(grep -E '^MAXMIND_LICENSE_KEY=' .env | tail -n1 | cut -d= -f2- | tr -d '"')"
fi
[ -n "$KEY" ] || {
  echo "ERROR: no MaxMind license key. Pass it as an argument, set MAXMIND_LICENSE_KEY," >&2
  echo "       or add MAXMIND_LICENSE_KEY=... to .env. Get one (free) at:" >&2
  echo "       https://www.maxmind.com/en/geolite2/signup" >&2
  exit 1
}

BASE="https://download.maxmind.com/app/geoip_download"
TARBALL_URL="${BASE}?edition_id=${EDITION}&license_key=${KEY}&suffix=tar.gz"
SHA_URL="${BASE}?edition_id=${EDITION}&license_key=${KEY}&suffix=tar.gz.sha256"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> Downloading ${EDITION} from MaxMind"
curl -fsSL "$TARBALL_URL" -o "$TMP/geo.tar.gz" || {
  echo "ERROR: download failed — check the license key and that it has GeoLite2 access." >&2
  exit 1
}

echo "==> Verifying checksum"
if curl -fsSL "$SHA_URL" -o "$TMP/geo.sha256" 2>/dev/null && [ -s "$TMP/geo.sha256" ]; then
  expected="$(cut -d' ' -f1 "$TMP/geo.sha256")"
  actual="$(sha256sum "$TMP/geo.tar.gz" | cut -d' ' -f1)"
  if [ "$expected" != "$actual" ]; then
    echo "ERROR: checksum mismatch (expected $expected, got $actual)." >&2
    exit 1
  fi
  echo "    ok ($actual)"
else
  echo "    (no checksum returned by MaxMind — skipping verification)"
fi

# The archive nests the .mmdb under a dated directory (e.g. GeoLite2-Country_20260101/).
# Extract everything to a temp dir, then copy just the database out — portable
# across GNU/BSD tar (no reliance on --wildcards/--strip-components).
echo "==> Extracting ${EDITION}.mmdb"
tar -xzf "$TMP/geo.tar.gz" -C "$TMP"
mmdb="$(find "$TMP" -name "${EDITION}.mmdb" -print -quit)"
[ -n "$mmdb" ] || { echo "ERROR: ${EDITION}.mmdb not found inside the archive." >&2; exit 1; }

mkdir -p "$DEST_DIR"
cp "$mmdb" "$DEST_FILE"

[ -s "$DEST_FILE" ] || { echo "ERROR: ${DEST_FILE} is missing/empty after extraction." >&2; exit 1; }
echo "==> Done: $DEST_FILE ($(du -h "$DEST_FILE" | cut -f1))"
echo "    Bake it into the image with a rebuild:"
echo "    docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build go-api"
