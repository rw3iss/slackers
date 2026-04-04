#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="slackers"
INSTALL_DIR="$HOME/.local/bin"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=== Slackers Install Script ==="
echo ""

# Detect OS
OS="$(uname -s)"
case "$OS" in
    Linux)
        echo "Detected OS: Linux"
        ;;
    Darwin)
        echo "Detected OS: macOS"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        echo "Detected OS: Windows (via $OS)"
        echo "Note: On Windows, consider using 'go install' directly."
        ;;
    *)
        echo "Warning: Unknown OS '$OS'. Proceeding anyway."
        ;;
esac

echo ""

# Build the binary
echo "Building $BINARY_NAME..."
cd "$PROJECT_DIR"
go build -ldflags "-s -w" -o "$BINARY_NAME" ./cmd/slackers
echo "Build successful."

# Create install directory if needed
if [ ! -d "$INSTALL_DIR" ]; then
    echo "Creating $INSTALL_DIR..."
    mkdir -p "$INSTALL_DIR"
fi

# Install binary
echo "Installing to $INSTALL_DIR/$BINARY_NAME..."
mv "$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
chmod +x "$INSTALL_DIR/$BINARY_NAME"

# Check if install dir is in PATH
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    echo ""
    echo "NOTE: $INSTALL_DIR is not in your PATH."
    echo "Add it by appending this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
    echo ""
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
fi

# Create desktop entry on Linux
if [ "$OS" = "Linux" ]; then
    DESKTOP_DIR="$HOME/.local/share/applications"
    DESKTOP_FILE="$DESKTOP_DIR/slackers.desktop"
    mkdir -p "$DESKTOP_DIR"
    cat > "$DESKTOP_FILE" <<DESKTOP
[Desktop Entry]
Name=Slackers
Comment=Terminal-based Slack client
Exec=$INSTALL_DIR/$BINARY_NAME
Type=Application
Terminal=true
Categories=Network;Chat;
DESKTOP
    echo "Created desktop entry at $DESKTOP_FILE"
fi

echo ""
echo "=== Installation complete ==="
echo ""
echo "Usage:"
echo "  slackers          Launch the TUI"
echo "  slackers setup    Run interactive setup"
echo "  slackers version  Show version"
echo "  slackers --help   Show all commands"
