# shellcheck shell=bash
# Sourced by panel/deploy/install-panel.sh and node/deploy/install-node.sh.
# Expects say, ok, warn, die and GH_OWNER from the script that sources it.

# fetch_release_binary <name> <release tag> <destination>
#
# Downloads <name>-linux-<arch> from the GitHub release and checks it against
# the release's sha256sums.txt before putting it at <destination>.
#
# Returns non-zero when there is nothing usable to download — an architecture
# CI does not build for, a release without the file, no route to GitHub — so
# the caller can compile instead. A file that downloads but does not match its
# checksum is a different matter: that is not a gap to route around, and the
# installer stops.
fetch_release_binary() {
    local name=$1 tag=$2 dest=$3 arch asset base tmp
    case "$(uname -m)" in
        x86_64|amd64)  arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) warn "no prebuilt binary for $(uname -m)"; return 1 ;;
    esac
    asset="$name-linux-$arch"
    base="https://github.com/${GH_OWNER}/veyra-panel/releases/download/$tag"
    tmp=$(mktemp -d)

    say "downloading $asset from release $tag"
    if ! curl -fsSL --retry 3 --connect-timeout 15 -o "$tmp/$asset" "$base/$asset" \
        || ! curl -fsSL --retry 3 --connect-timeout 15 -o "$tmp/sha256sums.txt" "$base/sha256sums.txt"; then
        rm -rf "$tmp"
        warn "could not download $asset from $tag"
        return 1
    fi
    # No line for the file counts as a mismatch: sha256sum -c given nothing to
    # check fails, which is the answer wanted.
    if ! (cd "$tmp" && grep -E "^[0-9a-f]{64}  $asset\$" sha256sums.txt | sha256sum -c --status -); then
        rm -rf "$tmp"
        die "$asset does not match the checksum published with $tag; refusing to install it"
    fi
    chmod 0755 "$tmp/$asset"
    # The right file for the wrong machine (or a libc surprise) fails here, not
    # after it has replaced a working binary and the service will not start.
    if ! "$tmp/$asset" -version >/dev/null 2>&1; then
        rm -rf "$tmp"
        warn "$asset does not run on this host"
        return 1
    fi
    mv -f "$tmp/$asset" "$dest"
    rm -rf "$tmp"
    ok "$asset verified"
}
