#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
STAGE_ROOT=""
STAGE_DIR=""
IMAGE_TAG="devboard/hub:dogfood"
ARCHIVE_NAME="devboard-hub-linux-amd64-image.tar"
OUTPUT="$DIST_DIR/DevBoard-Hub-linux-amd64.tar.gz"

cleanup() {
    if [[ -n "$STAGE_ROOT" && -d "$STAGE_ROOT" ]]; then
        rm -rf -- "$STAGE_ROOT"
    fi
}
trap cleanup EXIT HUP INT TERM

fail() {
    echo "!! $*" >&2
    exit 1
}

command -v docker >/dev/null 2>&1 || fail "Docker is required on the build machine."
docker buildx version >/dev/null 2>&1 || fail "Docker Buildx is required on the build machine."
command -v tar >/dev/null 2>&1 || fail "tar is required on the build machine."
command -v mktemp >/dev/null 2>&1 || fail "mktemp is required on the build machine."
if command -v sha256sum >/dev/null 2>&1; then
    SHA_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    SHA_TOOL=shasum
else
    fail "sha256sum or shasum is required on the build machine."
fi

for file in docker-compose.yml bootstrap.sh install.sh README.md; do
    [[ -f "$REPO_ROOT/deploy/hub/$file" ]] || fail "Missing product asset: deploy/hub/$file"
done

STAGE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-hub-bundle.XXXXXX")"
STAGE_DIR="$STAGE_ROOT/DevBoard-Hub"
mkdir -p "$STAGE_DIR" "$DIST_DIR"

echo "==> Building $IMAGE_TAG for linux/amd64"
docker buildx build --platform linux/amd64 --tag "$IMAGE_TAG" --load "$REPO_ROOT"
docker image inspect "$IMAGE_TAG" >/dev/null 2>&1 || fail "Build did not produce the exact $IMAGE_TAG tag."
IMAGE_PLATFORM="$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE_TAG")"
[[ "$IMAGE_PLATFORM" == "linux/amd64" ]] || fail "Built image platform is $IMAGE_PLATFORM, expected linux/amd64."

echo "==> Saving the tagged image archive"
docker save "$IMAGE_TAG" --output "$STAGE_DIR/$ARCHIVE_NAME"
[[ -s "$STAGE_DIR/$ARCHIVE_NAME" ]] || fail "Docker produced an empty image archive."

case "$SHA_TOOL" in
    sha256sum)
        (cd "$STAGE_DIR" && sha256sum "$ARCHIVE_NAME" > "$ARCHIVE_NAME.sha256")
        ;;
    shasum)
        (cd "$STAGE_DIR" && shasum -a 256 "$ARCHIVE_NAME" > "$ARCHIVE_NAME.sha256")
        ;;
esac

for file in docker-compose.yml bootstrap.sh install.sh README.md; do
    cp "$REPO_ROOT/deploy/hub/$file" "$STAGE_DIR/$file"
done
chmod 0755 "$STAGE_DIR/bootstrap.sh" "$STAGE_DIR/install.sh"
chmod 0644 "$STAGE_DIR/docker-compose.yml" "$STAGE_DIR/README.md" "$STAGE_DIR/$ARCHIVE_NAME.sha256"

EXPECTED_FILES=(
    docker-compose.yml
    bootstrap.sh
    install.sh
    README.md
    "$ARCHIVE_NAME"
    "$ARCHIVE_NAME.sha256"
)
for file in "${EXPECTED_FILES[@]}"; do
    [[ -f "$STAGE_DIR/$file" && -s "$STAGE_DIR/$file" ]] || fail "Incomplete staged bundle: $file"
done
STAGED_FILES=("$STAGE_DIR"/*)
[[ "${#STAGED_FILES[@]}" -eq "${#EXPECTED_FILES[@]}" ]] || fail "Staged bundle contains unexpected files."

rm -f -- "$OUTPUT"
tar -C "$STAGE_ROOT" -czf "$OUTPUT" DevBoard-Hub
[[ -s "$OUTPUT" ]] || fail "Final bundle is missing or empty."

echo "==> Wrote $OUTPUT"
echo "==> Image: $IMAGE_TAG ($IMAGE_PLATFORM)"
echo "==> Bundle contents: exactly ${#EXPECTED_FILES[@]} product files"
