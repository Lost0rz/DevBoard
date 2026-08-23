#!/bin/bash
# DevBoard M5.5A — Mac Node uninstaller.
#
# Stops and removes the LaunchAgent and the installed binary. The config
# (which may hold the node bearer token) is PRESERVED by default; pass
# --purge to remove it as well. Provider hook configurations are never
# touched automatically.
set -euo pipefail

LABEL="com.devboard.node"
UID_="$(id -u)"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
SUPPORT_DIR="$HOME/Library/Application Support/DevBoard"
CONFIG_PATH="$SUPPORT_DIR/node.yaml"

PURGE=0
for arg in "$@"; do
    case "$arg" in
        --purge) PURGE=1 ;;
        *) echo "unknown option: $arg" >&2; exit 1 ;;
    esac
done

echo "==> Stopping LaunchAgent $LABEL"
launchctl bootout "gui/$UID_/$LABEL" 2>/dev/null || true

if [ -f "$PLIST" ]; then
    rm -f "$PLIST"
    echo "==> Removed $PLIST"
fi
if [ -f "$SUPPORT_DIR/bin/devboard" ]; then
    rm -f "$SUPPORT_DIR/bin/devboard"
    echo "==> Removed $SUPPORT_DIR/bin/devboard"
fi
if [ "$PURGE" -eq 1 ]; then
    rm -f "$CONFIG_PATH"
    echo "==> Purged $CONFIG_PATH"
else
    echo "==> Config preserved at $CONFIG_PATH (use --purge to remove)"
fi
echo "==> Logs remain at ~/Library/Logs/DevBoard"
