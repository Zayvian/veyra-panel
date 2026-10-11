#!/bin/sh
# Install Veyra Panel and Veyra Node on this host.
#
#   curl -fL --retry 3 --connect-timeout 15 \
#     https://raw.githubusercontent.com/Zayvian/veyra-panel/main/install-panel-and-node.sh \
#     -o /tmp/veyra-all-install.sh && bash /tmp/veyra-all-install.sh
#
# The node must have a join token.  Create a node in the panel after the panel
# installer finishes, then paste that one-time token when this script asks.
set -eu

OWNER=${VEYRA_GH_OWNER:-${SKYSBX_GH_OWNER:-Zayvian}}
# One repository holds both components now; VEYRA_PANEL_* are kept as aliases.
REPO=${VEYRA_REPO:-${VEYRA_PANEL_REPO:-${SKYSBX_REPO:-https://github.com/$OWNER/veyra-panel.git}}}
REF=${VEYRA_REF:-${VEYRA_PANEL_REF:-${SKYSBX_REF:-}}}
RELEASE=${VEYRA_RELEASE:-}
ROOT=${VEYRA_ROOT:-${SKYSBX_ROOT:-/opt/skysbx}}
DOMAIN=""
SUB_DOMAIN=""
EMAIL=""
SUB_DOMAIN_SET=0
EMAIL_SET=0
PANEL_URL=""
TOKEN=""
FROM_SOURCE=0
ACTION=install

RED=$(printf '\033[31m'); GRN=$(printf '\033[32m'); BLD=$(printf '\033[1m'); RST=$(printf '\033[0m')
say() { printf '%s==>%s %s\n' "$BLD" "$RST" "$*"; }
ok() { printf '%s  ok%s %s\n' "$GRN" "$RST" "$*"; }
die() { printf '%s fail%s %s\n' "$RED" "$RST" "$*" >&2; exit 1; }

usage() {
    cat <<'EOF'
Usage: sudo sh install-panel-and-node.sh [options]

Installs the panel first, then installs a node connected to that same panel.
After the panel is online, create a node in its web UI and paste the one-time
join token when prompted (or provide it with --token).

  --domain <fqdn>       Panel domain (prompts when omitted; must resolve here).
  --sub-domain <fqdn>   Separate HTTPS subscription domain.
  --email <addr>        Let's Encrypt contact email for the panel.
  --panel <url>         Panel URL for the node (default: https://<panel-fqdn>).
  --token <token>       Node join token created in the panel UI.
  --version             Show the installed panel and node versions.
  --upgrade             Install the current release of both and restart them.
  --from-source         Compile both here instead of downloading binaries.
  --uninstall           Remove both services; keep their data and certificates.
  --purge               Remove both services and all data (cannot be undone).
  -h, --help            Show this help.

The node reuses the certificate already obtained by the local panel. Do not
pass a node domain: requesting another HTTP-01 certificate would contend with
the panel for port 80.

Environment overrides: VEYRA_REPO, VEYRA_REF (a branch or tag to build from
instead of the newest release), VEYRA_RELEASE, GITHUB_TOKEN. Legacy SKYSBX_*
aliases also work.
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --domain) DOMAIN=${2-}; shift 2 ;;
        --sub-domain) SUB_DOMAIN=${2-}; SUB_DOMAIN_SET=1; shift 2 ;;
        --email) EMAIL=${2-}; EMAIL_SET=1; shift 2 ;;
        --panel) PANEL_URL=${2-}; shift 2 ;;
        --token) TOKEN=${2-}; shift 2 ;;
        --version) ACTION=version; shift ;;
        --upgrade) ACTION=upgrade; shift ;;
        --uninstall) ACTION=uninstall; shift ;;
        --purge) ACTION=purge; shift ;;
        --from-source) FROM_SOURCE=1; shift ;;
        --node-domain|--cf-token)
            die "$1 is not supported for a same-host install; the node reuses the panel certificate" ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
done

[ "$(id -u)" = 0 ] || die 'run as root (for example: sudo sh -c "$(wget -qO- ...) ")'

# Like the single-component launchers, recover the terminal after wget | sh so
# the panel administrator and node-token prompts remain interactive. Lifecycle
# actions have nothing to ask, so they must keep the pipeline's stdin detached
# and return as soon as both component installers finish.
if [ "$ACTION" = install ] && (exec 3>/dev/tty) 2>/dev/null; then
    exec </dev/tty
fi

