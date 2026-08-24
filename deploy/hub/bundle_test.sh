#!/bin/sh
# Offline bundle installer acceptance test. It uses a fake Docker CLI and
# never talks to a Docker daemon, registry, NAS, or production data directory.
set -eu

ROOT=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/devboard-hub-bundle-test.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

# Compose interpolation is a build/install gate and does not require a
# running daemon. Verify both fail-closed omission and an explicit immutable
# test tag before the fake Docker CLI is placed on PATH.
command -v docker >/dev/null 2>&1
docker compose version >/dev/null 2>&1
command -v jq >/dev/null 2>&1
grep -q 'DEVBOARD_PRODUCT_VERSION' "$ROOT/Dockerfile"
grep -q 'DEVBOARD_GIT_COMMIT' "$ROOT/Dockerfile"
grep -q 'status --porcelain --untracked-files=all' "$ROOT/scripts/build-hub-bundle.sh"
for ignored in '.playwright-cli/' 'dist/' 'macos/build/' 'macos/.derived-data/' 'deploy/hub/data/' 'deploy/hub/.env' 'config.local.yaml' '.codex-agent-team/'; do
    grep -Fq "$ignored" "$ROOT/.dockerignore"
done
COMPOSE_PROBE="$TMP/compose-probe"
mkdir -p "$COMPOSE_PROBE/data"
cp "$ROOT/deploy/hub/docker-compose.yml" "$COMPOSE_PROBE/docker-compose.yml"
if (cd "$COMPOSE_PROBE" && env -u DEVBOARD_HUB_IMAGE -u DEVBOARD_UID -u DEVBOARD_GID docker compose config >/dev/null 2>&1); then
    echo "compose accepted a missing DEVBOARD_HUB_IMAGE" >&2
    exit 1
fi
(cd "$COMPOSE_PROBE" && DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa docker compose config >/dev/null)

# A Docker engine image ID is not the portable digest of the Config blob in a
# saved archive. Exercise the archive reader with distinct local and shipped
# digests, and verify the Config bytes hash to the path named by save's
# manifest.json.
SAVE_FIXTURE_DIR="$TMP/docker-save-fixture"
mkdir -p "$SAVE_FIXTURE_DIR/blobs/sha256"
printf '%s\n' '{"architecture":"amd64","os":"linux","config":{"Labels":{"devboard.test":"fixture"}}}' > "$SAVE_FIXTURE_DIR/config.json"
ARCHIVE_CONFIG_HEX=$(shasum -a 256 "$SAVE_FIXTURE_DIR/config.json" | awk '{print $1}')
ARCHIVE_CONFIG_PATH="blobs/sha256/$ARCHIVE_CONFIG_HEX"
cp "$SAVE_FIXTURE_DIR/config.json" "$SAVE_FIXTURE_DIR/$ARCHIVE_CONFIG_PATH"
SAVE_IMAGE_TAG="devboard/hub:test-archive-digest"
printf '[{"Config":"%s","RepoTags":["%s"],"Layers":[]}]\n' "$ARCHIVE_CONFIG_PATH" "$SAVE_IMAGE_TAG" > "$SAVE_FIXTURE_DIR/manifest.json"
SAVE_ARCHIVE="$TMP/docker-save-fixture.tar"
(cd "$SAVE_FIXTURE_DIR" && tar -cf "$SAVE_ARCHIVE" manifest.json "$ARCHIVE_CONFIG_PATH")
LOCAL_IMAGE_ID=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
ARCHIVE_CONFIG_DIGEST="sha256:$ARCHIVE_CONFIG_HEX"
[ "$LOCAL_IMAGE_ID" != "$ARCHIVE_CONFIG_DIGEST" ]
[ "$(jq -r '.[0].Config' "$SAVE_FIXTURE_DIR/manifest.json")" = "$ARCHIVE_CONFIG_PATH" ]
[ "$(bash "$ROOT/scripts/read-docker-save-config-digest.sh" "$SAVE_ARCHIVE" "$SAVE_IMAGE_TAG")" = "$ARCHIVE_CONFIG_DIGEST" ]

