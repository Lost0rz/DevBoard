#!/bin/bash
# DevBoard M5.5A — Mac Node dogfood installer.
#
# Installs the devboard node binary as a per-user LaunchAgent:
#   binary  ~/Library/Application Support/DevBoard/bin/devboard
#   config  ~/Library/Application Support/DevBoard/node.yaml
#   logs    ~/Library/Logs/DevBoard/node.out.log / node.err.log
#   agent   ~/Library/LaunchAgents/com.devboard.node.plist
#
# No sudo. Idempotent: re-running upgrades the binary and restarts the
# service. An existing valid config is NEVER overwritten. The config file is
# mode 0600 because it may hold the node bearer token after pairing.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SUPPORT_DIR="$HOME/Library/Application Support/DevBoard"
BIN_DIR="$SUPPORT_DIR/bin"
BIN_PATH="$BIN_DIR/devboard"
CONFIG_PATH="$SUPPORT_DIR/node.yaml"
LOG_DIR="$HOME/Library/Logs/DevBoard"
OUT_LOG="$LOG_DIR/node.out.log"
ERR_LOG="$LOG_DIR/node.err.log"
PLIST="$HOME/Library/LaunchAgents/com.devboard.node.plist"
LABEL="com.devboard.node"
UID_="$(id -u)"

echo "==> Building devboard from $REPO_ROOT"
mkdir -p "$BIN_DIR" "$LOG_DIR" "$HOME/Library/LaunchAgents"
chmod 700 "$SUPPORT_DIR"
chmod 700 "$BIN_DIR" "$LOG_DIR"
TEMP_BIN=""
cleanup_temp_binary() {
    if [[ -n "$TEMP_BIN" ]]; then
        rm -f -- "$TEMP_BIN"
    fi
}
TEMP_BIN="$(mktemp "$BIN_DIR/.devboard.XXXXXX")"
trap cleanup_temp_binary EXIT HUP INT TERM
(cd "$REPO_ROOT" && go build -o "$TEMP_BIN" ./cmd/devboard)
chmod 755 "$TEMP_BIN"
mv -f "$TEMP_BIN" "$BIN_PATH"
TEMP_BIN=""
trap - EXIT HUP INT TERM

if [ ! -f "$CONFIG_PATH" ]; then
    echo "==> Creating default node config (uplink disabled) at $CONFIG_PATH"
    cat > "$CONFIG_PATH" <<'EOF'
runtime:
  role: "node"

server:
  host: "127.0.0.1"
  port: 8787

host:
  id: "local"
  display_name: "Local Mac"

agent:
  stale_after_seconds: 900

network:
  probe_address: "1.1.1.1:443"
  probe_timeout_milliseconds: 1500

uplink:
  enabled: false
  endpoint: ""
  node_id: ""
  token: ""
EOF
    chmod 600 "$CONFIG_PATH"
else
    echo "==> Existing config kept at $CONFIG_PATH (never overwritten)"
    chmod 600 "$CONFIG_PATH"
fi

echo "==> Generating LaunchAgent at $PLIST"
plist_replacement() {
    # XML-escape the value, then escape sed replacement metacharacters.
    printf '%s' "$1" | sed \
        -e 's/&/\&amp;/g' \
        -e 's/</\&lt;/g' \
        -e 's/>/\&gt;/g' \
        -e 's/[|\\&]/\\&/g'
}
PLIST_BIN="$(plist_replacement "$BIN_PATH")"
PLIST_CONFIG="$(plist_replacement "$CONFIG_PATH")"
PLIST_OUTLOG="$(plist_replacement "$OUT_LOG")"
PLIST_ERRLOG="$(plist_replacement "$ERR_LOG")"
sed -e "s|__DEVBOARD_BIN__|$PLIST_BIN|g" \
    -e "s|__DEVBOARD_CONFIG__|$PLIST_CONFIG|g" \
    -e "s|__DEVBOARD_OUTLOG__|$PLIST_OUTLOG|g" \
    -e "s|__DEVBOARD_ERRLOG__|$PLIST_ERRLOG|g" \
    "$REPO_ROOT/deploy/macos/com.devboard.node.plist.template" > "$PLIST"
chmod 600 "$PLIST"
plutil -lint "$PLIST" >/dev/null

echo "==> (Re)bootstrapping LaunchAgent $LABEL"
launchctl bootout "gui/$UID_/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_" "$PLIST"
launchctl kickstart -k "gui/$UID_/$LABEL"

launchagent_running_pid() {
    local job_info pid
    job_info="$(launchctl print "gui/$UID_/$LABEL" 2>/dev/null)" || return 1
    if ! printf '%s\n' "$job_info" | grep -Eq '^[[:space:]]*state = running[[:space:]]*$'; then
        return 1
    fi
    pid="$(printf '%s\n' "$job_info" | sed -n 's/^[[:space:]]*pid = \([0-9][0-9]*\)[[:space:]]*$/\1/p' | head -n 1)"
    [[ "$pid" =~ ^[0-9]+$ ]] && (( pid > 0 )) || return 1
    printf '%s\n' "$pid"
}

STARTUP_DEADLINE=$((SECONDS + 10))
HEALTHY=0
while (( SECONDS < STARTUP_DEADLINE )); do
    if LAUNCH_PID="$(launchagent_running_pid)"; then
        # The healthcheck itself is bounded to two seconds. Only start one
        # when it can finish inside the ten-second observation window.
        if (( STARTUP_DEADLINE - SECONDS < 2 )); then
            break
        fi
        if "$BIN_PATH" healthcheck --url http://127.0.0.1:8787/health --expect-role node >/dev/null 2>&1; then
            HEALTHY=1
            break
        fi
    fi
    if (( SECONDS < STARTUP_DEADLINE )); then
        sleep 1
    fi
done

if (( HEALTHY == 1 )); then
    echo "==> DevBoard node is running"
    echo "==> Binary:   $BIN_PATH"
    echo "==> Config:   $CONFIG_PATH"
    echo "==> Settings: http://127.0.0.1:8787/settings"
    open "http://127.0.0.1:8787/settings" 2>/dev/null || true
else
    echo "!! LaunchAgent did not become healthy; port 8787 may be occupied by another process." >&2
    exit 1
fi
