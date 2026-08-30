#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
VERSION="${DEVBOARD_PRODUCT_VERSION:-0.1.0-preproduct}"
COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD)"
IMAGE="devboard/quota-activator:${VERSION}-${COMMIT}"
ROOT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-activator-bundle.XXXXXX")"
STAGE="$ROOT/DevBoard-Quota-Activator"
ARCHIVE="devboard-quota-activator-linux-amd64-image.tar"
cleanup() { rm -rf "$ROOT"; }
trap cleanup EXIT HUP INT TERM
fail() { echo "!! $*" >&2; exit 1; }
hash() { if command -v sha256sum >/dev/null; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi; }

[[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || fail "Invalid product version."
[[ "$COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail "Invalid git commit."
[[ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=all)" ]] || fail "Bundle build requires a clean worktree."
command -v docker >/dev/null || fail "Docker is required."
command -v tar >/dev/null || fail "tar is required."
command -v cmp >/dev/null || fail "cmp is required."
command -v jq >/dev/null || fail "jq is required to inspect Docker save archives."
for file in docker-compose.yml bootstrap.sh install.sh README.md; do [[ -f "$REPO_ROOT/deploy/quota-activator/$file" ]] || fail "Missing activator asset: $file"; done

mkdir -p "$STAGE" "$DIST_DIR"
docker buildx build --platform linux/amd64 --provenance=false --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 --build-arg DEVBOARD_PRODUCT_VERSION="$VERSION" --build-arg DEVBOARD_GIT_COMMIT="$COMMIT" --tag "$IMAGE" --load -f "$REPO_ROOT/Dockerfile.quota-activator" "$REPO_ROOT"
PLATFORM="$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$IMAGE")"
[[ "$PLATFORM" == "linux/amd64" ]] || fail "Built image platform is $PLATFORM, expected linux/amd64."
docker save "$IMAGE" --output "$STAGE/$ARCHIVE"
for file in docker-compose.yml bootstrap.sh install.sh README.md; do cp "$REPO_ROOT/deploy/quota-activator/$file" "$STAGE/$file"; done
chmod 0755 "$STAGE/bootstrap.sh" "$STAGE/install.sh"
IMAGE_SHA="$(hash "$STAGE/$ARCHIVE")"
IMAGE_CONFIG_DIGEST="$(bash "$REPO_ROOT/scripts/read-docker-save-config-digest.sh" "$STAGE/$ARCHIVE" "$IMAGE")" || fail "Docker save archive config digest could not be verified."
IMAGE_DIGEST="$(docker image inspect --format '{{.Id}}' "$IMAGE")"
[[ "$IMAGE_DIGEST" =~ ^sha256:[0-9a-fA-F]{64}$ ]] || fail "Docker image digest is malformed."
[[ "$IMAGE_CONFIG_DIGEST" =~ ^sha256:[0-9a-fA-F]{64}$ ]] || fail "Docker save config digest is malformed."
EXPECTED_METADATA="$(printf '{"schemaVersion":1,"productVersion":"%s","gitCommit":"%s"}\n' "$VERSION" "$COMMIT")"
printf '%s' "$EXPECTED_METADATA" > "$ROOT/expected-runtime.json"
printf '\n' >> "$ROOT/expected-runtime.json"
docker run --rm --platform linux/amd64 --entrypoint /usr/local/bin/devboard "$IMAGE" version --json > "$ROOT/runtime.json" || fail "Activator runtime metadata command failed."
cmp -s "$ROOT/expected-runtime.json" "$ROOT/runtime.json" || fail "Activator runtime metadata does not match the build provenance."
cat > "$STAGE/manifest.json" <<EOF
{
  "schemaVersion": 1,
  "productVersion": "$VERSION",
  "gitCommit": "$COMMIT",
  "platform": "linux/amd64",
  "imageTag": "$IMAGE",
  "imageArchive": "$ARCHIVE",
  "imageDigest": "$IMAGE_DIGEST",
  "imageConfigDigest": "$IMAGE_CONFIG_DIGEST",
  "imageSHA256": "$IMAGE_SHA",
  "files": [
    "docker-compose.yml",
    "bootstrap.sh",
    "install.sh",
    "README.md",
    "manifest.json",
    "SHA256SUMS",
    "$ARCHIVE"
  ]
}
EOF
: > "$STAGE/SHA256SUMS"
for file in docker-compose.yml bootstrap.sh install.sh README.md manifest.json "$ARCHIVE"; do printf '%s  %s\n' "$(hash "$STAGE/$file")" "$file" >> "$STAGE/SHA256SUMS"; done
for file in docker-compose.yml bootstrap.sh install.sh README.md manifest.json SHA256SUMS "$ARCHIVE"; do [[ -s "$STAGE/$file" ]] || fail "Incomplete staged bundle: $file"; done
OUTPUT="$DIST_DIR/DevBoard-Quota-Activator-linux-amd64.tar.gz"
COPYFILE_DISABLE=1 tar -C "$ROOT" -czf "$OUTPUT" DevBoard-Quota-Activator
printf '%s  %s\n' "$(hash "$OUTPUT")" "$(basename "$OUTPUT")" > "$OUTPUT.sha256"
echo "==> Wrote $OUTPUT"
