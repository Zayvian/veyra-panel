#!/bin/sh
# One-line installer for a Veyra Node.
#
#   curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-node.sh | bash
#
# It will ask for the panel URL, join token and optional node domain. Arguments
# go through to node/deploy/install-node.sh after `-s --` for unattended
# install or maintenance:
#
#   ... | sh -s -- --panel https://panel.example.com --token <token>
#   ... | sh -s -- --version      node and sing-box versions
#   ... | sh -s -- --upgrade      replace the binary and restart, sing-box core included
#   ... | sh -s -- --from-source  compile here instead of downloading a release
#   ... | sh -s -- --uninstall    remove the service; keep the certificate
#   ... | sh -s -- --purge        remove everything this installer created
#
# This file only fetches the repository and hands over. It takes the scripts
# from the newest release, so they match the prebuilt binary they install;
# VEYRA_REF=<branch or tag> overrides that and builds from those sources.
set -eu

OWNER=${VEYRA_GH_OWNER:-${SKYSBX_GH_OWNER:-Zayvian}}
REPO=https://github.com/$OWNER/veyra-panel.git
REF=${VEYRA_REF:-${SKYSBX_REF:-}}
RELEASE=${VEYRA_RELEASE:-}

RED=$(printf '\033[31m'); GRN=$(printf '\033[32m'); RST=$(printf '\033[0m')
say() { printf '%s==>%s %s\n' "$GRN" "$RST" "$*"; }
die() { printf '%s fail%s %s\n' "$RED" "$RST" "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root (sudo sh -c \"\$(wget -qO- ...)\")"

for tool in git curl; do
    command -v "$tool" >/dev/null 2>&1 && continue
    say "installing $tool"
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq "$tool"
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y -q "$tool"
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q "$tool"
    else
        die "install $tool first"
    fi
done
command -v bash >/dev/null 2>&1 || die "bash is required"

# The newest release, by following GitHub's /releases/latest redirect. No
# release (or no network to ask) leaves it empty, and the installer builds
# from main as it always used to.
if [ -z "$REF" ] && [ -z "$RELEASE" ]; then
    latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' --max-time 20 \
        "https://github.com/$OWNER/veyra-panel/releases/latest" 2>/dev/null || true)
    case "$latest" in
        */releases/tag/*) RELEASE=${latest##*/} ;;
    esac
fi
REF=${REF:-${RELEASE:-main}}

SRC=$(mktemp -d)
trap 'rm -rf "$SRC"' EXIT
say "fetching $REPO@$REF"
git clone -q --branch "$REF" --depth 1 "$REPO" "$SRC/veyra" \
    || die "cannot clone $REPO@$REF"

set -- --src "$SRC/veyra" "$@"
[ -z "$RELEASE" ] || set -- --release "$RELEASE" "$@"

# A pipeline leaves stdin pointing at the downloaded script, not the terminal,
# so the installer would find nothing to prompt on and refuse. Reattach the
# terminal if there is one. The test is a subshell because under dash a failed
# redirection in the current shell is fatal; bash because the installer uses
# bash features. Not exec: the trap above removes the checkout afterwards.
if ( exec 3>/dev/tty ) 2>/dev/null; then
    bash "$SRC/veyra/node/deploy/install-node.sh" "$@" </dev/tty
else
    bash "$SRC/veyra/node/deploy/install-node.sh" "$@"
fi
