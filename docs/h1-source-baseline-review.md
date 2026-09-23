# H1 Source Baseline and Commit Separation Review

Review date: 2026-09-22 (Asia/Bangkok)
Repository: `D:\Projects\harness\hermetrix-harness`
Scope: source classification and reconstruction plan only. No source implementation, staging, commit, reset, push, Pi change, or H2 work was performed.

## Executive finding

The current working tree is not a truthful H1 commit candidate. It contains 73 modified tracked files and 139 untracked files on top of `08fcac4`, with no staged files. H1 itself is concentrated in a new adapter package, a fixture command, migration v49, a transactional Task Engine creation seam, and a project-root read seam. It also relies on pre-existing uncommitted work: schema v37-v48, principal/ownership columns, the current task creation shape, and the bounded Task Engine decision path.

Two tracked files contain inseparable-at-file-level but separable-at-hunk-level work:

- `internal/store/store.go`: pre-existing v37-v48 migration chain and store safety changes plus H1's v49 dispatch and schema-version bump.
- `internal/taskengine/service.go`: H1's `Create` -> `CreateInTx` delegation plus pre-existing ownership filtering in `Get` and `List`.

Two untracked documentation/index files contain material from several earlier work streams plus H1 references:

- `docs/NEXT_SESSION.md`
- `docs/FILE_MAP.json`

They must not be copied wholesale into an H1 commit.

The safest source strategy is **C — patch/worktree reconstruction from the exact HEAD**, using a dedicated integration branch only as the destination for that reconstruction. This preserves the current dirty tree and produces independently testable prerequisite commits before the two H1 commits.

## 1. Current Git state

This snapshot was taken before this review document was created.

| Item | Observed value |
|---|---|
| HEAD | `08fcac4ab8a7c367da07d773b5d9dbde0f049b1d` |
| HEAD subject | `feat(ui): workspace chat pane, context meter, and editor breadth` |
| Branch | `main` |
| Upstream | `origin/main` |
| origin/main | `08fcac4ab8a7c367da07d773b5d9dbde0f049b1d` |
| Ahead / behind | `+0 / -0` |
| Tracked modified | 73 |
| Untracked | 139 |
| Staged | 0 |

`git status --short` therefore consists only of unstaged `M` entries and untracked `??` entries. There are no staged changes to preserve or unstage.

### Modified tracked files (exact pre-document snapshot)

```text
README.md
cmd/hermetrix/main.go
docs/ARCHITECTURE.md
go.mod
go.sum
internal/agent/mcpbridge.go
internal/agent/models.go
internal/agent/runtool.go
internal/agent/service.go
internal/agent/service_test.go
internal/curator/maintenance.go
internal/learning/corpus_test.go
internal/learning/model_reviewer.go
internal/mcp/models.go
internal/mcp/pool_test.go
internal/mcp/service.go
internal/mcp/stdio.go
internal/mcp/stdio_test.go
internal/mcp/stdio_unix.go
internal/mcp/stdio_windows.go
internal/product/backup.go
internal/product/browser.go
internal/product/browser_chrome_e2e_test.go
internal/product/commands.go
internal/product/commands_windows.go
internal/product/models.go
internal/product/service.go
internal/product/service_test.go
internal/product/terminal.go
internal/product/terminal_windows.go
internal/product/workbench.go
internal/providers/anthropic.go
internal/providers/gemini.go
internal/providers/models.go
internal/providers/native_test.go
internal/providers/openai.go
internal/providers/service.go
internal/providers/service_test.go
internal/qualification/models.go
internal/qualification/service.go
internal/secrets/vault.go
internal/secrets/vault_test.go
internal/skills/models.go
internal/skills/service.go
internal/store/store.go
internal/store/store_test.go
internal/taskcoord/service.go
internal/taskcoord/service_test.go
internal/taskengine/codeproposal.go
internal/taskengine/execution.go
internal/taskengine/execution_test.go
internal/taskengine/models.go
internal/taskengine/packet_test.go
internal/taskengine/service.go
internal/web/auth.go
internal/web/product.go
internal/web/server.go
internal/web/server_test.go
internal/web/tasks.go
internal/web/ui/app.js
internal/web/ui/codex-theme.css
internal/web/ui/index.html
internal/web/ui/runtime.js
internal/web/ui/runtime.test.js
internal/web/ui/vendor/THIRD_PARTY_NOTICES.md
internal/web/ui/vendor/ide.css
internal/web/ui/vendor/ide.js
internal/web/ui_browser_e2e_test.go
internal/web/ui_contract_test.go
internal/web/vendor-src/ide-entry.js
internal/web/vendor-src/package-lock.json
internal/web/vendor-src/package.json
internal/worker/planner.go
internal/worker/planner_test.go
```

