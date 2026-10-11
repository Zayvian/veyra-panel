#!/usr/bin/env bash
# Install a Veyra Node on a Debian/Ubuntu host and point it at a panel.
#
#   sudo ./install-node.sh --panel https://panel.example.com --token <token>
#
# Re-running upgrades the binary in place.
set -euo pipefail

ROOT=${VEYRA_ROOT:-${SKYSBX_ROOT:-/opt/skysbx}}
PANEL=""
TOKEN=""
DOMAIN=""
EMAIL=""
CF_TOKEN=""
SKIP_CERT=0
SRC_DIR=""
FORK_DIR=""
GH_TOKEN=${GITHUB_TOKEN:-}
GH_OWNER=${VEYRA_GH_OWNER:-${SKYSBX_GH_OWNER:-Zayvian}}
# Kept for compatibility with the original one-line installer. When set, this
# is a full Git URL for the patched core; otherwise it follows GH_OWNER.
CORE_REPO=${VEYRA_CORE_REPO:-${SKYSBX_FORK:-veyra-core}}
REF=${VEYRA_REF:-${SKYSBX_REF:-main}}

RED=$'\e[31m'; GRN=$'\e[32m'; YLW=$'\e[33m'; BLD=$'\e[1m'; RST=$'\e[0m'
say()  { printf '%s==>%s %s\n' "$BLD" "$RST" "$*"; }
ok()   { printf '%s  ok%s %s\n' "$GRN" "$RST" "$*"; }
warn() { printf '%s warn%s %s\n' "$YLW" "$RST" "$*"; }
die()  { printf '%s fail%s %s\n' "$RED" "$RST" "$*" >&2; exit 1; }

ACTION=install

usage() {
    cat <<EOF
Usage: sudo ./install-node.sh [--panel <url> --token <token>] [options]

Run it with no install options in a terminal for the guided setup.

Actions (default: install)
  --version         What is installed, including the sing-box it embeds.
  --upgrade         Rebuild from the current sources and restart. Reads the
                    panel URL and token back from ${ROOT}/node.env, so it needs
                    no arguments. This is also how the sing-box core is updated:
                    the node links it, so a rebuild is the upgrade.
  --uninstall       Stop and remove the service and the binary. Keeps the
                    certificate and ${ROOT}/node.env, so a reinstall is a
                    no-argument --upgrade away.
  --purge           --uninstall, and delete everything else this installer
                    created: the environment file, the certificate, the build
                    cache, the Let's Encrypt account for this node's domain,
                    and Docker if this script was the one that installed it.

Install options
  --panel <url>     Panel base URL, e.g. https://panel.example.com
  --token <token>   Join token, shown once when the node was added in the panel.

  --domain <fqdn>   This node's own domain. AnyTLS, Hysteria2 and TUIC need a
                    certificate; Reality and Shadowsocks do not. Use DNS-only
                    records: these proxy endpoints cannot use an HTTP CDN.
  --email <addr>    Let's Encrypt contact address (default admin@<domain>).
  --cf-token <tok>  Cloudflare API token, to validate over DNS-01 when port 80
                    is not reachable.
  --no-cert         Skip certificate issuance.

  --src <dir>       Build from a checkout on disk instead of cloning.
  --fork <dir>      Path to the patched sing-box; defaults to a sibling clone.
  -h, --help        This text.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version)   ACTION=version; shift ;;
        --upgrade)   ACTION=upgrade; shift ;;
        --uninstall) ACTION=uninstall; shift ;;
        --purge)     ACTION=purge; shift ;;
        --panel)     PANEL=$2; shift 2 ;;
        --token)     TOKEN=$2; shift 2 ;;
        --domain)    DOMAIN=$2; shift 2 ;;
        --email)     EMAIL=$2; shift 2 ;;
        --cf-token)  CF_TOKEN=$2; shift 2 ;;
        --no-cert)   SKIP_CERT=1; shift ;;
        --src)       SRC_DIR=$2; shift 2 ;;
        --fork)      FORK_DIR=$2; shift 2 ;;
        -h|--help)   usage; exit 0 ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
done

# ───────────────────────── version / uninstall / purge ─────────────────────

