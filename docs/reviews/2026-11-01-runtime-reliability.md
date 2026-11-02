# Runtime recovery and usable project coding

Subsequent readiness assessment: [the actual running-instance audit](2026-11-01-production-readiness.md) found that a two-file repair still failed compilation and automatic retries could continue stale work. The mechanism and small-file checks below remain valid; they are not unattended production qualification.

Status: outcome verified on the actual local Bonsai runtime and installed architecture pilot. Updated **2026-11-01 09:29:32 Bangkok**; final verification **09:32:08**. The user explicitly requested repair after Hermetrix completed the PO migration but could not finish the remaining client changes.

This release updates the pilot at <http://127.0.0.1:18816/> using its existing data directory, writer/reviewer profiles and schema82. The separate original installed application and its historical Goals were not replaced. Existing architecture/voice/Project Brain/learning acceptance remains separate from this repair.

## Evidence and causes

The original Play Bua session had actual migration write receipts, then subsequent requests failed with `cancelled_runtime_idle_unconfirmed`. A recovery attempt succeeded once; a later request remained quarantined. Its other coding turn consumed nine model steps and170,430 tokens, ending with prose followed by raw XML tool syntax rather than implementation. Those original transcripts remain preserved privately; they were not rewritten as successful runs.

Two regressions reproduced against the unchanged code:

```text
TestNextRequestRecoversCancelledLocalStreamOnlyAfterIdle:
  chats=1 probes=0 err=inference resource is quarantined
TestProseFollowedByRawToolMarkupIsNotACompletedAnswer:
  unexecuted tool syntax was committed as an answer
```

The scheduler correctly quarantined a cancelled local generation, because closing an HTTP stream does not prove the underlying model stopped. The ordinary next-request path never checked the now-idle runtime, leaving it blocked until manual intervention. The answer guard only recognized XML at the beginning of the reply; introductory prose bypassed it.

The old workspace policy also told every project task to begin by listing files. The captured narrow implementation turn repeatedly read unchanged source and recovered spilled evidence before reaching its budget. This is evidence of ineffective execution in that run, not a benchmark proving general model performance.

The full regression exposed an independent launcher defect. Windows reported the actual bound endpoints on8088/18816 with `State=0`, a PID and remote port0. The launcher filtered only `State=Listen`, so it missed both an existing Hermetrix instance and foreign listeners. Five existing ownership/startup tests failed. The same tests passed after identifying bound endpoints by local address/port, remote port0 and a nonzero PID, followed by the original executable/data/reviewer and HTTP-health checks.

## Changes

- Before admitting a new request to a quarantined local provider, probe its actual `/slots` state with a bounded deadline. All slots must be present, valid and idle. Busy, missing, malformed, duplicate, ambiguous or unreachable evidence keeps inference paused. Normal requests do not incur an idle probe.
- The probe uses the profile's credential and refuses redirects. It requires one complete bounded JSON value. Cancellation itself never releases quarantine and the cancelled request is never replayed.
- A quarantine epoch binds idle evidence to its particular cancellation. A stale concurrent probe cannot release a newer cancellation. Other runtimes retain their admission capacity.
- Raw tool-call syntax following prose is treated as unfinished work rather than a completed answer. Fenced XML examples and inline explanations remain valid text. No XML is converted into an executable action.
- New sessions freeze policyv5: start at supplied paths, search file contents appropriately, make the scoped change once sufficient evidence exists and verify it. A receipt-derived reminder after four observation calls helps an authorized implementation task move forward. It grants no additional authority and does not force read-only tasks to write.
- Existing policyv4 sessions remain supported with their original access policy and fragment version. Session history, approval checks, optimistic file hashes, model selection and all budgets remain intact.
- The architecture-pilot and Easiiot launchers use bound endpoint ownership without relying on the unreliable State enum. Foreign processes, wrong data roots and reviewer mismatches remain rejected.

## Verification

