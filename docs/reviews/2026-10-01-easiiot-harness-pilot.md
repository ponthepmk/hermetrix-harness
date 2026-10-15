# Easiiot harness outcome pilot — 2026-10-01

State: coding, verification, rollback, recovery, scoped knowledge reuse and isolated live IoT integration verified locally on 2026-10-02. Docker was restored by the user; PostgreSQL integration and real API/worker/consumer/MQTT/RabbitMQ/Redis smoke passed. The bounded Task is technically verified and awaits actual human acceptance. This is not an acceptance claim for every Easiiot feature or physical device.

## Authorized outcome

Improve Hermetrix using the real local Easiiot project, exercise the complete workflow, repair discovered failures, and deliver a usable local result. Preserve existing projects, provider profiles, credentials and unrelated work. Work in a Git worktree and cloned harness data while reproducing failures; integrate verified source fixes into the original checkout. Publication, production deployment and real-device control are outside this local pilot.

## Targets and evidence

- Harness checkout: `D:/Projects/harness/hermetrix-harness`, baseline `020f107`, schema 73.
- Easiiot checkout: `D:/Jobs/G2Tech/Mr.SB/easiiot_project`, baseline `d1f9870`, initially clean.
- Easiiot worktree: `.tmp-ui/easiiot-pilot-project`, branch `codex/easiiot-harness-pilot`.
- Harness pilot data: `.tmp-ui/easiiot-pilot-data-20261001`, cloned through SQLite backup from the stopped preview. Preserve vault files without exposing their contents.
- Initial Node harness suite: 114 passed, 0 failed, 0 skipped.
- Harness: full Go suite passed twice before the final command/Skill-scope fixes; final `go test ./... -p 1 -count=1 -timeout=240s` passed after all source fixes (including Chrome web E2E). Node UI suite: 115/115 passed. `go vet ./...`, build and diff checks passed.
- Easiiot original checkout: harness-managed backend Go suite, seven frontend regression cases and Nuxt production build all exited zero.
- Docker 29.7.2 Linux engine is now healthy after the user opened Docker. Both isolated database/runtime suites exited zero. Historical blocker: inaccessible `sailor-ingest.sock`, with socket removal rejected by automatic review (`blocked by policy`); that removal was not executed or bypassed.
- Installed CUDA Bonsai runs on loopback 8088. Worker and reviewer use distinct profiles of the same local model, not independent model families. Startup now explicitly selects the local background reviewer with `--reviewer-provider-id`.

## Reproduction hypothesis

Original source reproduced three failures in four bounded real Axios/HTTP cases: waiters hung after refresh rejection/transient failure, and a retried resource 403 deleted the renewed session. Expanded seven-case regression also caught a cached rejected refresh promise after a missing token and a lost original 401 response. These were separate causal failures, repaired and verified against the unchanged final regression suite.

## Acceptance matrix

| Gate | Required observable result | State |
| --- | --- | --- |
| Original reproduction | Bounded original-source failures measured with actual Axios and a local HTTP server | pass |
| Real proposal | Local model selected files and produced immutable proposals before source writes | pass |
| Exact review/apply | Exact review/apply receipts; ambiguous preimage refused without writes | pass |
| Regression | Seven cases include 200, refresh 401/403/503, retry 403 and recovery after login/503 | 7/7 pass |
| Real checks | Frozen syntax/concurrency checks plus managed original backend, frontend test and build logs | pass |
| Independent review | Fresh distinct local profile reviewed exact source and saved actual output | pass; same underlying model |
| Failure/rollback | Failed mandatory checks blocked completion; bad proposals rolled back against exact hashes | pass |
| Denial/recovery | Invalid edit diagnostics persisted; new attempt received bounded failure evidence | pass |
| Restart | In-flight review paused safely; completed verification survived restart with unchanged effect IDs/source | pass |
| UX | Actual desktop and 390px Task navigation reaches the human gate; no horizontal overflow at 390px | pass for this journey |
| Completion | Six criteria, two checks and completed step; 16/17 gates | human acceptance pending |
| Learn/reuse | Scoped native Skill, explicit fixture replay, actual local turn body injection; other/personal catalogs exclude it | pass for retrieval; effectiveness unmeasured |
| Usable delivery | Original source integrated with backup; foreground launcher tested from a stopped server | pass for local harness |
| Full isolated IoT runtime | PostgreSQL, MQTT, RabbitMQ, Redis, API, worker and consumer with current source | pass; synthetic device |
| Real frontend/API refresh | Correctly signed expired access JWT, one shared refresh, invalid refresh logout and later-login recovery | pass |

