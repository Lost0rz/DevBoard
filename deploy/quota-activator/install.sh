#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1 && pwd)
MANIFEST="$SCRIPT_DIR/manifest.json"
CHECKSUMS="$SCRIPT_DIR/SHA256SUMS"
ARCHIVE_NAME="devboard-quota-activator-linux-amd64-image.tar"
ARCHIVE="$SCRIPT_DIR/$ARCHIVE_NAME"
ENV_FILE="${DEVBOARD_ACTIVATOR_ENV_FILE:-$SCRIPT_DIR/.env}"

fail() { echo "!! $*" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || fail "Docker is required."
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required."
if command -v sha256sum >/dev/null 2>&1; then HASH=sha256sum; else HASH="shasum -a 256"; fi
hash() { $HASH "$1" | awk '{print $1}'; }

for file in docker-compose.yml bootstrap.sh install.sh README.md manifest.json SHA256SUMS "$ARCHIVE_NAME"; do
    [ -f "$SCRIPT_DIR/$file" ] && [ ! -L "$SCRIPT_DIR/$file" ] || fail "Bundle file is missing or unsafe: $file"
done
while read -r digest file; do
    [ -n "$digest" ] && [ -n "$file" ] || fail "Malformed SHA256SUMS."
    [ "$(hash "$SCRIPT_DIR/$file")" = "$digest" ] || fail "Checksum verification failed: $file"
done < "$CHECKSUMS"
manifest_value() {
    awk -F'"' -v key="$1" '$2 == key { count++; value=$4 } END { if (count != 1 || value == "") exit 1; print value }' "$MANIFEST"
}
SCHEMA=$(manifest_value schemaVersion) || fail "Manifest schemaVersion is invalid."
[ "$SCHEMA" = "1" ] || fail "Unsupported manifest schemaVersion."
PLATFORM=$(manifest_value platform) || fail "Manifest platform is invalid."
[ "$PLATFORM" = "linux/amd64" ] || fail "Manifest platform must be linux/amd64."
IMAGE=$(manifest_value imageTag) || fail "Manifest imageTag is invalid."
IMAGE_ARCHIVE=$(manifest_value imageArchive) || fail "Manifest imageArchive is invalid."
IMAGE_DIGEST=$(manifest_value imageDigest) || fail "Manifest imageDigest is invalid."
IMAGE_CONFIG_DIGEST=$(manifest_value imageConfigDigest) || fail "Manifest imageConfigDigest is invalid."
IMAGE_SHA=$(manifest_value imageSHA256) || fail "Manifest imageSHA256 is invalid."
PRODUCT_VERSION=$(manifest_value productVersion) || fail "Manifest productVersion is invalid."
GIT_COMMIT=$(manifest_value gitCommit) || fail "Manifest gitCommit is invalid."
[ "$IMAGE_ARCHIVE" = "$ARCHIVE_NAME" ] || fail "Manifest image archive is not the shipped linux/amd64 archive."
case "$IMAGE" in devboard/quota-activator:[A-Za-z0-9._-]*) ;; *) fail "Manifest image tag is invalid.";; esac
case "$IMAGE_DIGEST" in sha256:*) [ "${#IMAGE_DIGEST}" -eq 71 ] || fail "Manifest image digest is malformed.";; *) fail "Manifest image digest is malformed.";; esac
case "$IMAGE_CONFIG_DIGEST" in sha256:*) [ "${#IMAGE_CONFIG_DIGEST}" -eq 71 ] || fail "Manifest imageConfigDigest is malformed.";; *) fail "Manifest imageConfigDigest is malformed.";; esac
case "$IMAGE_SHA" in [0-9a-fA-F]*) [ "${#IMAGE_SHA}" -eq 64 ] || fail "Manifest image SHA-256 is malformed.";; *) fail "Manifest image SHA-256 is malformed.";; esac
case "$GIT_COMMIT" in [0-9a-fA-F]*) [ "${#GIT_COMMIT}" -eq 40 ] || fail "Manifest gitCommit must be a full commit.";; *) fail "Manifest gitCommit is malformed.";; esac
[ "$IMAGE" = "devboard/quota-activator:${PRODUCT_VERSION}-${GIT_COMMIT}" ] || fail "Manifest image tag does not bind product version and commit."
MANIFEST_FILES=$(awk '/"files"[[:space:]]*:/ { inside=1; next } inside && /^[[:space:]]*\]/ { inside=0; next } inside && match($0, /"[^"]+"/) { line=substr($0, RSTART+1, RLENGTH-2); print line }' "$MANIFEST" | sort | tr '\n' ' ')
EXPECTED_FILES=$(printf '%s\n' docker-compose.yml bootstrap.sh install.sh README.md manifest.json SHA256SUMS "$ARCHIVE_NAME" | sort | tr '\n' ' ')
[ "$MANIFEST_FILES" = "$EXPECTED_FILES" ] || fail "Manifest inventory does not match the bundle."
CHECKSUM_LINES=$(awk 'NF == 2 { count++ } END { print count + 0 }' "$CHECKSUMS")
[ "$CHECKSUM_LINES" -eq 6 ] || fail "SHA256SUMS must cover exactly six non-checksum product files."
[ "$(hash "$ARCHIVE")" = "$IMAGE_SHA" ] || fail "Image archive SHA-256 does not match manifest."

docker load --input "$ARCHIVE" >/dev/null || fail "Docker could not load the verified activator image."
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "Loaded activator image is unavailable."
LOADED_DIGEST="$(docker image inspect --format '{{.Id}}' "$IMAGE")"
[ "$LOADED_DIGEST" = "$IMAGE_DIGEST" ] || [ "$LOADED_DIGEST" = "$IMAGE_CONFIG_DIGEST" ] || fail "Loaded activator image digest does not match manifest."
[ "$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE")" = "linux/amd64" ] || fail "Loaded activator image platform is not linux/amd64."
sh "$SCRIPT_DIR/bootstrap.sh"
[ -f "$ENV_FILE" ] && [ ! -L "$ENV_FILE" ] || fail "Activator .env is unavailable."
TMP="$ENV_FILE.tmp.$$"
awk -v image="$IMAGE" 'index($0,"DEVBOARD_QUOTA_ACTIVATOR_IMAGE=") == 1 { count++; if (count > 1) exit 2; print "DEVBOARD_QUOTA_ACTIVATOR_IMAGE=" image; found=1; next } { print } END { if (!found) print "DEVBOARD_QUOTA_ACTIVATOR_IMAGE=" image }' "$ENV_FILE" > "$TMP" || { rm -f "$TMP"; fail "Activator image selection update rejected."; }
chmod 600 "$TMP" && mv "$TMP" "$ENV_FILE"
docker compose --env-file "$ENV_FILE" -f "$SCRIPT_DIR/docker-compose.yml" up -d --no-build --force-recreate devboard-quota-activator
echo "==> DevBoard Quota Activator installed separately from Hub."
echo "==> Image: $IMAGE"
