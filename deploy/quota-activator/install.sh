#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1 && pwd)
MANIFEST="$SCRIPT_DIR/manifest.json"
CHECKSUMS="$SCRIPT_DIR/SHA256SUMS"
ARCHIVE="$SCRIPT_DIR/devboard-quota-activator-linux-amd64-image.tar"
ENV_FILE="${DEVBOARD_ACTIVATOR_ENV_FILE:-$SCRIPT_DIR/.env}"

fail() { echo "!! $*" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || fail "Docker is required."
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required."
if command -v sha256sum >/dev/null 2>&1; then HASH=sha256sum; else HASH="shasum -a 256"; fi
hash() { $HASH "$1" | awk '{print $1}'; }

for file in docker-compose.yml bootstrap.sh install.sh README.md manifest.json SHA256SUMS devboard-quota-activator-linux-amd64-image.tar; do
    [ -f "$SCRIPT_DIR/$file" ] && [ ! -L "$SCRIPT_DIR/$file" ] || fail "Bundle file is missing or unsafe: $file"
done
while read -r digest file; do
    [ -n "$digest" ] && [ -n "$file" ] || fail "Malformed SHA256SUMS."
    [ "$(hash "$SCRIPT_DIR/$file")" = "$digest" ] || fail "Checksum verification failed: $file"
done < "$CHECKSUMS"
IMAGE=$(awk -F'"' '$2 == "imageTag" { count++; value=$4 } END { if (count != 1 || value == "") exit 1; print value }' "$MANIFEST") || fail "Manifest imageTag is invalid."
case "$IMAGE" in devboard/quota-activator:[A-Za-z0-9._-]*) ;; *) fail "Manifest image tag is invalid.";; esac

docker load --input "$ARCHIVE" >/dev/null || fail "Docker could not load the verified activator image."
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "Loaded activator image is unavailable."
sh "$SCRIPT_DIR/bootstrap.sh"
[ -f "$ENV_FILE" ] && [ ! -L "$ENV_FILE" ] || fail "Activator .env is unavailable."
TMP="$ENV_FILE.tmp.$$"
awk -v image="$IMAGE" 'index($0,"DEVBOARD_QUOTA_ACTIVATOR_IMAGE=") == 1 { count++; if (count > 1) exit 2; print "DEVBOARD_QUOTA_ACTIVATOR_IMAGE=" image; found=1; next } { print } END { if (!found) print "DEVBOARD_QUOTA_ACTIVATOR_IMAGE=" image }' "$ENV_FILE" > "$TMP" || { rm -f "$TMP"; fail "Activator image selection update rejected."; }
chmod 600 "$TMP" && mv "$TMP" "$ENV_FILE"
docker compose --env-file "$ENV_FILE" -f "$SCRIPT_DIR/docker-compose.yml" up -d --no-build --force-recreate devboard-quota-activator
echo "==> DevBoard Quota Activator installed separately from Hub."
echo "==> Image: $IMAGE"