### Untracked files (exact pre-document snapshot)

```text
assets/brand/hermetrix-discord-assistant.png
assets/brand/hermetrix-discord-icon.png
cmd/hermetrix-h1/main.go
docs/DEBUGGER.md
docs/DECISION-ENGINE.md
docs/DISCORD-REMOTE.md
docs/FILE_MAP.json
docs/IDE-WORKSPACE.md
docs/NEXT_SESSION.md
docs/agent-platform-contract-v1-final-harness-review.md
docs/agent-platform-contract-v1-harness-review.md
docs/decisions/ADR-001-bounded-decision-engine.md
docs/decisions/ADR-002-local-decision-shadow.md
docs/h1-agent-platform-readonly-integration-proposal.md
docs/handoffs/2026-09-19-sol-implementation.md
docs/harness-gap-analysis-arch-v3.md
docs/managed-execution-v1-final-harness-review.md
docs/managed-execution-v1-lock-review.md
docs/redesign/2026-09-20-task-first-ux.md
docs/reviews/2026-09-18-probes.md
docs/reviews/2026-09-18-project-review.md
docs/specs/2026-09-18-correctness-performance-remediation.md
docs/specs/2026-09-19-hermetrix-master-project-specification.md
docs/specs/2026-09-19-local-multimodal-privacy-implementation.md
internal/agent/multipart.go
internal/agent/multipart_test.go
internal/agentplatform/binding.go
internal/agentplatform/canonical.go
internal/agentplatform/canonical_test.go
internal/agentplatform/contract.go
internal/agentplatform/fixture/receiver.go
internal/agentplatform/intake.go
internal/agentplatform/integration_test.go
internal/agentplatform/observe.go
internal/agentplatform/outbox.go
internal/agentplatform/schema.go
internal/agentplatform/schemas/ack.json
internal/agentplatform/schemas/envelope.json
internal/agentplatform/schemas/manifest.json
internal/agentplatform/schemas/run_update.json
internal/agentplatform/schemas/task_assignment.json
internal/agentplatform/service.go
internal/agentplatform/source_guard_test.go
internal/agentplatform/validate.go
internal/discordbridge/agent.go
internal/discordbridge/agent_test.go
internal/discordbridge/gateway.go
internal/discordbridge/interactions.go
internal/discordbridge/rest.go
internal/discordbridge/service.go
internal/discordbridge/service_test.go
internal/identity/principal.go
internal/inference/context.go
internal/inference/presets.go
internal/inference/presets_test.go
internal/inference/scheduler.go
internal/inference/scheduler_test.go
internal/product/command_env_posix.go
internal/product/command_env_windows.go
internal/product/debugger.go
internal/product/debugger_dap.go
internal/product/debugger_dap_test.go
internal/product/debugger_protocol.go
internal/product/debugger_test.go
internal/product/directory_sync_posix.go
internal/product/directory_sync_windows.go
internal/product/file_url_posix.go
internal/product/file_url_windows.go
internal/product/ide.go
internal/product/ide_test.go
internal/product/media.go
internal/product/media_test.go
internal/product/mutations.go
internal/product/pathlock.go
internal/product/platform_capabilities.go
internal/product/platform_capabilities_darwin.go
internal/product/platform_capabilities_linux.go
internal/product/platform_capabilities_other.go
internal/product/platform_capabilities_windows.go
internal/product/portability.go
internal/product/portability_test.go
internal/product/project_binding.go
internal/product/recovery.go
internal/product/recovery_test.go
internal/product/retrieval.go
internal/product/retrieval_test.go
internal/product/sharing.go
internal/product/sharing_test.go
internal/product/terminal_windows_test.go
internal/product/visibility.go
internal/providers/completion.go
internal/providers/openai_completion_test.go
internal/providers/reasoning_test.go
internal/qualification/runtime_fingerprint.go
internal/qualification/runtime_fingerprint_test.go
internal/secrets/format_posix.go
internal/secrets/format_windows.go
internal/store/agent_platform.go
internal/store/agent_platform_test.go
internal/store/process_lock_unix.go
internal/store/process_lock_windows.go
internal/taskcoord/decision.go
internal/taskcoord/decision_benchmark.go
internal/taskcoord/decision_test.go
internal/taskengine/create_tx.go
internal/taskengine/decision.go
internal/taskengine/decision_benchmark.go
internal/taskengine/decision_benchmark_test.go
internal/taskengine/decision_evaluation.go
internal/taskengine/decision_test.go
internal/taskengine/orchestration.go
internal/taskengine/orchestration_test.go
internal/taskengine/progress.go
internal/taskengine/progress_test.go
internal/web/boundary.go
internal/web/boundary_test.go
internal/web/debugger.go
internal/web/debugger_test.go
internal/web/decision.go
internal/web/discord.go
internal/web/discord_test.go
internal/web/ide.go
internal/web/ui/discord-settings.js
internal/web/ui/discord-settings.test.js
internal/web/ui/flow-state.test.js
internal/web/ui/hydration.test.js
internal/web/ui/ide-assistant.js
internal/web/ui/ide-assistant.test.js
internal/web/ui/ide-workspace.js
internal/web/ui/ide-workspace.test.js
internal/web/ui/navigation-render.test.js
internal/web/ui/tasks.test.js
internal/web/ui/vendor/formatter.js
internal/web/ui_csp_test.go
internal/web/ui_discord_e2e_test.go
internal/web/ui_ide_actions_e2e_test.go
internal/web/vendor-src/build.mjs
internal/web/vendor-src/formatter-entry.js
internal/web/vendor-src/formatter.test.mjs
```

