#!/usr/bin/env bash
#
# Cipher Proxy installer (cross-platform)
#
# Platforms: Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64).
# Downloads the prebuilt binary / app bundle from the latest GitHub Release
# (or a specific tagged release) and installs it. If a previous install
# exists it is cleaned up first, then replaced.
#
# Usage:
#   ./installer.sh                 # install latest for current OS/arch (user)
#   ./installer.sh --system        # Linux/macOS: install to system dir (sudo)
#   VERSION=v1.2.3 ./installer.sh  # install a specific tagged release
#   ARCH=arm64 ./installer.sh      # override detected architecture
#
set -euo pipefail

REPO="cipher-proxy/cipher-proxy"
BINARY_NAME="cipherproxy"
ICON_NAME="cipherproxy"
VERSION="${VERSION:-latest}"

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

# --- Detect OS / arch -----------------------------------------------
OS="$(uname -s)"
ARCH="${ARCH:-$(uname -m)}"
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l|arm)    ARCH="arm"   ;;
esac

case "$OS" in
    Linux)  ASSET="${BINARY_NAME}-linux-${ARCH}" ;;
    Darwin) ASSET="${BINARY_NAME}-darwin-${ARCH}.zip" ;;
    *MINGW*|*MSYS*|*CYGWIN*) OS="Windows"; ASSET="${BINARY_NAME}-windows-${ARCH}.exe" ;;
    *) echo "ERROR: unsupported OS: $OS" >&2; exit 1 ;;
esac

# --- Download URL ----------------------------------------------------
if [ "$VERSION" = "latest" ]; then
    DL_URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
    DL_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

# --- Install destinations --------------------------------------------
if [ "$OS" = "Windows" ]; then
    if [ "$INSTALL_MODE" = "system" ]; then
        INSTALL_DIR="${PROGRAMFILES:-C:/Program Files}/cipherproxy"
        SUDO=""
    else
        INSTALL_DIR="${LOCALAPPDATA:-$HOME/AppData/Local}/cipherproxy"
        SUDO=""
    fi
elif [ "$OS" = "Darwin" ]; then
    if [ "$INSTALL_MODE" = "system" ]; then
        INSTALL_DIR="/Applications"
        SUDO="sudo"
    else
        INSTALL_DIR="${HOME}/Applications"
        SUDO=""
    fi
else # Linux
    if [ "$INSTALL_MODE" = "system" ]; then
        INSTALL_DIR="/usr/local/bin"
        SUDO="sudo"
    else
        INSTALL_DIR="${HOME}/.local/bin"
        SUDO=""
    fi
fi

echo "==> Cipher Proxy installer"
echo "    Repo:    ${REPO}"
echo "    Version: ${VERSION}"
echo "    Target:  ${OS}/${ARCH}"
echo "    Asset:   ${ASSET}"
echo "    Install: ${INSTALL_DIR}"

if [ "$(uname -s)" = "Linux" ] && [ "$OS" = "Windows" ]; then
    echo "ERROR: this installer cannot run from a Linux shell for Windows targets." >&2
    exit 1
fi

# --- Fetch tool ------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
    FETCH="curl -fL --retry 5 --retry-delay 2 --retry-connrefused --retry-all-errors -C - -o"
elif command -v wget >/dev/null 2>&1; then
    FETCH="wget --tries=5 --continue -O"
else
    echo "ERROR: need either curl or wget installed." >&2
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# --- Clean previous install ------------------------------------------
# Stop any running instance first so an in-use binary can be replaced,
# then remove every trace of a previous install before laying down the new one.
clean_previous() {
    pkill -f "${BINARY_NAME}" 2>/dev/null || true
    pkill -f "${BINARY_NAME}.exe" 2>/dev/null || true
    if [ "$OS" = "Linux" ]; then
        $SUDO rm -f "${INSTALL_DIR}/${BINARY_NAME}" 2>/dev/null || true
        rm -f "${HOME}/.local/bin/${BINARY_NAME}" 2>/dev/null || true
        rm -f "${HOME}/.local/share/applications/${BINARY_NAME}.desktop" 2>/dev/null || true
        rm -f "${HOME}/.local/share/icons/${ICON_NAME}.png" 2>/dev/null || true
        rm -f "${HOME}/.local/share/icons/hicolor/*/apps/${ICON_NAME}.png" 2>/dev/null || true
        $SUDO rm -f "/usr/local/bin/${BINARY_NAME}" 2>/dev/null || true
        $SUDO rm -f "/usr/share/applications/${BINARY_NAME}.desktop" 2>/dev/null || true
        $SUDO rm -f "/usr/share/icons/${ICON_NAME}.png" 2>/dev/null || true
        $SUDO rm -f "/usr/share/icons/hicolor/*/apps/${ICON_NAME}.png" 2>/dev/null || true
    elif [ "$OS" = "Darwin" ]; then
        rm -rf "${HOME}/Applications/${BINARY_NAME}.app" 2>/dev/null || true
        $SUDO rm -rf "/Applications/${BINARY_NAME}.app" 2>/dev/null || true
    elif [ "$OS" = "Windows" ]; then
        rm -rf "${INSTALL_DIR}" 2>/dev/null || true
    fi
}
echo "==> Cleaning any previous install"
clean_previous

# --- Download --------------------------------------------------------
echo "==> Downloading ${DL_URL}"
# shellcheck disable=SC2086
$FETCH "${TMP}/${ASSET}" "${DL_URL}"
if [ ! -s "${TMP}/${ASSET}" ]; then
    echo "ERROR: download failed or produced an empty file." >&2
    echo "       Make sure a release with asset '${ASSET}' exists." >&2
    exit 1