# Exercise the real runtime metadata command with the same ldflags used by
# Dockerfile/build-hub-bundle.sh. Compare complete files so extra fields,
# missing fields, wrong values, or extra lines cannot pass.
VERSION_BIN="$TMP/devboard-version"
VERSION_OUTPUT="$TMP/runtime-metadata.json"
VERSION_EXPECTED="$TMP/expected-runtime-metadata.json"
VERSION_WRONG="$TMP/wrong-runtime-metadata.json"
VERSION_EXTRA="$TMP/extra-runtime-metadata.json"
VERSION_PRODUCT="test"
VERSION_COMMIT="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
go build -trimpath -ldflags "-s -w -X main.productVersion=$VERSION_PRODUCT -X main.gitCommit=$VERSION_COMMIT" -o "$VERSION_BIN" "$ROOT/cmd/devboard"
"$VERSION_BIN" version --json > "$VERSION_OUTPUT"
VERSION_LINES=$(wc -l < "$VERSION_OUTPUT" | tr -d '[:space:]')
[ "$VERSION_LINES" -eq 1 ] || { echo "version metadata was not a single line" >&2; exit 1; }
printf '{"schemaVersion":1,"productVersion":"%s","gitCommit":"%s"}\n' "$VERSION_PRODUCT" "$VERSION_COMMIT" > "$VERSION_EXPECTED"
cmp -s "$VERSION_EXPECTED" "$VERSION_OUTPUT" || { echo "valid runtime metadata did not match exactly" >&2; exit 1; }
printf '{"schemaVersion":1,"productVersion":"wrong","gitCommit":"%s"}\n' "$VERSION_COMMIT" > "$VERSION_WRONG"
if cmp -s "$VERSION_WRONG" "$VERSION_OUTPUT"; then
    echo "wrong runtime metadata unexpectedly matched" >&2
    exit 1
fi
printf '{"schemaVersion":1,"productVersion":"%s","gitCommit":"%s","extra":true}\n' "$VERSION_PRODUCT" "$VERSION_COMMIT" > "$VERSION_EXTRA"
if cmp -s "$VERSION_EXTRA" "$VERSION_OUTPUT"; then
    echo "extra runtime metadata unexpectedly matched" >&2
    exit 1
fi

make_fixture() {
    DIR=$1
    mkdir -p "$DIR"
    for file in docker-compose.yml bootstrap.sh install.sh rollback.sh README.md; do
        cp "$ROOT/deploy/hub/$file" "$DIR/$file"
    done
    chmod 0755 "$DIR/bootstrap.sh" "$DIR/install.sh" "$DIR/rollback.sh"
    printf 'offline-test-image\n' > "$DIR/devboard-hub-linux-amd64-image.tar"
    IMAGE_SHA=$(shasum -a 256 "$DIR/devboard-hub-linux-amd64-image.tar" | awk '{print $1}')
    cat > "$DIR/manifest.json" <<EOF
{
  "schemaVersion": 1,
  "productVersion": "test",
  "gitCommit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "platform": "linux/amd64",
  "imageTag": "devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "imageArchive": "devboard-hub-linux-amd64-image.tar",
  "imageDigest": "$ARCHIVE_CONFIG_DIGEST",
  "imageSHA256": "$IMAGE_SHA",
  "files": [
    "docker-compose.yml",
    "bootstrap.sh",
    "install.sh",
    "rollback.sh",
    "README.md",
    "manifest.json",
    "SHA256SUMS",
    "devboard-hub-linux-amd64-image.tar"
  ]
}
EOF
    : > "$DIR/SHA256SUMS"
    for file in docker-compose.yml bootstrap.sh install.sh rollback.sh README.md manifest.json devboard-hub-linux-amd64-image.tar; do
        printf '%s  %s\n' "$(shasum -a 256 "$DIR/$file" | awk '{print $1}')" "$file" >> "$DIR/SHA256SUMS"
    done
}

