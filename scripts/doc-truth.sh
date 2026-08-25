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
model-reviewer|func \(r \*ModelReviewer\) Review|internal/learning/model_reviewer.go
workspace-search|func searchFiles|internal/tools/registry.go
reasoning-aware-budget|func answerBudget|internal/agent/service.go
analyzer-retrieval|func similarityScore|internal/skills/analyzer.go
reasoning-calibration|func \(s \*Service\) ObserveReasoning|internal/providers/service.go
empty-answer-fails|returned no answer|internal/agent/service.go
read-window-keeps-whole-hash|metadata\["total_lines"\]|internal/tools/registry.go
reviewer-fails-closed|func parseReviewerDecision|internal/learning/model_reviewer.go
adr7-exit-threshold|NoSkillRequestedRate > 0.5|internal/agent/service.go
token-ledger-balances|func \(r \*Report\) reconcile|internal/context/compiler.go
token-ledger-has-witnesses|attributed to deduplication but no fragment was removed|internal/context/compiler.go
prompt-not-budget|PredictedPrompt|internal/context/compiler.go
transport-priced|func transportCost|internal/context/compiler.go
overhead-measured-not-fitted|func \(s \*Service\) MeasureTokenOverhead|internal/providers/service.go
overhead-refuses-nonsense|is not a chat template|internal/providers/service.go
script-rate-learned|func \(s \*Service\) ObserveNonASCIIRate|internal/providers/service.go
script-rate-applied|type ScriptEstimator|internal/context/estimator.go
calibration-divides-out-applied|ratio := applied \* float64\(actual\)|internal/providers/service.go
calibration-persisted|token_multiplier = \(\(token_multiplier \* token_sample\)|internal/providers/service.go
token-accuracy-windowed|TokenAccuracyWindow|internal/agent/models.go
token-observation-per-step|func \(s \*Service\) recordTokenObservation|internal/agent/service.go
unsendable-args-not-budgeted|content = replayableArguments\(content\)|internal/agent/service.go
thai-retrieval-shared-tokenizer|textmatch.Terms|internal/agent/service.go
health-reports-real-schema|func \(s \*Store\) SchemaVersion|internal/store/store.go
import-reports-what-it-dropped|func tablesNotRestored|internal/product/backup.go
replay-implicit-only|replay_implicit_only|internal/skills/replay.go
fidelity-pressure-case|func pressureCase|internal/fidelity/service.go
error-page-not-conversation|func summariseErrorBody|internal/providers/openai.go
corpus-label-required|case is unlabelled|internal/learning/corpus.go
corpus-invented-evidence|func inventedEvidence|internal/learning/corpus.go
corpus-splits-provenance|driven", "synthetic", "all|internal/learning/corpus.go
reviewer-error-not-judgement|Unusable bool|internal/learning/models.go
corpus-worst-of-n|func worstOf|internal/learning/corpus.go
corpus-reports-instability|UnstableCases|internal/learning/corpus.go
corpus-retries-transient-faults|func reviewWithRetry|internal/learning/corpus.go
corpus-citation-punctuation|trailingPunctuation|internal/learning/corpus.go
corpus-shape-sampling|func digestShape|cmd/hermetrix/corpus.go
program-is-versioned|/hermetrix|.gitignore
ci-builds-the-program|the program exists and runs|.github/workflows/ci.yml
verdict-reads-decision-recall|metrics.DecisionRecall == 1|internal/fidelity/service.go
pinned-retention-is-binary|func TestPinnedEssentialsAreRetainedExactlyOrTheCompileFails|internal/fidelity/service_test.go
unpinned-retention-can-fail|func TestVerdictFailsWhenDeclaredDecisionsAreDropped|internal/fidelity/service_test.go
causal-pairs-held-or-refused|func TestCausalPairsSurviveTogetherOrTheCompileRefuses|internal/context/compiler_test.go
pair-split-refuses-compile|causal pair %s was split|internal/context/compiler.go
field-census-exists|NEVER PRODUCED|scripts/fragment-census.py
approvals-become-decisions|Kind: ctxcompiler.KindDecision|internal/agent/service.go
open-task-has-no-reachable-producer|func TestNoCompileRunsWhileAnApprovalIsOutstanding|internal/agent/service_test.go
derived-not-user-speech|func isTranscriptKind|internal/agent/service.go
corpus-carries-real-shapes|func approvalCase|internal/fidelity/service.go
retrieval-blindness-named|retrieval_blind|internal/agent/service.go
retrieval-blindness-counted|TurnsGoalScriptUnmatched|internal/agent/models.go
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
