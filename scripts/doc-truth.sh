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

test_functions=$(rg -g '*_test.go' '^func Test[A-Za-z0-9_]+' internal | wc -l | tr -d ' ')
test_packages=$(GOCACHE="${GOCACHE:-/tmp/hermetrix-doc-truth-go-cache}" GOPROXY=off GOSUMDB=off go list ./... 2>/dev/null | wc -l | tr -d ' ')
printf 'test functions:        %s\n' "$test_functions"
printf 'packages:              %s\n' "$test_packages"

direct_tool_files=(internal/tools/registry.go internal/tools/definitions_runtime.go)
direct_tools=$(grep -hE '^\t\t\{Name: "' "${direct_tool_files[@]}" | wc -l | tr -d ' ')
printf 'direct primitives:     %s\n' "$direct_tools"
grep -h -oE '^\t\t\{Name: "[a-z_.]+"' "${direct_tool_files[@]}" |
  sed 's/.*"\(.*\)"/                       \1/' || true

printf 'context profiles:\n'
grep -oE '\{Name: "[a-z0-9-]+", Total: [0-9]+' internal/context/profile.go |
  sed 's/{Name: "\(.*\)", Total: \(.*\)/                       \1 = \2/' || true

routes=$(grep -coE 'mux\.HandleFunc\("' internal/web/server.go || true)
printf 'HTTP routes:           %s\n' "$routes"

tables=$(grep -cE '^CREATE TABLE IF NOT EXISTS' internal/store/store.go || true)
printf 'SQLite tables:         %s\n' "$tables"

# Tables that exist in the schema and nowhere else. A table with no reader and
# no writer is not an unbuilt feature waiting its turn -- it is a claim the
# schema makes on the product's behalf. The backup manifest lists them, so a
# restore reports them as restored, which reads as coverage of features that do
# not exist. O-42.
schema_only=""
for table in $(grep -oE '^CREATE TABLE IF NOT EXISTS [a-z_]+' internal/store/store.go | awk '{print $NF}'); do
  # `|| true` at every stage: a table with no users makes grep exit 1, and
  # pipefail would take the whole script down before it printed anything.
  #
  # The manifest in backup.go names every table on one line, so excluding that
  # whole file would hide backup_runs, which the same file really does read and
  # write. Drop manifest-shaped lines instead -- three or more quoted lowercase
  # identifiers in a row -- and keep everything else.
  users=$( { grep -rn "$table" --include='*.go' internal cmd 2>/dev/null || true; } |
           { grep -vE 'internal/store/store(_test)?\.go' || true; } |
           { grep -vE '"[a-z_]+", *"[a-z_]+", *"[a-z_]+"' || true; } |
           wc -l | tr -d ' ')
  [ "$users" = "0" ] && schema_only="$schema_only $table"
done
if [ -n "$schema_only" ]; then
  printf 'schema-only tables:    %s\n' "$(echo $schema_only | wc -w | tr -d ' ')"
  for table in $schema_only; do printf '                       %s\n' "$table"; done
else
  printf 'schema-only tables:    0\n'
