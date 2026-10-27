#!/bin/sh
# paddlekeys installer.
#
#   curl -fsSL https://raw.githubusercontent.com/stack-bound/ham-cwusb-paddle-keys-usb/master/install.sh | sh
#
# Works out your CPU and package manager, downloads the matching package from
# the latest GitHub release, checks it against the release checksums and
# installs it (using sudo if needed). On distros without deb/rpm/pacman it
# installs the binary, systemd unit and udev rule from the tarball instead.
#
# Environment:
#   PADDLEKEYS_VERSION   install this tag (e.g. v1.0.0) instead of the latest.
set -eu

REPO="stack-bound/ham-cwusb-paddle-keys-usb"
NAME="paddlekeys"

say()  { printf '%s\n' "$*" >&2; }
die()  { say "error: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }

need curl
need uname
[ "$(uname -s)" = "Linux" ] || die "paddlekeys only runs on Linux"

# --- which CPU -----------------------------------------------------------------
# Prefer the package manager's idea of the architecture: a 32-bit userland on a
# 64-bit kernel (common on Raspberry Pi OS) reports aarch64 from uname but
# needs armhf packages.
arch=""
if command -v dpkg >/dev/null 2>&1; then
    case "$(dpkg --print-architecture 2>/dev/null)" in
        amd64) arch=amd64 ;;
        arm64) arch=arm64 ;;
        armhf) arch=armv7 ;;
    esac
fi
if [ -z "$arch" ]; then
    case "$(uname -m)" in
        x86_64|amd64)   arch=amd64 ;;
        aarch64|arm64)  arch=arm64 ;;
        armv7l|armv7*)  arch=armv7 ;;
        armv6l)         die "armv6 (Pi Zero / Pi 1) is not supported; build from source" ;;
        *)              die "unsupported CPU: $(uname -m)" ;;
    esac
fi

# --- which package format ------------------------------------------------------
if command -v dpkg >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then
    fmt=deb
elif command -v rpm >/dev/null 2>&1 && { command -v dnf >/dev/null 2>&1 || command -v zypper >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; }; then
    fmt=rpm
elif command -v pacman >/dev/null 2>&1; then
    fmt=pkg.tar.zst
else
    fmt=tar.gz
fi

# --- which version -------------------------------------------------------------
tag="${PADDLEKEYS_VERSION:-}"
if [ -z "$tag" ]; then
    # Follow the /releases/latest redirect; no API token or jq needed.
    tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/##')
    case "$tag" in v[0-9]*) ;; *) die "could not determine the latest release (no published releases yet?)" ;; esac
fi
version="${tag#v}"
base="https://github.com/$REPO/releases/download/$tag"
file="${NAME}_${version}_linux_${arch}.${fmt}"

say "Installing $NAME $tag for linux/$arch as .$fmt"

# --- download and verify -------------------------------------------------------
tmp=$(mktemp -d) || die "could not create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM
cd "$tmp"

curl -fsSL -o "$file" "$base/$file" || die "download failed: $base/$file"
curl -fsSL -o checksums.txt "$base/checksums.txt" || die "download failed: $base/checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
    grep " $file\$" checksums.txt | sha256sum -c - >/dev/null || die "checksum mismatch for $file"
else
    say "warning: sha256sum not found, skipping checksum verification"
fi

# --- install -------------------------------------------------------------------
if [ "$(id -u)" -eq 0 ]; then
    sudo=""
else
    need sudo
    sudo="sudo"
    say "Root is needed to install the package; sudo may ask for your password."
fi

case "$fmt" in
    deb)
        $sudo apt-get install -y "./$file"
        ;;
    rpm)
        if command -v dnf >/dev/null 2>&1; then
            $sudo dnf install -y "./$file"
        elif command -v zypper >/dev/null 2>&1; then
            $sudo zypper --non-interactive install --allow-unsigned-rpm "./$file"
        else
            $sudo yum install -y "./$file"
        fi
        ;;
    pkg.tar.zst)
        $sudo pacman -U --noconfirm "./$file"
        ;;
    tar.gz)
        say "No deb/rpm/pacman found; installing files from the tarball."
        mkdir extract
        tar -xzf "$file" -C extract
        $sudo install -Dm755 extract/$NAME /usr/bin/$NAME
        $sudo install -Dm644 extract/packaging/$NAME.service /usr/lib/systemd/system/$NAME.service
        $sudo install -Dm644 extract/packaging/80-$NAME.rules /usr/lib/udev/rules.d/80-$NAME.rules
        [ -f /etc/default/$NAME ] || $sudo install -Dm644 extract/packaging/$NAME.default /etc/default/$NAME
        $sudo sh extract/packaging/postinstall.sh
        ;;
esac

say ""
say "$NAME $tag installed. Plug in the adapter and it will start automatically."
say "Config: /etc/default/$NAME    Check: $NAME -version"
