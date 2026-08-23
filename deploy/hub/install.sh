#!/bin/sh
set -eu

SCRIPT_DIR=$(
    CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1
    pwd
)
IMAGE_TAG="devboard/hub:dogfood"
ARCHIVE_NAME="devboard-hub-linux-amd64-image.tar"
ARCHIVE="$SCRIPT_DIR/$ARCHIVE_NAME"
CHECKSUM="$ARCHIVE.sha256"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
BOOTSTRAP="$SCRIPT_DIR/bootstrap.sh"

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

[ -f "$ARCHIVE" ] && [ -s "$ARCHIVE" ] || fail "The non-empty Hub image archive is missing: $ARCHIVE_NAME"
[ -f "$CHECKSUM" ] && [ -s "$CHECKSUM" ] || fail "The image checksum file is missing: $ARCHIVE_NAME.sha256"
[ -f "$COMPOSE_FILE" ] || fail "The product Compose file is missing: docker-compose.yml"
[ -f "$BOOTSTRAP" ] || fail "The product bootstrap is missing: bootstrap.sh"

if ! EXPECTED=$(
    LC_ALL=C awk -v name="$ARCHIVE_NAME" '
        NR == 1 && NF == 2 && length($1) == 64 && $1 ~ /^[0-9a-fA-F]+$/ && $2 == name {
            digest = $1
            valid = 1
        }
        END {
            if (NR != 1 || !valid) exit 1
            print digest
        }
    ' "$CHECKSUM"
); then
    fail "The checksum file is malformed or does not name $ARCHIVE_NAME."
fi

case "$SHA_TOOL" in
    sha256sum) ACTUAL=$(sha256sum "$ARCHIVE" | awk '{print $1}') ;;
    shasum) ACTUAL=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}') ;;
esac
EXPECTED=$(printf '%s' "$EXPECTED" | tr 'A-F' 'a-f')
ACTUAL=$(printf '%s' "$ACTUAL" | tr 'A-F' 'a-f')
[ "$EXPECTED" = "$ACTUAL" ] || fail "Hub image archive checksum verification failed; docker load was not run."
echo "==> Image archive SHA-256 verified."

# The checksum gate above must succeed before this command is reached.
if ! docker load --input "$ARCHIVE" >/dev/null; then
    fail "Docker could not load the verified Hub image archive."
fi
docker image inspect "$IMAGE_TAG" >/dev/null 2>&1 || fail "The exact $IMAGE_TAG image tag is unavailable after docker load."
IMAGE_PLATFORM=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE_TAG")
[ "$IMAGE_PLATFORM" = "linux/amd64" ] || fail "The loaded $IMAGE_TAG image platform is '$IMAGE_PLATFORM', expected 'linux/amd64'."
echo "==> Loaded and verified $IMAGE_TAG (linux/amd64)."

sh "$BOOTSTRAP"
(cd "$SCRIPT_DIR" && docker compose up -d --no-build --force-recreate)

HUB_PORT="${DEVBOARD_HUB_PORT:-}"
if [ -z "$HUB_PORT" ] && [ -f "$SCRIPT_DIR/.env" ]; then
    HUB_PORT=$(awk -F= '$1 == "DEVBOARD_HUB_PORT" { value = substr($0, index($0, "=") + 1) } END { print value }' "$SCRIPT_DIR/.env")
fi
HUB_PORT="${HUB_PORT:-8787}"
echo "==> DevBoard Hub was installed from the verified prebuilt image."
echo "==> Status: cd $SCRIPT_DIR && docker compose ps"
echo "==> Display: http://<NAS>:$HUB_PORT/display"
echo "==> Admin:   http://<NAS>:$HUB_PORT/admin"
echo "==> Admin credential contents remain private under $SCRIPT_DIR/data and were not printed."