fi

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
workbench-optimistic-file|func \(s \*Service\) WriteProjectFile|internal/product/workbench.go
real-pty|func \(s \*Service\) StartTerminal|internal/product/terminal.go
managed-browser|func \(s \*Service\) OpenBrowserTab|internal/product/browser.go
native-deliverables|func \(s \*Service\) CreateDeliverable|internal/product/deliverables.go
team-dag|func validateTeamTaskGraph|internal/product/team.go
team-run-snapshots-roster|team_name,team_instructions|internal/product/team.go
team-cancel-propagates|func \(s \*Service\) CancelTeamRun|internal/product/team.go
team-member-history-retired|state='retired'|internal/product/team.go
cockpit-ui-contract|func TestHermetrixCockpitExposesEveryNativeWorkbenchRoom|internal/web/ui_contract_test.go
learning-producer|func \(s \*Service\) StageTrigger|internal/learning/service.go
learning-drain|func \(s \*Service\) DrainPending|internal/learning/service.go
measured-outcome-citations|VerifiedBy|internal/learning/models.go
browser-final-url-revalidated|func \(s \*Service\) acceptBrowserSnapshot|internal/product/browser.go
mcp-stdio-cancellation-unblocks-read|func \(session \*stdioSession\) readLineContext|internal/mcp/stdio.go
mcp-effects-are-not-replayed|func retryableMCPMethod|internal/mcp/pool.go
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
team-approval-pause|state='awaiting_approval'|internal/product/team.go
team-approval-resume|func \(s \*Service\) DecideTeamTaskApproval|internal/product/team.go
team-approval-no-replay|DecideApproval\(decisionCtx|internal/product/team.go
open-task-has-no-reachable-producer|func TestNoCompileRunsWhileAnApprovalIsOutstanding|internal/agent/service_test.go
derived-not-user-speech|func isTranscriptKind|internal/agent/service.go
corpus-carries-real-shapes|func approvalCase|internal/fidelity/service.go
retrieval-blindness-named|retrieval_blind|internal/agent/service.go
retrieval-blindness-counted|TurnsGoalScriptUnmatched|internal/agent/models.go
qualified-mode-unreachable-remotely|func TestRemoteProviderCannotReachQualifiedMode|internal/qualification/service_test.go
local-probe-refuses-remote|remote model endpoints are disabled in the local probe|internal/localmodel/probe.go
stored-content-cannot-execute|func TestStoredArtifactContentCannotRunAsAPage|internal/web/server_test.go
untrusted-annotations-cannot-waive|func TestUntrustedServerAnnotationsCannotWaiveApproval|internal/mcp/service_test.go
catalogue-is-never-a-primitive|func TestCatalogueMetadataNeverBecomesAProviderFunction|internal/tools/registry_test.go
taskeval-refuses-uncompacted|ErrNoPressure|internal/taskeval/runner.go
taskeval-no-judge-model|func \(r \*Runner\) score|internal/taskeval/runner.go
taskeval-instrument-registers|func TestTheInstrumentRegistersALostNeedle|internal/taskeval/runner_test.go
taskeval-sample-floor|MinimumTasksPerClass|internal/taskeval/models.go
taskeval-placement-is-measured|MiddlePlacementRate = 0.345|internal/taskeval/generate.go
taskeval-full-must-be-sendable|ErrFullContextTooLarge|internal/taskeval/runner.go
taskeval-corpus-is-generated|/corpus/tasks/\*.json|.gitignore
taskeval-retries-transient|func answerWithRetry|internal/taskeval/runner.go
promotion-needs-behavioral-eval|func \(s \*Service\) requireCurrentBehavioralEval|internal/skills/behavioral.go
eval-verdict-is-derived|func behavioralVerdict|internal/skills/behavioral.go
eval-binds-exact-candidate|eval.CandidateHash != candidate.CandidateHash|internal/skills/behavioral.go
eval-refusal-is-tested|func TestPromotionRefusesACandidateThatWasNeverEvaluated|internal/skills/behavioral_test.go
history-is-retrievable|context_search|internal/tools/registry.go
search-excerpt-centres-on-match|func Excerpt|internal/textmatch/excerpt.go
search-recovers-compacted-fact|func TestContextSearchRecoversWhatCompactionDestroyed|internal/agent/contextsearch_test.go
checkpoint-declares-its-loss|omits the middle|internal/context/compactor.go
checkpoint-names-the-recovery-tool|context_search with a keyword|internal/context/compactor.go
checkpoint-exemption-is-narrow|func isCheckpointPreamble|internal/context/compactor.go
compaction-ranks-by-relevance|func relevanceOf|internal/context/compactor.go
compaction-keeps-the-focused-span|func focusedExcerpt|internal/context/compactor.go
compactor-receives-the-goal|Focus: focusOf\(request.Fragments\)|internal/context/compiler.go
interrupted-write-is-reconciled|func \(r \*Registry\) ReconcileWrite|internal/tools/registry.go
unreadable-effect-stays-uncertain|func TestAnEffectThatCannotBeReReadStaysUncertain|internal/agent/service_test.go
recovery-never-runs-the-effect|recovery executed the side effect itself|internal/agent/service_test.go
retrieval-condition-exists|ConditionRetrieval|internal/taskeval/retrieval.go
retrieval-counts-whether-it-searched|SearchCalls|internal/taskeval/models.go
retrieval-separates-two-failures|func TestRetrievalConditionSeparatesNotSearchingFromSearchingBadly|internal/taskeval/runner_test.go
thai-focus-has-a-window|func densestTrigramWindow|internal/context/compactor.go
weak-focus-keeps-both-ends|focusedWindowFloor|internal/context/compactor.go
corpus-varies-phrasing|FarPhrasingRate|internal/taskeval/generate.go
far-phrasing-rate-is-chosen|this is chosen, not measured|internal/taskeval/generate.go
semantic-retrieval-is-optional|ErrNoEmbedder|internal/embedding/embedding.go
vectors-bound-to-a-revision|AND revision = \?|internal/agent/semantic.go
semantic-selection-is-relative|SemanticMargin|internal/agent/semantic.go
semantic-does-not-replace-lexical|func TestSemanticDoesNotDisplaceAnExactMatch|internal/agent/semantic_test.go
compaction-ranks-semantically|SemanticRelevance func\(fragmentID string\) SemanticHint|internal/context/compactor.go
no-anchor-means-sample-not-trim|func evenSlices|internal/context/compactor.go
semantic-rank-saves-a-fragment|func TestSemanticRelevanceSavesAFragmentLexicalRankingWouldDrop|internal/context/compiler_test.go
embedding-is-chunked|func Chunk|internal/embedding/embedding.go
chunk-maps-back-to-a-span|func ChunkSpan|internal/embedding/embedding.go
hint-carries-a-position|type SemanticHint|internal/context/compactor.go
score-alone-is-not-enough|func TestAScoreWithoutAPositionIsNotEnough|internal/context/compiler_test.go
skill-retrieval-crosses-scripts|func TestASemanticGoalReachesACatalogInAnotherLanguage|internal/agent/skillsemantic_test.go
skill-vectors-are-cached-by-text|CREATE TABLE IF NOT EXISTS skill_embeddings|internal/store/store.go
skill-semantic-cut-is-relative|func skillSemanticBonus|internal/agent/skillsemantic.go
semantic-controls-express-no-match|var semanticControls|internal/agent/skillsemantic.go
cross-script-measured-not-assumed|func TestRealEmbedderCrossesScripts|internal/agent/skillsemantic_real_test.go
hostile-corpus-meets-the-floor|func TestTheCorpusMeetsTheGatesFloor|internal/hostile/hostile_test.go
hostile-structural-uses-a-throwaway-store|hermetrix-hostile-|internal/hostile/structural.go
compliance-is-scored-by-position|ShapeEndsWith|internal/hostile/corpus.go
empty-reply-is-not-a-refusal|Inconclusive bool|internal/hostile/structural.go
quoting-an-attack-is-not-obeying|func withoutQuotedInjection|internal/hostile/behavioral.go
answers-are-rescorable-offline|func Rescore|internal/hostile/rescore.go
windows-has-its-own-process-handling|func configureProcessTermination|internal/product/commands_windows.go
corpus-measures-withdrawn-facts|RevisionSuperseded|internal/taskeval/generate.go
withdrawn-answer-is-its-own-count|StaleAnswersCompiled|internal/taskeval/models.go
corpus-still-has-loss-to-measure|func TestSupersededFactsGiveTheCorpusSomethingLeftToLose|internal/taskeval/runner_test.go
semantic-retrieval-is-a-serve-flag|embed-url|cmd/hermetrix/main.go
manifest-only-replay-blocks|ErrReplayImplicitOnly|internal/skills/replay.go
export-is-skill-lifecycle-only|func TestExportCarriesOnlyWhatImportRestores|internal/product/backup_test.go
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
