#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
STAGE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-hub-bundle.XXXXXX")"
STAGE_DIR="$STAGE_ROOT/DevBoard-Hub"
ARCHIVE_NAME="devboard-hub-linux-amd64-image.tar"

cleanup() { rm -rf -- "$STAGE_ROOT"; }
trap cleanup EXIT HUP INT TERM

mkdir -p "$STAGE_DIR" "$DIST_DIR"

echo "==> Building devboard/hub:dogfood for linux/amd64"
docker buildx build --platform linux/amd64 --tag devboard/hub:dogfood --load "$REPO_ROOT"

echo "==> Saving the verified image archive"
docker save devboard/hub:dogfood --output "$STAGE_DIR/$ARCHIVE_NAME"
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$STAGE_DIR" && sha256sum "$ARCHIVE_NAME" > "$ARCHIVE_NAME.sha256")
else
    (cd "$STAGE_DIR" && shasum -a 256 "$ARCHIVE_NAME" > "$ARCHIVE_NAME.sha256")
fi

for file in docker-compose.yml bootstrap.sh install.sh README.md; do
    cp "$REPO_ROOT/deploy/hub/$file" "$STAGE_DIR/$file"
done
chmod 0755 "$STAGE_DIR/bootstrap.sh" "$STAGE_DIR/install.sh"

OUTPUT="$DIST_DIR/DevBoard-Hub-linux-amd64.tar.gz"
rm -f -- "$OUTPUT"
tar -C "$STAGE_ROOT" -czf "$OUTPUT" DevBoard-Hub
test -s "$OUTPUT"
echo "==> Wrote $OUTPUT"
