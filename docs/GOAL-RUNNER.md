# Durable coding goals

Status: implemented in source, with automated process/SQLite/API verification.
Live model quality, the installed desktop release and real MT5 acceptance remain
unverified. This is the bounded coding path; it does not automate every browser,
native desktop or MCP workflow.

## Run a goal

Build/start a source server with a separate data directory for qualification:

```powershell
go run ./cmd/hermetrix serve --data ./.hermetrix-goal-pilot --listen 127.0.0.1:7331
```

Use an existing owned Project directory with executable acceptance tests. Exact
writable source leaves may be absent if their parent directories already exist.
Configure two enabled provider profiles, one writer/planner and one reviewer.
Their exact request-semantic revisions are frozen into the goal. They can use the
same model/endpoint; that proves a separate review pass, not model diversity.

Create an operator-owned JSON spec (replace project/provider IDs and file paths):

```json
{
  "task": {
    "project_id": "PROJECT_ID",
    "title": "Correct addition",
    "objective": "Addition passes positive and negative input cases",
    "original_request": "Fix sum.js and prove the unchanged acceptance test passes",
    "actor": "owner",
    "criteria": [{"id": "AC-1", "description": "node test.js passes the stated cases"}],
    "constraints": ["Preserve test.js and the public function signature"],
    "contract": {
      "writable_paths": ["sum.js"],
      "verification": "Run the existing acceptance command and review its actual output",
      "require_plan_review": true,
      "require_user_acceptance": true
    }
  },
  "runner": {
    "provider_id": "WRITER_ID",
    "reviewer_provider_id": "REVIEWER_ID",
    "apply_changes": true,
    "max_actions": 100,
    "max_attempts": 8,
    "max_plans": 3,
    "verification_timeout_seconds": 120,
    "max_seconds": 3600
  }
}
```

```powershell
go run ./cmd/hermetrix-goal -spec goal.local.json
go run ./cmd/hermetrix-goal -goal GOAL_ID -action watch
go run ./cmd/hermetrix-goal -goal GOAL_ID -action stop
go run ./cmd/hermetrix-goal -goal GOAL_ID -action resume -revision CURRENT_TASK_REVISION
```

`-spec` creates the Task, prints its ID, then starts its goal exactly once. If a
response is lost, inspect the task list before resubmitting. Use the API below to
start a goal for a Task that already exists. `watch` returns a nonzero exit status
for waiting/stopped/exhausted goals, so shell automation cannot mistake them for
success. Closing the client leaves server-owned work running; use Stop to cancel.

Plan and result acceptance remain ordinary exact-hash Task actions in Plans/API.
After addressing a waiting gate, resume against the current Task revision.
`apply_changes: false` permits proposals but waits for explicit review/apply.
`true` requires a nonempty exact `writable_paths` allowlist; it records scoped
owner consent rather than pretending that the model proposal received a human
review. Post-test provider review is still required.

Remote Task egress keeps the existing `local_only` default. If using a remote
provider, the Task must explicitly carry its existing `remote_allowed` egress
approval. API credentials never belong in this spec.

## Server API

| Operation | Method/path |
|---|---|
| Start for existing Task | `POST /api/tasks/{id}/goals` with runner fields, actor and expected_task_revision |
| Recover a lost creation response | `GET /api/tasks/{id}/goals` (latest 20 goals) |
| State and 50 latest action receipts | `GET /api/goals/{id}` |
| Stop dispatch and cancel owned active work | `POST /api/goals/{id}/stop` |
| Resume same consent/budget | `POST /api/goals/{id}/resume` with expected_task_revision |
| Current evidence gate | Existing `GET /api/tasks/{id}/completion` |

The server owns two workers, uses database compare-and-set claims, and serializes
each goal's actions. Inference retains its existing shared scheduling/context
limits. All actions are charged **before** dispatch. Default limits are 100
actions, 8 attempts, 3 plans and 3,600 elapsed seconds; maxima are 10,000/128/32/
86,400. The deadline includes waiting time. Pause/resume never resets counters.

Verification dispatches a managed job and returns its operation/job IDs. The
worker is released while the process runs; later observations reuse that job.
Polling does not consume actions or attempts. At most two durable verification
commands run per database; additional goals wait for capacity. Terminal-result
collection is an action and remains within the goal budget. Status/watch includes
the current verification checkpoint; use its job ID with existing job/log APIs.
Run leases renew during observation. Stop, a changed scope or the total goal
deadline cancels only the goal's own jobs and retains uncertain effects.

The loop is `plan → run → packet-bound attempt → file selection/proposal → scoped
approval → apply → frozen checks → separate review → next step/completion`.
Failed mandatory checks restore the exact preimage before a new attempt. Actual
observed reviewer findings and failure output feed the correction/planner as
bounded, version-bound untrusted diagnostics. Two
confirmed matching failures invoke the existing evidence-bearing planner
escalation, still inside the goal's plan/attempt limits. Transport/configuration
or uncertain effects wait for inspection; an identical external operation is
never retried merely because its response was lost.

## Remote execution and MT5

Run the source harness on the Windows machine that owns the project and MT5.
Connect this CLI using `-server https://YOUR_AUTHENTICATED_HOST` and the
`HERMETRIX_API_TOKEN` environment variable. The client requires HTTPS outside
loopback, uses system certificate validation and refuses redirects, including
credential forwarding. Existing Host/Origin/owner checks still apply.
The private tailnet gateway may be configured with the existing launcher; its
cookie-login route is separate from direct Bearer-authenticated API access.

