# Automatic retry and requested-fix audit — 2026-10-01

Branch: `dev`. Changes are uncommitted.

## Automatic continuation

Failed or explicitly unfinished turns schedule another turn after five minutes. Scheduling is persisted in SQLite and owned by the server; closing the chat does not cancel committed work. Restart preserves pending deadlines. Completed answers stop the schedule. The composer shows the next attempt and a pause control.

Continuation uses the saved goal, notebook and bounded context search. Existing tool receipts must be checked before repeating effects. Session leases, an expected source event and idempotency keys prevent stale or overlapping retry requests. Uncommitted transport failures retain the original draft and attachments; the client retries while the app is open after verifying no answer was committed.

Approval mode is stored per session. Auto approve uses the normal audited approval path; Ask all waits for the user. Mode changes and pause are checked again when claiming an automatic approval. Requests requiring a user decision wait for that decision.

## Actual provider test

Used 9arm Gateway / `qwen3.8-27b-fp8`, entered a two-step task through the UI and pressed Enter. The first response explicitly left step two unfinished. No subsequent send or retry click was made.

- First response completed: `2026-10-01T02:16:57.099839Z`.
- Automatic next turn started: `2026-10-01T02:21:57.938941Z`.
- Observed delay: **300.839102 seconds**.
- Second response completed: `2026-10-01T02:22:06.061925Z`, with `AUTO-RETRY-5MIN-OK`.
- Saved schedule: completed, attempts 1, interval 300 seconds.

Evidence: `/private/tmp/hermetrix-auto-retry-real-result.json` and `/private/tmp/hermetrix-auto-retry-five-minute.png`. This verifies real automatic scheduling and a real provider response, not an exhaustive project review.

## Original project build

Reproduced the user's project build through the running desktop command endpoint, in `easiiot_project/backend`.

Two root causes prevented execution: the data directory remained relative, producing an invalid TMPDIR from a different working directory; macOS Seatbelt then denied writes to the job's temporary directory outside the project. Store paths are now absolute and sandbox write access includes only the current job's temp directory.

`go build ./...`: completed, exit 0, 4843 ms, job `job_3ce3a095711c8bed32a6b47fd0fc3d7f`. macOS Seatbelt enforcement and network isolation remain enabled. This does not establish that every test in that project passes.

## Requested-fix checklist

| Request | Implemented behavior / validation |
| --- | --- |
| Enter, focus and attachments | Shared composer; Enter sends, Shift+Enter adds newline; drafts and attachments survive failed transport. Browser and Node regressions. |
| Approve modes | Auto approve / Ask all; server persists selection and checks exact approval claims. Automated tests verify actual write vs waiting. |
| Slash commands and character | Slash commands retained; large command buttons removed; character is a small icon with scroll control. |
| Chat space and sizing | Chat defaults to 720 px, other panes 360 px; compact composer, model/actions collapsed; input remains visible at short/mobile viewports. |
| Header and project clutter | Chat title block removed; context meter by input; workspace project scope implicit. |
| Model context / 75% | Model-qualified capacity determines the window; 75% threshold is for usable input budget; cumulative token usage is labelled separately. |
| Durable context | Original tool receipts are saved before bounded context; notebook keeps current goal; context_search retrieves relevant excerpts. |
| Review and files | Review displays the answer as a report, rendered tables, folded diagnostics; file rows use compact consistent typography. |
| Tool result readability | Friendly action/status labels; raw event IDs are folded into provenance details. |
| Scroll | Submission follows the newest message; streaming follows only while at the bottom, preserving history reading position. |
| Retry | Manual retry plus server automatic continuation every five minutes; no replay after a committed completed answer. |
| Desktop startup | One owner per data root; update the existing idle server instead of launching a competing process. |
| Workspace pane layout | Fixed an empty task pane dereferencing an absent selected task, which prevented adding and saving panes. |

## Final regression results

- `node --test internal/web/ui/*.test.js`: **130 passed, 0 failed**.
- `go test ./internal/agent ./internal/context ./internal/inference ./internal/providers ./internal/store ./internal/tools ./internal/product ./internal/web`: **all eight packages passed**, including Chrome browser tests (web 48.774 seconds).
- `git diff --check`: passed.
- Latest binary built successfully and installed as the existing idle Desktop server at `127.0.0.1:7331`, PID 39214, `/private/tmp/hermetrix-auto-retry-audited`.
- Final logs: `/private/tmp/hermetrix-auto-retry-final-all.log`, `/private/tmp/hermetrix-auto-retry-final-node.log`.

## Verification limits

A full security review of easiiot_project and every external provider/network failure are not verified. Live image inference is not verified for this text model. Retry can recover temporary failures and continue saved work; completion still depends on provider availability, required permissions and correct model decisions.
