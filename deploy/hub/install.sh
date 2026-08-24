#!/bin/sh
set -eu

SCRIPT_DIR=$(
    CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1
    pwd
)
MANIFEST="$SCRIPT_DIR/manifest.json"
CHECKSUMS="$SCRIPT_DIR/SHA256SUMS"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
BOOTSTRAP="$SCRIPT_DIR/bootstrap.sh"
ROLLBACK="$SCRIPT_DIR/rollback.sh"
ARCHIVE_NAME="devboard-hub-linux-amd64-image.tar"
ARCHIVE="$SCRIPT_DIR/$ARCHIVE_NAME"
PRODUCT_FILES="docker-compose.yml bootstrap.sh install.sh rollback.sh README.md manifest.json $ARCHIVE_NAME"

fail() {
    echo "!! $*" >&2
    exit 1
}

command -v docker >/dev/null 2>&1 || fail "Docker is required."
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required (the 'docker compose' command)."
if command -v sha256sum >/dev/null 2>&1; then
    SHA_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    SHA_TOOL=shasum
else
    fail "A local SHA-256 utility is required (sha256sum or shasum)."
fi

hash_file() {
    case "$SHA_TOOL" in
        sha256sum) sha256sum "$1" | awk '{print $1}' ;;
        shasum) shasum -a 256 "$1" | awk '{print $1}' ;;
    esac
}

is_private_regular() {
    [ -f "$1" ] && [ ! -L "$1" ]
}

for file in $PRODUCT_FILES SHA256SUMS; do
    is_private_regular "$SCRIPT_DIR/$file" || fail "Bundle file is missing or not a regular file: $file"
done

manifest_value() {
    KEY=$1
    awk -F'"' -v key="$KEY" '
        $2 == key {
            count++
            if ($4 != "") { value = $4 }
            else { value = $3; sub(/^[^:]*:[[:space:]]*/, "", value); sub(/,[[:space:]]*$/, "", value); gsub(/[[:space:]]/, "", value) }
        }
        END { if (count != 1) exit 1; print value }
    ' "$MANIFEST"
}

SCHEMA=$(manifest_value schemaVersion) || fail "manifest schemaVersion is missing or duplicated."
[ "$SCHEMA" = "1" ] || fail "unsupported manifest schemaVersion."
PLATFORM=$(manifest_value platform) || fail "manifest platform is missing or duplicated."
[ "$PLATFORM" = "linux/amd64" ] || fail "manifest platform must be linux/amd64."
IMAGE_TAG=$(manifest_value imageTag) || fail "manifest imageTag is missing or duplicated."
IMAGE_ARCHIVE=$(manifest_value imageArchive) || fail "manifest imageArchive is missing or duplicated."
IMAGE_DIGEST=$(manifest_value imageDigest) || fail "manifest imageDigest is missing or duplicated."
IMAGE_SHA=$(manifest_value imageSHA256) || fail "manifest imageSHA256 is missing or duplicated."
PRODUCT_VERSION=$(manifest_value productVersion) || fail "manifest productVersion is missing or duplicated."
GIT_COMMIT=$(manifest_value gitCommit) || fail "manifest gitCommit is missing or duplicated."
[ "$IMAGE_ARCHIVE" = "$ARCHIVE_NAME" ] || fail "manifest image archive is not the shipped linux/amd64 archive."
case "$IMAGE_TAG" in devboard/hub:[A-Za-z0-9._-]*) ;; *) fail "manifest image tag is malformed." ;; esac
case "$IMAGE_DIGEST" in sha256:[0-9a-fA-F][0-9a-fA-F]*) [ "${#IMAGE_DIGEST}" -eq 71 ] || fail "manifest image digest is malformed." ;; *) fail "manifest image digest is malformed." ;; esac
case "$IMAGE_SHA" in [0-9a-fA-F][0-9a-fA-F]*) [ "${#IMAGE_SHA}" -eq 64 ] || fail "manifest image SHA-256 is malformed." ;; *) fail "manifest image SHA-256 is malformed." ;; esac
[ -n "$PRODUCT_VERSION" ] && [ -n "$GIT_COMMIT" ] || fail "manifest provenance is incomplete."
case "$GIT_COMMIT" in
    [0-9a-fA-F][0-9a-fA-F]*) [ "${#GIT_COMMIT}" -eq 40 ] || fail "manifest gitCommit must be a full commit." ;;
    *) fail "manifest gitCommit must be hexadecimal." ;;