| Check | Result |
|---|---|
| Focused before reproduction | Both captured defects failed before changes and passed afterward |
| Full `go test ./... -json -count=1` | **1,640 passed, 0 failed, 25 skipped**,44 packages |
| Actual model checks, run separately | Both native checks passed; combined coverage is1,642 passes and23 unverified optional checks |
| `go vet ./...` and candidate build | Passed; final compatibility/budget/markup checks passed after retaining legacy policy fragment versions |
| `go test -race` on recovery/admission cases | Passed for inference and providers |
| Windows launcher regression | All scripts-package checks passed, including real foreign-port and identity preservation |
| Actual launcher reuse | Same updated PID retained on `-CheckOnly` |
| Installed HTTP request | Actual Play Bua file read once, correct seven-decimal answer, no write/command receipts |
| Edge UI | Saved answer and actual read receipt visible; no console errors observed |
| Data | SQLite integrityok; existing two sessions and two projects preserved; provider configuration unchanged |

The remaining skips require separately configured Pi/Comfy/embeddings, installed fixtures, optional network/browser cases, voice hardware/services or explicit live experiment opt-ins. The two new Bonsai opt-in tests were actually run and passed separately. This release does not claim those other integrations or the broader architecture objective are qualified.

### Native cancellation and recovery

`TestLiveBonsaiCancelThenNewRequest` cancelled a real streaming response on the already-running Bonsai model. The scheduler retained quarantine. A distinct next request then completed with `HERMETRIX_RECOVERED` after verified idle, without a manual reset. Result: **2.90s**.

### Native code repair and command completion

`TestLiveBonsaiRepairsRealPurchaseModuleAndRunsCheck` copied the real client Go module into an isolated temporary project. The host deliberately seeded the old two-decimal validator and first reproduced a failing immutable acceptance test. This was a seeded regression in a real module, not an unseeded production discovery.

Hermetrix/Bonsai then read the source, changed only `internal/modules/purchase/precision.go` using its actual guarded write tool, and ran:

```text
go test ./internal/modules/purchase -run TestNativeSevenDecimalContract -count=1
ok  github.com/playbua/factory-management/backend/internal/modules/purchase  1.185s
```

The actual product command runner recorded completed/exit0. Acceptance covered seven-decimal valid prices, trailing zeroes, negative/overprecision/range rejection and112-baht rounding. Before/after hashes proved every other file, including the acceptance test, was unchanged. Codex did not apply or repair the model's output. The session released its lease and returned a complete Thai summary. Total native test duration: **72.42s**; seven tool receipts;43,335 tokens. The original limits remained12 model steps /24 tools /600seconds /196,608 tokens, with bounded1024 reasoning tokens per request. No general speed/quality improvement is claimed from this one case.

### Installed pilot

The candidate replaced only the pilot executable after confirming no active turns or background jobs and taking an online SQLite backup plus complete pilot-data/binary copies. PID59580 became56184. The ordinary launcher started the candidate and subsequently reused that exact process. Schema remained82.

A fresh installed policyv5 session read `backend/internal/modules/purchase/precision.go` in the actual Play Bua project and answered correctly through the HTTP streaming API. Edge reopened that persisted answer and its actual tool receipt:

![Updated Hermetrix reading the real project](../validation/2026-11-01-runtime-reliability/installed-ui.png)

The existing session rows stayed byte-for-byte equivalent. Expected differences were the initial project's startup/open timestamps and the writer's observed token/reasoning calibration after the smoke request. Roots, models, credentials, permissions and session contracts were preserved. No Play Bua business data or deployed application source was changed by this repair.

## Installed artifact and rollback

Source manifest SHA256: `17855a7898d3430c295d92816f84e009e691bbf5ccc874f423f39b9e98c30d93` across921 source/test/embedded-asset files. Installed executable SHA256: `4bff3e765b59c6a22b9eac41d64f7ce8ee41142444bdafcdbfca87238f7ece66`. Base commit: `f3e540f`. Existing dirty work was retained; no commit or push was performed.

Private complete receipts, source manifest, original binary, consistent database backup and full pilot-data copy: `C:/Users/ZP2E0/.codex/reports/hermetrix-reliability-20261101`. Keep those private; they contain session history. The sanitized [verification receipt](../validation/2026-11-01-runtime-reliability/summary.json) records the current installed state. The installer can restore the prior binary without resetting data if startup fails; no rollback or database downgrade was needed.

Normal reopen command:

```powershell
powershell -NoProfile -File scripts/start-architecture-pilot.ps1 `
  -ConfigPath .tmp-ui/architecture-20261029/pilot.json -Open
```

Start a new project session to receive policyv5. Old policyv4 sessions keep their frozen policy; automatic verified-idle recovery also applies to them. Approval-required effects still use the existing approval flow. Unresolved side effects are not blindly retried.
