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

# --- output helpers ------------------------------------------------------------
# Colour only when stderr is a terminal and nobody has asked us not to.
if [ -t 2 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != "dumb" ]; then
    B="$(printf '\033[1m')"  D="$(printf '\033[2m')"  R="$(printf '\033[0m')"
    GRN="$(printf '\033[32m')" YEL="$(printf '\033[33m')" RED="$(printf '\033[31m')" CYN="$(printf '\033[36m')"
else
    B="" D="" R="" GRN="" YEL="" RED="" CYN=""
fi
case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
    *UTF-8*|*utf8*|*UTF8*|*utf-8*) TICK="✓" ARROW="→" H="─" V="│" TL="┌" TR="┐" BL="└" BR="┘" ;;
    *)                             TICK="*" ARROW=">" H="-" V="|" TL="+" TR="+" BL="+" BR="+" ;;
esac

say()  { printf '%s\n' "$*" >&2; }
step() { say "${CYN}${ARROW}${R} $*"; }
ok()   { say "${GRN}${TICK}${R} $*"; }
warn() { say "${YEL}warning:${R} $*"; }
die()  { say "${RED}${B}error:${R} $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }

# Draw one horizontal rule of $1 characters.
rule() { i=0; while [ "$i" -lt "$1" ]; do printf '%s' "$H"; i=$((i+1)); done; }

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

say ""
say "${B}paddlekeys installer${R} ${D}$tag${R}"
say ""
step "Detected ${B}linux/$arch${R}, installing as ${B}.$fmt${R}"

# --- download and verify -------------------------------------------------------
tmp=$(mktemp -d) || die "could not create temp dir"
trap 'rm -rf "$tmp"' EXIT INT TERM
# mktemp makes the dir 0700. apt downloads/reads local packages as the
# unprivileged _apt user, so open it up or apt prints a permission notice.
chmod 755 "$tmp"
cd "$tmp"

step "Downloading ${D}$file${R}"
curl -fsSL -o "$file" "$base/$file" || die "download failed: $base/$file"
chmod 644 "$file"
curl -fsSL -o checksums.txt "$base/checksums.txt" || die "download failed: $base/checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
    grep " $file\$" checksums.txt | sha256sum -c - >/dev/null || die "checksum mismatch for $file"
    ok "Checksum verified"
else
    warn "sha256sum not found, skipping checksum verification"
fi

# --- install -------------------------------------------------------------------
if [ "$(id -u)" -eq 0 ]; then
    sudo=""
else
    need sudo
    sudo="sudo"
    say "${D}Root is needed to install the package; sudo may ask for your password.${R}"
fi
step "Installing with $(case "$fmt" in deb) echo apt;; rpm) echo "dnf/zypper/yum";; pkg.tar.zst) echo pacman;; *) echo "install(1)";; esac)"

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
        warn "no deb/rpm/pacman found; installing files from the tarball"
        mkdir extract
        tar -xzf "$file" -C extract
        $sudo install -Dm755 extract/$NAME /usr/bin/$NAME
        $sudo install -Dm644 extract/packaging/$NAME.service /usr/lib/systemd/system/$NAME.service
        $sudo install -Dm644 extract/packaging/80-$NAME.rules /usr/lib/udev/rules.d/80-$NAME.rules
        [ -f /etc/default/$NAME ] || $sudo install -Dm644 extract/packaging/$NAME.default /etc/default/$NAME
        $sudo sh extract/packaging/postinstall.sh
        ;;
esac

# --- summary -------------------------------------------------------------------
# A boxed card. Padding is computed on the plain text, colour is added after,
# so the right-hand border lines up whether or not colour is on.
W=62                                   # inner width
ESC=$(printf '\033')
vlen() { printf '%s' "$1" | sed "s/$ESC\[[0-9;]*m//g" | wc -m; }   # visible length
row() {                                # row LABEL VALUE  (LABEL may be empty)
    if [ -n "$1" ]; then
        text=$(printf '  %s%-9s%s%s' "$B" "$1" "$R" "$2")
    else
        text=$(printf '  %s' "$2")
    fi
    pad=$((W - $(vlen "$text"))); [ "$pad" -lt 0 ] && pad=0
    printf '%s%s%s%s%*s%s%s%s\n' "$GRN" "$V" "$R" "$text" "$pad" "" "$GRN" "$V" "$R" >&2
}
blank() { row "" ""; }

say ""
printf '%s%s%s%s%s\n' "$GRN" "$TL" "$(rule $W)" "$TR" "$R" >&2
blank
row "" "${GRN}${TICK}${R} ${B}$NAME $tag installed${R}"
blank
row "" "Plug in the adapter and it starts automatically."
blank
row "Config"  "${CYN}/etc/default/$NAME${R}"
row " "       "${D}edit, then: sudo systemctl restart $NAME${R}"
row "Keys"    "$NAME -list-keys"
row "Status"  "systemctl status $NAME"
row "Logs"    "journalctl -u $NAME -f"
blank
printf '%s%s%s%s%s\n' "$GRN" "$BL" "$(rule $W)" "$BR" "$R" >&2
say ""
