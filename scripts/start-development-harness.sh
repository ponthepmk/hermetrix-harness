#!/bin/sh
# Development-only launcher for macOS/Linux. Never migrates installed data.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
exe=${1:?Usage: start-development-harness.sh EXECUTABLE [check|initialize|start]}
mode=${2:-check}
case "$mode" in check|initialize|start) ;; *) echo 'Invalid launcher mode' >&2; exit 2;; esac
case "$exe" in /*) ;; *) exe="$PWD/$exe";; esac
[ -f "$exe" ] && [ -x "$exe" ] || { echo 'Executable is missing or not executable' >&2; exit 2; }
stage="$repo/.hermetrix-g1-staging"
data="$stage/runtime-posix"
[ ! -L "$stage" ] && [ ! -L "$data" ] || { echo 'Linked staging data is not allowed' >&2; exit 2; }
report=$("$exe" preflight --data "$data")
printf '%s\n' "$report"
[ "$mode" != check ] || exit 0
if printf '%s\n' "$report" | grep -q '"state": "not_initialized"'; then
    [ "$mode" = initialize ] || { echo 'New staging data requires initialize mode' >&2; exit 2; }
else
    printf '%s\n' "$report" | grep -q '"ready_without_migration": true' || { echo 'Offline migration/restore verification required; nothing started' >&2; exit 2; }
fi
# Keep providers independent of application startup. Bonsai belongs to the PC
# provider; Mac installation must never require a local Bonsai process. Configure
# cross-machine providers explicitly with authenticated transport/egress approval.
export HERMETRIX_REQUIRE_OS_SANDBOX=1
exec "$exe" serve --data "$data" --workspace "$repo" --listen 127.0.0.1:18962