esac
EXPECTED_IMAGE_TAG="devboard/hub:${PRODUCT_VERSION}-${GIT_COMMIT}"
[ "$IMAGE_TAG" = "$EXPECTED_IMAGE_TAG" ] || fail "manifest image tag must bind product version and full gitCommit."

# The manifest is the authority for the exact shipped inventory. Parse only
# the JSON files array; all other strings in the document are ignored.
MANIFEST_FILES=$(awk '
    /"files"[[:space:]]*:/ { inside=1; next }
    inside && /^[[:space:]]*\]/ { inside=0; next }
    inside && match($0, /"[^"]+"/) { line=substr($0, RSTART+1, RLENGTH-2); print line }
' "$MANIFEST" | sort | tr '\n' ' ')
EXPECTED_FILES=$(printf '%s\n' $PRODUCT_FILES SHA256SUMS | sort | tr '\n' ' ')
[ "$MANIFEST_FILES" = "$EXPECTED_FILES" ] || fail "manifest inventory does not match the bundle."

checksum_entry() {
    NAME=$1
    awk -v name="$NAME" '$2 == name { count++; value=$1 } END { if (count != 1) exit 1; print value }' "$CHECKSUMS"
}

for file in $PRODUCT_FILES; do
    EXPECTED=$(checksum_entry "$file") || fail "SHA256SUMS entry missing or duplicated: $file"
    case "$EXPECTED" in [0-9a-fA-F][0-9a-fA-F]*) [ "${#EXPECTED}" -eq 64 ] || fail "malformed checksum for $file" ;; *) fail "malformed checksum for $file" ;; esac
    ACTUAL=$(hash_file "$SCRIPT_DIR/$file")
    [ "$EXPECTED" = "$ACTUAL" ] || fail "checksum verification failed for $file"
done
CHECKSUM_LINES=$(awk 'NF == 2 { count++ } END { print count + 0 }' "$CHECKSUMS")
[ "$CHECKSUM_LINES" -eq 7 ] || fail "SHA256SUMS must cover exactly the seven non-checksum product files."
[ "$(hash_file "$ARCHIVE")" = "$IMAGE_SHA" ] || fail "image archive SHA-256 does not match manifest."
echo "==> Bundle manifest, inventory and internal checksums verified."
CURRENT_MANIFEST=$(hash_file "$MANIFEST")

# Read the old verified selection before docker load. A missing or non-local
# old image is deliberately not recorded as rollback material.
ENV_FILE="${DEVBOARD_ENV_FILE:-$SCRIPT_DIR/.env}"
if [ -L "$ENV_FILE" ]; then
    fail ".env must not be a symbolic link."
fi
OLD_IMAGE=""
OLD_MANIFEST=""
if [ -f "$ENV_FILE" ]; then
    OLD_IMAGE=$(awk -F= '$1 == "DEVBOARD_HUB_IMAGE" { count++; value=substr($0,index($0,"=")+1) } END { if (count > 1) exit 1; print value }' "$ENV_FILE") || fail ".env has duplicate DEVBOARD_HUB_IMAGE entries."
    OLD_MANIFEST=$(awk -F= '$1 == "DEVBOARD_HUB_MANIFEST_SHA256" { count++; value=substr($0,index($0,"=")+1) } END { if (count > 1) exit 1; print value }' "$ENV_FILE") || fail ".env has duplicate manifest entries."
    if [ -n "$OLD_IMAGE" ] && [ "$OLD_IMAGE" != "$IMAGE_TAG" -o "$OLD_MANIFEST" != "$CURRENT_MANIFEST" ] && ! docker image inspect "$OLD_IMAGE" >/dev/null 2>&1; then
        OLD_IMAGE=""
        OLD_MANIFEST=""
    fi
