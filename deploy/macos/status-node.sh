#!/bin/bash
# DevBoard M5.5A — Mac Node status.
set -euo pipefail

LABEL="com.devboard.node"
UID_="$(id -u)"

echo "==> LaunchAgent state"
launchctl print "gui/$UID_/$LABEL" 2>/dev/null | grep -E "state =|pid =|last exit code =" || echo "not loaded"

echo "==> Health"
curl -fsS -m 3 http://127.0.0.1:8787/health && echo || echo "unreachable"

echo "==> Settings page"
curl -fsS -m 3 -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8787/settings || true

log_size() {
    log_path=$1
    if [ -f "$log_path" ]; then
        printf '%s: %s bytes\n' "$log_path" "$(wc -c < "$log_path")"
    else
        printf '%s: (missing)\n' "$log_path"
    fi
}

echo "==> Log sizes"
log_size "$HOME/Library/Logs/DevBoard/node.out.log"
log_size "$HOME/Library/Logs/DevBoard/node.err.log"
