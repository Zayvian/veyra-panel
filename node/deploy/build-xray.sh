#!/bin/sh
# Build the Xray named in node/xray.lock.
#
#   node/deploy/build-xray.sh <output file>
#
# GOOS and GOARCH pick the target as for go build. CI runs this for the
# release, and the node installer runs it in Docker when there is no release
# binary to download, so both produce the same program from the same lock.
#
# Xray is built from its Go module rather than downloaded from Xray's own
# releases: the module proxy checks every module against the checksum
# database, and the build needs no route to github.com.
set -eu

out=$1
lock=$(cd "$(dirname "$0")/.." && pwd)/xray.lock
XRAY_VERSION=$(sed -n 's/^XRAY_VERSION=//p' "$lock")
XRAY_MODULE=$(sed -n 's/^XRAY_MODULE=//p' "$lock")
[ -n "$XRAY_VERSION" ] && [ -n "$XRAY_MODULE" ] || { echo "cannot read $lock" >&2; exit 1; }
case "$out" in /*) ;; *) out=$(pwd)/$out ;; esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
# A throwaway module to build in: `go install pkg@version` would be simpler,
# but it refuses to cross-compile into a chosen file.
go mod init veyra-xray-build >/dev/null 2>&1
go get "github.com/xtls/xray-core/main@$XRAY_MODULE" >/dev/null 2>&1
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -buildid=" -o "$out" github.com/xtls/xray-core/main

# Run it to check the version only when it can run here.
if [ "${GOARCH:-$(go env GOHOSTARCH)}" = "$(go env GOHOSTARCH)" ]; then
    v=$("$out" version | head -1)
    case "$v" in
        "Xray $XRAY_VERSION "*) ;;
        *) echo "built $v, want Xray $XRAY_VERSION" >&2; exit 1 ;;
    esac
fi
