#!/bin/sh
# One-line installer for Veyra Panel.
#
#   curl -fsSL https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install.sh | bash
#
# With no arguments the installer opens a guided setup: panel domain,
# optional subscription domain, ACME email and the first administrator.
#
# Arguments go through to panel/deploy/install-panel.sh after `-s --`; use them
# for unattended installation or maintenance:
#
#   ... | sh -s -- --domain panel.example.com --email you@example.com
#   ... | sh -s -- --version      what is installed
#   ... | sh -s -- --upgrade      replace the binary and restart; the database is untouched
#   ... | sh -s -- --from-source  compile here instead of downloading a release
#   ... | sh -s -- --uninstall    remove the service; keep the database
#   ... | sh -s -- --purge        remove everything, database included
#
# This file only fetches the repository and hands over. It takes the scripts
# from the newest release, so they match the prebuilt binary they install;
# VEYRA_REF=<branch or tag> overrides that and builds from those sources.
set -eu

# VEYRA_* is the public interface. SKYSBX_* remains an upgrade-compatible
# alias for operators who already automated the previous release.
OWNER=${VEYRA_GH_OWNER:-${SKYSBX_GH_OWNER:-Zayvian}}
REPO=${VEYRA_REPO:-${SKYSBX_REPO:-https://github.com/$OWNER/veyra-panel.git}}
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
#
# Not exec: the checkout has to outlive the installer, and the trap above
# removes it once the installer is done.
if ( exec 3>/dev/tty ) 2>/dev/null; then
    bash "$SRC/veyra/panel/deploy/install-panel.sh" "$@" </dev/tty
else
    bash "$SRC/veyra/panel/deploy/install-panel.sh" "$@"
fi