## 2. Classification rules

The four requested labels are applied by dependency and content, not by file timestamp. Git cannot prove when an untracked file was first written.

| Label | Meaning in this review |
|---|---|
| `H1_ONLY` | Introduced solely to implement, test, demonstrate, or document H1. |
| `PREEXISTING_PREREQUISITE` | Existing uncommitted source that H1 needs for compilation, schema continuity, or the approved safety behavior. |
| `SHARED_HUNK` | A file contains both prerequisite/earlier material and H1 material, or an untracked aggregate document contains multiple work streams. It must be reconstructed by hunk/section. |
| `UNRELATED` | It may be valuable prior work, but H1 does not need it to compile or preserve its accepted runtime boundary. It must stay out of the H1 history. |

## 3. H1-only inventory

The following 24 files are `H1_ONLY`:

```text
cmd/hermetrix-h1/main.go
docs/h1-agent-platform-readonly-integration-proposal.md
internal/agentplatform/binding.go
internal/agentplatform/canonical.go
internal/agentplatform/canonical_test.go
internal/agentplatform/contract.go
internal/agentplatform/fixture/receiver.go
internal/agentplatform/intake.go
internal/agentplatform/integration_test.go
internal/agentplatform/observe.go
internal/agentplatform/outbox.go
internal/agentplatform/schema.go
internal/agentplatform/schemas/ack.json
internal/agentplatform/schemas/envelope.json
internal/agentplatform/schemas/manifest.json
internal/agentplatform/schemas/run_update.json
internal/agentplatform/schemas/task_assignment.json
internal/agentplatform/service.go
internal/agentplatform/source_guard_test.go
internal/agentplatform/validate.go
internal/product/project_binding.go
internal/store/agent_platform.go
internal/store/agent_platform_test.go
internal/taskengine/create_tx.go
```

