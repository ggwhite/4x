#!/bin/sh
set -e

REPO="ggwhite/4x"
BINARY="4x"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
    darwin) OS="darwin" ;;
    linux)  OS="linux" ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
  esac

  case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac
}

get_latest_version() {
  curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" |
    grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
}

main() {
  detect_platform

  VERSION=$(get_latest_version)
  if [ -z "$VERSION" ]; then
    echo "Failed to fetch latest version" >&2
    exit 1
  fi

  echo "Installing ${BINARY} v${VERSION} (${OS}/${ARCH})..."

  EXT="tar.gz"
  if [ "$OS" = "windows" ]; then
    EXT="zip"
  fi

  FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}.${EXT}"
  URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"

  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Downloading ${URL}..."
  curl -sSfL -o "${TMPDIR}/${FILENAME}" "$URL"

  if [ "$EXT" = "zip" ]; then
    unzip -q "${TMPDIR}/${FILENAME}" -d "$TMPDIR"
  else
    tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
  fi

  if [ ! -w "$INSTALL_DIR" ]; then
    echo "Installing to ${INSTALL_DIR} (requires sudo)..."
    sudo install -m 755 "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    install -m 755 "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi

  echo "Installed ${BINARY} v${VERSION} to ${INSTALL_DIR}/${BINARY}"
  "${INSTALL_DIR}/${BINARY}" --version
}

main
