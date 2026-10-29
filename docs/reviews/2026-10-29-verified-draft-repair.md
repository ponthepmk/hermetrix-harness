# Verified failed drafts and bounded proposal recovery

This work improves the large-project repair loop. It does **not** establish that
Bonsai can complete the marketplace, perform every O-SOUL journey, or learn useful
new behavior autonomously. All three actual API repair trials below failed.

## Change and reason

Rollback must restore actual files, including absence. Previously the next worker
had to regenerate whole files from diagnostic text. The new coordinator supplies
at most two complete historical files, sharing an 8,192-byte UTF-8 budget, as
`repair_drafts`. They are separate from current `Files` / `AbsentFiles`.

The origin must be a checksum-verified verification-failed proposal from the same
task, project and requirement revision. The frozen attempt, proposal bundle,
failure log, root and current rolled-back preimage must match. A prior plan in
the same requirement is eligible; another requirement is not. Only selected whole
files with explicit writable permission qualify. Oversize drafts are omitted,
never clipped, and must fit the existing inference budget.

The worker accepts exact edits against a draft, or explicit `retain_draft:true`
for an unchanged companion. It derives full unverified content while retaining
the real workspace preimage. The persisted proof is rederived before use.
Ordinary paths keep their existing source contract. Credential, byte, file-view,
scope, legacy-input and result-size guards remain enforced.

An actual model response exposed a contradictory ordinary-content instruction
and permissive schema. The draft-mode instruction and schema now agree on each
path's allowed forms; ordinary requests retain the previous contract.

A complete invalid proposal can now close a failed attempt and start a fresh
bounded attempt only after its observed failed proposer receipt is verified and
all attempt effects are settled provider effects. No operation is replayed and
goal counters/deadline are retained. Transport, truncated responses, uncertainty,
partially persisted output and workspace effects still require inspection.

The final coordinator also rejects proposals that only reproduce unchanged
verification-failed drafts, before artifact/apply dispatch. Retaining a companion
alongside an actual repair remains allowed. This addresses the observed native
model behavior without pretending that the model fixed its mistake.

## Actual Bonsai trials — negative results

All three use the existing local 65,536-context / 8,192-output profiles, one physical
model, ordinary goal/apply/command/rollback machinery, isolated data (v1/v2 schema82;
published-v3 schema81),
and an explicitly labelled first-source transport fixture of earlier native Go.
They are seeded component trials, not unseeded project generation or a fair A/B.
The unchanged native regression runs an actual configured HTTP server and catches
EOF when a five-second handler deadline equals the five-second write timeout.
No standalone application was served. Original schema-81 data was not migrated.

| Trial | Actual model calls | Seed calls | Outcome |
|---|---:|---:|---|
| v1 | 4 | 1 | Waiting after complete response submitted forbidden full content; one failing test/rollback. No repaired source retained. |
| v2 | 7 | 1 | 31 actions, 3 attempts, 2 plans; budget exhausted after three failing checks/rollbacks. Draft retention was valid in form, but unchanged source still failed. |
| v3, published9476f93 | 7 | 1 | 18 actions, 3 attempts, 2 plans; budget exhausted. One seeded test failure/rollback; two unchanged proposals rejected before apply and recovered automatically within the same goal budget. |

v2's model claimed the draft already contained a ten-second write timeout while
the actual draft still contained five seconds. Real tests prevented completion.
The readonly module files and the native regression retained their hashes.
No rejected output was salvaged; none of the trials was reset or resumed for a
better score. The final unchanged-draft rejection was added **after** v2; v3
observed its two before-apply rejections, but still no successful model repair.

## Verification and publication state

Read the [validation record](../validation/2026-10-29-verified-draft-repair.json)
for exact hashes, scope and the latest terminal outcomes. Raw evidence stays in
ignored `.tmp-ui/draft-repair-integration-20261029/`; generated projects stay out
of Git. Codex authored the current integration under the owner's updated Harness
authorization. Two worker test files retain their earlier native-authored bytes.

Working-tree targeted tests passed actual SQLite, apply, failing command,
rollback, repair, post-test fixture review and reopen for existing/absent files.
They also cover resource/provenance rejection, rejected-response retry limits,
transport/truncation stops, and unchanged-draft rejection before apply. Model
transport in these tests is a fixture, not proof of Bonsai reasoning.

The earlier three-package run passed 415 test events / one skip, before the final
proposal-recovery changes. A later full working-tree run recorded 1,562 pass,
23 skip, six per-test failures and a coordinator package timeout. The Discord
deadline case passed in isolation; pilot preflight's foreign-listener case still
failed in isolation. These results remain unresolved; no full-suite green claim.