Notes:

- `internal/store/agent_platform.go` contains only migration v49.
- `internal/taskengine/create_tx.go` is H1's transaction-composition seam. Its implementation intentionally carries the existing ownership and egress rules; those rules are prerequisites, not H1 inventions.
- `internal/product/project_binding.go` only exports the existing canonical project-root resolver for a read-only binding check.
- H1's JSON Schema dependency is already present at HEAD: `github.com/santhosh-tekuri/jsonschema/v6 v6.0.3`. The current `go.mod` and `go.sum` diffs are not H1 changes.

## 4. Pre-existing prerequisite inventory

### 4.1 Direct source and schema prerequisites

These are the minimum dependency families that must exist before H1 is applied:

| Prerequisite | Required behavior | Files/hunks |
|---|---|---|
| Schema v37-v48 chain | v49 must follow a real, tested v48 database; upgrades cannot skip or relabel existing migrations. | Pre-H1 portions of `internal/store/store.go`; migration tests in `internal/store/store_test.go`. |
| Store safety used by the current baseline | H1 opens the same store and relies on its current single-root/migration behavior. | Pre-H1 store-open/lock hunks in `internal/store/store.go`; `internal/store/process_lock_unix.go`; `internal/store/process_lock_windows.go`. |
| Ownership identity | Bindings, projects, tasks, and evidence use a local owner principal. | `internal/identity/principal.go`; v41 portions of `internal/store/store.go`; ownership portions of `internal/product/models.go`, `internal/product/service.go`, and `internal/taskengine/models.go`/`service.go`. |
| Durable Task Engine safety | v37-v38 changed effect authority, recovery, and mutation semantics that the current v48 schema represents. | `internal/taskengine/execution.go`, `internal/taskengine/execution_test.go`, `internal/taskengine/codeproposal.go`, `internal/product/mutations.go`, `internal/product/pathlock.go`, `internal/product/recovery.go`, `internal/product/recovery_test.go`. |
| Bounded decision engine | H1 obtains committed `CompactState` and candidates through `DecideNextAction`, then invokes `RuleDecision` with read-only candidates. | `internal/taskengine/decision.go`, `internal/taskengine/decision_test.go`, `internal/taskengine/decision_evaluation.go`; supporting benchmark files may be committed with the v48 decision slice. |
| Current artifact shape | H1 writes an immutable projection artifact using owner, visibility, export policy, sharing revision, and lineage columns established before v49. | v41-v46 portions of `internal/store/store.go`; prerequisite portions of `internal/product/models.go` and `internal/product/service.go`. |

The prerequisite baseline should be reconstructed as complete feature slices. Copying only SQL migration functions would create intermediate commits whose application code no longer matches their schema.

### 4.2 Migration ownership map

| Migration | Existing concern | H1 relationship |
|---|---|---|
| v37 | Run generation on durable effects | Safety regression prerequisite; H1 must not weaken effect no-replay. |
| v38 | File mutation intent, rollback/verification artifact binding, recovery audit | CAS/mutation prerequisite. |
| v39 | Compiled context payloads moved to CAS references | Context/evidence baseline prerequisite. |
| v40 | Inference usage ledger and related durable accounting | Existing schema-chain prerequisite, not called by H1. |
| v41 | Local principal, owner, visibility/export/egress columns | Direct H1 prerequisite. |
| v42 | Runtime fingerprints and qualification state | Existing schema-chain prerequisite. |
| v43 | Multipart agent event records | Existing schema-chain prerequisite. |
| v44 | Media and derivation records | Existing schema-chain prerequisite. |
| v45 | Sharing/export/import records | Existing schema-chain prerequisite. |
| v46 | Workspace portability records | Existing schema-chain prerequisite. |
| v47 | Decision shadow-run records | Decision regression baseline; H1 never invokes the provider-backed path. |
| v48 | Decision benchmark records | Decision admission/regression baseline. |
| v49 | Agent Platform binding/assignment/stream/outbox | H1 only. |

### 4.3 `taskcoord` finding

