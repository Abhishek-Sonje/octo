#!/bin/sh
# install.sh — octo installer
# Usage: curl -fsSL octo.sh/install | sh
set -e

REPO="abhishek-sonje/octo"
BINARY="octo"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) echo "error: unsupported OS: $OS" && exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture: $ARCH" && exit 1 ;;
esac

# Fetch latest release tag from GitHub API
LATEST=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' \
  | cut -d'"' -f4)

if [ -z "$LATEST" ]; then
  echo "error: could not fetch latest release. Check your internet connection."
  exit 1
fi

URL="https://github.com/$REPO/releases/download/$LATEST/${BINARY}-${OS}-${ARCH}"
DEST="/usr/local/bin/$BINARY"

echo "Installing octo $LATEST ($OS/$ARCH)..."

# Download — use sudo only if /usr/local/bin is not writable
if [ -w "/usr/local/bin" ]; then
  curl -fsSL "$URL" -o "$DEST"
  chmod +x "$DEST"
else
  curl -fsSL "$URL" -o "/tmp/$BINARY"
  chmod +x "/tmp/$BINARY"
  sudo mv "/tmp/$BINARY" "$DEST"
fi

echo ""
echo "✓ octo installed to $DEST"
echo ""
echo "  Run: octo"
echo ""