#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
STAGE_ROOT=""
STAGE_DIR=""
PRODUCT_VERSION="${DEVBOARD_PRODUCT_VERSION:-0.1.0-preproduct}"
GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD)"
IMAGE_TAG="devboard/hub:${PRODUCT_VERSION}-${GIT_COMMIT}"
ARCHIVE_NAME="devboard-hub-linux-amd64-image.tar"
OUTPUT="$DIST_DIR/DevBoard-Hub-linux-amd64.tar.gz"
SIDECAR="$OUTPUT.sha256"

PRODUCT_FILES=(
    docker-compose.yml
    bootstrap.sh
    install.sh
    rollback.sh
    README.md
    manifest.json
    "$ARCHIVE_NAME"
)
ALL_FILES=("${PRODUCT_FILES[@]}" SHA256SUMS)

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

hash_file() {
    case "$SHA_TOOL" in
        sha256sum) sha256sum "$1" | awk '{print $1}' ;;
        shasum) shasum -a 256 "$1" | awk '{print $1}' ;;
    esac
}

[[ "$PRODUCT_VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || fail "DEVBOARD_PRODUCT_VERSION contains unsupported characters."
[[ "$PRODUCT_VERSION" != "development" && "$PRODUCT_VERSION" != "unknown" ]] || fail "Authoritative bundles cannot use a development/unknown product version."
[[ "$GIT_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail "Git commit provenance is not a full immutable commit."
if [[ -n "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=all)" ]]; then
    fail "Authoritative Hub bundle build requires a clean worktree; refusing to mark dirty content as $GIT_COMMIT."
fi
command -v docker >/dev/null 2>&1 || fail "Docker is required on the build machine."
docker buildx version >/dev/null 2>&1 || fail "Docker Buildx is required on the build machine."
command -v tar >/dev/null 2>&1 || fail "tar is required on the build machine."
command -v mktemp >/dev/null 2>&1 || fail "mktemp is required on the build machine."
command -v cmp >/dev/null 2>&1 || fail "cmp is required on the build machine."
command -v jq >/dev/null 2>&1 || fail "jq is required on the build machine."
if command -v sha256sum >/dev/null 2>&1; then
    SHA_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    SHA_TOOL=shasum
else
    fail "sha256sum or shasum is required on the build machine."
fi

for file in docker-compose.yml bootstrap.sh install.sh rollback.sh README.md; do
    [[ -f "$REPO_ROOT/deploy/hub/$file" ]] || fail "Missing product asset: deploy/hub/$file"
done
[[ -f "$REPO_ROOT/scripts/read-docker-save-config-digest.sh" ]] || fail "Missing archive digest helper."

STAGE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-hub-bundle.XXXXXX")"
STAGE_DIR="$STAGE_ROOT/DevBoard-Hub"
mkdir -p "$STAGE_DIR" "$DIST_DIR"

echo "==> Building immutable $IMAGE_TAG for linux/amd64"
docker buildx build --platform linux/amd64 --provenance=false --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 --build-arg DEVBOARD_PRODUCT_VERSION="$PRODUCT_VERSION" --build-arg DEVBOARD_GIT_COMMIT="$GIT_COMMIT" --tag "$IMAGE_TAG" --load "$REPO_ROOT"
docker image inspect "$IMAGE_TAG" >/dev/null 2>&1 || fail "Build did not produce the exact immutable image tag."
IMAGE_PLATFORM="$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE_TAG")"
[[ "$IMAGE_PLATFORM" == "linux/amd64" ]] || fail "Built image platform is $IMAGE_PLATFORM, expected linux/amd64."

echo "==> Verifying runtime provenance from the built image"
RUNTIME_METADATA_FILE="$STAGE_ROOT/runtime-metadata.json"
EXPECTED_RUNTIME_METADATA_FILE="$STAGE_ROOT/expected-runtime-metadata.json"
EXPECTED_RUNTIME_METADATA="$(printf '{"schemaVersion":1,"productVersion":"%s","gitCommit":"%s"}' "$PRODUCT_VERSION" "$GIT_COMMIT")"
printf '%s\n' "$EXPECTED_RUNTIME_METADATA" > "$EXPECTED_RUNTIME_METADATA_FILE"
docker run --rm --platform linux/amd64 --entrypoint /usr/local/bin/devboard "$IMAGE_TAG" version --json > "$RUNTIME_METADATA_FILE" || fail "Built image runtime metadata command failed."
cmp -s "$EXPECTED_RUNTIME_METADATA_FILE" "$RUNTIME_METADATA_FILE" || fail "Built image runtime metadata does not exactly match the manifest plan."

echo "==> Saving the tagged image archive"
docker save "$IMAGE_TAG" --output "$STAGE_DIR/$ARCHIVE_NAME"
[[ -s "$STAGE_DIR/$ARCHIVE_NAME" ]] || fail "Docker produced an empty image archive."
IMAGE_SHA="$(hash_file "$STAGE_DIR/$ARCHIVE_NAME")"
IMAGE_DIGEST="$(bash "$REPO_ROOT/scripts/read-docker-save-config-digest.sh" "$STAGE_DIR/$ARCHIVE_NAME" "$IMAGE_TAG")" || fail "Docker save archive config digest could not be verified."
[[ "$IMAGE_DIGEST" =~ ^sha256:[0-9a-fA-F]{64}$ ]] || fail "Docker save archive config digest is malformed."

for file in docker-compose.yml bootstrap.sh install.sh rollback.sh README.md; do
    cp "$REPO_ROOT/deploy/hub/$file" "$STAGE_DIR/$file"
done
chmod 0755 "$STAGE_DIR/bootstrap.sh" "$STAGE_DIR/install.sh" "$STAGE_DIR/rollback.sh"
chmod 0644 "$STAGE_DIR/docker-compose.yml" "$STAGE_DIR/README.md"

cat > "$STAGE_DIR/manifest.json" <<EOF
{
  "schemaVersion": 1,
  "productVersion": "$PRODUCT_VERSION",
  "gitCommit": "$GIT_COMMIT",
  "platform": "linux/amd64",
  "imageTag": "$IMAGE_TAG",
  "imageArchive": "$ARCHIVE_NAME",
  "imageDigest": "$IMAGE_DIGEST",
  "imageSHA256": "$IMAGE_SHA",
  "files": [
    "docker-compose.yml",
    "bootstrap.sh",
    "install.sh",
    "rollback.sh",
    "README.md",
    "manifest.json",
    "SHA256SUMS",
    "$ARCHIVE_NAME"
  ]
}
EOF
chmod 0644 "$STAGE_DIR/manifest.json"

: > "$STAGE_DIR/SHA256SUMS"
for file in "${PRODUCT_FILES[@]}"; do
    printf '%s  %s\n' "$(hash_file "$STAGE_DIR/$file")" "$file" >> "$STAGE_DIR/SHA256SUMS"
done
chmod 0644 "$STAGE_DIR/SHA256SUMS"

for file in "${ALL_FILES[@]}"; do
    [[ -f "$STAGE_DIR/$file" && -s "$STAGE_DIR/$file" ]] || fail "Incomplete staged bundle: $file"
done
STAGED_SORTED="$(for path in "$STAGE_DIR"/*; do basename "$path"; done | sort | paste -sd ' ' -)"
EXPECTED_SORTED="$(printf '%s\n' "${ALL_FILES[@]}" | sort | paste -sd ' ' -)"
[[ "$STAGED_SORTED" == "$EXPECTED_SORTED" ]] || fail "Staged bundle contains unexpected files."

rm -f -- "$OUTPUT" "$SIDECAR"
# Prevent macOS Archive Utility/libarchive metadata files (._*) and xattrs
# from being embedded in the NAS bundle. They are not product files and can
# confuse extraction/verification on BusyBox-based NAS systems.
COPYFILE_DISABLE=1 tar -C "$STAGE_ROOT" -czf "$OUTPUT" DevBoard-Hub
[[ -s "$OUTPUT" ]] || fail "Final bundle is missing or empty."
printf '%s  %s\n' "$(hash_file "$OUTPUT")" "$(basename "$OUTPUT")" > "$SIDECAR"

echo "==> Wrote $OUTPUT"
echo "==> Sidecar: $SIDECAR"
echo "==> Image: $IMAGE_TAG ($IMAGE_PLATFORM, $IMAGE_DIGEST)"
echo "==> Image archive SHA-256: $IMAGE_SHA"
echo "==> Bundle contents: exactly ${#ALL_FILES[@]} product files"