if [ "$ACTION" = version ]; then
    if [ -x "$ROOT/skysbx-node" ]; then
        "$ROOT/skysbx-node" -version
        printf 'installed  %s\n' "$(stat -c %y "$ROOT/skysbx-node" 2>/dev/null | cut -d. -f1)"
        systemctl is-active --quiet skysbx-node \
            && printf 'service    running\n' || printf 'service    not running\n'
    else
        printf 'Veyra Node is not installed at %s\n' "$ROOT"
    fi
    exit 0
fi

if [ "$ACTION" = uninstall ] || [ "$ACTION" = purge ]; then
    [ "$(id -u)" = 0 ] || die "run as root"

    say "removing the service"
    systemctl disable --now skysbx-node >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/skysbx-node.service
    systemctl daemon-reload 2>/dev/null || true
    systemctl reset-failed 2>/dev/null || true
    ok "Veyra Node stopped and removed"

    rm -f "$ROOT/skysbx-node"
    # The build tree is this installer's scratch space, not data: it is a fresh
    # clone on every run.
    rm -rf "$ROOT/build/skysbx-node" "$ROOT/build/skysbx-core"
    ok "binary and build cache removed"

    # Keep the shared command if the panel is still installed on this host.
    if [ ! -f /etc/systemd/system/skysbx-panel.service ] && [ ! -x "$ROOT/skysbx-panel" ]; then
        rm -f /usr/local/bin/veyra
    fi

    if [ "$ACTION" = purge ]; then
        say "purging"
        # Read the domain back before deleting the hook that names it.
        #
        # Guarded, because there may be no hook: with pipefail a failing sed
        # makes the whole pipeline fail, a failing pipeline makes the
        # assignment fail, and set -e then ends the script — silently, halfway
        # through a purge, having removed the service but nothing else.
        PURGE_DOMAIN=""
        if [ -f "$ROOT/certbot-deploy.sh" ]; then
            PURGE_DOMAIN=$( (sed -n 's|^LIVE=/etc/letsencrypt/live/||p' \
                "$ROOT/certbot-deploy.sh" || true) | head -1)
        fi
        rm -f "$ROOT/node.env" "$ROOT/cert.pem" "$ROOT/key.pem" "$ROOT/certbot-deploy.sh"
        if [ -n "$PURGE_DOMAIN" ] && command -v certbot >/dev/null 2>&1; then
            certbot delete --cert-name "$PURGE_DOMAIN" --non-interactive >/dev/null 2>&1 \
                && ok "certificate for $PURGE_DOMAIN deleted" || true
        fi
        # Only what this script pulled, and only if nothing is using it.
        if command -v docker >/dev/null 2>&1; then
            docker image rm golang:1.26.5 >/dev/null 2>&1 \
                && ok "build image removed" || true
        fi
        # Docker goes only if this script was the one that installed it. A host
        # that already ran Docker is running something in it.
        if [ -f "$ROOT/.docker-installed-by-skysbx" ] && command -v docker >/dev/null 2>&1; then
            say "removing docker (this script installed it)"
            systemctl disable --now docker docker.socket containerd >/dev/null 2>&1 || true
            apt-get purge -y -qq docker-ce docker-ce-cli containerd.io \
                docker-buildx-plugin docker-compose-plugin >/dev/null 2>&1 || true
            apt-get autoremove -y -qq >/dev/null 2>&1 || true
            rm -rf /var/lib/docker /var/lib/containerd /etc/docker
            rm -f "$ROOT/.docker-installed-by-skysbx"
            ok "docker removed"
        fi
    fi

    # Shared with the panel when both are on one host, so it goes only if this
    # was the last thing in it.
    rmdir "$ROOT/build" 2>/dev/null || true
    if rmdir "$ROOT" 2>/dev/null; then
        ok "$ROOT removed"
    else
        warn "$ROOT kept — it still holds files (the panel's, or your own):"
        (ls -A "$ROOT" 2>/dev/null || true) | sed 's/^/       /'
    fi

    printf '\n%sVeyra Node removed.%s\n' "$GRN" "$RST"
    [ "$ACTION" = uninstall ] && printf \
        'The certificate and %s/node.env were kept; --purge removes those too.\n' "$ROOT"
    exit 0
fi