FAKEBIN="$TMP/bin"
mkdir -p "$FAKEBIN"
export FAKE_DOCKER_LOCAL_ID="$LOCAL_IMAGE_ID"
export FAKE_DOCKER_ARCHIVE_DIGEST="$ARCHIVE_CONFIG_DIGEST"
cat > "$FAKEBIN/docker" <<'EOF'
#!/bin/sh
set -eu
case "${1:-}" in
    pull|push|build|buildx) echo "forbidden docker operation" >&2; exit 91 ;;
    compose)
        [ "${2:-}" = version ] && exit 0
        if [ "${2:-}" = up ] && [ "${FAKE_DOCKER_COMPOSE_FAIL:-0}" = 1 ]; then
            exit 42
        fi
        [ "${2:-}" = up ] && exit 0
        exit 0
        ;;
    load)
        touch "$FAKE_DOCKER_LOAD_MARKER"
        exit 0
        ;;
    image)
        [ "${2:-}" = inspect ] || exit 1
        if [ -n "${FAKE_DOCKER_MISSING_CURRENT:-}" ] && [ ! -e "${FAKE_DOCKER_LOAD_MARKER:-}" ] && [ "${3:-}" = "$FAKE_DOCKER_MISSING_CURRENT" ]; then
            exit 1
        fi
        case "${3:-}" in
            --format)
                case "${4:-}" in
                    *Id*)
                        if [ -e "${FAKE_DOCKER_LOAD_MARKER:-}" ]; then
                            printf '%s\n' "$FAKE_DOCKER_ARCHIVE_DIGEST"
                        else
                            printf '%s\n' "$FAKE_DOCKER_LOCAL_ID"
                        fi
                        ;;
                    *) printf 'linux/amd64\n' ;;
                esac
                ;;
            *) exit 0 ;;
        esac
        ;;
    *) exit 0 ;;
esac
EOF
chmod 0755 "$FAKEBIN/docker"

GOOD="$TMP/good"
make_fixture "$GOOD"

# Re-close a source-free bundle fixture: manifest inventory, internal
# SHA256SUMS and the external sidecar must all describe the same bytes.
CLOSURE_DIR="$TMP/closure"
mkdir -p "$CLOSURE_DIR"
make_fixture "$CLOSURE_DIR/DevBoard-Hub"
CLOSURE_BUNDLE="$TMP/DevBoard-Hub.tar.gz"
(cd "$CLOSURE_DIR" && tar -czf "$CLOSURE_BUNDLE" DevBoard-Hub)
CLOSURE_SIDECAR="$CLOSURE_BUNDLE.sha256"
printf '%s  %s\n' "$(shasum -a 256 "$CLOSURE_BUNDLE" | awk '{print $1}')" "$(basename "$CLOSURE_BUNDLE")" > "$CLOSURE_SIDECAR"
(cd "$(dirname "$CLOSURE_BUNDLE")" && shasum -a 256 -c "$(basename "$CLOSURE_SIDECAR")" >/dev/null)
CLOSURE_CHECK="$TMP/closure-check"
mkdir -p "$CLOSURE_CHECK"
tar -xzf "$CLOSURE_BUNDLE" -C "$CLOSURE_CHECK"
(cd "$CLOSURE_CHECK/DevBoard-Hub" && shasum -a 256 -c SHA256SUMS >/dev/null)
[ "$(jq -r '.imageDigest' "$CLOSURE_CHECK/DevBoard-Hub/manifest.json")" = "$ARCHIVE_CONFIG_DIGEST" ]

export PATH="$FAKEBIN:$PATH"
export FAKE_DOCKER_LOAD_MARKER="$TMP/load-marker"
export DEVBOARD_DATA_DIR="$GOOD/data"
export DEVBOARD_ENV_FILE="$GOOD/.env"
sh "$GOOD/install.sh" >/dev/null
[ -f "$FAKE_DOCKER_LOAD_MARKER" ]
grep -q '^DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q '^DEVBOARD_HUB_PREVIOUS_IMAGE=$' "$GOOD/.env"
grep -q '^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$' "$GOOD/.env"
[ -f "$GOOD/data/config.yaml" ] && [ -f "$GOOD/data/admin.token" ]
DATA_BEFORE=$(shasum -a 256 "$GOOD/data/config.yaml" "$GOOD/data/admin.token")

replace_env() {
    KEY=$1
    VALUE=$2
    TMP_FILE="$GOOD/.env.test.$$"
    awk -v key="$KEY" -v value="$VALUE" 'index($0, key "=") == 1 { print key "=" value; next } { print }' "$GOOD/.env" > "$TMP_FILE"
    chmod 600 "$TMP_FILE"
    mv "$TMP_FILE" "$GOOD/.env"
}
TARGET_MANIFEST=$(shasum -a 256 "$GOOD/manifest.json" | awk '{print $1}')
OLD_IMAGE=devboard/hub:old-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
OLD_MANIFEST=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
replace_env DEVBOARD_HUB_IMAGE "$OLD_IMAGE"
replace_env DEVBOARD_HUB_MANIFEST_SHA256 "$OLD_MANIFEST"
replace_env DEVBOARD_HUB_PREVIOUS_IMAGE ""
replace_env DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256 ""

