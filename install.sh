#!/bin/bash
set -euo pipefail

REPO="${MMCLI_REPO:-jneb802/mod-manager-cli}"
DEFAULT_INSTALL_DIR="$HOME/.local/bin"

choose_install_dir() {
  if [ -n "${MMCLI_INSTALL_DIR:-}" ]; then
    echo "$MMCLI_INSTALL_DIR"
    return
  fi

  existing="$(command -v mmcli 2>/dev/null || true)"
  if [ -n "$existing" ] && [ ! -L "$existing" ] && [ -w "$existing" ] && [ -w "$(dirname "$existing")" ]; then
    dirname "$existing"
    return
  fi

  echo "$DEFAULT_INSTALL_DIR"
}

path_contains() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  arm64) BINARY="mmcli-darwin-arm64" ;;
  x86_64) BINARY="mmcli-darwin-amd64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "Failed to fetch latest release."
  exit 1
fi
echo "Latest version: $TAG"

# Download
URL="https://github.com/$REPO/releases/download/$TAG/$BINARY"
TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT
echo "Downloading $BINARY..."
curl -fsSL -o "$TMPFILE" "$URL"

# Install
chmod +x "$TMPFILE"
INSTALL_DIR="$(choose_install_dir)"
mkdir -p "$INSTALL_DIR"

if [ ! -w "$INSTALL_DIR" ]; then
  echo "Install directory is not writable: $INSTALL_DIR"
  echo "Choose a user-writable directory with:"
  echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | MMCLI_INSTALL_DIR=\"\$HOME/.local/bin\" bash"
  exit 1
fi

echo "Installing to $INSTALL_DIR/mmcli..."
mv "$TMPFILE" "$INSTALL_DIR/mmcli"
trap - EXIT

echo "mmcli installed successfully."

if ! path_contains "$INSTALL_DIR"; then
  echo
  echo "Add this directory to your PATH so your shell can find mmcli:"
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
fi

CURRENT="$(command -v mmcli 2>/dev/null || true)"
if [ -n "$CURRENT" ] && [ "$CURRENT" != "$INSTALL_DIR/mmcli" ]; then
  echo
  echo "Note: your shell currently finds mmcli at:"
  echo "  $CURRENT"
  echo "If that is an older copy, put $INSTALL_DIR earlier in PATH or remove the old copy."
fi

echo
echo "Run 'mmcli init' to get started."