if [ "$ACTION" = upgrade ]; then
    [ -f "$ROOT/node.env" ] || die "nothing installed at $ROOT (run without --upgrade first)"
    # shellcheck disable=SC1090
    PANEL=${PANEL:-$(sed -n 's/^SKYSBX_PANEL=//p' "$ROOT/node.env")}
    TOKEN=${TOKEN:-$(sed -n 's/^SKYSBX_TOKEN=//p' "$ROOT/node.env")}
    [ -n "$PANEL" ] && [ -n "$TOKEN" ] || die "cannot read the panel URL and token from $ROOT/node.env"
    # certbot renews on its own timer; an upgrade has no business reissuing.
    SKIP_CERT=1
    say "upgrading — panel $PANEL"
fi

ask() { # ask <var> <prompt>
    local __var=$1 __prompt=$2 __reply=""
    [ -n "${!__var}" ] && return 0
    [ -t 0 ] || die "$__prompt is required (no terminal to ask on)"
    printf '  %s: ' "$__prompt"
    read -r __reply
    printf -v "$__var" '%s' "$__reply"
    [ -n "${!__var}" ] || die "$__prompt is required"
}

ask_secret() { # ask_secret <var> <prompt>
    local __var=$1 __prompt=$2 __reply=""
    [ -n "${!__var}" ] && return 0
    [ -t 0 ] || die "$__prompt is required (no terminal to ask on)"
    printf '  %s: ' "$__prompt"
    stty -echo
    if ! read -r __reply; then
        stty echo
        printf '\n'
        die "$__prompt is required"
    fi
    stty echo
    printf '\n'
    printf -v "$__var" '%s' "$__reply"
    [ -n "${!__var}" ] || die "$__prompt is required"
}

# An upgrade already knows all of this: it read the panel URL and token out of
# node.env, and the certificate is certbot's business, not this run's.
if [ "$ACTION" != upgrade ]; then
    say "node configuration"
    ask PANEL "Panel URL (https://panel.example.com)"
    ask_secret TOKEN "Join token (input hidden)"
    if [ -z "$DOMAIN" ] && [ -t 0 ]; then
        printf "  Node domain (required for AnyTLS/Hysteria2/TUIC; otherwise blank): "
        read -r DOMAIN
    fi
    if [ -n "$DOMAIN" ] && [ -z "$EMAIL" ]; then
        DEFAULT_EMAIL="admin@$DOMAIN"
        if [ -t 0 ]; then
            printf "  Let's Encrypt contact email [%s]: " "$DEFAULT_EMAIL"
            read -r EMAIL
        fi
        EMAIL=${EMAIL:-$DEFAULT_EMAIL}
    fi
fi

# ─────────────────────────────── preflight ────────────────────────────────

say "preflight"
[ "$(id -u)" = 0 ] || die "run as root"

command -v nft >/dev/null || { apt-get update -qq && apt-get install -y -qq nftables; }
command -v curl >/dev/null || { apt-get update -qq && apt-get install -y -qq curl; }
for p in git dig; do
    command -v "$p" >/dev/null || apt-get install -y -qq git dnsutils
done

if [ -n "$DOMAIN" ]; then
    PUBLIC_IP=$(curl -fsS --max-time 10 https://api.ipify.org || echo "")
    RESOLVED=$( (dig +short "$DOMAIN" A @1.1.1.1 || true) | tail -1)
    if [ -z "$RESOLVED" ]; then
        warn "$DOMAIN has no A record; check DNS before using AnyTLS/Hysteria2/TUIC"
    elif [ -n "$PUBLIC_IP" ] && [ "$RESOLVED" != "$PUBLIC_IP" ]; then
        warn "$DOMAIN resolves to $RESOLVED but this host is $PUBLIC_IP"
        warn "if the record is proxied, switch it to DNS only: these proxy"
        warn "protocols cannot use an HTTP CDN"
    else
        ok "$DOMAIN -> $RESOLVED (this host)"
    fi
fi

# The panel has to be reachable before anything is built.
STATUS=$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 "${PANEL%/}/login" || echo 000)
[ "$STATUS" = 000 ] && die "cannot reach $PANEL"
ok "panel reachable"

# ──────────────────────────────── build ───────────────────────────────────

install -d -m 0700 "$ROOT"
BUILD=$ROOT/build
mkdir -p "$BUILD"