See [the MT5 adapter](../scripts/mt5/README.md). Add its exact Node command to the
checked plan and make only the EA source files writable. The adapter/config/
acceptance files should remain outside that write allowlist. The same command's
real exit status and report summary feed the ordinary verification/repair loop.
No MT5 target or real broker dataset has been qualified for this change.

## Recovery and current limits

- SQLite schema 78 adds verification checkpoints and poll scheduling to schema77
  goals/actions. They survive reopening the database.
  Startup retains interrupted goal actions and pauses them for inspection.
  Existing task recovery retains uncertain operation IDs and artifacts. Resume
  is explicit; it does not automatically replay a command, model request or input.
- Stop cancels the current model call and exact managed command jobs. It pauses
  only this goal's execution authority. Applied files/effects remain inspectable;
  interruption is not reported as verified rollback or completion.
  Cancellation includes owned jobs whose effects have already become uncertain
  through a concurrent interruption; final action cleanup rechecks after job
  dispatch settles. An uncertain effect retains its operation identity.
- Requirements changing invalidates consent: stop the old run and start a new
  goal. Provider/model/context or canonical project-root changes likewise require
  a new goal. Apply, rollback and verification commands bind their I/O to the
  captured canonical root. Project-root changes serialize with batch mutation
  within one service; model requests run outside that critical section.
- Verification saves the canonical root and hashes/absence of every explicit
  writable path plus current proposal outputs. Dispatch/collection, saved bundle
  binding, review acceptance and the final Goal receipt recheck that snapshot.
  Source/root drift waits for inspection and preserves newer owner bytes. Model
  calls and command lifetimes hold no source locks. Older saved bundles without
  a snapshot need inspection. These service guards do not freeze arbitrary
  external dependencies or prevent future edits by another process. See
  [snapshot evidence](reviews/2026-10-23-goal-snapshot-fence.md).
- Final Goal state and its current database completion gates share one SQLite
  transaction. Failed or superseded evidence waits with a released claim;
  successful receipts bind the transaction's subject hash and Task/requirement
  revisions. Concurrent Stop takes precedence. See
  [atomic completion evidence](reviews/2026-10-23-goal-atomic-completion.md).
- Proposals edit or create up to 32 explicitly writable source files per attempt.
  New files carry an `absent` preimage, distinct from an existing empty file.
  Creation uses an atomic same-directory hard link and refuses an intervening
  file; the filesystem must support hard links. Failed checks move exact created
  bytes to retained `.hermetrix-rollback-*` quarantine files and record evidence
  that the source path is absent. Newer edits cause a conflict, not removal.
  A crash without a quarantine receipt remains uncertain. Keep those retained
  files until their operation receipts have been inspected.
- Existing parent directories are required. Directory creation, deletion and
  arbitrary repository-wide refactors still need separate mutation contracts.
  Discovery now recognizes `.mq4`, `.mq5` and `.mqh`, retaining secret filters.
- Planner/selector discovery ranks current source paths against requirements,
  step/check terms and writable directories. Exact writable paths stay first.
  Candidate counts and actual serialized input/preset ceilings bound context;
  truncated lists and unavailable hashes are explicit. Planner hashing reads at
  most 8 MiB plus one overflow byte per request, after fitting entries. It performs
  a fresh metadata walk, capped at 100,000 callbacks; very large flat-directory
  enumeration and semantic/dependency search are not qualified. Discovery receipts
  live on planner/selection artifacts. See [repository context evidence](reviews/2026-10-23-goal-repository-context.md).
- Goal checks default to 120 seconds. Explicit `verification_timeout_seconds`
  freezes 1–86,400 seconds per command; the total `max_seconds` deadline still
  applies across planning, checks and waiting. Ordinary interactive commands keep
  their existing 600-second maximum. MT5's adapter accepts up to 86,380 seconds;
  allow at least 20 seconds of harness headroom. Actual MT5 history/optimization
  performance is not qualified. Interrupted checks wait for inspection, including
  after database reopen; they are not automatically launched again.
- No goal-runner UI controls were added. Use the CLI/API and existing Task plan,
  evidence and acceptance views. Desktop/remote input remains separately scoped.
- Completion proves the declared checks and review passed. Requirements must name
  actual user workflows, edge cases and performance targets to establish those
  outcomes. No implementation can certify unspecified “all cases” or an
  unspecified scale target.

## Verification receipts

`go test ./internal/taskengine ./internal/taskcoord ./internal/web ./cmd/hermetrix-goal -run TestGoal -count=1`
passes: real SQLite close/reopen, exclusive concurrent claims, retained budgets,
owner boundary, stale revisions, review/acceptance gates, real Node writes/tests,
two rollbacks followed by evidence-bound replan and successful completion, Stop
cancellation of a provider and actual managed command, observed reviewer finding
correction, API lifecycle, HTTPS/redirect handling. The final focused goal tests
also pass with `-race`.

New-file and root-binding qualification is recorded in
[the file-creation review](reviews/2026-10-23-goal-file-creation.md). It includes
real Node failures, two verified returns to absence, replanning, successful code,
concurrent writers, injected receipt failure and a changed root at verification.

Async check qualification is recorded in
[the long-check review](reviews/2026-10-23-goal-async-verification.md), including
real process observation, capacity bounds, exact cancellation, saved job recovery
and preservation of goal budgets while waiting.

`node --test scripts/mt5/runner.test.mjs` passes six tests. MT5 process transport is
simulated in these tests; they do not certify a real MetaEditor or backtest run.
