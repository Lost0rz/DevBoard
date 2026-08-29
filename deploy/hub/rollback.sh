#!/bin/sh
set -eu

SCRIPT_DIR=$(
    CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1
    pwd
)
ENV_FILE="${DEVBOARD_ENV_FILE:-$SCRIPT_DIR/.env}"

fail() {
    echo "!! $*" >&2
    exit 1
}

command -v docker >/dev/null 2>&1 || fail "Docker is required."
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 is required."
[ -f "$ENV_FILE" ] && [ ! -L "$ENV_FILE" ] || fail "The private .env file is unavailable."

env_value() {
    KEY=$1
    awk -F= -v key="$KEY" '$1 == key { count++; value=substr($0,index($0,"=")+1) } END { if (count != 1) exit 1; print value }' "$ENV_FILE"
}

CURRENT_IMAGE=$(env_value DEVBOARD_HUB_IMAGE) || fail "Current verified image selection is missing."
CURRENT_MANIFEST=$(env_value DEVBOARD_HUB_MANIFEST_SHA256) || fail "Current verified manifest marker is missing."
PREVIOUS_IMAGE=$(env_value DEVBOARD_HUB_PREVIOUS_IMAGE) || fail "No verified previous image is available."
PREVIOUS_MANIFEST=$(env_value DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256) || fail "Previous verified manifest marker is missing."

case "$CURRENT_IMAGE" in devboard/hub:[A-Za-z0-9._-]*) ;; *) fail "Current image selection is malformed." ;; esac
case "$PREVIOUS_IMAGE" in devboard/hub:[A-Za-z0-9._-]*) ;; *) fail "Previous image selection is malformed." ;; esac
case "$CURRENT_MANIFEST" in [0-9a-fA-F][0-9a-fA-F]*) [ "${#CURRENT_MANIFEST}" -eq 64 ] || fail "Current manifest marker is malformed." ;; *) fail "Current manifest marker is malformed." ;; esac
case "$PREVIOUS_MANIFEST" in [0-9a-fA-F][0-9a-fA-F]*) [ "${#PREVIOUS_MANIFEST}" -eq 64 ] || fail "Previous manifest marker is malformed." ;; *) fail "Previous manifest marker is malformed." ;; esac

docker image inspect "$PREVIOUS_IMAGE" >/dev/null 2>&1 || fail "The verified previous image is not present locally; rollback stopped."
PLATFORM=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$PREVIOUS_IMAGE")
[ "$PLATFORM" = "linux/amd64" ] || fail "The verified previous image platform is '$PLATFORM'; rollback stopped."

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

set_env_value DEVBOARD_HUB_IMAGE "$PREVIOUS_IMAGE"
set_env_value DEVBOARD_HUB_MANIFEST_SHA256 "$PREVIOUS_MANIFEST"
set_env_value DEVBOARD_HUB_PREVIOUS_IMAGE "$CURRENT_IMAGE"
set_env_value DEVBOARD_HUB_PREVIOUS_MANIFEST_SHA256 "$CURRENT_MANIFEST"

(cd "$SCRIPT_DIR" && docker compose up -d --no-build --force-recreate)
echo "==> Rolled back the running Hub image to the verified previous linux/amd64 image."
echo "==> Persistent config, Admin credentials, Node registry and data were preserved."
