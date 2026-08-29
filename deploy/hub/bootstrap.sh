#!/bin/sh
# Prepare private, persistent Hub data without starting the service. Existing
# config, registry data, admin credentials, and Compose identity are preserved.
set -eu

SCRIPT_DIR=$(
    CDPATH= cd "$(dirname "$0")" >/dev/null 2>&1
    pwd
)
DATA_DIR="${DEVBOARD_DATA_DIR:-$SCRIPT_DIR/data}"
ENV_FILE="${DEVBOARD_ENV_FILE:-$SCRIPT_DIR/.env}"
CONFIG_PATH="$DATA_DIR/config.yaml"
ADMIN_TOKEN_PATH="$DATA_DIR/admin.token"
ADMIN_PASSWORD_PATH="$DATA_DIR/admin.password"
AGENT_QUOTA_AUDIT_TOKEN_PATH="$DATA_DIR/agent-quota-audit.token"
CONFIG_TMP="$CONFIG_PATH.tmp.$$"
TOKEN_TMP="$ADMIN_TOKEN_PATH.tmp.$$"
AUDIT_TOKEN_TMP="$AGENT_QUOTA_AUDIT_TOKEN_PATH.tmp.$$"

cleanup() {
    rm -f "$CONFIG_TMP" "$TOKEN_TMP" "$AUDIT_TOKEN_TMP"
}
trap cleanup 0 HUP INT TERM

fail() {
    echo "!! $*" >&2
    exit 1
}

reject_symlink_or_wrong_type() {
    TARGET=$1
    KIND=$2
    if [ -L "$TARGET" ]; then
        fail "$KIND must not be a symbolic link."
    fi
    if [ -e "$TARGET" ]; then
        case "$KIND" in
            data-directory)
                [ -d "$TARGET" ] || fail "$KIND must be a directory."
                ;;
            *)
                [ -f "$TARGET" ] || fail "$KIND must be a regular file."
                ;;
        esac
    fi
}

if [ "$(id -u)" -eq 0 ]; then
    DEFAULT_UID=65532
    DEFAULT_GID=65532
else
    DEFAULT_UID="$(id -u)"
    DEFAULT_GID="$(id -g)"
fi

umask 077
reject_symlink_or_wrong_type "$DATA_DIR" data-directory
mkdir -p "$DATA_DIR"
if [ -L "$DATA_DIR" ] || [ ! -d "$DATA_DIR" ]; then
    fail "data-directory is unavailable."
fi
chmod 700 "$DATA_DIR"

reject_symlink_or_wrong_type "$CONFIG_PATH" config.yaml
if [ ! -e "$CONFIG_PATH" ]; then
	if [ -e "$CONFIG_TMP" ] || [ -L "$CONFIG_TMP" ]; then
		fail "temporary config target already exists."
	fi
    cat > "$CONFIG_TMP" <<'EOF'
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
  password_file: "/var/lib/devboard/admin.password"
EOF
    chmod 600 "$CONFIG_TMP"
	mv "$CONFIG_TMP" "$CONFIG_PATH"
	echo "==> Created private Hub config: $CONFIG_PATH"
else
	echo "==> Existing Hub config and Registry preserved: $CONFIG_PATH"
fi
if [ -L "$CONFIG_PATH" ] || [ ! -f "$CONFIG_PATH" ]; then
    fail "config.yaml is unavailable."
fi
chmod 600 "$CONFIG_PATH"

reject_symlink_or_wrong_type "$ADMIN_TOKEN_PATH" admin.token
if [ ! -e "$ADMIN_TOKEN_PATH" ]; then
	if [ -e "$TOKEN_TMP" ] || [ -L "$TOKEN_TMP" ]; then
		fail "temporary admin credential target already exists."
	fi
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32 > "$TOKEN_TMP"
    elif [ -r /dev/urandom ] && command -v od >/dev/null 2>&1 && command -v tr >/dev/null 2>&1; then
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > "$TOKEN_TMP"
        printf '\n' >> "$TOKEN_TMP"
    else
        fail "Secure random generation requires openssl or readable /dev/urandom with od and tr."
    fi
    chmod 600 "$TOKEN_TMP"
    if ! LC_ALL=C awk 'NR == 1 && length($0) == 64 && $0 ~ /^[0-9a-fA-F]+$/ { ok = 1; next } { ok = 0 } END { exit !(NR == 1 && ok) }' "$TOKEN_TMP"; then
        fail "Secure admin credential generation failed."
    fi
	mv "$TOKEN_TMP" "$ADMIN_TOKEN_PATH"
	echo "==> Created private admin credential file: $ADMIN_TOKEN_PATH"
else
	echo "==> Existing admin credential preserved: $ADMIN_TOKEN_PATH"
fi
if [ -L "$ADMIN_TOKEN_PATH" ] || [ ! -f "$ADMIN_TOKEN_PATH" ]; then
    fail "admin.token is unavailable."
fi
chmod 600 "$ADMIN_TOKEN_PATH"
if ! LC_ALL=C awk 'NR == 1 && length($0) == 64 && $0 ~ /^[0-9a-fA-F]+$/ { ok = 1; next } { ok = 0 } END { exit !(NR == 1 && ok) }' "$ADMIN_TOKEN_PATH"; then
    fail "Existing admin credential must contain exactly 64 hexadecimal characters; it was preserved unchanged."