`internal/taskcoord/*` is **not a direct H1 dependency**. H1's source guard forbids provider, MCP, and agent imports; `ObserveInitial` calls `taskengine.Service.DecideNextAction` and then `taskengine.RuleDecision`. It does not import or call `taskcoord.BonsaiDecision`, shadow decision, AutoPlan, or any provider selector.

For source separation:

- `internal/taskcoord/decision.go`, `decision_benchmark.go`, `decision_test.go`, and current `service.go`/`service_test.go` changes belong to the pre-existing DecisionEngine/Bonsai work stream.
- They may be included in the **decision prerequisite commit** only when that commit is explicitly the complete v47-v48 DecisionEngine baseline and passes independently.
- They must never be labelled as H1 code.
- They remain in the final regression gate even if the smallest H1 compile graph does not import them.

## 5. Shared-hunk analysis

| File | Earlier/prerequisite content | H1 content | Required separation |
|---|---|---|---|
| `internal/store/store.go` | Store root lock; migration signature using blob store; owner lookup; v37-v48 dispatch/bodies; version 36 -> 48. | `if version < 49 { migrateV49(...) }` and version 48 -> 49. | Reconstruct the complete pre-H1 store at v48 first. Add only v49 dispatch/version advance in the H1 adapter commit. Do not stage this file wholesale from the current tree. |
| `internal/taskengine/service.go` | Ownership-aware `Get`/`List` and other pre-existing Task Engine behavior. | Replace the original `Create` body with a transaction that delegates to `CreateInTx`. | Put ownership hunks in the prerequisite baseline. Put only the `Create` delegation hunk in H1. Verify old `Create` behavior remains covered. |
| `docs/NEXT_SESSION.md` | Status for IDE, Discord, safety, decision, and other earlier work. | H1 status and commands. | Rebuild the prerequisite version first or omit this rolling document from H1. Add only an H1 section after code commits are settled. |
| `docs/FILE_MAP.json` | Index entries for several earlier features. | H1 adapter/command/doc entries. | Regenerate/update in the final documentation commit from the reconstructed tree; never copy the current aggregate file blindly. |

`internal/taskengine/models.go` is not an H1 shared file: its current differences are ownership/egress prerequisites. H1 does not introduce those fields. `internal/product/models.go` and `internal/product/service.go` are likewise prerequisite/other earlier work; H1's only product source is `project_binding.go`.

## 6. Dependency graph

```text
08fcac4 (schema v36)
  |
  +-- prerequisite durable execution/recovery slice
  |     +-- migrations v37-v40
  |     +-- effect generation/lease rules
  |     +-- mutation intents + CAS recovery
  |     +-- context/inference persistence
  |
  +-- prerequisite ownership/platform-data slice
  |     +-- migrations v41-v46
  |     +-- local principal + project/task/artifact ownership
  |     +-- schema-matched subsystem implementations
  |
  +-- prerequisite bounded-decision slice
  |     +-- migrations v47-v48
  |     +-- CompactState/CandidateAction/DecisionEngine/RuleDecision
  |     +-- existing taskcoord shadow/Bonsai and benchmark regression surface
  |
  +-- H1 contract adapter
  |     +-- v49 tables
  |     +-- schemas/canonical signatures/validation
  |     +-- trusted local repository binding
  |     +-- TaskAssignment -> CreateInTx projection
  |     +-- committed-state RunUpdate projection + outbox/ACK
  |
  +-- H1 safety/tests/demo
        +-- forbidden capability guards and DB backstop
        +-- durable receiver/replay tests
        +-- hermetrix-h1 acceptance fixture
        +-- H1 proposal/status documentation
```

Direct H1 code edges are:

```text
agentplatform.Intake
  -> store v49 tables
  -> trusted v41 owner/project binding
  -> taskengine.CreateInTx

agentplatform.ObserveInitial
  -> taskengine.DecideNextAction
  -> existing CompactState/CandidateAction
  -> taskengine.RuleDecision (selection only)
  -> existing CAS + artifact table
  -> v49 durable outbox

cmd/hermetrix-h1
  -> agentplatform + fixture receiver
  -> product.ResolveProjectRootBinding
```

