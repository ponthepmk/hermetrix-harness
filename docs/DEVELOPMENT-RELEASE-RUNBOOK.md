# Development release / operations runbook

Scope: Windows development candidate, additive schema 83. Godot deferred. Production approval is separate. Consult PLATFORM-RELEASE-STATUS.json before interpreting any package as qualified.

## Build and inspect

Use PowerShell 7 at the repository root:

```powershell
$exe = & ./scripts/build-development-release.ps1
& $exe version
& ./scripts/start-development-harness.ps1 -ExecutablePath $exe -CheckOnly
& ./scripts/start-development-harness.ps1 -ExecutablePath $exe -Initialize
```

The launcher accepts only .hermetrix-g1-staging data and never starts an existing installation. Preflight refuses nonempty WAL; inspect an offline consistent snapshot instead of guessing that the main SQLite file is current. A migration-required result is not permission to migrate.

The development launcher now requires an OS command sandbox by default. On Windows, the available Job Object controls process lifetime but does not isolate filesystem/network access. In that environment commands fail closed. Only trusted development work may use the explicit `-AllowUnisolatedCommands` opt-in; do not use it to execute untrusted projects. No global environment setting is changed permanently.

## Normal use

Configure a local/cloud provider through the existing provider screen. A declared context size is not qualification. Opening the web app is possible while inference is unavailable; UI availability does not mean coding/model readiness.

Chat and Workspace requests carry stable idempotency keys. Automatic server continuation and client connectivity retries each stop after three attempts. A paused/exhausted request remains inspectable. Review the actual receipts before explicitly requesting further work. Stop does not prove a local provider stopped; runtime idle reconciliation remains required.

Use the durable Plans/Goal flow for reviewed code work: requirements, plan, source selection, proposal, explicit apply, actual command checks, independent review, and exact-version completion evidence. A success message without matching file/command receipts is not acceptance. A separate reviewer profile can use the same model and is not model-diverse assurance.

Teams retain a maximum parallelism of four. Their child sessions receive shares of a finite aggregate allowance, cannot widen the normal session limits, and cannot create independent automatic continuation loops. Partial child work fails the team task for explicit review. Reported provider usage is an external trust boundary; qualification must also measure real allocation and cancellation behavior.

## Scheduled/event task intake

Open Settings → Maintenance → งานตามกำหนดเวลา. Select an owned project, objective, acceptance criterion and interval. The scheduler creates a local-only **draft task for review**, not an approved effect or unattended coding run. Missed intervals coalesce rather than replaying a backlog. An existing unfinished task suppresses overlapping intake. Pausing a schedule stops future intake; existing drafts are cancelled separately through task controls.

POST /api/automations requires a stable request_id for creation; edits require id/revision. POST /api/automations/{id}/trigger requires a stable request_id for each event. Repeat uncertain requests with the same key; do not invent a new key to retry an unknown outcome. The API uses the same authentication/Host/Origin protections as the rest of the application. Do not put credentials or private evidence bodies in event keys.

## Backup, upgrade and rollback

1. Record exact executable/source hashes, schema, data root and ownership of running processes. Check active work and unresolved effects; stop new admissions and reconcile before maintenance.
2. After separate production approval, create a consistent SQLite/CAS full recovery package through the existing recovery flow. Credential vaults are separate, platform-protected and must not enter release/test packages.
3. Verify and restore the package to a different approved directory. Validate integrity, foreign keys, artifacts, sessions, paused tasks and pending/uncertain work. Schema 50→83 was tested on synthetic fixtures only; real-data acceptance remains required before installed upgrade.
4. Review the exact release manifest and migrations. No downgrade-in-place: old executable plus migrated database is not a rollback plan.
5. Roll out only after owner approval. On failure stop new work, preserve receipts and switch back to the pre-upgrade executable/config/data snapshot according to the tested plan. Account for any post-upgrade data before restoring an older snapshot.

For the present candidate, stop only owned staging processes; installed executable/data and Pi services have not been replaced. Keep logs/lock files and recovery artifacts. Do not clean live data directories.

## Brain / Platform operations

Pi remains owner of work registry and curated knowledge. Harness owns local execution/context/effect authority. Brain read, candidate-submit and curate grants remain distinct. Production writes, positive retrieval that refreshes a live cache, credential rotation and service restarts require their respective authorization.

Platform credential expiry observed during G0: 2026-11-07 16:34:06 as returned without timezone. Brain grants mostly expire 2026-12-22T19:30:19Z; submit grant 2026-12-23T04:14:13Z. Rotate through the authorized operator process and verify least privilege; never fall back to a shared legacy token.

Project Brain source was exported read-only to an isolated development copy for compatibility tests. The original Linux code imports fcntl and cannot be qualified by disabling that lock on Windows. Run its real tests in a Linux environment; record Linux/Windows differences instead of patching production during this release.

## Release handoff

For a model runtime already opened by the operator, `scripts/verify-live-readiness.ps1` first checks loopback discovery without starting services. Add `-RunTests -WorkspaceSource <explicit fixture module>` to run the existing live cancellation/recovery and single-file coding acceptances. It preserves separate raw receipts and rejects skipped tests as success. It never marks production ready; representative multi-file work and acoustic acceptance remain separate. External URLs, redirects and proxy discovery are refused to prevent accidental remote dispatch/cost.

`scripts/package-development-release.ps1` builds a reviewable development ZIP containing binary, manifest, input ledger, checksums, API additions and runbooks. It deliberately contains no credentials, installed databases, vault, tests or raw results. Launchers stay in the checkout so extraction cannot silently change their workspace boundary. This package is not an installer.

Before production sign-off: qualify the live model/GPU and a representative multi-file coding task, reproduce acoustic voice behavior, validate the intended worker isolation, reconcile missing specification files 00–22, test real-data restore/upgrade, and review all remaining failures/skips in the status ledger. Never turn a blocked or skipped gate into PASS because build/unit tests are green.
