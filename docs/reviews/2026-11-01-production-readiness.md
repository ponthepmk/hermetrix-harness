# Running-instance readiness audit — 2026-11-01

**Verdict: not ready for unattended production coding.** Basic execution, interruption and boundary controls work, but the installed chat workflow failed a real-module two-file repair after an initial round and one natural automatic continuation. Assessment completed09:54:51 Bangkok. This qualifies the earlier [runtime repair report](2026-11-01-runtime-reliability.md); its small-file/recovery successes remain valid, but do not establish reliable completion of larger tasks.

The checked target was the actual running **local architecture pilot** at `http://127.0.0.1:18816/`, PID56184, schema82, using the existing Bonsai8088 writer. This is not a new Pi/remote production deployment or an upgrade of the separate original installed application. No executable or production source was changed during this audit.

## Passed on the running instance

| Check | Observed result |
|---|---|
| Running binary and source | Exact match to the installed manifest; no drift |
| Health / SQLite | HTTP200, schema82, integrityok |
| Actual HTTP stop/resume | Model delta observed, exact owned turn stopped, lease released, distinct next request read go.mod and answered correctly |
| Request replay | Same idempotency key returned only duplicate; event/tool/user-message counts stayed38/12/1 |
| Stale file write | HTTP409; original canary bytes preserved |
| Foreign-origin mutation | HTTP403; original canary bytes preserved |
| Untrusted Host | HTTP421 / untrusted_host, as specified by the request-boundary code |
| Out-of-project read | File disclosure prevented; HTTP mapping failed as described below |
| Browser | Saved result reopened; no console errors observed |
| Client source | All frozen Play Bua source files still match; no real client source edits |

The previous release's full1640-pass suite and two explicit native checks were supporting evidence, not rerun or counted as new production acceptance in this audit. A live one-file fix is useful evidence of capability; this audit separately tested a larger task on the actual installed HTTP path.

## P1 — Two-file coding acceptance failed

An isolated copy of the real Play Bua backend was registered as a separate project. Two known regressions were deliberately seeded: purchase validation rounded to two decimals, and inclusive-VAT unit-price calculation rounded too early. Existing immutable `TestPurchasePricePrecision` and `TestSevenDecimalPurchaseTotals` reproduced both failures before dispatch.

The request named the two source files, required preserving every other part of the source and all tests, and specified the exact Go test command. No replacement code or corrective coaching was supplied after dispatch. Normal production limits stayed12 model steps /24 tool calls /600seconds /196608 tokens per turn. The ordinary five-minute auto-continuation was allowed once; its actual scheduled execution was observed without forcing the timer or editing the database.

| Round | Tokens | Result |
|---|---:|---|
| Initial | 102,779 |12 read/search/evidence calls; no file changed; partial fallback |
| Natural automatic continuation | 177,800 | Two files written, many further reads; only git status started; partial fallback |
| Total | **280,579** | Requested business outcome not completed |

Final independent checks:

- `precision.go` matched the expected minimal repair.
- `service.go` was rewritten with unrelated changes: Repository→Repo, a different constructor/sequence API, duplicate/mismatched types and changed receipt/cancellation behavior. It invented `internal/shared/sequence`; the existing project uses `internal/platform/sequence`.
- Immutable tests and all files outside the two allowed paths were preserved. **Code inside service.go outside the requested calculation was not preserved.** The file-level optimistic hash protected freshness, not the semantic scope of the replacement.
- There was no completed model-issued Go test command. The one command invocation started git status and was not polled. Its queued invocation receipt is not a completed successful command.
- The observer independently ran the specified Go test. Compilation failed:

```text
FAIL github.com/playbua/factory-management/backend/internal/modules/purchase [setup failed]
internal/modules/purchase/service.go:11:2: no required module provides package
github.com/playbua/factory-management/backend/internal/shared/sequence
```

The missing path was invented by the model; installing a new dependency is not the repair. The source copy intentionally omits Git metadata, so git status also failed outside a Git working tree. That fixture property is disclosed and is not used as the reason for the readiness verdict: the unrelated source rewrite and independent compilation failure already occurred before that command and also invalidate the result in the original module layout.

Both round outcomes correctly retained continuation_needed/partial. The UI did not claim the whole task was complete. Honest incomplete status is working, but it does not satisfy the requested implementation outcome.

## P1 — Stale work continued without an overall retry ceiling

The original Codex-supervised PO session had retry counter **29**, still pending, despite the real client fix having already been completed externally. Most historical attempts were quarantined failures; this is not a claim that29 full generations ran. After recovery, the latest automatic round consumed173494 tokens, inspected unchanged source and ended partial again.

`RunDueAutoRetry` admits pending work every five minutes and increments attempts without an attempt ceiling. Per-turn budgets do not bound cumulative automatic retries for the original request. This was observed in the real persisted queue, not inferred from health status.

The original supervised queue was paused through its normal API. After exactly one natural continuation of the new QA task, that owned queue was also paused. History and all failure artifacts were retained. The stop/resume QA session was paused as cleanup. No unrelated user session or global model service was stopped.

## P2 — Wrong HTTP status for a refused project-path escape

An actual request to `GET /api/projects/{owned-qa-id}/file?path=../outside-canary.txt` returned HTTP500 with path escapes project root. The outside canary was not disclosed and no file was changed.

The project resolver returns a generic error, which falls through writeError to500. This is an API error-classification defect, not evidence of a filesystem escape. A client-error mapping is needed. The stale-write and foreign-origin/Host guards passed separately.

The guard fixture initially expected200 for project creation and403 for an untrusted Host. Source inspection confirmed the existing contracts are201 and421; those fixture expectations were corrected. The real path-error500 remains a recorded failed check and was not reclassified as passing.

## Readiness judgment and next work

Use the current system for bounded, reviewed work with explicit verification. The evidence does not support unattended edits of production source or a general completion-rate claim. Two targeted live scenarios do not establish a statistical success rate or rule out other failure modes.

Before an unattended release:

1. Bound automatic continuation across the whole request and stop/escalate when verified progress is absent. A new turn must not reset the effective lifetime budget indefinitely.
2. Avoid reconstructing large existing files from spilled context. Qualify a targeted edit/proposal workflow with actual preimages, unrelated-code preservation, required behavioral checks and rollback before completion. The durable coordinator has a different verification/rollback path; that path was not qualified by this chat audit.
3. Correct project-path error mapping and keep the refusal regression.
4. Repeat representative multi-file acceptance on the intended installed workflow after those changes, preserving this failed case. Do not tune away tests, enlarge budgets until one run passes, or replace these receipts with a green build.

No new production fix was silently deployed during this verification request. Source changes in the prior repair remain installed; this audit adds readiness findings and controlled-queue cleanup.

![Installed task remained incomplete after the automatic continuation](../validation/2026-11-01-production-readiness/production-incomplete.png)

Sanitized [assessment receipt](../validation/2026-11-01-production-readiness/assessment.json). Full raw session data, copied source, failed Go output, diff, stream and consistent pre-audit backup are private at `C:/Users/ZP2E0/.codex/reports/hermetrix-production-audit-20261101`. The guard-only project was archived; the clearly named two-file QA project remains for review with automatic continuation paused.

Final checks found zero active turns, no running owned jobs, all owned retry queues paused, unchanged running binary/source and unchanged real Play Bua source. The failed generated file exists only in the isolated QA copy.
