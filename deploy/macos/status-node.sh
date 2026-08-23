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

echo "==> Recent errors (~/Library/Logs/DevBoard/node.err.log)"
tail -n 5 "$HOME/Library/Logs/DevBoard/node.err.log" 2>/dev/null || echo "(none)"
