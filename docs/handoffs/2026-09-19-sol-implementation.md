# Implementation handoff — Local multimodal and privacy

Date: 2026-09-19
State: O0-O5 software paths, O7 project sharing, and O8 encrypted workspace migration/full database-CAS recovery are implemented. O5 native vision, O6 native audio/video, browser acceptance, and native release gates remain blocked or incomplete as recorded below.

## Start here

Read [implementation specification](../specs/2026-09-19-local-multimodal-privacy-implementation.md), [remediation](../specs/2026-09-18-correctness-performance-remediation.md) and relevant [master specification](../specs/2026-09-19-hermetrix-master-project-specification.md) sections. The new overlay supersedes the old Downloads proposal for its scope. No need to rediscover decisions already fixed in the specification.

Working directory: D:/Projects/harness/hermetrix-harness.
Implementation base: 08fcac4. Working tree schema is now v46. Verify actual current state before changing files.
Existing uncommitted work includes README edits and docs/reviews, docs/specs. Preserve it.

## Prompt to give GPT-5.6 Sol

```text
Implement the approved scope in docs/specs/2026-09-19-local-multimodal-privacy-implementation.md.
Read this handoff and repository instructions first. Work in the existing architecture and preserve all existing uncommitted changes.

Begin with O0: record HEAD, schema, toolchain, working-tree changes and baseline tests, then inspect the exact production paths for the remediation prerequisites. Do not assume Target requirements are implemented because they appear in the master specification.

Proceed in dependency order through O0–O5 for the first release slice; continue O6–O8 as separate milestones when prerequisites are met. Use small reviewable changes. Do not implement all migrations in one unreviewable patch or bump schema to a reserved number without the preceding migrations.

Keep a progress table in this handoff: work package, requirement IDs, implementation status, test commands/results, outcome evidence, blockers and next action. Update it at each milestone so another task can resume.

Preserve live lease validation, frozen contracts, exact tool/proposal approvals, no-replay recovery, private defaults and the independent model/endpoint reviewer gate. Do not downgrade these to make the single-model demo pass. If no permitted reviewer is configured, surface reviewer_required and retain awaiting_post_review.

Use existing provider/model credentials only within the user's existing authorization. Do not reveal them. Do not silently download models, call paid providers, publish/share data or weaken local-only egress. When native runtime/hardware/dependencies are absent, finish independent code and deterministic tests, then record the exact pending real-world gate.

Run focused tests and appropriate integration/build/vet/UI checks after each package. Run race/native/browser qualification on supported environments before claiming the relevant gate. Never convert mandatory tests to skips or claim real hardware qualification from mocks.

Report concrete files changed, passed/failed/not-run checks and next unfinished package. Do not stop at a plan or mark the complete overlay done when only its first slice passes.
```

## Existing evidence and caveats

- Earlier Windows review found six failing packages; it is historical evidence, not the current baseline. Rerun relevant checks.
- PATH previously resolved Go windows/386; C:/Program Files/Go/bin/go.exe was an available amd64 alternative. Recheck paths/versions.
- doc-truth.sh previously failed under CRLF/GNU grep/toolchain conditions; inspect/fix its cause only if needed for the current gate. Its anchor checks do not prove semantic spec accuracy.
- GPU speed/VRAM and 98,304-token allocation in the original proposal were user-reported, not reproduced in this task.
- Provider Message currently has string Content and ChatRequest lacks normalized reasoning controls. Multipart/preset integration needs changes across adapters, not only new SQLite fields.
- Existing coordinator rejects same-model/same-endpoint post-review. Keep that restriction.
- Keep legacy Session Contract history honest; a backfill may label legacy behavior but cannot invent a preset that was never used.

## Progress ledger

