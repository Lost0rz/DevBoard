#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1 && pwd)
ENV_FILE="${DEVBOARD_ACTIVATOR_ENV_FILE:-$SCRIPT_DIR/.env}"
DATA_DIR="${DEVBOARD_HUB_DATA_DIR:-$SCRIPT_DIR/../DevBoard-Hub/data}"
HUB_ENV="$(dirname "$DATA_DIR")/.env"

fail() { echo "!! $*" >&2; exit 1; }
[ ! -L "$ENV_FILE" ] || fail "Activator .env must not be a symbolic link."
[ -d "$DATA_DIR" ] && [ ! -L "$DATA_DIR" ] || fail "DEVBOARD_HUB_DATA_DIR must be the existing non-symlink Hub data directory."
[ -f "$DATA_DIR/config.yaml" ] && [ ! -L "$DATA_DIR/config.yaml" ] || fail "Hub config.yaml is missing or unsafe."
[ -f "$HUB_ENV" ] && [ ! -L "$HUB_ENV" ] || fail "Hub Compose identity file is missing or unsafe."

read_env() {
    awk -F= -v key="$1" '$1 == key { count++; value=substr($0,index($0,"=")+1) } END { if (count != 1 || value == "") exit 1; print value }' "$HUB_ENV"
}
UID_VALUE=$(read_env DEVBOARD_UID) || fail "Hub .env must contain exactly one DEVBOARD_UID."
GID_VALUE=$(read_env DEVBOARD_GID) || fail "Hub .env must contain exactly one DEVBOARD_GID."
case "$UID_VALUE:$GID_VALUE" in *[!0-9:]*|:) fail "Hub UID:GID is invalid.";; esac

umask 077
touch "$ENV_FILE"
chmod 600 "$ENV_FILE"
set_value() {
    key=$1 value=$2 tmp="$ENV_FILE.tmp.$$"
    awk -v key="$key" -v value="$value" 'index($0, key "=") == 1 { count++; if (count > 1) exit 2; print key "=" value; found=1; next } { print } END { if (!found) print key "=" value }' "$ENV_FILE" > "$tmp" || { rm -f "$tmp"; fail "Activator .env update rejected."; }
    chmod 600 "$tmp" && mv "$tmp" "$ENV_FILE"
}
set_value DEVBOARD_HUB_DATA_DIR "$DATA_DIR"
set_value DEVBOARD_UID "$UID_VALUE"
set_value DEVBOARD_GID "$GID_VALUE"
echo "==> Reusing private Hub data: $DATA_DIR"
echo "==> Activator will run as Hub UID:GID $UID_VALUE:$GID_VALUE"
