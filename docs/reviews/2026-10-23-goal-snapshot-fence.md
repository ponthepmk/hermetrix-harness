# Completion evidence and current source

Date: 2026-10-23. Broad goal remains active. This is source qualification;
the installed pilot was not replaced or executed with these changes.

## Reproduced failure

A barrier in the provider fixture holds `submit_review` after a real Node
acceptance command has passed. An external write then changes `sum.js`, or
`SaveProject` points the project at a different directory. Releasing the reviewer
with an approval completed both Task and Goal in both cases before this fix.
The earlier source check preceded the model call and could not establish that
the reviewed bytes remained current when its answer arrived.

## Implementation

Verification captures a canonical project root and SHA256/absence map covering
all explicit Task writable paths and the current proposal's output paths.
Capture and short evidence commits use the service's root/path guards. Single
file writes now participate in the root guard as batch writes already did, and
write path keys use the same Windows case normalization.

The saved snapshot is checked before command dispatch, after terminal command
observation, when binding the verification bundle, before and after provider
review, and independently before the final Goal receipt. No guard spans a model
request or managed process lifetime. Approved review finalization and the final
Goal receipt retain guards for their short commits. Rejected reviews release
them before invoking ordinary rollback.

A changed snapshot waits for inspection. An actual model response remains in
its artifact; the effect is observed as `review_snapshot_stale`, with
`accepted:false`. It does not create an approved post-review record, replay the
model, or roll back newer owner edits. A saved bundle/checkpoint without this
snapshot fails closed and needs inspection; a database schema change is not
required for these additive JSON fields.

## Observable acceptance

Permanent tests exercise real SQLite, writes and Node processes; model transport
alone is simulated:

- Hold a review, then edit its source, change its project root, or change another
  writable output outside the current proposal. All three wait with a released
  Goal claim, retain one observed review, and save no approved review. Owner
  bytes remain intact.
- Hold a Node check, change source to another implementation which still passes,
  then release the actual command. Its exit code is zero, but stale verification
  cannot complete the Task/Goal. The process starts exactly once; no rollback
  overwrites the owner's later bytes.
- Complete a Task through a separate review, resume the waiting Goal at that
  exact Task revision, then change the output. The final Goal action refuses
  the stale completed Task as proof for the current workspace.
- Hold a product snapshot guard: single writes, batch creation and project-root
  updates cannot cross it. Releasing it allows a write; the old snapshot then
  refuses the new bytes. Observing an absent path creates no placeholder.

Focused commands pass before broader qualification:

```text
go test ./internal/taskcoord -run '^TestGoalRejects' -count=1 -v
go test ./internal/taskcoord -run '^TestGoalFinishRechecks' -count=1 -v
go test ./internal/product -run '^TestSnapshotGuard' -count=1 -v
```

The pre-fix overlay reproduction remains under the ignored
`.tmp-ui/goal-20261023/review-fence-probe/` directory. It was promoted into the
permanent tests; the overlay is not required to run them.

The snapshot-fence revision before the later Stop fix also passes:

```text
HERMETRIX_REQUIRE_BROWSER_E2E=1 go test ./... -count=1
ok internal/agent       95.644s
ok internal/product    106.701s
ok internal/taskcoord  204.262s
ok internal/taskengine  29.528s
ok internal/web        151.474s
```

All packages pass or have no tests; process exit0. Full log:
`.tmp-ui/goal-20261023/snapshot-full-go-suite.txt`. Focused snapshot and ordinary
rollback/replan/completion tests also pass under `-race` (product13.446s,
taskcoord67.038s); log `snapshot-race.txt`. `go vet ./...`, gofmt, FILE_MAP302
existing unique paths and `git diff --check` pass. The whole CI race rerun is a
separate outstanding observation; do not substitute the focused result for it.

Prepared server `hermetrix-goal-with-snapshot-fence.exe` is51,268,096 bytes,
SHA256 `85B99ECD86AC102BE2DD6C11531C67555A58C4E59447630B38843E36D69CE824`.
It was built, not executed or installed.