| Package | State | Evidence / next action |
|---|---|---|
| O0 baseline/remediation | Implemented for production boundaries; native cross-OS/race qualification remains | R01-R07 core boundaries, direct proposal artifacts, atomic file apply/rollback, Windows process/vault/capability paths, context v39 and inference ledger v40 |
| O1 privacy foundation | Implemented | schema v41 local principal, owner filtering, private/deny defaults, revision-checked sharing audit, local-only egress gates; expand adversarial graph tests with O7 |
| O2 runtime/scheduler | Implemented in code; native hardware evidence remains | runtime fingerprints preserve unknown evidence honestly; provider aliases can share a resource group; local cap=1, queue cap=64, bounded aging, cancel quarantine, durable reservation/reconciliation |
| O3 presets/budgets | Implemented for task planner/worker/reviewer paths | immutable seeded presets, central wire override, context/output validation, session snapshots, attempt/request bindings and effective digest |
| O4 planner/worker | Implemented in code | revisioned deterministic classifier; existing-plan coverage checks; bounded one-step plans; normalized confirmed-failure evidence; planner escalation only after two consecutive same-signature failures; two-escalation cap; transport/configuration block; uncertain effects reconcile first; `reviewer_required` is visible without invalidating live authority |
| O5 images | Implemented in code; native gate pending | schema v43 multipart/derivations plus v44 durable media jobs; decoded JPEG/PNG/WebP MIME/dimension/animation bounds; owner-scoped upload, idempotency conflict, cancellation/recovery, atomic derived-evidence/job commit; provider wire translations and qualified-vision fail-closed gate. No live vision model was listening during verification |
| O6 audio/video | Fail-closed foundation only; native blocker | durable schema accepts the defined processor kinds, but API returns typed `unsupported` until a contained FFmpeg/FFprobe/Whisper toolchain is installed and qualified. This host had none of those executables, so decode/STT/video acceptance was not claimed |
| O7 project sharing | Implemented in code | v45 owner-scoped preview/export/import; explicit files, artifacts, Task requirement projections, Skill packages and Memory projections; VCS/secret exclusion; transitive private derivation refusal and dependency-edge preservation; 30-minute digest-bound previews; source/policy TOCTOU checks; idempotency conflict; documented 512 MiB compressed/2 GiB expanded/10,000-entry/256 MiB-entry streaming validation; traversal, symlink, duplicate/case collision, undeclared entry and compression-ratio rejection. Imports receive new IDs/private defaults, Tasks become drafts and Skills become candidates without live authority |
| O8 workspace migration/recovery | Implemented in code | v46 job/map metadata; pinned `filippo.io/age` v1.3.2 age-v1 scrypt envelope; passphrase never persisted; selected private projects/files/artifacts/memories restore under explicit root maps; new IDs/private defaults; no credentials/sessions/approvals/attempts/effects/runtime authority. Full recovery creates a consistent SQLite snapshot, packages only referenced CAS without recursively embedding prior recovery packages, verifies hashes/integrity/FKs/schema, restores atomically only to a new root, and keeps the protected credential vault in a separate same-machine capture path. Cross-machine credentials require reenrollment |

## Implemented correctness foundation

The working tree contains the corrected execution and privacy foundation required before multimodal work:

- R01: listener-derived trusted Host validation, browser mutation provenance checks and strict single-document JSON decoding. Production wiring accepts repeatable/comma-separated `--trusted-host` values.
- R03: schema v37 persists run and lease generation on effect intents. Planning, dispatch and attempt completion require a live run authority and validate it transactionally. Observation and reconciliation remain available after lease expiry. The full barrier-driven concurrency matrix in section 8.5 remains to be added.
- R04: chat sampling contracts are validated at turn start, before every model dispatch and after approval. Task-owned planner, selection, proposal and review calls re-read the bound provider revision immediately before dispatch. An approved effect receipt remains final when provider configuration drifts, while continuation stops with `contract_drift`.
- R05 / PRV-001-007: OpenAI-compatible SSE and all provider JSON responses fail closed on empty or incomplete terminal states, multiple JSON documents, unsupported finish states and partial tool calls. Native Anthropic/Gemini stop reasons retain protocol-specific normalization and tests. Partial output is persisted as untrusted `provider_incomplete` evidence and cannot execute tools.
- R02/R14: workbench writes have durable mutation intents, no-replay startup reconciliation, sorted multi-file locks, all-preimage validation and bounded reverse rollback with truthful per-file receipts. Proposals bind rollback and verification artifacts directly; the 1,000-unrelated-artifact review scenario passes.
- R06/R07/R15: child commands use isolated per-job temp/environment state. Windows commands and MCP stdio launch suspended, enter a kill-on-close Job Object and only then resume. Windows credentials use DPAPI CurrentUser format 2 with verified legacy migration and copy-on-write updates. Runtime capabilities are reported by `/api/capabilities`.
- OS-008: stdio MCP configuration is persisted as structured executable/argument JSON, retains argument boundaries, reads legacy command strings and marks them for review.
- Context/storage: schema v39 stores new context payloads in CAS and adds derived checkpoints; the bounded session event pagination endpoint is available. Schema v40 adds the inference usage ledger and lexical-feature table.
- Scheduler: provider service owns the single admission point. Local resources serialize at one request, remote profiles default to four, the process ceiling is eight, priority is stable FIFO within class, waiting cancellation releases the reservation, and reconciliation is durable/idempotent by request ID.
- O1: schema v41 backfills an installation-local principal and private/deny ownership metadata. Projects, sessions, tasks, artifacts, memories, Skills and candidates default private and owner-filter public service queries. Project/session/task references are owner-checked. Session/task egress defaults `local_only`; remote dispatch requires an explicit durable binding. Sharing metadata changes require an expected revision and write an audit receipt in the same transaction.

