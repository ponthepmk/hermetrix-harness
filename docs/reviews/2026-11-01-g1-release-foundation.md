# G1 — Release/data foundation acceptance

Date: 2026-11-01, Asia/Bangkok. Scope: approved development/staging only. Architecture A remains the basis. No Pi deployment, installed executable replacement, installed database migration, credential copy/rotation or network policy change.

## Implemented

- `hermetrix version` emits release metadata and the actual executable SHA-256. Release builds bind source commit, dirty state and a digest of the enumerated source inputs. Ordinary builds report unknown provenance when unavailable rather than inventing a revision.
- `hermetrix preflight --data PATH` does not call `store.Open`. It never initializes the data root, creates the application lock or migrates. Immutable read-only SQLite inspection refuses nonempty WAL rather than ignoring committed WAL data or checkpointing it. It reports missing/current/upgrade-required/newer schema. This is schema inspection, not application health or permission to upgrade.
- `scripts/build-development-release.ps1` creates a new isolated release directory, manifest and source-input ledger; verifies binary hash and unchanged inputs after build. Its input digest covers cmd/internal/assets/go.mod/go.sum and the build script, excluding Go/JS test files. The ledger is not a full repository archive. Dirty builds remain explicitly marked.
- `scripts/start-development-harness.ps1` accepts only non-linked data underneath `.hermetrix-g1-staging`, requires explicit initialization for a new root, refuses schemas needing migration, offers CheckOnly, and does not require a local inference server. Existing installed launchers are unchanged. Runtime warnings about unavailable inference are retained.
- Migration/restore coverage uses a synthetic schema-50 database freshly created by the installed legacy binary in a new G1 directory. It contains no copied production history, credentials or vault. Tests create a consistent SQLite snapshot using VACUUM INTO, seed synthetic bilingual history and a paused task, then upgrade/reopen/restore in isolated test directories.

## Actual acceptance evidence

| Check | Result |
|---|---|
| Legacy binary with isolated new data | health schema=50, ok=true; owned fixture process stopped afterward |
| Schema 50→82 | PASS: bilingual event text and paused-task state preserved |
| SQLite integrity/foreign keys | PASS on upgraded and restored copies |
| Reopen migrated data | PASS; schema and fixture facts unchanged |
| Restore pre-upgrade backup | PASS: restored file reports 50, can upgrade again, backup digest unchanged |
| Preflight absent data | PASS: returned not_initialized; did not create root |
| Preflight old/current/future versions | PASS: source bytes preserved and no new sidecars |
| Preflight nonempty WAL | PASS: rejected rather than ignoring WAL |
| Development launcher with installed data path | PASS: rejected before launch |
| Development app without local LLM | PASS: / returns HTTP 200; health schema=82/ok=true; providers=[]; no :8088 listener |
| Real installed DB inspected afterward | Still schema=50; migration_required; ready_without_migration=false |
| Repeat release builds | Identical executable SHA-256 for the same input ledger |
| Go build ./... | PASS |
| Linux ARM64 cross-build | PASS with CGO_ENABLED=0; compile-only, not Pi runtime acceptance. Initial CGO-enabled attempt failed because the Windows host lacks a Linux ARM64 C toolchain |
| go vet ./cmd/hermetrix ./internal/releaseinfo | PASS |
| go test ./internal/store ./internal/releaseinfo ./cmd/hermetrix -count=1 -timeout=180s | PASS; explicit G1 fixture test also separately executed, not counted from a default skip |

First release identity:

```text
source commit: b3de3a855b83562291555ea24b7c61133f3e6495
source dirty: true
source input digest: dfb6723d056383308bf8f653a0c92f328a66d7f399f04292ca16a0be7c8df483
executable SHA256: 735d92833806b8dfc82d4ea9556b11a2d9bdda2f4bc5d9e0dbea087ad7d6e400
schema target: 82
```

The release includes the existing uncommitted voice fixes and other code already present in the checkout; the input ledger identifies actual contents. It is a development candidate, not a clean tagged release or a claim that every earlier change is qualified.

## Reproduce on the development machine

Use PowerShell 7 and the existing Go toolchain from the repository root:

```powershell
$exe = & ./scripts/build-development-release.ps1
& $exe version
& ./scripts/start-development-harness.ps1 -ExecutablePath $exe -CheckOnly
& ./scripts/start-development-harness.ps1 -ExecutablePath $exe -Initialize
```

The last command runs a foreground development server on 127.0.0.1:18962. Stop it before rebuilding/reusing its data. No provider is seeded automatically; opening the app and running inference are distinct states.

For the synthetic legacy snapshot already retained locally:

```powershell
$env:HERMETRIX_G1_FIXTURE = 'D:\Projects\harness\hermetrix-harness\.hermetrix-g1-staging\legacy-fixture-ce252906a05c4f1ab436b3f810b21bb7\data\hermetrix.db'
go test ./internal/store -run '^TestG1Legacy50MigrationAndRestore$' -v -count=1 -timeout=90s
```

This is a machine-local fixture path, not a production data path. Raw logs, manifests and binaries remain ignored under `.hermetrix-g1-staging`. Test files remain local according to the user's commit scope.

## Limits, rollback and next checkpoint

Synthetic migration success does not authorize migration of real user history/blob stores. Before installed upgrade, obtain approval for a consistent backup of the full required data, verify real-data invariants on an approved isolated copy, and demonstrate old-runtime rollback with that copy. Do not raw-copy a live DB without WAL or restore an old executable over upgraded data.

Preflight requires an offline/consistent snapshot and cannot attest to a concurrently changing database or qualify model availability. The development launcher deliberately refuses automatic old-schema upgrades. Installed launcher coupling to Bonsai has not been changed because installed configuration is outside this approval.

Rollback now: stop only owned G1 staging processes and use the old installed launcher/data unchanged; revert only the G1 code changes if desired. Retain test logs and manifests. No production rollback is needed because production was not changed.

Next approval is G2 conversational/workspace reliability; G3 integrations/GPU work also depends on its scoped approval. No new gate or production rollout was started automatically.
