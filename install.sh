#!/bin/sh
# install.sh — download the latest sci binary from GitHub releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/sciminds/sci/main/install.sh | sh
#
# POSIX sh — no bashisms. Runs on bare macOS and Linux.

set -e

REPO="sciminds/sci"
INSTALL_DIR="${HOME}/.local/bin"

# --- Detect platform ---

OS="$(uname -s)"
case "${OS}" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *)
    echo "Error: unsupported OS: ${OS}" >&2
    exit 1
    ;;
esac

ARCH="$(uname -m)"
case "${ARCH}" in
  arm64 | aarch64) ARCH="arm64" ;;
  x86_64)          ARCH="amd64" ;;
  *)
    echo "Error: unsupported architecture: ${ARCH}" >&2
    exit 1
    ;;
esac

ASSET="sci-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/latest/${ASSET}"

# --- Download ---

echo "Downloading ${ASSET}..."
TMPDIR="${TMPDIR:-/tmp}"
TMP="$(mktemp "${TMPDIR}/sci-install-XXXXXX")"
trap 'rm -f "${TMP}"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fSL --progress-bar -o "${TMP}" "${URL}"
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress -O "${TMP}" "${URL}"
else
  echo "Error: curl or wget required" >&2
  exit 1
fi

chmod +x "${TMP}"

# --- Install ---

mkdir -p "${INSTALL_DIR}"
mv "${TMP}" "${INSTALL_DIR}/sci"
trap - EXIT

echo "Installed sci to ${INSTALL_DIR}/sci"

# Ensure INSTALL_DIR is on PATH
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    LINE="export PATH=\"${INSTALL_DIR}:\$PATH\""

    # Pick the right shell rc file
    case "$(basename "${SHELL:-/bin/sh}")" in
      zsh)  RC="${HOME}/.zshrc" ;;
      bash) RC="${HOME}/.bashrc" ;;
      *)    RC="" ;;
    esac

    if [ -n "${RC}" ]; then
      # Append only if the line isn't already there
      if ! grep -qF "${INSTALL_DIR}" "${RC}" 2>/dev/null; then
        echo "" >> "${RC}"
        echo "# Added by sci installer" >> "${RC}"
        echo "${LINE}" >> "${RC}"
        echo "Added ${INSTALL_DIR} to PATH in ${RC}"
        echo "Run 'source ${RC}' or open a new terminal to use sci."
      fi
    else
      echo ""
      echo "Add ${INSTALL_DIR} to your PATH:"
      echo "  ${LINE}"
    fi
    ;;
esac