There is no H1 edge to Discord, IDE/editor/debugger, MCP execution, provider execution, `taskcoord.BonsaiDecision`, workspace mutation, effect planning, or effect dispatch.

## 7. Unrelated inventory

The following work streams are `UNRELATED` to H1 and must not enter either H1 commit:

- Discord bridge, Discord UI, and brand icons: `assets/brand/*`, `internal/discordbridge/*`, `internal/web/discord*`, `internal/web/ui/discord-settings*`, `docs/DISCORD-REMOTE.md`.
- IDE/editor/debugger/terminal and browser workspace work: debugger/IDE/terminal/browser files under `internal/product`, `internal/web`, `internal/web/ui`, and `internal/web/vendor-src`; `docs/DEBUGGER.md`; `docs/IDE-WORKSPACE.md`.
- General UI redesign and navigation changes: modified UI assets/tests, `docs/redesign/*`, and UI review/spec documents.
- Provider/model, MCP, qualification, secrets, skills, worker, and agent changes except the exact prerequisite slices deliberately reconstructed for v37-v48.
- Contract review documents and managed-execution review documents. They are review artifacts, not H1 implementation.
- Current dependency-file changes in `go.mod`/`go.sum`: `age`, HPKE, and x/* version updates are unrelated. HEAD already supplies H1's JSON Schema library.
- `README.md`, `cmd/hermetrix/main.go`, and `docs/ARCHITECTURE.md` current changes unless a later prerequisite commit proves a specific v37-v48 dependency hunk. No current H1 runtime imports or modifies them.

For an exact mechanical classification, define the sets as follows:

1. `H1_ONLY` is the 24-file list in section 3.
2. `SHARED_HUNK` is the four-file list in section 5.
3. `PREEXISTING_PREREQUISITE` is the source/hunks selected by the complete v37-v48 feature slices in sections 4.1 and 4.2, validated at each intermediate schema version boundary.
4. Every path in the exact current-state inventories in section 1 that is not selected into sets 1-3 is `UNRELATED` for the H1 history.

This set rule is intentional: several current backend files combine multiple earlier initiatives, and their chronological origin cannot be proven from Git because they were never committed. A file must earn inclusion through a build/test dependency or schema-feature mapping; proximity to H1 is insufficient.

## 8. Source strategy evaluation

### A — prerequisite baseline commits in the current working tree

**Not recommended.** The tree contains 212 pre-document changed paths, four shared files, and large unrelated UI/Discord/IDE work. Interactive staging in place has a high risk of including an unrelated hunk or leaving a commit dependent on an untracked file. A commit that passes only because uncommitted files remain present would violate the reproducibility requirement.

### B — dedicated integration branch using the current dirty tree

**Useful only as containment, not sufficient as separation.** Creating a branch would preserve the pointer but would not give the uncommitted work history. A single snapshot commit would hide unrelated work under a prerequisite or H1 label. This can be used later as a safety/archive branch after hashes and patches are captured, but should not become the reviewable history.

### C — patch/worktree reconstruction from HEAD

**Recommended.** Create a new worktree from the exact `08fcac4` commit and reconstruct only complete, testable slices. The current working directory remains untouched and serves as the source reference. Shared files are rebuilt deliberately rather than staged wholesale. Each commit is tested from a clean index and with no reliance on files outside that commit.

Recommended operational form when execution is later authorized:

1. Record a NUL-safe status manifest, tracked binary patch, untracked-file archive, and SHA-256 manifest for the current dirty tree.
2. Create a separate worktree and branch, suggested name `codex/h1-source-reconstruction`, at `08fcac4`.
3. Reconstruct prerequisite slices in dependency order. After each slice, require `git status` to show only the intended files, then test before committing.
4. Reconstruct H1 adapter source and H1 safety/demo separately.
5. Compare the reconstructed final behavior and H1 source hashes/semantic diffs against the approved dirty-tree implementation.
6. Leave all unrelated paths only in the original working tree.

## 9. Exact proposed commit sequence

The proposed history is:

```text
08fcac4  feat(ui): workspace chat pane, context meter, and editor breadth
  |
  +-- P1  prerequisite: harden durable execution and recovery (schema v37-v40)
  |
  +-- P2  prerequisite: add local ownership and platform data services (schema v41-v46)
  |
  +-- P3  prerequisite: add bounded decision engine and admission records (schema v47-v48)
  |
  +-- H1A feat(agent-platform): add read-only contract adapter (schema v49)
  |
  +-- H1B test(agent-platform): add safety guards, receiver, demo, and H1 docs
```

Commit boundaries:

- **P1** must contain the application changes matching effect generation, mutation intent/recovery, CAS context, and inference accounting migrations. It ends at a real `CurrentSchemaVersion = 40` and must pass independently.
- **P2** must contain local principal/ownership plus the runtime-fingerprint, multipart, media, sharing, and portability source necessary to make v41-v46 an honest coherent baseline. It ends at schema 46.
- **P3** contains `taskengine` bounded decision types/selection, the existing `taskcoord` shadow/Bonsai integration and benchmark/admission records, plus v47-v48. It ends at schema 48. H1 must not alter these core semantics.
- **H1A** contains the contract schemas/types, validation/canonicalization, binding, transactional task projection, committed-state projection, durable outbox, and v49. The only shared tracked hunks are v49 in `store.go` and `Create` delegation in `taskengine/service.go`.
- **H1B** contains source/DB capability guards, integration/receiver tests, the fixture command, acceptance demo material, and H1-specific documentation/index sections.

If reconstruction proves P1 or P2 is too broad to pass as one coherent commit, it may be split only at a real schema/feature boundary. Do not create file-by-file micro-commits, and do not combine P1-P3 merely to make staging easier.

## 10. Tests per commit and final state

No tests were rerun during this analysis-only review. The following gates are required during reconstruction.

### Every commit

- Clean-index proof: after committing, no tracked or untracked file needed by the build/test may remain outside the commit.
- Compile all packages from that commit, preferably with a fresh Go build cache and temporary data roots.
- Run the tests for every changed package and its direct consumers.
- Run migration from a fixture at the immediately preceding schema version and reopen at the new version.
- Run `go vet` for changed packages; run repository-wide `go vet ./...` before the final H1 commit.

### P1 — v37-v40

- Task Engine run/lease/effect recovery regressions.
- Effect intent generation fencing and effect no-replay checks.
- File mutation intent, rollback artifact, recovery, and CAS integrity tests.
- v36 -> v40 migration and reopen tests, including aged fixtures.

### P2 — v41-v46

- Ownership isolation for projects, tasks, artifacts, memories, sessions, and affected jobs.
- Migration tests for v40 -> v46 and aged fixtures.
- Qualification/runtime-fingerprint, multipart, media, sharing, portability, and secrets tests for the source actually included.
- Product and Task Engine regressions affected by ownership/egress columns.

### P3 — v47-v48

- `taskengine` CompactState/candidate/RuleDecision tests.
- `taskcoord` shadow/Bonsai selection tests and decision benchmark/admission tests.
- Proof that selection remains proposal-only and cannot dispatch an effect.
- v46 -> v48 migration and reopen tests.

### H1A/H1B and final state

The final gate must include all of the following requested evidence:

| Gate | Required proof |
|---|---|
| H1 tests | Contract/schema/canonicalization, intake idempotency/conflict, binding drift, observation projection, durable outbox, contiguous ACK, replay/restart, retry exhaustion, and source/DB guards all pass. |
| Task Engine regressions | Task creation behavior, criteria stable IDs, revision checks, decision state/candidates, lease/effect recovery, and no-replay tests pass. |
| Migration tests | Fresh database, aged fixtures, v48 -> v49, failed migration version behavior, and reopen at v49 pass. |
| CAS integrity | Projection bytes hash to their immutable EvidenceRef/CAS identity; stored blob and artifact checksum agree. |
| `go vet` | Changed packages and `go vet ./...` pass from the reconstructed commit. |
| Acceptance demo | The `hermetrix-h1` fixture completes intake, decision-only observation, ACK-loss/restart recovery, duplicate/conflict cases, and contiguous ACK behavior. |
| Workspace before/after digest | Digest excludes only fixture-owned data paths; repository/worktree files are identical before and after the demo. |
| Forbidden invocation counters | Provider, MCP tool, agent turn, planner, command, workspace write, BeginRun, BeginStepAttempt, PlanEffect, and DispatchEffect remain zero. |
| Clean dependency proof | Clone/worktree at H1B with a clean index can reproduce the full result without files from the original dirty tree. |

`go test -race` is a known Windows environment limitation in the current setup: the cgo race build failed even when the MSYS2 UCRT64 GCC path was supplied. Record that as an explicit environment-limited gate until the race-capable Go/cgo toolchain is independently repaired. It must not be reported as passing or silently omitted.

A prior repository-wide sequential run also encountered an existing `internal/agent` timeout during SQLite migration/fsync. Reconstruction must either make that suite complete with an appropriate controlled test timeout/environment or record the exact independent failure. It must not use a partial run as proof that every commit is green.

## 11. Rollback and recovery plan

The source rollback unit is the new reconstruction worktree/branch, not the current dirty tree.

1. Before reconstruction, capture status, tracked diff (including binary data), untracked-file hashes/archive, HEAD, and toolchain versions.
2. Never reset, clean, checkout, or move files in the original `main` working directory.
3. Perform reconstruction only in the new worktree. Validate its resolved absolute path before any later removal.
4. After every commit, record the commit SHA, schema version, exact test commands/results, and workspace digest.
5. If a slice fails, abandon or repair only the new worktree commit. The original dirty tree remains the recovery source.
6. Source rollback does not downgrade a database. Test each migration against disposable copied fixture roots; never open an upgraded production/data fixture with an older binary as a rollback method.
7. Before declaring equivalence, compare the final reconstructed H1 behavior, generated envelope bytes/digests, schema, demo receipt, and forbidden counters with the approved implementation.

## 12. Risks

| Risk | Impact | Control |
|---|---|---|
| No committed pre-H1 snapshot exists | Chronological authorship of untracked/shared files cannot be proven from Git. | Classify by dependency/content; reconstruct from HEAD; archive and hash the dirty source before execution. |
| `store.go` spans 13 migrations and store-open changes | Whole-file staging would hide v49 among earlier work. | Rebuild v48 first; apply only the v49 dispatch/version hunk afterward. |
| Task creation hunk depends on ownership-era fields | H1 could compile only because uncommitted v41/task model files are present. | Land ownership/task shape in P2; verify H1A on a clean P3 parent. |
| Package-level Go compilation can conceal unrelated files | Copying a directory wholesale may bring IDE/media/provider changes into H1. | Copy explicit manifest paths; inspect `git diff --name-status` and imports per commit. |
| Rolling docs are untracked aggregates | Whole-file commit would mislabel prior work as H1. | Regenerate only after source history is stable; commit H1-specific sections in H1B. |
| Existing full-suite timeout | Could obscure a real regression or make a commit appear untestable. | Reproduce independently with controlled temp storage/timeout and record exact result before accepting history. |
| Race toolchain unavailable | Concurrency proof is incomplete on this host. | Keep as declared limitation; run on a supported race-capable environment before release if it is a release gate. |
| Line-ending warnings on Windows | Patch reconstruction may create noisy or misleading hunks. | Preserve repository attributes, compare normalized content hashes, and inspect diffs before each commit. |

## Decision

The code can be separated without rewriting or discarding the existing working tree. The final H1 history must be reconstructed from `08fcac4`; it must not be produced by committing the current tree in place. The proposed P1-P3 -> H1A -> H1B sequence provides a truthful schema and dependency chain, while keeping unrelated IDE, Discord, UI, and other work outside H1.

```yaml
H1_SOURCE_BASELINE_STATUS: READY_FOR_REVIEW
```
