#!/bin/sh
# This script installs ollama-cluster (github.com/borism/ollama-cluster, a
# fork of Ollama adding LAN GPU pooling via llama.cpp RPC -- see the
# README's Cluster section) on Linux and macOS.
#
# ponytail: release builds are CPU-only and macOS binaries are unsigned for
# now (no CUDA/ROCm/Vulkan backend builds, no Apple Developer ID/notarization
# secrets yet -- see .github/workflows/release.yaml). Upgrade path: add
# backend matrix entries there and drop the CPU-only notice below; add
# signing there and this script goes back to needing no changes, since it
# never handled signing itself.
#
# Downloads from this fork's own GitHub Releases, not ollama.com.

# Wrap script in main function so that a truncated partial download doesn't end
# up executing half a script.
main() {

set -eu

red="$( (/usr/bin/tput bold || :; /usr/bin/tput setaf 1 || :) 2>&-)"
plain="$( (/usr/bin/tput sgr0 || :) 2>&-)"

status() { echo ">>> $*" >&2; }
error() { echo "${red}ERROR:${plain} $*"; exit 1; }
warning() { echo "${red}WARNING:${plain} $*"; }

TEMP_DIR=$(mktemp -d)
cleanup() { rm -rf $TEMP_DIR; }
trap cleanup EXIT

available() { command -v $1 >/dev/null; }
require() {
    local MISSING=''
    for TOOL in $*; do
        if ! available $TOOL; then
            MISSING="$MISSING $TOOL"
        fi
    done

    echo $MISSING
}

OS="$(uname -s)"
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH" ;;
esac

case "$OS" in
    Darwin) PLATFORM=darwin ;;
    Linux) PLATFORM=linux ;;
    *) error 'This script is intended to run on Linux and macOS only.' ;;
esac

REPO="borism/ollama-cluster"
if [ -n "${OLLAMA_VERSION:-}" ]; then
    # GitHub release assets are addressed by tag, not a query-string
    # version param the way ollama.com's download redirector works.
    RELEASE_BASE="https://github.com/${REPO}/releases/download/v${OLLAMA_VERSION#v}"
else
    RELEASE_BASE="https://github.com/${REPO}/releases/latest/download"
fi

NEEDS=$(require curl tar grep tee)
if [ -n "$NEEDS" ]; then
    status "ERROR: The following tools are required but missing:"
    for NEED in $NEEDS; do
        echo "  - $NEED"
    done
    exit 1
fi

# download_and_extract fetches "$RELEASE_BASE/$1.tgz" and extracts it into
# $2. Both platforms' release tarballs share one layout (bin/ollama,
# lib/ollama/*) -- ml/path.go's runtime lookup accepts that layout on
# macOS too (exeDir/../lib/ollama), not just Linux -- so one function
# covers both instead of macOS needing its own install path.
download_and_extract() {
    local filename="$1"
    local dest_dir="$2"

    status "Downloading ${filename}.tgz"
    curl --fail --show-error --location --progress-bar \
        "${RELEASE_BASE}/${filename}.tgz" | \
        $SUDO tar -xzf - -C "${dest_dir}"
}

SUDO=
if [ "$(id -u)" -ne 0 ]; then
    if ! available sudo; then
        error "This script requires superuser permissions to install into a system directory. Please re-run as root."
    fi
    SUDO="sudo"
fi

for BINDIR in /usr/local/bin /usr/bin /bin; do
    echo $PATH | grep -q $BINDIR && break || continue
done
OLLAMA_INSTALL_DIR=$(dirname ${BINDIR})

if [ -d "$OLLAMA_INSTALL_DIR/lib/ollama" ] ; then
    status "Cleaning up old version at $OLLAMA_INSTALL_DIR/lib/ollama"
    $SUDO rm -rf "$OLLAMA_INSTALL_DIR/lib/ollama"
fi
status "Installing ollama to $OLLAMA_INSTALL_DIR"
$SUDO install -o0 -g0 -m755 -d $BINDIR
$SUDO install -o0 -g0 -m755 -d "$OLLAMA_INSTALL_DIR/lib/ollama"

if [ "$PLATFORM" = "darwin" ]; then
    download_and_extract "ollama-darwin" "$OLLAMA_INSTALL_DIR"
else
    download_and_extract "ollama-linux-${ARCH}" "$OLLAMA_INSTALL_DIR"
fi

if [ "$OLLAMA_INSTALL_DIR/bin/ollama" != "$BINDIR/ollama" ] ; then
    status "Making ollama accessible in the PATH in $BINDIR"
    $SUDO ln -sf "$OLLAMA_INSTALL_DIR/bin/ollama" "$BINDIR/ollama"
fi

install_success() {
    status 'Install complete. Run "ollama serve", then "ollama" from the command line.'
    warning "This fork's release builds are CPU-only for now (no CUDA/ROCm/Vulkan/Metal-MLX) -- see https://github.com/${REPO}#readme"
}
trap install_success EXIT

if [ "$PLATFORM" = "darwin" ]; then
    exit 0
fi

###########################################
# Linux only from here
###########################################

IS_WSL2=false

KERN=$(uname -r)
case "$KERN" in
    *icrosoft*WSL2 | *icrosoft*wsl2) IS_WSL2=true;;
    *icrosoft) error "Microsoft WSL1 is not currently supported. Please use WSL2 with 'wsl --set-version <distro> 2'" ;;
    *) ;;
esac

# Everything from this point onwards is optional.

configure_systemd() {
    if ! id ollama >/dev/null 2>&1; then
        status "Creating ollama user..."
        $SUDO useradd -r -s /bin/false -U -m -d /usr/share/ollama ollama
    fi
    if getent group render >/dev/null 2>&1; then
        status "Adding ollama user to render group..."
        $SUDO usermod -a -G render ollama
    fi
    if getent group video >/dev/null 2>&1; then
        status "Adding ollama user to video group..."
        $SUDO usermod -a -G video ollama
    fi

    status "Adding current user to ollama group..."
    $SUDO usermod -a -G ollama $(whoami)

    status "Creating ollama systemd service..."
    cat <<EOF | $SUDO tee /etc/systemd/system/ollama.service >/dev/null
[Unit]
Description=Ollama Service
After=network-online.target

[Service]
ExecStart=$BINDIR/ollama serve
User=ollama
Group=ollama
Restart=always
RestartSec=3
Environment="PATH=$PATH"

[Install]
WantedBy=default.target
EOF
    SYSTEMCTL_RUNNING="$(systemctl is-system-running || true)"
    case $SYSTEMCTL_RUNNING in
        running|degraded)
            status "Enabling and starting ollama service..."
            $SUDO systemctl daemon-reload
            $SUDO systemctl enable ollama

            start_service() { $SUDO systemctl restart ollama; }
            trap start_service EXIT
            ;;
        *)
            warning "systemd is not running"
            if [ "$IS_WSL2" = true ]; then
                warning "see https://learn.microsoft.com/en-us/windows/wsl/systemd#how-to-enable-systemd to enable it"
            fi
            ;;
    esac
}

if available systemctl; then
    configure_systemd
fi

warning "GPU acceleration (CUDA/ROCm/Vulkan) isn't in this fork's release builds yet -- Ollama will run in CPU-only mode. Build from source for GPU support in the meantime (see docs/development.md)."
}

main
