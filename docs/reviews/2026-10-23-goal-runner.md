# Goal runner implementation and verification

Overall goal: **active, not completed**. Source implements a bounded coding loop
and an MT5 adapter. Actual model quality, real remote/EA outcomes, large-job
support and release acceptance are open. See the
[delivery plan](../redesign/2026-10-23-goal-execution-plan.md) and
[operator guide](../GOAL-RUNNER.md).

## Evidence

`go test ./internal/taskengine ./internal/taskcoord ./internal/worker -count=1`:

```text
ok hermetrix-harness/internal/taskengine 11.196s
ok hermetrix-harness/internal/taskcoord 26.269s
ok hermetrix-harness/internal/worker 2.809s
```

These tests execute actual SQLite, file writes, Node commands and rollback.
Model transport is simulated. Goal tests cover exclusive claims, DB close/reopen,
retained budgets, owner/stale-scope rejection, plan/result acceptance, two failed
executions followed by replan/completion, reviewer rejection followed by a
correction receiving actual findings, client-free scheduling, provider Stop and
actual managed-process Stop. Interrupted verification retains an uncertain effect.

An audit found existing worker feedback omitted provider post-review findings.
Observed rejection artifacts now feed bounded failure evidence, bound to active
requirements and artifact checksum. Escalated planners receive actual saved
failure output too. Diagnostics cannot change checks or writable paths.

Focused Goal API/CLI tests passed: lost-response goal lookup, missing-goal 404,
stale resume and HTTPS/redirect safety. `go vet ./...`, `go build ./...`, Node
syntax and `git diff --check` passed. Seven Node runtime tests and six MT5 tests
passed. MT5 transport is simulated; its launcher test runs actual Node and checks
the nonzero exit status. No MetaEditor or broker-history run occurred.
Final `go test -race ./internal/taskengine ./internal/taskcoord -run TestGoal -count=1`
passed: taskengine27.336s, taskcoord55.503s, including reviewer feedback and actual
managed-command cancellation.

## Full repository checks remain qualified

Do not report a wholly green repository suite:

- Initial `go test ./...` failed three existing browser tests. Native pointer
  instrumentation hit the previous compositor layout while DOM geometry already
  reflected the new view. Capturing a frame made the same checks pass. Tests now
  wait for two frames after relevant layout/viewport changes; original native
  input and fit assertions remain. All three passed three focused runs. Inspected
  desktop/mobile screenshots: `.tmp-ui/goal-20261023/browser/`. Product UI JS/CSS
  was not changed.
- A later parallel suite failed the existing Team DAG two-second deadline under
  load; focused run passed in 0.31 seconds. Deadline was not relaxed.
- `go test -p 1 ./...` failed existing mobile plan-review reachability; focused
  run passed in 4.46 seconds. A later [browser qualification](2026-10-23-goal-browser-qualification.md)
  reproduces the transient viewport layout, synchronizes frames, adds native
  confirmation/cancel checks and passes the complete uncached `-p 1` Go suite.
- `scripts/doc-truth.sh check` stopped at its tab regex. POSIX blank matching
  now lets it inspect claims, but it still detects older ignored-SQL errors.
  New goal SQL writes handle database errors. Bash syntax passed; ShellCheck was
  unavailable. This is a surfaced gap, not a passed gate.

## Runtime and activation boundary

Isolated pilot: <http://127.0.0.1:18816>, PID `203556`, binary
`.tmp-ui/goal-20261023/hermetrix-goal.exe`, data `pilot-data` in the same directory.
Health returned `{"expected_schema":77,"ok":true,"schema":77,"service":"hermetrix-harness"}`;
missing Goal returned 404; providers/tasks lists were empty.

That pilot **predates final reviewer-feedback and file-creation changes**. First prepared build:
`.tmp-ui/goal-20261023/hermetrix-goal-current.exe`, 51,061,760 bytes, SHA-256
`DA879C93FAFA74520EF42C5ED565AC71C1F65AC508EEDA0F8B8CF8BC3C9BA494`.
CLI: `.tmp-ui/goal-20261023/hermetrix-goal-control.exe`.

An exact owned/idle identity-checked request to stop PID203556 and replace the
pilot with the current binary was rejected BEFORE execution by automatic approval
review: only **blocked by policy**, no detailed class. Pilot remains running;
no alternate route/port/process was used. Do not bypass or retry the rejection
without resolving the execution boundary. Original installed runtimes,
credentials/data and model services were not replaced.

## Remaining outcome acceptance

Select real writer/reviewer profiles, reachable model, Windows host, MT4/MT5,
EA/project, history/date range and thresholds. Qualify an actual model-authored
correction and real compile/backtest/report loop. MT5 currently fits a 100-second
experiment inside inherited 120-second frozen checks. Exact new-file creation
is now qualified in [the follow-up review](2026-10-23-goal-file-creation.md).
Directory/delete contracts, async tester jobs, out-of-sample evaluation and measured large-repo
throughput remain planned. The broad Codex goal stays open.
