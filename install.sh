#!/bin/bash
set -euo pipefail

REPO="${MMCLI_REPO:-jneb802/mod-manager-cli}"
DEFAULT_INSTALL_DIR="$HOME/.local/bin"

choose_install_dir() {
  local existing dir old_ifs

  if [ -n "${MMCLI_INSTALL_DIR:-}" ]; then
    echo "$MMCLI_INSTALL_DIR"
    return
  fi

  existing="$(command -v mmcli 2>/dev/null || true)"
  if [ -n "$existing" ] && [ ! -L "$existing" ] && [ -w "$existing" ] && [ -w "$(dirname "$existing")" ]; then
    dirname "$existing"
    return
  fi

  old_ifs="$IFS"
  IFS=":"
  for dir in ${PATH:-}; do
    if [ -n "$dir" ] && [ "${dir#/}" != "$dir" ] && [ -d "$dir" ] && [ -w "$dir" ]; then
      IFS="$old_ifs"
      echo "$dir"
      return
    fi
  done
  IFS="$old_ifs"

  echo "$DEFAULT_INSTALL_DIR"
}

path_contains() {
  case ":${PATH:-}:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

shell_quote() {
  printf "%q" "$1"
}

shell_profile() {
  local shell_name
  shell_name="$(basename "${SHELL:-}")"
  case "$shell_name" in
    zsh) echo "$HOME/.zshrc" ;;
    bash)
      if [ "$(uname -s)" = "Darwin" ]; then
        echo "$HOME/.bash_profile"
      else
        echo "$HOME/.bashrc"
      fi
      ;;
    *)
      if [ -f "$HOME/.zshrc" ]; then
        echo "$HOME/.zshrc"
      elif [ -f "$HOME/.bashrc" ]; then
        echo "$HOME/.bashrc"
      elif [ "$(uname -s)" = "Darwin" ]; then
        echo "$HOME/.zshrc"
      else
        echo "$HOME/.profile"
      fi
      ;;
  esac
}

add_to_shell_profile() {
  local dir profile marker line quoted_dir
  dir="$1"
  profile="$(shell_profile)"
  marker="# Added by mmcli installer"
  quoted_dir="$(shell_quote "$dir")"
  line="export PATH=$quoted_dir:\$PATH"

  if [ -f "$profile" ] && grep -F "$line" "$profile" >/dev/null 2>&1; then
    echo "$profile"
    return 0
  fi

  if [ -e "$profile" ] && [ ! -w "$profile" ]; then
    return 1
  fi

  mkdir -p "$(dirname "$profile")" 2>/dev/null || return 1
  if {
    echo
    echo "$marker"
    echo "$line"
  } 2>/dev/null >> "$profile"; then
    echo "$profile"
    return 0
  fi

  return 1
}

# Detect platform
OS=$(uname -s)
ARCH=$(uname -m)
case "$OS/$ARCH" in
  Darwin/arm64) BINARY="mmcli-darwin-arm64" ;;
  Darwin/x86_64) BINARY="mmcli-darwin-amd64" ;;
  Linux/x86_64) BINARY="mmcli-linux-amd64" ;;
  *) echo "Unsupported platform: $OS/$ARCH"; exit 1 ;;
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

if path_contains "$INSTALL_DIR"; then
  echo
  echo "Run 'mmcli init' to get started."
else
  echo
  if PROFILE="$(add_to_shell_profile "$INSTALL_DIR")"; then
    echo "Added $INSTALL_DIR to PATH in $PROFILE."
    echo "Restart your terminal, or run this now:"
    echo "  source \"$PROFILE\""
  else
    echo "Add this directory to your PATH so your shell can find mmcli:"
    echo "  export PATH=$(shell_quote "$INSTALL_DIR"):\$PATH"
  fi
  echo
  echo "You can also run mmcli immediately with:"
  echo "  \"$INSTALL_DIR/mmcli\" init"
fi

CURRENT="$(command -v mmcli 2>/dev/null || true)"
if [ -n "$CURRENT" ] && [ "$CURRENT" != "$INSTALL_DIR/mmcli" ]; then
  echo
  echo "Note: your shell currently finds mmcli at:"
  echo "  $CURRENT"
  echo "If that is an older copy, put $INSTALL_DIR earlier in PATH or remove the old copy."
fi