`bash scripts/doc-truth.sh check` still exits1 at its ignored-Exec-error negative
check; log `snapshot-doc-truth.txt`. Existing registered anchors remain present.
This is an outstanding durability/error-reporting gate elsewhere in the existing
code and is not waived by the passing Go suites.

## Concurrent Stop handoff

The whole race rerun exposed a second real bug: Stop occasionally returned while
its Node check remained `running`, `cancel_requested:false`. An isolated unchanged
five-run race reproduction failed once too. The cancellation traversal accepted
only `dispatched` effects, so another worker's committed interruption could move
the same effect to `uncertain` before Stop traversed it, hiding its live job.

`TestGoalStopCancelsOwnJobAfterConcurrentInterruption` forces this ordering with
real SQLite and a held Node process. It fails before the fix (0.95s), showing the
live owned job and no cancellation request. Cancellation now includes both
dispatched and uncertain command effects within this Goal's exact owned run.
Final stopped/exhausted/interrupted action cleanup also cancels after dispatch
has settled, covering a job saved after the initial Stop traversal found none.
It uses the existing operation/job identity; it neither starts nor replays work.

`go test -race ./internal/taskcoord -run '^TestGoalStop' -count=5 -v` now passes
all15 cases (package92.256s). This includes the original intermittent test, the
forced handoff regression, durable cancellation acknowledgment, actual terminal
`canceled`/process-group receipt, and the existing root-change Stop case. Log:
`.tmp-ui/goal-20261023/stop-handoff-race.txt`. No wait/assertion was relaxed.

The whole race command preceding that Stop fix still failed (exit1): agent
cumulative10-minute timeout while creating/migrating a new test database,
taskcoord Stop race, and web Chrome profile cleanup access denied after its actual
input assertions passed. No DATA RACE report was observed. Product567.456s and
taskengine317.049s passed; taskcoord462.493s/web509.457s are failed package runs.
Log `snapshot-full-race-suite.txt`. The earlier HTTP fixture fix did not resolve
the separate cumulative migration-time budget. Full race qualification remains
open, including Chrome process/profile lifecycle and migration fixture cost.
The unchanged isolated browser workflow subsequently passes three race runs
(10.86/10.36/9.12s; package36.077s), log `browser-profile-race-probe.txt`.
That does not reproduce or repair the whole-run profile cleanup failure.

Latest prepared-only build including the Stop fix:
`hermetrix-goal-with-snapshot-stop-fence.exe`,51,268,096 bytes, SHA256
`E2203BE68A3C2C8FA785D0E469921F83076511D53A657FD9A56D350009BFFA6B`.
It was not executed or installed. Final `go vet ./...` passes.

The final source including the Stop fix passes the whole normal suite with
required Chrome: `go test ./... -count=1`, exit0, all packages pass or have no
tests. Product119.051s, taskcoord210.465s (includes the actual121-second held
command), taskengine43.679s, web143.474s. Log:
`.tmp-ui/goal-20261023/snapshot-stop-full-go-suite.txt`. The full race failures
above and ignored-SQL gate remain outstanding; the normal result does not waive
them.

## Limits

This binds known writable outputs, including earlier outputs on those explicit
paths. It does not hash every possible dependency, acceptance script, external
service, market history or input read by an arbitrary command. Service guards
serialize operations within one service instance; they are not kernel locks
against an unrelated editor/process or another service instance. External
changes are detected at the documented observations, not prevented forever.
Review/Task/Goal completion are separate durable commits, and the Goal has an
additional snapshot check. A later milestone reproduces and fixes the separate
database completion/claim gap with real SQLite forced-order tests; see
[atomic completion](2026-10-23-goal-atomic-completion.md). Do not describe this as an immutable filesystem or
proof of unspecified requirements.

Real selected models, EA compilation/backtest/history criteria and authenticated
remote acceptance remain unproven. The prior pilot replacement was rejected by
automatic approval review (`blocked by policy`, no detailed reason); this work
does not change or bypass that boundary.