fi

# --- Install ----------------------------------------------------------
echo "==> Installing to ${INSTALL_DIR}"
$SUDO mkdir -p "${INSTALL_DIR}"

if [ "$OS" = "Darwin" ]; then
    ( cd "$TMP" && unzip -oq "${ASSET}" )
    APP_SRC="$(find "$TMP" -maxdepth 2 -name '*.app' -type d | head -1)"
    if [ -z "$APP_SRC" ]; then
        echo "ERROR: could not find .app bundle in downloaded archive." >&2
        exit 1
    fi
    $SUDO rm -rf "${INSTALL_DIR}/${BINARY_NAME}.app"
    $SUDO cp -R "$APP_SRC" "${INSTALL_DIR}/"
    echo "    Installed: ${INSTALL_DIR}/${BINARY_NAME}.app"
elif [ "$OS" = "Windows" ]; then
    mkdir -p "${INSTALL_DIR}"
    cp "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY_NAME}.exe"
    echo "    Installed: ${INSTALL_DIR}/${BINARY_NAME}.exe"
else # Linux
    $SUDO install -m 0755 "${TMP}/${ASSET}" "${INSTALL_DIR}/${BINARY_NAME}"

    # Icon: install into the hicolor icon theme (multiple sizes) so GNOME/KDE/XFCE
    # reliably show the app icon in menus, the launcher and the taskbar. The .desktop
    # file then references it by absolute path (falling back to the theme name).
    ICON_DIR="${HOME}/.local/share/icons"
    if [ "$INSTALL_MODE" = "system" ]; then
        ICON_DIR="/usr/share/icons"
    fi
    ICON_BASE="https://raw.githubusercontent.com/${REPO}/main/assets/icons/v4_lock_satellite/png"
    $SUDO mkdir -p "$ICON_DIR/hicolor"
    for size in 16 32 64 128 256 512 1024; do
        tgt="$ICON_DIR/hicolor/${size}x${size}/apps"
        url="$ICON_BASE/icon_${size}.png"
        if command -v curl >/dev/null 2>&1; then
            curl -fsSL "$url" -o "$TMP/icon_${size}.png" 2>/dev/null || true
        elif command -v wget >/dev/null 2>&1; then
            wget -qO "$TMP/icon_${size}.png" "$url" 2>/dev/null || true
        fi
        if [ -s "$TMP/icon_${size}.png" ]; then
            $SUDO mkdir -p "$tgt"
            $SUDO install -m 0644 "$TMP/icon_${size}.png" "$tgt/${ICON_NAME}.png"
        fi
    done
    # Fallback: a top-level copy referenced by absolute path if hicolor is unavailable.
    if [ -s "$TMP/icon_256.png" ]; then
        $SUDO install -m 0644 "$TMP/icon_256.png" "$ICON_DIR/${ICON_NAME}.png" 2>/dev/null || true
    fi
    if [ -s "$ICON_DIR/hicolor/256x256/apps/${ICON_NAME}.png" ]; then
        ICON_REF="$ICON_DIR/hicolor/256x256/apps/${ICON_NAME}.png"
    elif [ -s "$ICON_DIR/${ICON_NAME}.png" ]; then
        ICON_REF="$ICON_DIR/${ICON_NAME}.png"
    else
        ICON_REF="cipherproxy"
    fi

    # .desktop launcher (user-level by default).
    DESKTOP_DIR="${HOME}/.local/share/applications"
    if [ "$INSTALL_MODE" = "system" ]; then
        DESKTOP_DIR="/usr/share/applications"
    fi
    $SUDO mkdir -p "$DESKTOP_DIR"
    cat > "$TMP/${BINARY_NAME}.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Cipher Proxy
Exec=${INSTALL_DIR}/${BINARY_NAME}
Icon=${ICON_REF}
Comment=SOCKS5/HTTP proxy over SSH
Categories=Network;
Terminal=false
StartupWMClass=${BINARY_NAME}
EOF
    $SUDO install -m 0644 "$TMP/${BINARY_NAME}.desktop" "$DESKTOP_DIR/${BINARY_NAME}.desktop"

    # Refresh icon cache if available (best-effort).
    if command -v gtk-update-icon-cache >/dev/null 2>&1; then
        $SUDO gtk-update-icon-cache -q -f -t "$ICON_DIR" 2>/dev/null || true
        $SUDO gtk-update-icon-cache -q -f -t "$ICON_DIR/hicolor" 2>/dev/null || true
    fi
    if command -v update-desktop-database >/dev/null 2>&1; then
        $SUDO update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
    fi
    echo "    Binary:   ${INSTALL_DIR}/${BINARY_NAME}"
    echo "    Icon:     ${ICON_REF:-<embedded>}"
    echo "    Launcher: ${DESKTOP_DIR}/${BINARY_NAME}.desktop"

    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            echo
            echo "NOTE: ${INSTALL_DIR} is not on your PATH."
            echo "      Add this to your ~/.bashrc or ~/.profile:"
            echo "        export PATH=\"${INSTALL_DIR}:\$PATH\""
            ;;
    esac
fi

echo
echo "==> Done. Run it with: ${BINARY_NAME}"
if [ "$OS" = "Darwin" ]; then
    echo "    (macOS: launch '${BINARY_NAME}.app' from Applications; if Gatekeeper"
    echo "     blocks it, run: xattr -cr \"${INSTALL_DIR}/${BINARY_NAME}.app\")"
fi