fetch() { # fetch <repo-name-or-url> <dest>
    local repo=$1 dest=$2 url label
    case "$repo" in
        http://*|https://*|ssh://*|git@*)
            url=$repo
            label=${repo##*/}
            label=${label%.git}
            ;;
        *)
            url="https://github.com/${GH_OWNER}/${repo}.git"
            label=$repo
            ;;
    esac
    rm -rf "$dest"
    # The token goes in a per-command header, not in the URL: git writes the
    # remote URL into the clone's .git/config, and a token in it would sit on
    # disk for as long as the build directory does.
    if [ -n "$GH_TOKEN" ]; then
        git -c "http.extraHeader=Authorization: Basic $(printf 'x-access-token:%s' \
            "$GH_TOKEN" | base64 -w0)" \
            clone -q --branch "$REF" --depth 1 "$url" "$dest" \
            || die "cannot clone ${GH_OWNER}/${repo} (check GITHUB_TOKEN)"
    else
        git clone -q --branch "$REF" --depth 1 "$url" "$dest" \
            || die "cannot clone ${GH_OWNER}/${repo} (a private repo needs GITHUB_TOKEN)"
    fi
    ok "${label}@$(git -C "$dest" rev-parse --short HEAD)"
}

say "sources"
if [ -n "$SRC_DIR" ]; then
    rm -rf "$BUILD/veyra-node"; cp -a "$SRC_DIR" "$BUILD/veyra-node"; ok "using $SRC_DIR"
else
    fetch veyra-node "$BUILD/veyra-node"
fi
if [ -n "$FORK_DIR" ]; then
    rm -rf "$BUILD/veyra-core"; cp -a "$FORK_DIR" "$BUILD/veyra-core"; ok "using $FORK_DIR"
else
    fetch "$CORE_REPO" "$BUILD/veyra-core"
fi

find "$BUILD" -type f -name '*.sh' -exec sed -i 's/\r$//' {} + 2>/dev/null || true

if ! command -v docker >/dev/null; then
    say "installing docker (used only to build; nothing runs in it)"
    curl -fsSL https://get.docker.com | sh >/dev/null
    # Remembered so --purge can remove Docker again without guessing. A host
    # that already had it is running something in it.
    touch "$ROOT/.docker-installed-by-skysbx"
fi

# Stamped into the binary so `--version` can answer what is running without
# anyone reading a build log.
VER=$(git -C "$BUILD/veyra-node" rev-parse --short HEAD 2>/dev/null || echo unknown)

say "building"
# The build tags are not optional: without them the binary compiles but exits at
# startup on "clash api is not included in this build". Go must be 1.26.x —
# 1.27 fails to link, because sing-box reaches an unexported http2 field through
# go:linkname.
docker run --rm -v "$BUILD:/src" -w /src/veyra-node \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -e GOOS=linux \
    golang:1.26.5 \
    go build -trimpath \
        -tags 'with_clash_api,with_v2ray_api,with_utls,with_acme,with_quic' \
        -ldflags "-s -w -X main.version=$VER \
                  -X github.com/sagernet/sing-box/constant.Version=1.14.0" \
        -o /src/veyra-node/skysbx-node ./cmd/node
install -m 0755 "$BUILD/veyra-node/skysbx-node" "$ROOT/skysbx-node"
ok "node binary installed"

# ────────────────────────────── certificate ───────────────────────────────

# AnyTLS, Hysteria2 and TUIC need a certificate. Reality uses its own key pair;
# Shadowsocks 2022 has no TLS layer.
if [ -n "$DOMAIN" ] && [ "$SKIP_CERT" = 0 ]; then
    # A panel and node may share this host and domain.  The panel's CertMagic
    # listener already owns :80, so certbot --standalone cannot work here.
    # Reuse the panel certificate just as the combined installer does.  This
    # also makes the ordinary node installer safe to use after a panel was
    # installed first.
    PANEL_DOMAIN=""
    if [ -f "$ROOT/panel.env" ]; then
        PANEL_DOMAIN=$(sed -n 's/^SKYSBX_DOMAIN=//p' "$ROOT/panel.env" | head -1)
    fi
    if [ "$PANEL_DOMAIN" = "$DOMAIN" ]; then
        PANEL_CERT=$(find "$ROOT/certs/certificates" -type f -name "$DOMAIN.crt" -print -quit 2>/dev/null || true)
        PANEL_KEY=${PANEL_CERT%.crt}.key
        if [ -n "$PANEL_CERT" ] && [ -f "$PANEL_KEY" ]; then
            ln -sfn "$PANEL_CERT" "$ROOT/cert.pem"
            ln -sfn "$PANEL_KEY" "$ROOT/key.pem"
            ok "reusing the local panel certificate"
            SKIP_CERT=1
        else
            warn "local panel domain matches, but its certificate was not found under $ROOT/certs"
        fi
    fi
