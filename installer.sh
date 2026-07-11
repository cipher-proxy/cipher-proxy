#!/usr/bin/env bash
#
# Cipher Proxy installer
#
# Downloads the prebuilt Linux binary produced by CI (GitHub Releases) and
# installs it, plus an optional desktop launcher.
#
# Usage:
#   ./installer.sh                 # install latest release for current user
#   ./installer.sh --system        # install to /usr/local/bin (needs sudo)
#   VERSION=v1.2.3 ./installer.sh  # install a specific tagged release
#
set -euo pipefail

REPO="cipher-proxy/cipher-proxy"
BINARY_NAME="cipherproxy"
ASSET_NAME="cipherproxy-linux-amd64"
VERSION="${VERSION:-latest}"

# Where the CI-built Linux binary lives on GitHub Releases.
if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
fi

# Install destination.
INSTALL_MODE="user"
for arg in "$@"; do
    case "$arg" in
        --system) INSTALL_MODE="system" ;;
        -h|--help)
            grep '^#' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
    esac
done

if [ "$INSTALL_MODE" = "system" ]; then
    INSTALL_DIR="/usr/local/bin"
    SUDO="sudo"
else
    INSTALL_DIR="${HOME}/.local/bin"
    SUDO=""
fi

echo "==> Cipher Proxy installer"
echo "    Repo:    ${REPO}"
echo "    Version: ${VERSION}"
echo "    Target:  ${INSTALL_DIR}/${BINARY_NAME}"

# Sanity checks.
if [ "$(uname -s)" != "Linux" ]; then
    echo "ERROR: this installer only supports Linux." >&2
    exit 1
fi

if command -v wget >/dev/null 2>&1; then
    FETCH="wget --tries=5 --continue -O"
elif command -v curl >/dev/null 2>&1; then
    FETCH="curl -fL --retry 5 --retry-delay 2 --retry-connrefused --retry-all-errors -C - -o"
else
    echo "ERROR: need either wget or curl installed." >&2
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> Downloading ${DOWNLOAD_URL}"
# shellcheck disable=SC2086
$FETCH "${TMP}/${BINARY_NAME}" "${DOWNLOAD_URL}"

if [ ! -s "${TMP}/${BINARY_NAME}" ]; then
    echo "ERROR: download failed or produced an empty file." >&2
    echo "       Make sure a release with asset '${ASSET_NAME}' exists." >&2
    exit 1
fi

chmod +x "${TMP}/${BINARY_NAME}"

echo "==> Installing to ${INSTALL_DIR}"
$SUDO mkdir -p "${INSTALL_DIR}"
$SUDO install -m 0755 "${TMP}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

# Desktop launcher (user-level).
DESKTOP_DIR="${HOME}/.local/share/applications"
mkdir -p "${DESKTOP_DIR}"
cat > "${DESKTOP_DIR}/cipherproxy.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Cipher Proxy
Exec=${INSTALL_DIR}/${BINARY_NAME}
Comment=SOCKS5/HTTP proxy over SSH
Categories=Network;
Terminal=false
EOF

echo "==> Installed successfully."
echo "    Binary:  ${INSTALL_DIR}/${BINARY_NAME}"
echo "    Launcher: ${DESKTOP_DIR}/cipherproxy.desktop"

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        echo
        echo "NOTE: ${INSTALL_DIR} is not on your PATH."
        echo "      Add this to your ~/.bashrc or ~/.profile:"
        echo "        export PATH=\"${INSTALL_DIR}:\$PATH\""
        ;;
esac

echo
echo "Run it with: ${BINARY_NAME}"
