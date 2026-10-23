# Goal completion and the database commit

Date: 2026-10-23. Broad goal remains active. This qualifies source behavior with
real SQLite and Node processes; installed runtime, live model, remote host and EA
acceptance remain unproven.

## Before evidence

`FinishGoalAction` evaluated Task completion before opening its transaction.
Appending a current failed validation does not increment the Task revision.
Consequently, a later failed acceptance check could arrive between the evaluation
and commit without invalidating the Goal's precomputed success decision.

`goal_commit_test.go` delegates all operations to real SQLite and pauses at the
driver's transaction boundary. A second connection uses the public validation
service to commit a failure before the first connection begins its final
transaction. The original implementation produced:

```text
TestGoalCompletionRechecksEvidenceInsideItsCommit
later failed evidence closed Goal: state=completed claim="" err=<nil>
FAIL (0.23s)
```

A second case exposes an earlier failure path: evidence already failing when
`FinishGoalAction` starts caused an error but retained the dispatched claim:

```text
.../already_failed
commit boundary state=active claim="goalaction_..."
err=goal completion gate criterion:AC-1 is not passing
FAIL (0.24s)
```

The known working sibling is `CompleteTask`: it checks Task revision and current
completion gates inside the same SQLite transaction as its state update.

## Source change and acceptance

Goal finalization now reads current Task revision, requirement revision and Goal
Stop state inside its transaction. Completion gates are evaluated there once,
before committing the Goal and action receipt. A current failure, whether already
present or arriving before that transaction, commits `waiting`, clears the claim,
saves `requires_inspection` and returns a stale-authority error. Its receipt does
not retain an old completion hash. No successful outcome is inferred from an
error or an expired claim.

A completed Goal's receipt binds the actual transaction's completion subject
hash, Task revision and requirement revision. A new passing observation gets its
new actual hash. Stop wins with `stopped` and the owner's Stop reason. A changed
requirement cannot be silently adopted by resuming the original consent.

Permanent forced-order tests cover all four completion cases plus a public
requirement revision during ordinary action finalization. Existing scope tests
now also require invalid completion to release its claim and reject later use of
the released token. The barrier is test-only; production has no test hook, fake
database, global callback or dependency addition.

Final focused race qualification passes:

```text
go test -race ./internal/taskengine ./internal/taskcoord \
  -run '^TestGoal(Completion|Commit|Scope|Survives|Claims|Rejects|Writable|RunnerWrites|StopCancels|FinishRechecks)' -count=1 -v
ok internal/taskengine 64.833s
ok internal/taskcoord  65.618s
```

Log `.tmp-ui/goal-20261023/goal-commit-final-race.txt`. Coordinator coverage uses
real files, Node checks, rollback/replan/completion, Stop handoff, snapshot drift
and separately completed Task/final Goal gates. Model transport alone is simulated.
The final whole normal command with required Chrome exits1:
`goal-commit-full-go-suite.txt`. Every package except web passes (agent88.594s,
product106.664s, taskcoord203.845s, taskengine37.952s). Web165.919s fails
`TestResponsiveWorkspaceFlowsInARealBrowser/1280x720` at `choice-preserved`:
the expected reopened model dialog/second-provider selection did not appear.
The diagnostic's `view:chat` is the expected starting surface at this assertion,
not evidence of a navigation regression. Original generic diagnostics did not
record the setup dialog state/value, so the cause remains unproven.

The unchanged isolated desktop case passes3 runs7.528s. Test-only event recording
and failure diagnostics now include native pointer/click/change/close events,
draft/select values and setup-dialog open/focus state; at most40 events are kept.
Twenty instrumented focused desktop runs pass39.059s, which does not establish a
repair. No product UI JS/CSS, timeout or assertion changed. The full web diagnostic
run with required Chrome also passes97.109s, including all three responsive
viewports; `goal-commit-web-diagnostic.txt`. Its existing external WAV/Whisper
acceptance tests skip because those fixtures/services are absent. This separate
pass neither erases the earlier whole-command failure nor proves its cause was
repaired. Logs `model-choice-reproduction.txt`,
`model-choice-events-reproduction.txt`; failure-only images are enabled under
`model-choice-failures/`. Do not relax the original assertion or infer a cause
from isolated passes.

Prepared-only server `hermetrix-goal-with-atomic-completion.exe`:51,273,216 bytes,
SHA256 `DFA05E8BD9A6CF73E032A1DC0A77EE6F2179288BA8AB588321DE9DBCD4B28210`.
`go vet ./...` and build pass; the executable was not run or installed.

## Separate full-race investigation

Prior whole race failures are retained in `snapshot-full-race-suite.txt`: agent's
cumulative package timeout during fixture migration and Windows Chrome profile
cleanup are unresolved. The proven Stop bug from that run was fixed previously.
Do not substitute the focused pass above for a whole-repository race pass.

A CPU profile of the same test named in the cumulative timeout,
`TestLoopDetectorIgnoresCallsWithDifferentArguments`, now gives stronger evidence:
6.68s run,6.48s sampled CPU; `store.Open`3.37s/52.01%, `store.migrate`3.16s/48.77%,
`RunTurn`1.05s/16.20%. This is one instrumented fixture, not a universal latency
claim or proof of the whole timeout's complete cause. Profile/raw output and
pprof tables are under `.tmp-ui/goal-20261023/agent-bootstrap*`. Investigate the
repeated fixture bootstrap before changing time limits or product code.

The source-output guards from the previous milestone remain complementary:
SQLite transactions bind database evidence, while coordinator snapshot guards
recheck the known writable outputs/root. Neither freezes every external input or
prevents future owner edits. Real outcomes and every broader gate remain required
before closing the overall objective.
