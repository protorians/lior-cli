#!/usr/bin/env bash
#
# dev-uninstall.sh — Removes the dev binary installed by dev-install.sh
# and optionally cleans the build directory.
#
# Usage:
#   ./scripts/dev-uninstall.sh [--prefix=PATH] [--clean]
#
# Options:
#   --prefix=PATH  directory where the binary was installed (default: auto-detect).
#   --clean        also removes the dist/ build directory.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BINARY="sentients"
BUILD_DIR="$ROOT_DIR/dist"

PREFIX=""
CLEAN=false
for arg in "$@"; do
  case "$arg" in
    --prefix=*) PREFIX="${arg#--prefix=}" ;;
    --clean) CLEAN=true ;;
    *) echo "Usage: $0 [--prefix=PATH] [--clean]" >&2; exit 1 ;;
  esac
done

# ---- Determine install directory ------------------------------------------

if [ -n "$PREFIX" ]; then
  INSTALL_DIR="$PREFIX"
elif [ -f "/usr/local/bin/$BINARY" ]; then
  INSTALL_DIR="/usr/local/bin"
elif [ -f "${HOME}/.local/bin/$BINARY" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
else
  echo "No installed '$BINARY' found in /usr/local/bin or ~/.local/bin." >&2
  exit 1
fi

INSTALL_PATH="$INSTALL_DIR/$BINARY"

# ---- Remove installed binary -----------------------------------------------

if [ -f "$INSTALL_PATH" ]; then
  rm "$INSTALL_PATH"
  echo "Removed: $INSTALL_PATH"
else
  echo "Nothing to remove: $INSTALL_PATH does not exist."
fi

# ---- Optionally clean build directory -------------------------------------

if [ "$CLEAN" = true ]; then
  if [ -d "$BUILD_DIR" ]; then
    rm -rf "$BUILD_DIR"
    echo "Cleaned: $BUILD_DIR"
  else
    echo "Nothing to clean: $BUILD_DIR does not exist."
  fi
fi

echo ""
echo "Dev version uninstalled."