# Keep the usual installation path fully guided. Command-line options are still
# useful for automation, but a person should not have to remember flags just to
# install both services on a fresh host.
if [ "$ACTION" = install ]; then
    printf '\n%s==>%s Same-host setup: panel and node will share one certificate.\n' "$BLD" "$RST"
fi
if [ "$ACTION" = install ] && [ -z "$DOMAIN" ] && [ -t 0 ]; then
    printf '  [1/5] Panel domain (must already resolve here): '
    read -r DOMAIN
fi
[ "$ACTION" != install ] || [ -n "$DOMAIN" ] \
    || { usage >&2; die '--domain is required without a terminal'; }

if [ "$ACTION" = install ] && [ "$SUB_DOMAIN_SET" = 0 ] && [ -t 0 ]; then
    printf '  [2/5] Subscription domain [blank = use the panel domain]: '
    read -r SUB_DOMAIN
fi
if [ "$ACTION" = install ] && [ -z "$EMAIL" ] && [ "$EMAIL_SET" = 0 ] && [ -t 0 ]; then
    printf "  [3/5] Let's Encrypt contact email [skip]: "
    read -r EMAIL
fi
[ "$ACTION" != install ] || PANEL_URL=${PANEL_URL:-"https://$DOMAIN"}

# The component installers install everything else they need. This only needs
# git and curl, to find the release and clone the repository.
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
command -v bash >/dev/null 2>&1 || die 'bash is required'

# The newest release, by following GitHub's /releases/latest redirect. Both
# components come from it, scripts and binaries alike. No release (or no
# network to ask) leaves it empty, and both are built from main.
if [ -z "$REF" ] && [ -z "$RELEASE" ]; then
    latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' --max-time 20 \
        "https://github.com/$OWNER/veyra-panel/releases/latest" 2>/dev/null || true)
    case "$latest" in
        */releases/tag/*) RELEASE=${latest##*/} ;;
    esac
fi
REF=${REF:-${RELEASE:-main}}

SRC=$(mktemp -d)
# The token is typed with echo disabled below. Always restore it before
# returning to the caller, including Ctrl-C or a failed node installation.
trap 'stty echo >/dev/null 2>&1 || true; rm -rf "$SRC"' EXIT
say "fetching $REPO@$REF"
git clone -q --branch "$REF" --depth 1 "$REPO" "$SRC/veyra" \
    || die "cannot clone $REPO@$REF"

# Passed to both component installers.
set_common() {
    COMMON="--src $SRC/veyra"
    [ -z "$RELEASE" ] || COMMON="$COMMON --release $RELEASE"
    [ "$FROM_SOURCE" = 0 ] || COMMON="$COMMON --from-source"
}
set_common

# Run the real panel installer from the checked-out source, rather than piping
# it, so its administrator prompt keeps a usable stdin.
if [ "$ACTION" = install ]; then
    printf '\n  Next, create the first panel administrator.\n'
    # Pass explicit blanks too: the child installer should not ask the same
    # optional values a second time after the guided launcher collected them.
    set -- --domain "$DOMAIN" --sub-domain "$SUB_DOMAIN" --email "$EMAIL"
else
    set -- "--$ACTION"
fi
[ "$ACTION" != upgrade ] || [ -z "$DOMAIN" ] || set -- "$@" --domain "$DOMAIN"
[ -n "$SUB_DOMAIN" ] && set -- "$@" --sub-domain "$SUB_DOMAIN"
# shellcheck disable=SC2086 # COMMON is word-split on purpose; mktemp paths have no spaces
bash "$SRC/veyra/panel/deploy/install-panel.sh" $COMMON "$@"

if [ "$ACTION" = install ] || [ "$ACTION" = upgrade ]; then
# The child installer resolves upgrade defaults and persists the effective
# domain. Read it back before finding the certificate, including old installs
# whose domain originally came from their systemd unit.
DOMAIN=$(sed -n 's/^SKYSBX_DOMAIN=//p' "$ROOT/panel.env")
[ -n "$DOMAIN" ] || die "cannot read the installed panel domain from $ROOT/panel.env"
# CertMagic keeps the panel certificate under its own storage tree, while the
# node's default AnyTLS paths are /opt/skysbx/cert.pem and key.pem. Symlinking
# them lets both services use one certificate and prevents the node installer
# from starting certbot, which would otherwise fail because the panel owns :80.
PANEL_CERT=$(find "$ROOT/certs/certificates" -type f -name "$DOMAIN.crt" -print -quit 2>/dev/null || true)
PANEL_KEY=${PANEL_CERT%.crt}.key
[ -n "$PANEL_CERT" ] && [ -f "$PANEL_KEY" ] \
    || die "cannot find the panel certificate for $DOMAIN under $ROOT/certs"