An isolated 20-file commit candidate excludes the unrelated shared-worktree
architecture/Skills/UI/learning changes. It integrates equivalent repair hooks
into the published coordinator monolith. Its initial broad/default-temp runs
timed out in SQLite Windows file IO; one goal fixture also failed to finish in
its helper window. A focused rerun using child-only temporary storage on D passed
unchanged acceptance. This observation does not establish the IO root cause.
Candidate vet/build passed. The final serialized candidate run passed all three affected packages: 419 pass
events, one conditional skip, zero failures. It used child-only D temporary
storage and a 600-second per-package test timeout; goal/provider/check budgets
were unchanged. The initial two broad code-review replies and the first bounded v2 reply
truncated at8,192tokens. Investigation found that these operator drivers omitted
the production reviewer preset: their ledger was unscoped with empty preset and
parameter digest. They are retained failed experiments, not a valid evaluation
of the production reviewer contract. No partial output was salvaged.

Corrected v3 uses the existing ResolveCodeReviewPreset plus inference.WithOwner,
matching the production path. The same frozen source is covered by five bounded
parts (18–27KB each), including every changed path and all changed hunks for
large tracked files. Actual HTTP captures confirm max_tokens4096, reasoning
budget0 and enable_thinking=false; each ledger row pins the preset/digest. All
five complete decisions approved with zero findings. Same physical Bonsai model,
no diversity or fair speed comparison. Provider defaults and validators were not
changed. Commit9476f93 publishes only the20qualified repair Go files and was
pushed todev, preserving88a979c and unrelated uncommitted architecture work.
Review decomposition here is operator qualification; no automatic production
review partitioner is claimed.

The fresh independent native API trialv3 used published9476f93 Go source in a
supported schema81 copy, the exact seed/regression and3attempt/2plan/600second
budgets. It ended budget_exhausted:18actions,7actual model calls,1seed,1test-failed
rollback,2complete unchanged proposals rejected before apply. Automatic recovery
and attempt limits worked; the model still did not repair the code. The actual
worker brief explicitly contained WriteTimeout10, so missing that target from
context is not supported as this trial's cause. No previous run was resumed or
reset, no accepted repair was retained, and no post-test success review occurred.
These observations are not a fair action-count or performance A/B.

## Diagnosis and causal verification prototype

A separate read-only operator prototype used the existing production review-role
transport against the checksum-verified failed proposal, original owner request,
readonly regression and actual failure. It did not resume or change the exhausted
goal or project source. The model gave the correct direction (WriteTimeout10,
handler deadline5) but quoted ReadTimeout5 and admitted uncertainty about the
causal line. The prototype's mechanically passing quote/hash validation proves
only that the quoted bytes existed uniquely, not that the quote explains failure.
It is not a successful semantic diagnosis or an autonomous repair.

An operator-authored HTTP experiment against unchanged native failing source
separated the two hypotheses. Changing only ReadTimeout to10 still yielded EOF;
changing only WriteTimeout to10 yielded503 not_ready JSON. Both cases passed their
behavioral expectations (3pass events including the parent). They used runtime
server-field overrides in an ignored experiment, not production source edits or
new passing evidence for the exhausted goal. Native source/module/regression
hashes and raw results are recorded. No actual database/main process was used.

Next work is to ground a failure analyst's proposed symbol/hypothesis in real
behavior before allowing it to guide an edit, then integrate that bounded phase
into the ordinary goal lifecycle with retained authority, budgets and uncertainty.
Do not repeat the unchanged goal to fish for a successful model answer or use
byte-match validation as semantic proof.

The full goal remains autonomous detailed planning, relevant bounded context and
Skills, decomposition, repair, real verification, reverse engineering and the
complete marketplace journeys. Positive held-out learning transfer, deployment,
physical voice acceptance, complete website/security coverage and a full native
large-project success remain unproved.

## Separate database-version protection published

Commit `88a979c` was pushed to `dev`, containing only the future-schema open guard
and its regression. The source checks the existing version before journal-mode
changes or migration and rejects databases newer than the binary supports.
The published schema-81 baseline reproduced the defect; guarded schema-81 and
current schema-82 regressions passed, as did store vet/build. The isolated
published-schema-81 store suite passed21events/1conditional skip/0fail. Tests verify exact
unchanged database bytes, marker data, journal mode, version and released lock.
This commit does not include the repair component or the unrelated schema-82
migration/architecture work, and no installed application was deployed.

The operator's isolated reviewer compiled the old schema-81 baseline and opened
the schema-82 trial copy. That exposed the old bug by lowering its version tag to
81, while its schema-82 table remained. After both review calls were terminal,
the current schema-82 opener restored the tag through its idempotent migration.
Canonical snapshots of six task/attempt/effect/proposal/goal tables were identical
before/after restoration. The original schema-81 runtime was untouched. Backup
and before/after hashes are retained locally; no task budgets, consumed IDs or
trial outcomes were reset.