## Repairs in Hermetrix

- Optional exact `GoalContract.writable_paths` is enforced in worker validation and again immediately before apply, and remains visible/editable in the Task UI. Read-only regression files cannot become writable merely because a worker selected them.
- Invalid text edits report file, edit index and match count. Fresh attempts receive bounded same-step diagnostics and failed check logs with artifact provenance and hashes; source/log text remains untrusted data.
- Post-review receives actual saved check output. Review schema/decoded output bounds prevent excessive findings/rationales; truncated responses require a fresh explicit review rather than a fake success.
- Approved technical review no longer returns an erroneous failure while waiting for the human gate. Restart preserves completed `verifying` Tasks, while interrupting unfinished effects safely.
- Task scroll position survives pane remounts. Completed steps do not offer an empty start-step form; pending acceptance has a direct navigation button.
- Command TEMP/TMP paths are absolute even with relative `--data`, so `go -C backend` works. The regression reproduced the exact original failure before the fix and then passed with a real Go command.
- Session Skill catalogs honor exact workspace/project IDs. Runtime compilation and Skill tools also narrow older saved catalogs, retaining their bound versions and preventing cross-project bodies/metadata. Personal chats see only global user Skills; agent scopes are excluded because this runtime has no durable agent identity to bind them to.
- `serve --reviewer-provider-id` selects an exact enabled, credential-ready profile, with no fallback for a missing/disabled explicit choice. Existing profiles and vault credentials were preserved.

## Reproducible local use

Open `http://127.0.0.1:18801/`. Select **easiiot-pilot-project → Plans → Easiiot: concurrent token refresh recovery** for the preserved acceptance ledger. Select **easiiot_project** for new work in the actual checkout. The two project roots are deliberately distinct.

```powershell
# From the Hermetrix checkout; background process, returns when ready.
.\scripts\start-easiiot-harness.ps1 -Open
# Reuse/check a running instance without launching another one.
.\scripts\start-easiiot-harness.ps1 -CheckOnly
# Optional foreground process; Ctrl+C stops it.
.\scripts\start-easiiot-harness.ps1 -Foreground
```

The launcher uses the preserved isolated pilot data and the verified executable. If the executable is missing it builds from source; after stopping the server, `-Rebuild` forces a new build. As of [the 2026-10-03 startup fix](2026-10-03-main-startup.md), Bonsai8088 is required only for inference using that profile, not opening the UI/history. Background startup, identity/readiness checks and offline warnings are now implemented. The earlier model restoration was policy-denied and remains pending owner authorization; do not run the historical vendor command below without that authorization:

```powershell
& 'D:\llm-server\Bonsai-demo\bin\cuda\llama-server.exe' `
  -m 'D:\llm-server\Bonsai-demo\models\bonsai2-gguf\27B\Ternary-Bonsai-2-27B-PQ2_0.gguf' `
  -ngl 999 -c 65536 -fa on -ctk q8_0 -ctv q8_0 --jinja --host 127.0.0.1 --port 8088 --slots
```

The Task has `require_user_acceptance=true`; the operator did not impersonate the user or close that gate. Use **ตรวจและรับรองผล** after reviewing the concrete current evidence. This accepts the bounded token-refresh Task, not full IoT deployment.

## Evidence and original-source integration

Local raw receipts are preserved under `.tmp-ui/easiiot-pilot-receipts/`; prior failed/rollback cycles remain in sibling `easiiot-pilot-receipts-cycle*` folders. These folders are ignored because they contain private local project/runtime evidence.

