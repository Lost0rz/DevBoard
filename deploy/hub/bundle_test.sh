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
  "imageDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
                    *Id*) printf 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n' ;;
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