ln -sfn "$PANEL_CERT" "$ROOT/cert.pem"
ln -sfn "$PANEL_KEY" "$ROOT/key.pem"
ok "node will reuse the panel certificate"
fi

if [ "$ACTION" = install ] && [ -z "$TOKEN" ]; then
    if [ -t 0 ]; then
        printf '\n  [4/5] Open %s, sign in, then create a node and copy its join token.\n' "$PANEL_URL"
        printf '  [5/5] Paste the join token (input hidden): '
        stty -echo
        if ! read -r TOKEN; then
            stty echo
            printf '\n'
            die 'a node join token is required'
        fi
        stty echo
        printf '\n'
    fi
    [ -n "$TOKEN" ] || die 'a node join token is required; rerun with --token <token>'
fi

# Call the real installer directly instead of the node launcher. The launcher
# deliberately reattaches /dev/tty for standalone interactive use. All
# required node values are already supplied here, and leaving that tty
# attached can keep this combined command alive after it prints success.
NODE_INSTALL=$SRC/veyra/node/deploy/install-node.sh
[ -f "$NODE_INSTALL" ] || die "node installer is missing from $REPO@$REF"

if [ "$ACTION" = install ]; then
    set -- --panel "$PANEL_URL" --token "$TOKEN"
else
    set -- "--$ACTION"
fi

# When the panel was compiled here, install-panel.sh left its Docker-mounted Go
# caches under $ROOT. A tiny docker shim, first in PATH only while the node
# installer runs, mounts the same caches into every `docker run` it makes, so
# modules shared by both are downloaded once. Installing release binaries
# runs no Docker at all, and then there is nothing to shim.
REAL_DOCKER=$(command -v docker 2>/dev/null || true)
if { [ "$ACTION" = install ] || [ "$ACTION" = upgrade ]; } && [ -n "$REAL_DOCKER" ]; then
    GO_MOD_CACHE=$ROOT/go-mod-cache
    GO_BUILD_CACHE=$ROOT/go-build-cache
    install -d -m 0700 "$GO_MOD_CACHE" "$GO_BUILD_CACHE"
    DOCKER_SHIM_DIR=$SRC/docker-shim
    mkdir -p "$DOCKER_SHIM_DIR"
    cat > "$DOCKER_SHIM_DIR/docker" <<'EOF'
#!/bin/sh
set -eu
if [ "${1-}" = run ]; then
    shift
    exec "$SKYSBX_REAL_DOCKER" run \
        -v "$SKYSBX_GO_MOD_CACHE:/go/pkg/mod" \
        -v "$SKYSBX_GO_BUILD_CACHE:/root/.cache/go-build" "$@"
fi
exec "$SKYSBX_REAL_DOCKER" "$@"
EOF
    chmod 700 "$DOCKER_SHIM_DIR/docker"
fi

# shellcheck disable=SC2086 # COMMON is word-split on purpose; mktemp paths have no spaces
if [ -n "${DOCKER_SHIM_DIR:-}" ]; then
    PATH="$DOCKER_SHIM_DIR:$PATH" \
    SKYSBX_REAL_DOCKER=$REAL_DOCKER \
    SKYSBX_GO_MOD_CACHE=$GO_MOD_CACHE \
    SKYSBX_GO_BUILD_CACHE=$GO_BUILD_CACHE \
    bash "$NODE_INSTALL" $COMMON "$@" </dev/null
else
    bash "$NODE_INSTALL" $COMMON "$@" </dev/null
fi

case "$ACTION" in
    install) printf '\n%sVeyra Panel and Node are installed on this host.%s\n' "$GRN" "$RST" ;;
    upgrade) printf '\n%sVeyra Panel and Node are upgraded.%s\n' "$GRN" "$RST" ;;
    uninstall) printf '\n%sVeyra Panel and Node are uninstalled.%s\n' "$GRN" "$RST" ;;
    purge) printf '\n%sVeyra Panel and Node are purged.%s\n' "$GRN" "$RST" ;;
esac

# Be explicit here rather than relying on end-of-file. This makes the pipe's
# reader close immediately after a non-interactive lifecycle command finishes.
exit 0