Changed production areas include `internal/web`, `internal/providers`, `internal/agent`, `internal/store`, `internal/taskengine`, `internal/taskcoord`, `internal/product` and `cmd/hermetrix`.

### Verification record

Toolchain: `C:/Program Files/Go/bin/go.exe` (amd64 Go toolchain) and installed Node.js.

Passed on Windows with `C:/Program Files/Go/bin/go.exe` (amd64):

```text
go test ./... -count=1
go build ./...
go vet ./...
node --test internal/web/ui/runtime.test.js
```

All Go packages passed on the current Windows host. The earlier Windows fixture, MCP timeout, command, PTY, file-URL, vault-mode and browser-profile cleanup failures are fixed. Linux/macOS native jobs, `go test -race`, required browser E2E, a second-Windows-account DPAPI check and real GPU/model qualification remain separate release evidence gates.

Additional 2026-09-19 evidence:

- `go test ./... -count=1`, `go build ./...`, `go vet ./...`, and `node --test internal/web/ui/runtime.test.js` passed after schema v46 and O4/O5/O7/O8 changes.
- A real process smoke test started `hermetrix serve` on loopback, served the SPA with HTTP 200, and returned `/api/health` as `{"expected_schema":46,"ok":true,"schema":46}`.
- The populated v43-to-v46 migration fixture passed with zero `pragma_foreign_key_check` rows.
- Focused restore drills passed for project share and encrypted workspace migration. The workspace test proves wrong-passphrase and one-byte ciphertext tamper rejection and verifies zero imported `tool_approvals`, `task_effect_intents`, `task_step_attempts`, and `agent_sessions`.
- The full-recovery drill passed for SQLite plus CAS creation, API download and verification, tamper rejection, atomic restore to a new data root, artifact-content recovery, and credential-vault exclusion. The vault capture remains an internal separate same-machine operation and is deliberately absent from the HTTP API.
- Project share regression now covers a valid package above the old 1,000-entry limit, streamed per-entry integrity checks, Task-to-draft import, Skill-to-candidate import, private Memory import and zero restored task-run/tool-approval authority. The UI presents included/omitted objects, remaining metadata and expiry before export, and separately names workspace migration, Skill portability and full recovery.
- A real in-app-browser smoke run opened the compiled UI, enabled explicit project sharing, previewed `README.md` and visibly showed one included object, zero omissions, its hash, remaining metadata and expiry. The same run created a full-recovery package through the confirmation dialog and displayed its completed checksum, database byte count and download link. Composer/native multimodal browser acceptance remains tied to the unavailable qualified runtime.
- Native host evidence: NVIDIA GeForce RTX 5070 Ti, driver 610.88, 16,303 MiB total / 13,121 MiB free during inspection; `llama-server` build 11026 commit `b49650adb` exists, but no server/model was listening. `ffmpeg.exe`, `ffprobe.exe`, `whisper-cli.exe`, and `whisper.exe` were absent.
- `go test -race` was not runnable on this host because `CGO_ENABLED=0` and no GCC/Clang/MSVC compiler is installed. This is a named release-evidence blocker, not a passing gate.

### First next actions

1. Install and pin a supported FFmpeg/FFprobe/Whisper toolchain outside the application, then implement/qualify O6 native audio/video processing without weakening the typed `unsupported` fallback.
2. Start a qualified vision model and run the real O5/A14 source-to-cited-answer workflow; add composer/share/task UI acceptance coverage.
3. Run the remaining composer/native multimodal browser acceptance after a qualified runtime is available, plus the encrypted migration upload/apply drill on a disposable destination host.
4. Run race tests on a CGO-capable runner, Linux/macOS native jobs, and second-Windows-account DPAPI qualification before closing the release gates.

## Completion record template

```text
Package / requirement IDs:
HEAD and changed paths:
Mechanism and before/after evidence:
Commands, toolchain, exit codes:
User-visible workflow verified:
Failed / skipped / not run:
Remaining work and first next action:
```