- Task: `task_d648a0d0795b9c60b4ef6b0fa3775d98`, requirement/plan 4, revision 31, `verifying`.
- Final proposal: `2c18590298e32db5fcf0b7ecb71d165ebae1b46560f788ebd137ddacf3d43edb`, `verified`.
- `source-integration.json` binds original preimage, final source hash, review artifact and backup location.
- Original backup: `.tmp-ui/easiiot-integration-backup-20261001/api.js` and `package.json`. Original changes are `frontend/services/api.js`, `frontend/tests/api-refresh.test.mjs`, and the `test:api-refresh` package script.
- Managed original backend retry: `job_80f1dfe9e54f7ab517fc086452182705`, exit 0. Preserve failed initial job `job_48343359a87970b852e2e8ec919b56b0` as diagnosis evidence.
- Managed frontend test: `job_60e44056df2c9f6944877f9bebb4d4ef`, 7/7, exit 0. Production build: `job_23693050573d25fe897f53d92ef05e67`, exit 0 (existing dependency deprecation warning).
- `verified-task-restart.json` proves unchanged effect IDs and source. Actual screenshots: `task-desktop.jpg`, `task-mobile.jpg`.
- Native Skill `easiiot-refresh-recovery`: explicit one-fixture replay passed. Pilot operator promoted a **create** candidate under existing manual policy; comparative behavioral gates apply to improvements, not this create. `knowledge-promotion.json`, matching/other/personal sessions, and `knowledge-turn-stream.json` record the scope test and actual compiled Skill fragment. This proves retrieval/injection, not higher success rates on future coding tasks.

## Docker continuation — 2026-10-02

The user restored Docker and authorized continuing. Existing isolated PostgreSQL integration passed migration up/down/up, exact 34-table inventory, Go model contracts, UTC, CITEXT, JSONB/Thai, identity/FK/cascade, concurrent count and quota admission. Runtime smoke rebuilt the current Linux API/worker/consumer, booted PostgreSQL/Redis/RabbitMQ/authenticated MQTT, and passed registration, duplicate email, login/profile, API-key boundaries, synthetic telemetry, `last_seen` and logical backup/restore.

Two Windows-specific test runner failures were reproduced and repaired: Git Bash's automatic container-path rewriting and a Linux-created MQTT password file unreadable through a Windows read-only bind. `scripts/test_host_compat.sh` converts host bind sources explicitly and disables rewriting only for Docker commands; the MQTT test generates credentials inside Linux with the correct owner/mode. Normal test cleanup removed its containers/networks/volumes.

The interrupted failing run's temp folder `C:/Users/ZP2E0/AppData/Local/Temp/easiiot-runtime.aKRz0B` remains: automatic approval review rejected its guarded native PowerShell removal with `blocked by policy`. The deletion was not executed or retried through another method. All test Docker containers/networks/volumes were removed; this temp folder is the remaining cleanup exception.

Real integration also exposed a product contract mismatch: invalid/expired refresh credentials returned 400, so the repaired frontend retained an invalid session. The backend now returns **401** for rejected refresh credentials; malformed request bodies remain 400. A handler regression reproduced 400 for expired/malformed/missing credentials before the fix, then passed. The opt-in `frontend/tests/api-refresh-runtime.mjs` loads actual API/auth-header/Axios source and uses the real API with a correctly signed expired access JWT. Two requests share one actual refresh and return 200; invalid refresh clears credentials/redirects and login on the same page recovers. It runs with a 45-second bound and never prints credentials.

```powershell
# From the original Easiiot checkout, with Docker Desktop Linux engine ready.
& 'C:\Program Files\Git\bin\bash.exe' scripts/postgres_integration_test.sh
& 'C:\Program Files\Git\bin\bash.exe' -c 'EASIIOT_FRONTEND_REFRESH_SMOKE=1 bash scripts/postgres_runtime_smoke_test.sh'
```

Receipts: `postgres-integration-final.txt`, `postgres-runtime-final.txt`, previous failing/baseline logs, and `docker-runtime-artifacts.json`. Actual logs are also saved in the original project's harness artifacts: `artifact_c62351086f727acc77ff8c4c9bfb03a6` and `artifact_a456d7a879d8a8839c097d60ce3f0413`. They identify the external operator runner rather than claiming a harness-managed command job. Backend full Go tests, vet/build, Bash syntax and the unchanged seven frontend cases passed after the follow-up. No harness source changed in this Docker continuation.

