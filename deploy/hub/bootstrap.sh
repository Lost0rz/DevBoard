#!/bin/sh
# Prepare the persistent Hub dogfood data directory without starting or
# replacing a running service. Existing config, admin secret, and .env files
# are preserved; missing non-secret UID/GID keys are appended to .env.
set -eu

SCRIPT_DIR=$(
    CDPATH= cd -- "$(dirname -- "$0")" >/dev/null 2>&1
    pwd
)
DATA_DIR="${DEVBOARD_DATA_DIR:-$SCRIPT_DIR/data}"
ENV_FILE="${DEVBOARD_ENV_FILE:-$SCRIPT_DIR/.env}"
CONFIG_PATH="$DATA_DIR/config.yaml"
ADMIN_TOKEN_PATH="$DATA_DIR/admin.token"

if [ "$(id -u)" -eq 0 ]; then
    CONTAINER_UID=65532
    CONTAINER_GID=65532
else
    CONTAINER_UID="$(id -u)"
    CONTAINER_GID="$(id -g)"
fi

umask 077
mkdir -p "$DATA_DIR"
chmod 700 "$DATA_DIR"

if [ ! -f "$CONFIG_PATH" ]; then
    cat > "$CONFIG_PATH" <<'EOF'
runtime:
  role: "hub"
server:
  host: "0.0.0.0"
  port: 8787
display:
  dashboard_refresh_seconds: 2
nodes:
  registered: ""
  disabled: ""
admin:
  enabled: true
  token_file: "/var/lib/devboard/admin.token"
EOF
    echo "==> Created Hub config: $CONFIG_PATH"
else
    echo "==> Existing Hub config preserved: $CONFIG_PATH"
fi
chmod 600 "$CONFIG_PATH"

if [ ! -f "$ADMIN_TOKEN_PATH" ]; then
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32 > "$ADMIN_TOKEN_PATH"
    else
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > "$ADMIN_TOKEN_PATH"
        printf '\n' >> "$ADMIN_TOKEN_PATH"
    fi
    echo "==> Created admin credential file: $ADMIN_TOKEN_PATH"
else
    echo "==> Existing admin credential preserved: $ADMIN_TOKEN_PATH"
fi
chmod 600 "$ADMIN_TOKEN_PATH"
if ! LC_ALL=C awk 'NR == 1 && length($0) == 64 && $0 ~ /^[0-9a-fA-F]+$/ { ok = 1; next } { ok = 0 } END { exit !(NR == 1 && ok) }' "$ADMIN_TOKEN_PATH"; then
    echo "!! Existing admin credential must contain exactly 64 hexadecimal characters; it was preserved unchanged." >&2
    exit 1
fi

touch "$ENV_FILE"
chmod 600 "$ENV_FILE"
if ! grep -q '^DEVBOARD_UID=' "$ENV_FILE"; then
    printf 'DEVBOARD_UID=%s\n' "$CONTAINER_UID" >> "$ENV_FILE"
fi
if ! grep -q '^DEVBOARD_GID=' "$ENV_FILE"; then
    printf 'DEVBOARD_GID=%s\n' "$CONTAINER_GID" >> "$ENV_FILE"
fi

RUN_UID="$(sed -n 's/^DEVBOARD_UID=//p' "$ENV_FILE" | tail -n 1)"
RUN_GID="$(sed -n 's/^DEVBOARD_GID=//p' "$ENV_FILE" | tail -n 1)"
case "$RUN_UID:$RUN_GID" in
    *[!0-9:]*|:*|*:|0:*|*:0)
        echo "!! $ENV_FILE must define a non-root numeric DEVBOARD_UID and numeric DEVBOARD_GID." >&2
        exit 1
        ;;
esac

if [ "$(id -u)" -eq 0 ]; then
    chown -R "$RUN_UID:$RUN_GID" "$DATA_DIR"
elif [ "$RUN_UID" != "$(id -u)" ] || [ "$RUN_GID" != "$(id -g)" ]; then
    echo "!! $ENV_FILE names UID:GID $RUN_UID:$RUN_GID, but bootstrap is running as $(id -u):$(id -g)." >&2
    echo "!! Run as that user or correct the non-secret identity file; config and token were preserved." >&2
    exit 1
fi

echo "==> Compose identity file: $ENV_FILE"
echo "==> Ensure the verified devboard/hub:dogfood image is loaded and tagged."
echo "==> Next: cd $SCRIPT_DIR && docker compose up -d --no-build"
echo "==> Admin URL: http://<NAS>:<PORT>/admin"
echo "==> The admin credential was not printed. Read it locally from $ADMIN_TOKEN_PATH when logging in."
