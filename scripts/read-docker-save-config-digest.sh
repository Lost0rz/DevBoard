#!/usr/bin/env bash
set -euo pipefail

fail() {
    echo "!! $*" >&2
    exit 1
}

[[ $# -eq 2 ]] || fail "usage: read-docker-save-config-digest.sh IMAGE_ARCHIVE IMAGE_TAG"
ARCHIVE=$1
IMAGE_TAG=$2
[ -f "$ARCHIVE" ] || fail "Docker save archive is missing."
command -v jq >/dev/null 2>&1 || fail "jq is required to inspect a Docker save manifest."
command -v tar >/dev/null 2>&1 || fail "tar is required to inspect a Docker save archive."

if command -v sha256sum >/dev/null 2>&1; then
    SHA_TOOL=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    SHA_TOOL=shasum
else
    fail "sha256sum or shasum is required to inspect a Docker save archive."
fi

hash_file() {
    case "$SHA_TOOL" in
        sha256sum) sha256sum "$1" | awk '{print $1}' ;;
        shasum) shasum -a 256 "$1" | awk '{print $1}' ;;
    esac
}

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/devboard-save-config.XXXXXX")
cleanup() {
    rm -rf -- "$WORK_DIR"
}
trap cleanup EXIT HUP INT TERM

SAVE_MANIFEST="$WORK_DIR/manifest.json"
CONFIG_FILE="$WORK_DIR/config.json"
tar -xOf "$ARCHIVE" manifest.json > "$SAVE_MANIFEST" || fail "Docker save manifest.json is missing."

CONFIG_PATH=$(jq -er --arg image_tag "$IMAGE_TAG" '
    [ .[] | select((.RepoTags // []) | index($image_tag)) | .Config ]
    | if length == 1 and .[0] != null then .[0] else error("image tag must select exactly one config") end
' "$SAVE_MANIFEST") || fail "Docker save manifest has no unique config for the image tag."
[[ "$CONFIG_PATH" =~ ^blobs/sha256/[0-9a-fA-F]{64}$ ]] || fail "Docker save config path is not a sha256 blob."

tar -xOf "$ARCHIVE" "$CONFIG_PATH" > "$CONFIG_FILE" || fail "Docker save config blob is missing."
CONFIG_DIGEST="sha256:${CONFIG_PATH##*/}"
ACTUAL_DIGEST="sha256:$(hash_file "$CONFIG_FILE")"
[ "$ACTUAL_DIGEST" = "$CONFIG_DIGEST" ] || fail "Docker save config path does not match the config blob digest."

printf '%s\n' "$CONFIG_DIGEST"