# First install of a new target records the verified old selection.
sh "$GOOD/install.sh" >/dev/null
grep -q '^DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

# Reinstalling the same target is idempotent and preserves the rollback pair.
sh "$GOOD/install.sh" >/dev/null
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

# Even when the already-selected target image is absent before docker load,
# the matching current marker still identifies this as the same installation;
# the old rollback pair is preserved while the target is reloaded.
rm -f "$FAKE_DOCKER_LOAD_MARKER"
export FAKE_DOCKER_MISSING_CURRENT=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
sh "$GOOD/install.sh" >/dev/null
unset FAKE_DOCKER_MISSING_CURRENT
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

# A failed start leaves the target plus its old rollback pointer. Retrying the
# same failed install must not replace that pointer with the target itself.
if (export FAKE_DOCKER_COMPOSE_FAIL=1; sh "$GOOD/install.sh" >/dev/null 2>&1); then
    echo "compose failure unexpectedly succeeded" >&2
    exit 1
fi
grep -q "^DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

# Rollback is immediately usable after a failed start and restores the old
# running selection before any retry of the installer.
sh "$GOOD/rollback.sh" >/dev/null
grep -q "^DEVBOARD_HUB_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q '^DEVBOARD_HUB_PREVIOUS_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$TARGET_MANIFEST$" "$GOOD/.env"

# Roll forward to the target again, retaining the old version as rollback
# material for the failed-retry matrix below.
sh "$GOOD/install.sh" >/dev/null
grep -q '^DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

if (export FAKE_DOCKER_COMPOSE_FAIL=1; sh "$GOOD/install.sh" >/dev/null 2>&1); then
    echo "failed reinstall unexpectedly succeeded" >&2
    exit 1
fi
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"

# Roll back and roll forward again: each direction records the version being
# replaced, without ever losing the verified pointer.
sh "$GOOD/rollback.sh" >/dev/null
grep -q "^DEVBOARD_HUB_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q '^DEVBOARD_HUB_PREVIOUS_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$TARGET_MANIFEST$" "$GOOD/.env"
sh "$GOOD/install.sh" >/dev/null
grep -q '^DEVBOARD_HUB_IMAGE=devboard/hub:test-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa$' "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_IMAGE=$OLD_IMAGE$" "$GOOD/.env"
grep -q "^DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256=$OLD_MANIFEST$" "$GOOD/.env"
[ "$DATA_BEFORE" = "$(shasum -a 256 "$GOOD/data/config.yaml" "$GOOD/data/admin.token")" ]

# A manifest with a digest that is neither the local pre-load image ID nor the
# archive Config digest must remain rejected after docker load.
WRONG="$TMP/wrong"
make_fixture "$WRONG"
WRONG_DIGEST=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
awk -v digest="$WRONG_DIGEST" '/"imageDigest"/ { sub(/sha256:[0-9a-fA-F]+/, digest) } { print }' "$WRONG/manifest.json" > "$TMP/wrong-manifest.json"
mv "$TMP/wrong-manifest.json" "$WRONG/manifest.json"
awk -v digest="$(shasum -a 256 "$WRONG/manifest.json" | awk '{print $1}')" '$2 == "manifest.json" { print digest "  manifest.json"; next } { print }' "$WRONG/SHA256SUMS" > "$TMP/wrong-checksums"
mv "$TMP/wrong-checksums" "$WRONG/SHA256SUMS"
if (export DEVBOARD_DATA_DIR="$WRONG/data" DEVBOARD_ENV_FILE="$WRONG/.env" FAKE_DOCKER_LOAD_MARKER="$TMP/wrong-load"; sh "$WRONG/install.sh" >/dev/null 2>&1); then
    echo "wrong archive Config digest unexpectedly installed" >&2
    exit 1
fi
[ -f "$TMP/wrong-load" ]

BAD="$TMP/bad"
make_fixture "$BAD"
printf 'tamper\n' >> "$BAD/devboard-hub-linux-amd64-image.tar"
rm -f "$FAKE_DOCKER_LOAD_MARKER"
if (export DEVBOARD_DATA_DIR="$BAD/data" DEVBOARD_ENV_FILE="$BAD/.env"; sh "$BAD/install.sh" >/dev/null 2>&1); then
    echo "tampered bundle unexpectedly installed" >&2
    exit 1
fi
[ ! -e "$FAKE_DOCKER_LOAD_MARKER" ]

echo "offline bundle manifest/checksum/docker-gate tests passed"
