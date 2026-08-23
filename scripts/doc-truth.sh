#!/usr/bin/env bash
# doc-truth.sh -- two layers of defence against documentation drift.
#
# Layer 1 (facts)  regenerates the numbers that go stale fastest, straight from
#                  the source, so nobody hand-maintains them.
# Layer 2 (claims) checks that every registered claim's evidence anchor still
#                  exists. A claim whose anchor disappeared is a claim about a
#                  system that no longer works that way.
#
# WHAT THIS IS NOT: an oracle. Layer 2 proves an anchor exists, never that the
# prose around it is still true. "The reviewer yields to foreground work" can
# be false while every symbol it names is present. Semantic claims still need a
# human pass over the findings list when a phase closes -- see P-1 in
# docs/FUTURE-ARCHITECTURE-PLAN.md.
set -euo pipefail
cd "$(dirname "$0")/.."

mode="${1:-report}"
status=0

section() { printf '\n%s\n%s\n' "$1" "$(printf '%*s' "${#1}" '' | tr ' ' '-')"; }

# ---------------------------------------------------------------- layer 1
section "Facts"

test_functions=$(grep -rhoE '^func Test[A-Za-z0-9_]+' --include='*_test.go' internal | wc -l | tr -d ' ')
test_packages=$(go list ./... 2>/dev/null | wc -l | tr -d ' ')
printf 'test functions:        %s\n' "$test_functions"
printf 'packages:              %s\n' "$test_packages"

direct_tools=$(grep -cE '^\t\t\{Name: "' internal/tools/registry.go || true)
printf 'direct primitives:     %s\n' "$direct_tools"
grep -oE '^\t\t\{Name: "[a-z_.]+"' internal/tools/registry.go | sed 's/.*"\(.*\)"/                       \1/' || true

printf 'context profiles:\n'
grep -oE '\{Name: "[a-z0-9-]+", Total: [0-9]+' internal/context/profile.go |
  sed 's/{Name: "\(.*\)", Total: \(.*\)/                       \1 = \2/' || true

routes=$(grep -coE 'mux\.HandleFunc\("' internal/web/server.go || true)
printf 'HTTP routes:           %s\n' "$routes"

tables=$(grep -cE '^CREATE TABLE IF NOT EXISTS' internal/store/store.go || true)
printf 'SQLite tables:         %s\n' "$tables"

go_lines=$(find internal cmd assets -name '*.go' -not -name '*_test.go' -exec cat {} + | wc -l | tr -d ' ')
test_lines=$(find internal -name '*_test.go' -exec cat {} + | wc -l | tr -d ' ')
printf 'Go lines (non-test):   %s\n' "$go_lines"
printf 'Go lines (test):       %s\n' "$test_lines"

# ---------------------------------------------------------------- layer 2
section "Claims"

# claim-id | evidence anchor (regex) | file it must appear in
claims=$(cat <<'CLAIMS'
turn-lease-cas|WHERE id=\? AND state='active' AND active_turn_id=''|internal/agent/service.go
session-contract-freeze|func \(s \*Service\) buildSessionContract|internal/agent/service.go
skills-frozen-once|SkillsInitialized|internal/agent/service.go
learning-producer|func \(s \*Service\) StageTrigger|internal/learning/service.go
learning-drain|func \(s \*Service\) DrainPending|internal/learning/service.go
task-budget|MaxCumulativeTokens|internal/agent/service.go
loop-detector|third identical call|internal/agent/service.go
qualification-exact-binding|AND provider_revision=\? AND requested_profile=\?|internal/agent/service.go
override-expiry|override expired|internal/agent/service.go
tier-ultra-1m|ultra-1m|internal/qualification/service.go
recall-positions|RecallPositions|internal/qualification/service.go
exact-tool-accounting|func \(t ToolSpec\) BillableText|internal/context/types.go
gc-compensating-rollback|RestoreFromQuarantine\(prior.Ref|internal/curator/maintenance.go
gc-no-hard-delete|partial_quarantine|internal/curator/maintenance.go
mcp-no-auto-retry|automatic_retry|internal/mcp/service.go
skill-retrieval-metric|func \(s \*Service\) SkillRetrievalMetrics|internal/agent/service.go
adr7-exit-threshold|NoSkillRequestedRate > 0.5|internal/agent/service.go
CLAIMS
)

while IFS='|' read -r id anchor file; do
  [ -z "$id" ] && continue
  if [ ! -f "$file" ]; then
    printf 'MISSING FILE  %-28s %s\n' "$id" "$file"
    status=1
  elif grep -qE "$anchor" "$file"; then
    printf 'ok            %-28s %s\n' "$id" "$file"
  else
    printf 'ANCHOR GONE   %-28s %s  (%s)\n' "$id" "$file" "$anchor"
    status=1
  fi
done <<< "$claims"

# ---------------------------------------------------------------- verdict
section "Verdict"
if [ "$status" -eq 0 ]; then
  echo "every registered claim still has its evidence anchor."
  echo "this does NOT mean the prose is accurate -- review the findings list by hand."
else
  echo "at least one claim lost its anchor. Update the claim or the docs that rest on it."
fi

[ "$mode" = "check" ] && exit "$status"
exit 0
