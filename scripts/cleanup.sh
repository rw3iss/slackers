#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="$HOME/.local/share/slackers"
CONFIG_DIR="$HOME/.config/slackers"
DEFAULT_CONFIG='{
  "bot_token": "",
  "app_token": "",
  "sidebar_width": 25,
  "timestamp_format": "15:04"
}'

echo "=== Slackers Cleanup Script ==="
echo ""

CLEANED=()

# Remove cached/temporary data
if [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
    CLEANED+=("Data directory: $DATA_DIR")
    echo "Removed $DATA_DIR"
else
    echo "No data directory found at $DATA_DIR (skipped)"
fi

# Remove any temp files in /tmp
TMP_FILES=$(find /tmp -maxdepth 1 -name "slackers*" 2>/dev/null || true)
if [ -n "$TMP_FILES" ]; then
    echo "$TMP_FILES" | xargs rm -rf
    CLEANED+=("Temp files in /tmp")
    echo "Removed temp files from /tmp"
else
    echo "No temp files found (skipped)"
fi

# Optionally reset config to defaults
if [ -d "$CONFIG_DIR" ]; then
    echo ""
    read -rp "Reset configuration to defaults? [y/N] " answer
    case "$answer" in
        [yY]|[yY][eE][sS])
            echo "$DEFAULT_CONFIG" > "$CONFIG_DIR/config.json"
            CLEANED+=("Config reset to defaults")
            echo "Config reset to defaults at $CONFIG_DIR/config.json"
            ;;
        *)
            echo "Kept existing configuration"
            ;;
    esac
fi

echo ""
echo "=== Cleanup complete ==="
if [ ${#CLEANED[@]} -gt 0 ]; then
    echo "Summary:"
    for item in "${CLEANED[@]}"; do
        echo "  - $item"
    done
else
    echo "Nothing to clean up."
fi
