# Durable asynchronous goal verification

Date: 2026-10-23. Broad goal remains active; real model/EA/remote acceptance is
still pending. This slice removes worker occupancy and the fixed 120-second goal
check limit without treating an uncertain operation as permission to run again.

## Grounded cause and implementation

`VerifyFrozen` waited for each managed command inside `AdvanceGoal`. The product
already persisted unique operation IDs and jobs, but the controller held a worker
and claim until the process ended. With two workers, two long tester commands
prevented other goals from progressing. Frozen commands always used 120 seconds.

Schema78 adds per-proposal verification checkpoints and a goal poll schedule.
Progress stores frozen checks/root/run/attempt, the current operation/job IDs,
completed job IDs and small validation records. Output stays in existing bounded
job logs/artifacts; polling does not copy it into checkpoint JSON.

The controller saves intent, dispatches a command once, saves the job binding and
releases its claim/worker. It later observes the same job and collects its actual
terminal result. Waiting/polling consumes no actions or attempts. Terminal-result
collection is charged. A bounded SQL publication admits at most two durable
verification jobs per database; additional goals wait for capacity. Model work
retains its existing scheduler. This is a concurrency bound, not a throughput
benchmark or a claim of fairness under every load.

`verification_timeout_seconds` freezes 1–86,400 seconds per goal check (default 120).
The cumulative goal deadline still includes planning, queued time and checks.
Interactive commands retain their 600-second cap. MT5 adapter v2 accepts up to
86,380 seconds with at least 20 seconds of harness headroom; its digest changes
with adapter bytes. Real MT5 transport/history remains unqualified.

Run leases renew while observing. Stop, deadline and invalidated scope cancel only
commands bound to the goal's own attempt; effects remain uncertain on interruption.
Stop tolerates a confirmed terminal result racing cancellation. A saved dispatch
with a lost job-checkpoint acknowledgement adopts the uniquely bound existing job
after checking command/root/actor/timeout. A dispatch with no confirmed job is
retained for inspection, never blindly launched again. Startup pauses active async
verification even when its goal had already released the action claim. Manual
synchronous Verify cannot duplicate a proposal's saved goal verification.

Status/watch exposes the verification checkpoint alongside goal/action receipts.
Use existing jobs/artifact APIs to inspect process results and terminal logs.
The source API/CLI carries the new owner-frozen timeout; no UI was added.

## Observable acceptance and test receipts

Actual Node processes, files and SQLite verify:

- Held command releases its goal claim; six concurrent polls preserve the same
  job and budgets. Another task can plan while it remains live. Releasing the
  command leads through checks, review and completion with exactly one start.
- Lost job-checkpoint acknowledgement adopts the existing operation/job. It does
  not create another command. Synchronous verification refuses duplication.
- Two occupied slots queue a third check without additional actions. Releasing a
  slot dispatches the waiting check; Stop cancels its job and preserves another
  action's running process. Eight concurrent direct publications admit exactly 2.
- Deadline and project-root change cancel the actual owned process tree, preserve
  an uncertain effect and do not claim completion.
- Close/reopen preserves the exact job and pauses verification; even an explicit
  goal resume cannot replay its interrupted command. This test closes the owned
  service cleanly; an installed-process crash remains separate live acceptance.
- An actual schema77 temporary database upgrades with the existing goal's action,
  plan budgets and deadline preserved and zero foreign-key violations. Existing
  preview-layout migration tests pass as well.

The long-run test deliberately waits in real time, not via a mocked clock:

```text
go test ./internal/taskcoord -run TestGoalAsyncVerificationOutlivesLegacyDeadline -count=1 -v
same Node job job_a8977daa6672e95fdb35e59a4ad5dc1e observed running for over 120 seconds with 7 unchanged actions
--- PASS: TestGoalAsyncVerificationOutlivesLegacyDeadline (122.17s)
ok hermetrix-harness/internal/taskcoord 126.142s
```

It freezes a 150-second check and 180-second total goal budget, observes the actual
same live job for 121 seconds, releases it, then proves review/completion and one
process start. A final cancellation-race correction does not change dispatch or
deadline behavior; focused cancellation qualification is recorded below.

Uncached full affected suites (before the added real-time test) pass:

```text
go test -p 1 ./internal/store ./internal/taskengine ./internal/taskcoord ./internal/worker ./cmd/hermetrix-goal -count=1
ok hermetrix-harness/internal/store              4.190s
ok hermetrix-harness/internal/taskengine        12.422s
ok hermetrix-harness/internal/taskcoord         39.710s
ok hermetrix-harness/internal/worker             5.217s
ok hermetrix-harness/cmd/hermetrix-goal           4.076s
go test ./internal/product -count=1
ok hermetrix-harness/internal/product          86.144s
```

Goal API/CLI focused tests pass9.581s/1.098s. MT5 Node tests pass6/6; actual Node
launch is tested, MetaEditor/tester transport is simulated. `go vet ./...` and
`go build ./...` pass. Existing whole-repository browser timing/ignored-SQL gates
remain qualified in [the first review](2026-10-23-goal-runner.md); no whole-repo
green claim is made.

Focused race qualification passes:

```text
go test -race -p 1 ./internal/product ./internal/taskengine ./internal/taskcoord -run 'TestDurableVerification|TestGoalAsyncVerification(Polls|Adopts|DeadlineAnd|Reopen|Waits)|TestGoalSchema77|TestGoalStopTerminates|TestGoalClaims' -count=1
ok hermetrix-harness/internal/product      9.347s
ok hermetrix-harness/internal/taskengine   9.127s
ok hermetrix-harness/internal/taskcoord   56.982s
```

After the terminal-result/cancellation race correction, focused ordinary checks
pass in 11.982s; final race checks for async deadline/root/capacity cancellation
and actual managed-command Stop pass in 31.736s. Final vet/build, changed-file
gofmt, file-map integrity and diff whitespace checks pass.

Latest prepared server: `.tmp-ui/goal-20261023/hermetrix-goal-with-async.exe`,
51,198,976 bytes, SHA256
`4FFCDD2E45856AB74E1BDC69B3276F335ADBE32451AF79E9EDA67D37CF03BDC2`.
Matching prepared CLI: `hermetrix-goal-control-with-async.exe`, 16,576,000 bytes.
Neither executable was activated. Earlier prepared binaries remain beside them.

## Diagnosis receipts

1. The old fixture advanced60 immediate synchronous calls. After dispatch became
   asynchronous it ended with an active job before its output arrived. The fixture
   now observes authoritative state with a wall-clock deadline and small polling
   interval; actual completion/source assertions remain.
2. The first schema78 migration assumed a missing column. Existing preview tests
   deliberately lower `user_version` while retaining newer columns, reproducing
   duplicate-column failures. Migration now checks actual column shape before
   applying the additive ALTER. All affected migration tests pass unchanged.

## Runtime boundary and remaining acceptance

The exact prior pilot PID203556 still runs
`.tmp-ui/goal-20261023/hermetrix-goal.exe` at127.0.0.1:18816. Read-only health again
reports expected_schema77/schema77/oktrue. It predates reviewer feedback, file
creation and async verification. New source needs schema78 and is prepared only.
The earlier identity-checked replacement was rejected before execution by
automatic approval review (`blocked by policy`, no detailed reason). No alternate
activation route/port/process was attempted.

Actual selected writer/reviewer, terminal host, EA, dates/history, thresholds and
authenticated remote workflow remain unverified. Directory/delete contracts,
repository indexing and measured scale/latency targets remain open. These source
and process tests do not close the broad Codex goal.