fi

# The read-only Agent Quota audit API has an independent bearer credential.
# Keep it separate from both Node provisioning and the GLM provider key so a
# monitoring client never receives mutation authority.
reject_symlink_or_wrong_type "$AGENT_QUOTA_AUDIT_TOKEN_PATH" agent-quota-audit.token
if [ ! -e "$AGENT_QUOTA_AUDIT_TOKEN_PATH" ]; then
	if [ -e "$AUDIT_TOKEN_TMP" ] || [ -L "$AUDIT_TOKEN_TMP" ]; then
		fail "temporary agent quota audit token target already exists."
	fi
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32 > "$AUDIT_TOKEN_TMP"
    elif [ -r /dev/urandom ] && command -v od >/dev/null 2>&1 && command -v tr >/dev/null 2>&1; then
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n' > "$AUDIT_TOKEN_TMP"
        printf '\n' >> "$AUDIT_TOKEN_TMP"
    else
        fail "Secure random generation requires openssl or readable /dev/urandom with od and tr."
    fi
    chmod 600 "$AUDIT_TOKEN_TMP"
    if ! LC_ALL=C awk 'NR == 1 && length($0) == 64 && $0 ~ /^[0-9a-fA-F]+$/ { ok = 1; next } { ok = 0 } END { exit !(NR == 1 && ok) }' "$AUDIT_TOKEN_TMP"; then
        fail "Secure agent quota audit token generation failed."
    fi
	mv "$AUDIT_TOKEN_TMP" "$AGENT_QUOTA_AUDIT_TOKEN_PATH"
	echo "==> Created private Agent Quota audit API token: $AGENT_QUOTA_AUDIT_TOKEN_PATH"
else
	echo "==> Existing Agent Quota audit API token preserved: $AGENT_QUOTA_AUDIT_TOKEN_PATH"
fi
if [ -L "$AGENT_QUOTA_AUDIT_TOKEN_PATH" ] || [ ! -f "$AGENT_QUOTA_AUDIT_TOKEN_PATH" ]; then
    fail "agent-quota-audit.token is unavailable."
fi
chmod 600 "$AGENT_QUOTA_AUDIT_TOKEN_PATH"
if ! LC_ALL=C awk 'NR == 1 && length($0) == 64 && $0 ~ /^[0-9a-fA-F]+$/ { ok = 1; next } { ok = 0 } END { exit !(NR == 1 && ok) }' "$AGENT_QUOTA_AUDIT_TOKEN_PATH"; then
    fail "Existing Agent Quota audit token must contain exactly 64 hexadecimal characters; it was preserved unchanged."
fi

# The human Admin password is created from the first-run web form. Preserve
# an existing record, but reject links and broad permissions before Compose
# starts so the application can never silently widen its secret boundary.
reject_symlink_or_wrong_type "$ADMIN_PASSWORD_PATH" admin.password
if [ -e "$ADMIN_PASSWORD_PATH" ]; then
    chmod 600 "$ADMIN_PASSWORD_PATH"
fi

reject_symlink_or_wrong_type "$ENV_FILE" .env
touch "$ENV_FILE"
chmod 600 "$ENV_FILE"
if ! grep -q '^DEVBOARD_UID=' "$ENV_FILE"; then
    printf 'DEVBOARD_UID=%s\n' "$DEFAULT_UID" >> "$ENV_FILE"
fi
if ! grep -q '^DEVBOARD_GID=' "$ENV_FILE"; then
    printf 'DEVBOARD_GID=%s\n' "$DEFAULT_GID" >> "$ENV_FILE"
fi
RUN_UID="$(awk -F= '$1 == "DEVBOARD_UID" { count++; value = substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$ENV_FILE")" ||
    fail "$ENV_FILE must contain exactly one DEVBOARD_UID entry."
RUN_GID="$(awk -F= '$1 == "DEVBOARD_GID" { count++; value = substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "$ENV_FILE")" ||
    fail "$ENV_FILE must contain exactly one DEVBOARD_GID entry."
IMAGE_COUNT="$(awk -F= '$1 == "DEVBOARD_HUB_IMAGE" { count++ } END { print count + 0 }' "$ENV_FILE")"
[ "$IMAGE_COUNT" -le 1 ] || fail "$ENV_FILE must contain at most one DEVBOARD_HUB_IMAGE entry."

case "$RUN_UID" in
    ''|0|*[!0-9]*) fail "DEVBOARD_UID must be a non-root numeric UID." ;;
esac
case "$RUN_GID" in
    ''|0|*[!0-9]*) fail "DEVBOARD_GID must be a non-root numeric GID." ;;
esac

if [ "$(id -u)" -eq 0 ]; then
    chown -R "$RUN_UID:$RUN_GID" "$DATA_DIR"
elif [ "$RUN_UID" != "$(id -u)" ] || [ "$RUN_GID" != "$(id -g)" ]; then
    fail "$ENV_FILE selects UID:GID $RUN_UID:$RUN_GID, but bootstrap is running as $(id -u):$(id -g). Run as that user or correct the non-secret identity file; existing Hub data was preserved."
fi

echo "==> Compose identity file ready: $ENV_FILE"
echo "==> Persistent Hub data ready: $DATA_DIR"
echo "==> Web Admin password is created on first visit; credential contents were not printed."
