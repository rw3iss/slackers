#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="slackers"
INSTALL_DIR="$HOME/.local/bin"
DESKTOP_FILE="$HOME/.local/share/applications/slackers.desktop"
CONFIG_DIR="$HOME/.config/slackers"

echo "=== Slackers Uninstall Script ==="
echo ""

REMOVED=()

# Remove binary
if [ -f "$INSTALL_DIR/$BINARY_NAME" ]; then
    rm "$INSTALL_DIR/$BINARY_NAME"
    REMOVED+=("Binary: $INSTALL_DIR/$BINARY_NAME")
    echo "Removed $INSTALL_DIR/$BINARY_NAME"
else
    echo "Binary not found at $INSTALL_DIR/$BINARY_NAME (skipped)"
fi

# Remove desktop entry
if [ -f "$DESKTOP_FILE" ]; then
    rm "$DESKTOP_FILE"
    REMOVED+=("Desktop entry: $DESKTOP_FILE")
    echo "Removed $DESKTOP_FILE"
else
    echo "No desktop entry found (skipped)"
fi

# Prompt to remove config
if [ -d "$CONFIG_DIR" ]; then
    echo ""
    read -rp "Remove configuration at $CONFIG_DIR? [y/N] " answer
    case "$answer" in
        [yY]|[yY][eE][sS])
            rm -rf "$CONFIG_DIR"
            REMOVED+=("Config directory: $CONFIG_DIR")
            echo "Removed $CONFIG_DIR"
            ;;
        *)
            echo "Kept $CONFIG_DIR"
            ;;
    esac
fi

echo ""
echo "=== Uninstall complete ==="
if [ ${#REMOVED[@]} -gt 0 ]; then
    echo "Removed:"
    for item in "${REMOVED[@]}"; do
        echo "  - $item"
    done
else
    echo "Nothing was removed."
fi
