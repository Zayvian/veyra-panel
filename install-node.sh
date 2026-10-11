#!/bin/sh
# One-line installer for a Veyra Node.
#
#   curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-node/main/install.sh | bash
#
# It will ask for the panel URL, join token and optional node domain. Arguments
# go through to deploy/install-node.sh after `-s --` for unattended install or
# maintenance:
#
#   ... | sh -s -- --panel https://panel.example.com --token <token>
#   ... | sh -s -- --version      node and sing-box versions
#   ... | sh -s -- --upgrade      rebuild and restart, sing-box core included
#   ... | sh -s -- --uninstall    remove the service; keep the certificate
#   ... | sh -s -- --purge        remove everything this installer created
#
# This file exists only to fetch the sources and hand over. Everything real is
# in deploy/install-node.sh, which is worth reading before running either.
set -eu

REPO=${VEYRA_REPO:-${SKYSBX_REPO:-https://github.com/Zayvian/veyra-node.git}}
REF=${VEYRA_REF:-${SKYSBX_REF:-main}}

RED=$(printf '\033[31m'); GRN=$(printf '\033[32m'); RST=$(printf '\033[0m')
say() { printf '%s==>%s %s\n' "$GRN" "$RST" "$*"; }
die() { printf '%s fail%s %s\n' "$RED" "$RST" "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root (sudo sh -c \"\$(wget -qO- ...)\")"

if ! command -v git >/dev/null 2>&1; then
    say "installing git"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq git
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y -q git
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q git
    else
        die "install git first"
    fi
fi

SRC=$(mktemp -d)
trap 'rm -rf "$SRC"' EXIT

# Removing something does not need the sources it was built from.
NEEDS_BUILD=1
for arg in "$@"; do
    case "$arg" in
        --version|--uninstall|--purge|-h|--help) NEEDS_BUILD=0 ;;
    esac
done

say "fetching $REPO@$REF"
git clone -q --branch "$REF" --depth 1 "$REPO" "$SRC/veyra-node" \
    || die "cannot clone $REPO"

# A pipeline leaves stdin pointing at the downloaded script, not the terminal,
# so the installer would find nothing to prompt on and refuse. Reattach the
# terminal if there is one.
#
# The test is a subshell on purpose: on a host with no controlling terminal,
# opening /dev/tty fails, and under dash a failed redirection in the current
# shell is fatal — which used to kill this script silently, with no output at
# all, in the one situation it most needed to explain itself.
#
# bash, not sh: this launcher is POSIX because it is piped into whatever /bin/sh
# is, but the installer it hands over to is bash — on Debian /bin/sh is dash,
# which fails on the first line with "Illegal option -o pipefail".
command -v bash >/dev/null 2>&1 || die "bash is required"
if [ "$NEEDS_BUILD" = 1 ]; then
    # The deploy script asks for panel credentials before it downloads and
    # builds the patched core. This keeps the one-line installer interactive
    # immediately instead of making the operator wait through the big clone.
    set -- --src "$SRC/veyra-node" "$@"
fi
if ( exec 3>/dev/tty ) 2>/dev/null; then
    exec bash "$SRC/veyra-node/deploy/install-node.sh" "$@" </dev/tty
fi
exec bash "$SRC/veyra-node/deploy/install-node.sh" "$@"