fi

if [ -n "$DOMAIN" ] && [ "$SKIP_CERT" = 0 ]; then
    say "certificate for $DOMAIN"
    command -v certbot >/dev/null || apt-get install -y -qq certbot

    cat > "$ROOT/certbot-deploy.sh" <<EOF
#!/bin/sh
# sing-box reads certificates once, at start, so copying the files is not
# enough — the node has to be restarted to pick them up.
set -eu
LIVE=/etc/letsencrypt/live/$DOMAIN
[ -f "\$LIVE/fullchain.pem" ] || exit 0
install -m 0644 "\$LIVE/fullchain.pem" "$ROOT/cert.pem"
install -m 0600 "\$LIVE/privkey.pem"   "$ROOT/key.pem"
systemctl is-active --quiet skysbx-node && systemctl restart skysbx-node || true
EOF
    chmod 0755 "$ROOT/certbot-deploy.sh"

    ARGS="certonly --non-interactive --agree-tos -m $EMAIL -d $DOMAIN
          --deploy-hook $ROOT/certbot-deploy.sh --keep-until-expiring"
    if [ -n "$CF_TOKEN" ]; then
        apt-get install -y -qq python3-certbot-dns-cloudflare
        mkdir -p /etc/letsencrypt
        printf 'dns_cloudflare_api_token = %s\n' "$CF_TOKEN" > /etc/letsencrypt/cloudflare.ini
        chmod 600 /etc/letsencrypt/cloudflare.ini
        # shellcheck disable=SC2086
        certbot $ARGS --dns-cloudflare \
            --dns-cloudflare-credentials /etc/letsencrypt/cloudflare.ini \
            --dns-cloudflare-propagation-seconds 30 \
            || warn "certbot failed; Reality and Shadowsocks still work"
    else
        # shellcheck disable=SC2086
        certbot $ARGS --standalone \
            || warn "certbot failed; Reality and Shadowsocks still work"
    fi
    # --keep-until-expiring makes a re-run a no-op, and a no-op does not fire
    # the deploy hook, so copy here too.
    "$ROOT/certbot-deploy.sh" || true
    systemctl enable -q --now certbot.timer 2>/dev/null || true
    [ -f "$ROOT/cert.pem" ] && ok "certificate at $ROOT/cert.pem"
fi

# ─────────────────────────────── service ──────────────────────────────────

say "service"
# The token goes in an environment file rather than the command line, which is
# readable by every process on the host.
cat > "$ROOT/node.env" <<EOF
SKYSBX_PANEL=${PANEL}
SKYSBX_TOKEN=${TOKEN}
SKYSBX_LOG=info
EOF
chmod 600 "$ROOT/node.env"

cat > /etc/systemd/system/skysbx-node.service <<EOF
[Unit]
Description=Veyra Node (embedded sing-box data plane)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${ROOT}
EnvironmentFile=${ROOT}/node.env
ExecStart=${ROOT}/skysbx-node
Restart=always
RestartSec=3
LimitNOFILE=1048576

# Binding low ports is what Reality on 443 needs; the rest is for the packet
# path.
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable -q skysbx-node
# restart, not `enable --now`: on an upgrade the binary has just been replaced
# and --now would leave the old process running.
systemctl restart skysbx-node
install -m 0755 "$BUILD/veyra-node/deploy/veyra-menu.sh" /usr/local/bin/veyra
ok "management command installed: veyra"
sleep 5

if systemctl is-active --quiet skysbx-node; then
    ok "node is running"
else
    warn "node did not start: journalctl -u skysbx-node -n 50"
fi

cat <<EOF

${GRN}Veyra Node
==========
Panel     ${PANEL}
$([ -n "$DOMAIN" ] && echo "Domain    ${DOMAIN}")
Data      ${ROOT}

Logs      journalctl -u skysbx-node -f

The node dials the panel, so it needs no inbound control port and no route from
it. Add inbounds in the panel; they take effect within seconds.${RST}
EOF
