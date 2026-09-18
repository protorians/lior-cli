#!/usr/bin/env bash
#
# dev-install.sh — Builds the `liorian` binary (dev mode), then installs it
# so it is available for the whole machine session (global PATH).
#
# The build is always performed first, separately from the installation.
#
# Usage:
#   ./scripts/dev-install.sh [--prefix=PATH] [--build-only] [--install-only]
#
# Behavior:
#   1. BUILD  : compiles the binary (step 1, mandatory).
#   2. INSTALL: copies the binary into /usr/local/bin when writable,
#      otherwise ~/.local/bin, then adds the directory to the current session
#      PATH. An already-installed binary is silently replaced.
#
# Options:
#   --prefix=PATH   installation destination (overrides /usr/local/bin).
#   --build-only    stops after the build (binary left in dist/).
#   --install-only  only installs the binary already present in dist/.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BINARY="liorian"
BUILD_DIR="$ROOT_DIR/dist"
BUILD_BIN="$BUILD_DIR/$BINARY"

PREFIX=""
BUILD_ONLY=false
INSTALL_ONLY=false
for arg in "$@"; do
  case "$arg" in
    --prefix=*) PREFIX="${arg#--prefix=}" ;;
    --build-only) BUILD_ONLY=true ;;
    --install-only) INSTALL_ONLY=true ;;
    *) echo "Usage: $0 [--prefix=PATH] [--build-only] [--install-only]" >&2; exit 1 ;;
  esac
done

if [ "$BUILD_ONLY" = true ] && [ "$INSTALL_ONLY" = true ]; then
  echo "Error: --build-only and --install-only are mutually exclusive." >&2
  exit 1
fi

# ---- Step 1: BUILD -------------------------------------------------------

build() {
  GO="$(command -v go || true)"
  if [ -z "$GO" ]; then
    echo "Error: 'go' is required to build the CLI in dev mode." >&2
    exit 1
  fi

  mkdir -p "$BUILD_DIR"
  echo "=== Step 1/2 — Build (dev) ==="
  GOFLAGS=-mod=mod CGO_ENABLED=0 go build \
    -ldflags "-X main.version=dev -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo none) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o "$BUILD_BIN" .
  echo "Built: $BUILD_BIN"
}

# ---- Step 2: INSTALL -----------------------------------------------------

install_binary() {
  if [ -n "$PREFIX" ]; then
    INSTALL_DIR="$PREFIX"
  else
    if [ -w /usr/local/bin ]; then
      INSTALL_DIR="/usr/local/bin"
    else
      INSTALL_DIR="${HOME}/.local/bin"
    fi
  fi

  mkdir -p "$INSTALL_DIR"

  echo "=== Step 2/2 — Install ==="
  install -m 0755 "$BUILD_BIN" "$INSTALL_DIR/$BINARY"

  # Current session PATH
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) export PATH="$INSTALL_DIR:$PATH" ;;
  esac

  echo ""
  echo "Installed: $INSTALL_DIR/$BINARY"
  "$INSTALL_DIR/$BINARY" --version
  echo "Available in this session as: liorian"

  if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "Add $INSTALL_DIR to your PATH to use it in every session:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  fi
}

if [ "$INSTALL_ONLY" = true ]; then
  if [ ! -f "$BUILD_BIN" ]; then
    echo "Error: $BUILD_BIN not found — run the build step first (without --install-only)." >&2
    exit 1
  fi
  install_binary
else
  build
  if [ "$BUILD_ONLY" = true ]; then
    echo "Build only — nothing installed."
  else
    install_binary
  fi
fi