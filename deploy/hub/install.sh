#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ARCHIVE="$SCRIPT_DIR/devboard-hub-linux-amd64-image.tar"
CHECKSUM="$ARCHIVE.sha256"

if ! command -v docker >/dev/null 2>&1; then
    echo "Docker is required." >&2
    exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
    echo "Docker Compose is required." >&2
    exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
    SHA_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    SHA_TOOL=shasum
else
    echo "A local SHA-256 tool is required." >&2
    exit 1
fi

if [ ! -f "$ARCHIVE" ] || [ ! -f "$CHECKSUM" ]; then
    echo "The Hub image archive or checksum is missing." >&2
    exit 1
fi

EXPECTED=$(awk 'NR == 1 {print $1; exit}' "$CHECKSUM")
case "$SHA_TOOL" in
    sha256sum) ACTUAL=$(sha256sum "$ARCHIVE" | awk '{print $1}') ;;
    shasum) ACTUAL=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}') ;;
esac
if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Hub image archive checksum verification failed." >&2
    exit 1
fi

# Checksum verification must complete before this load.
docker load --input "$ARCHIVE" >/dev/null
if ! docker image inspect devboard/hub:dogfood >/dev/null 2>&1; then
    echo "The exact devboard/hub:dogfood image tag is unavailable after load." >&2
    exit 1
fi

"$SCRIPT_DIR/bootstrap.sh"
(cd "$SCRIPT_DIR" && docker compose up -d --no-build)

echo "DevBoard Hub installed from the verified image archive."
echo "Next: docker compose ps"
echo "Next: open http://<NAS>:<PORT>/display"
echo "Admin credentials remain in the private data directory and were not printed."
