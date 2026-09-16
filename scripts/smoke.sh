#!/usr/bin/env bash
# smoke.sh -- one-command liveness check for a running Hermetrix server.
# Usage: ./scripts/smoke.sh [base-url]   (default http://127.0.0.1:7331)
# Fails non-zero on the first broken surface. Read-only: never writes.
set -euo pipefail

BASE="${1:-http://127.0.0.1:7331}"

fail() { echo "SMOKE FAIL: $1" >&2; exit 1; }
pass() { echo "SMOKE OK: $1"; }

health="$(curl -sf --max-time 5 "$BASE/api/health")" || fail "GET /api/health unreachable"
echo "$health" | grep -q '"ok":true' || fail "/api/health ok != true ($health)"
schema="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("schema"))')"
expected="$(echo "$health" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("expected_schema"))')"
[ "$schema" = "$expected" ] || fail "schema mismatch ($schema != $expected)"
pass "health schema=$schema"

bootstrap="$(curl -sf --max-time 5 "$BASE/api/bootstrap")" || fail "GET /api/bootstrap unreachable"
for key in skills candidates sessions providers profiles mcp_servers reviews; do
  echo "$bootstrap" | grep -q "\"$key\"" || fail "bootstrap missing key $key"
done
pass "bootstrap keys present"

curl -sf --max-time 5 "$BASE/api/providers" >/dev/null || fail "GET /api/providers unreachable"
pass "providers reachable"

curl -sf --max-time 5 "$BASE/api/projects" >/dev/null || fail "GET /api/projects unreachable"
pass "projects reachable"

code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$BASE/")"
[ "$code" = "200" ] || fail "GET / returned $code"
pass "UI serves 200"

echo "smoke: all green against $BASE"