The existing bounded Task/verified proposal was not widened or replayed: its frontend hash and revision 31 remain unchanged. The backend follow-up is separately documented and tested in the original checkout. Generated Swagger preview was checked and retained with receipts; the repository's intentionally tracked placeholder was preserved, and source annotations/API Reference describe the 401 contract.

## Reconfirmation from the running system — 2026-10-02

The user requested confirmation of the fixes and an explanation of Hermetrix. At inspection, the previous foreground harness/model processes had stopped; Docker was healthy. The existing installed Bonsai and preserved pilot launcher were restarted on loopback 8088/18801. Live health reports model `ok` and harness schema 73. The actual provider test through Hermetrix returned `HERMETRIX_OK` in 6901 ms.

Fresh current-source checks passed: frontend refresh regression 7/7, backend handler suite, and an isolated real Docker runtime with actual frontend/Axios. Observed rejected refresh: HTTP401, credentials0, `/login`; later login, concurrent shared refresh, telemetry and logical backup/restore passed (runtime exit0). Current frontend/backend hashes and previous before/after log hashes match the saved proof. The full harness suite/build results above are prior verification, not a fresh full-suite run in this reconfirmation.

Actual browser navigation reached the preserved Easiiot Task and its acceptance ledger without console warnings/errors. The page and revision-bound completion API agree: revision31, six criteria, two checks and one step pass; 16/17 gates, actual user acceptance still open. No completed proposal was replayed and no acceptance was submitted. New receipts: `reconfirmation-runtime-20261002.txt`, `reconfirmation-model-20261002.json`, `reconfirmation-task-20261002.json`, `reconfirmation-completion-20261002.json`, and `reconfirmation-task-20261002.jpg` under `.tmp-ui/easiiot-pilot-receipts/`.

## Remaining work and limits

### Explicit before/after reproduction — 2026-10-02

At the user's request, the outcome was rerun from clean isolated stacks with an exported baseline backend (`d1f98704d685bf5c48614c8542d765f826a9e180`) and the current fixed backend. Both use byte-identical current frontend and runtime checks; production source was not reverted or changed during reproduction.

| Observation | Before fix, real API | After fix, real API |
| --- | --- | --- |
| Rejected refresh | HTTP 400 | HTTP 401 |
| Credentials retained | 3 | 0 |
| Redirect | `/` | `/login` |
| Runtime check exit | 1, expected reproduced failure | 0 |
| Same-page login recovery, telemetry, backup/restore | Completion correctly blocked by the failing refresh gate | Passed |

Reproduce with `.\scripts\verify-easiiot-runtime.ps1` from the harness checkout. This script exports the baseline to an ignored evidence directory, reuses installed frontend dependencies, verifies exact before-failure and after-success signatures, binds logs/source/tests by SHA-256 and returns success only after both outcomes are observed. Fresh evidence: `.tmp-ui/easiiot-runtime-proof-20261002-101402/before.txt`, `after.txt`, `receipt.json`. No test Docker containers, networks or volumes remained after the run. The previously rejected old-temp deletion was not retried and did not prevent this reproduction or verification.

The measured database, runtime and frontend/API integration gates are closed. Actual user acceptance remains open; physical device behavior, every dashboard/control/schedule feature and production deployment require their own evidence. Prior 2026-09-15 receipts in Easiiot remain historical and were not substituted for this run.

Windows command receipts disclose `sandbox.enforced=false`, `process-hardening-only`, without OS write/network isolation. Goal write allowlists protect proposal/apply; they do not provide OS isolation for arbitrary project commands. The local model required operator diagnosis after several incorrect proposals; the measured result is evidence-gated recovery, not unattended success on every request. No commit, PR or deployment was made.

## Breadth boundary

The pilot proves the coding/debug/testing/recovery spine on a real repository. Security assessment, physical IoT devices, social publication, Mac/native packaging and every media workflow require their own targets and evidence. Do not label those accepted merely because the Easiiot coding journey succeeds.