fi
if [ -z "$OLD_IMAGE" ]; then
    OLD_MANIFEST=""
fi

case "$OLD_IMAGE" in
    ""|devboard/hub:[A-Za-z0-9._-]*) ;;
    *) fail "existing DEVBOARD_HUB_IMAGE is malformed." ;;
esac
case "$OLD_MANIFEST" in
    ""|[0-9a-fA-F][0-9a-fA-F]*)
        if [ -n "$OLD_MANIFEST" ] && [ "${#OLD_MANIFEST}" -ne 64 ]; then
            fail "existing DEVBOARD_HUB_MANIFEST_SHA256 is malformed."
        fi
        ;;
    *) fail "existing DEVBOARD_HUB_MANIFEST_SHA256 is malformed." ;;
esac

SAME_TARGET=0
if [ "$OLD_IMAGE" = "$IMAGE_TAG" ] && [ "$OLD_MANIFEST" = "$CURRENT_MANIFEST" ]; then
    SAME_TARGET=1
fi

if ! docker load --input "$ARCHIVE" >/dev/null; then
    fail "Docker could not load the verified Hub image archive."
fi
docker image inspect "$IMAGE_TAG" >/dev/null 2>&1 || fail "The exact manifest image tag is unavailable after docker load."
LOADED_DIGEST=$(docker image inspect --format '{{.Id}}' "$IMAGE_TAG")
[ "$LOADED_DIGEST" = "$IMAGE_DIGEST" ] || fail "loaded image digest does not match the manifest."
LOADED_PLATFORM=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE_TAG")
[ "$LOADED_PLATFORM" = "linux/amd64" ] || fail "loaded image platform is '$LOADED_PLATFORM', expected linux/amd64."
echo "==> Loaded and verified $IMAGE_TAG (linux/amd64)."

sh "$BOOTSTRAP"
ENV_FILE="${DEVBOARD_ENV_FILE:-$SCRIPT_DIR/.env}"
[ -L "$ENV_FILE" ] && fail ".env must not be a symbolic link."

set_env_value() {
    KEY=$1
    VALUE=$2
    TMP="$ENV_FILE.tmp.$$"
    [ ! -e "$TMP" ] && [ ! -L "$TMP" ] || fail "temporary .env target already exists."
    awk -v key="$KEY" -v value="$VALUE" '
        index($0, key "=") == 1 { count++; if (count > 1) exit 2; print key "=" value; found=1; next }
        { print }
        END { if (!found) print key "=" value }
    ' "$ENV_FILE" > "$TMP" || { rm -f "$TMP"; fail ".env update rejected."; }
    chmod 600 "$TMP"
    mv "$TMP" "$ENV_FILE"
}

set_env_value DEVBOARD_HUB_IMAGE "$IMAGE_TAG"
set_env_value DEVBOARD_HUB_MANIFEST_SHA256 "$CURRENT_MANIFEST"
if [ "$SAME_TARGET" -eq 0 ]; then
    set_env_value DEVBOARD_HUB_PREVIOUS_IMAGE "$OLD_IMAGE"
    set_env_value DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256 "$OLD_MANIFEST"
fi

(cd "$SCRIPT_DIR" && docker compose up -d --no-build --force-recreate)

echo "==> DevBoard Hub installed from the verified source-free bundle."
echo "==> Product version: $PRODUCT_VERSION ($GIT_COMMIT)"
echo "==> Image: $IMAGE_TAG (linux/amd64)"
echo "==> Rollback: sh $ROLLBACK"
echo "==> Display: http://<NAS>:${DEVBOARD_HUB_PORT:-8787}/display"
echo "==> Admin:   http://<NAS>:${DEVBOARD_HUB_PORT:-8787}/admin"
