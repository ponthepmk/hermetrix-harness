# H1 — Read-Only Agent Platform Integration Proposal

Date: 2026-09-22
Status: proposal only; no H1 implementation or production change.
Repository: D:/Projects/harness/hermetrix-harness, HEAD 08fcac4ab8a7c367da07d773b5d9dbde0f049b1d, including existing uncommitted source.

H1 proves assignment admission → local binding → one durable Harness task → existing decision selection → committed factual observation → durable delivery → ACK. It does not execute the selected action. Pi remains agent-agnostic; all Harness task, decision and persistence details remain client-side.

The smallest proposed proof is a separate fixture-only command, with an isolated Harness data root and an in-process durable receiver backed by a separate SQLite database. No running web application, Pi service or real model is needed.

Read-only means no task-directed workspace/external action. Local task metadata, evidence/CAS, outbox and receiver receipts must be written to prove durability; these writes are confined to the fixture data root outside the target workspace.

## 1. Current-code mapping and grounding

Inputs are the locked base contract plus the managed addendum, not new protocol designs:

| Artifact | SHA-256 |
|---|---|
| [Base contract](C:/Users/ZP2E0/Downloads/agent-platform-contract-v1.md) | ebed8f2dea6bd2b4464e75e6a1a039a92ce1ad2dcff2faf37dcf6a968ba790e4 |
| [Managed addendum](C:/Users/ZP2E0/Downloads/agent-platform-managed-execution-v1.md) | 3f49bd57876fe223ead6ae51362b9dbce50bcdb8b2e7806bb51d50c5174c5179 |

The base file's historical status heading does not reopen the user's locked-contract decision. Apply the accepted addendum wherever it refines the base. The [final lock review](D:/Projects/harness/hermetrix-harness/docs/managed-execution-v1-lock-review.md) remains the compatibility baseline.

| Actual code | Observed behavior | H1 use |
|---|---|---|
| [store.Open](D:/Projects/harness/hermetrix-harness/internal/store/store.go:27) | Owns data-root lock, SQLite WAL, foreign keys, one DB connection, migration and blob store. | Open a dedicated fixture root only. |
| [CurrentSchemaVersion](D:/Projects/harness/hermetrix-harness/internal/store/store.go:372) | Current schema is 48; migrations run in the existing store path. | Additive next migration, provisionally 49. |
| [taskengine.Service.Create](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:26) | Validates actor/criteria/project ownership; mints task and requirement IDs; commits draft + requirement revision together. | Reuse this implementation through a narrow transaction-sharing extraction. Do not duplicate its SQL in the platform adapter. |
| [CreateTaskInput / Task](D:/Projects/harness/hermetrix-harness/internal/taskengine/models.go:81) | Separate local task, requirement and plan revisions; local IDs. | Map Platform requirements, retain Harness ownership of IDs/revisions. |
| [CompactState / CandidateAction / DecisionEngine](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:23) | Existing bounded local state and selection-only interface. | Reuse unchanged. |
| [DecideNextAction](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:97) | Builds committed-state view, generates existing candidates, calls RuleDecision, returns recommendation. No dispatch. | Obtain actual state/candidates, then apply H1's read-only candidate restriction. |
| [candidateActions / ruleDecision](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:253) | Unplanned draft offers retrieve_memory and ask_planner; ordinary rule prefers ask_planner. | Do not fabricate a plan to obtain inspect_file. Restrict existing candidates and call the same RuleDecision on that subset. |
| [BonsaiDecision](D:/Projects/harness/hermetrix-harness/internal/taskcoord/decision.go:49) | Selection-only regarding task effects, but invokes providers.StreamChat. ShadowDecision also writes evaluation receipts. | Preserve unchanged; do not instantiate or invoke in H1. |
| [TaskProgress](D:/Projects/harness/hermetrix-harness/internal/taskengine/progress.go:32) | Task-wide attempts/budgets; not the Platform active-plan counter. | Do not copy its attempts blindly. Initial H1 task has no attempts. |
| [RecordValidation](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:337) | Passing check needs identity, subject and evidence. | No calls in initial H1; no fabricated passing checks. |
| [blob.Store.Put/Get](D:/Projects/harness/hermetrix-harness/internal/blob/store.go:38) | Content-addressed bytes; Put before SQL reference commitment; Get checks SHA-256. | Existing CAS for immutable projection snapshots. |
| [CreateArtifact](D:/Projects/harness/hermetrix-harness/internal/product/service.go:338) | Existing artifacts metadata + CAS; owner-scoped, private, export denied. | Reuse storage representation; projection stage inserts metadata in its own shared transaction, not a second evidence store. |
| [Project / SaveProject / root resolver](D:/Projects/harness/hermetrix-harness/internal/product/service.go:169) | Project.RootPath is local; resolveProjectRoot canonicalizes through EvalSymlinks and requires a directory. | Bind to existing local project representation; share resolver through a narrow read-only helper. |
| [resolveInside](D:/Projects/harness/hermetrix-harness/internal/product/service.go:623) | Rejects absolute relative paths and escapes; resolves symlinks for existing paths. | Preserve confinement; assignment paths never select the root. |
| [learning.StageTrigger](D:/Projects/harness/hermetrix-harness/internal/learning/service.go:75) | Stages an idempotent outbox record in caller SQL transaction. | Follow its transaction pattern; do not reuse learning jobs as transport. |
| [mcp schema compiler](D:/Projects/harness/hermetrix-harness/internal/mcp/schema.go:15) | Existing jsonschema/v6 dependency and deny-external-loader pattern. | Reuse the library/pattern with an embedded Platform registry; no runtime schema fetch. |
| [production composition](D:/Projects/harness/hermetrix-harness/cmd/hermetrix/main.go:156) | Constructs full services, recovery and background activity. | Do not attach H1 here. Dedicated command avoids these execution-capable paths. |

Relevant existing tables: projects, local_principals, durable_tasks, task_requirement_revisions, task_plan_revisions, task_steps, task_runs, task_step_attempts, task_effect_intents, task_validations, task_checkpoints, artifacts, background_jobs, task_planner_runs and task_code_proposals. No duplicate task/run lifecycle tables are proposed.

Assumptions made explicit: fixture-first, one trusted Platform peer namespace, one configured local node/agent, one accepted assignment revision per assignment/task/run for H1, serialized observation processing, no real provider or network receiver. These are proposed H1 restrictions, not changes to v1.

## 2. Proposed architecture

~~~text
Fixture setup outside measured task processing:
  isolated data root + fixture project + trusted local bindings + fixture keys

Signed fixture TaskAssignment + separate read/none credential
  → embedded schema + semantic + digest/signature validation
  → local identity/project/root binding
  → transaction A: existing Task Engine create + assignment cross-reference
  → existing DecideNextAction → real CompactState + existing candidates
  → retain only approved read candidates
  → existing RuleDecision.Decide
  → record selection only; no dispatcher
  → immutable source snapshot → existing blob.Store.Put
  → transaction B: artifacts row + immutable RunUpdate/outbox + sequence increment
  → in-process fixture receiver → receiver DB commit
  → highest contiguous ACK → sender ACK transaction
  → exact-byte replay after restart, with no decision/action invocation
~~~

The H1 data root must be outside the bound workspace. The command refuses the configured production root, a root already owned by another process, and a workspace-contained data root. A newly generated fixture marker is required; opening an arbitrary existing Hermetrix root is not an H1 option.

Only the fixture driver initializes project rows and trusted binding configuration. An incoming assignment cannot create a project, change RootPath, register a repository, fetch a repository or configure a provider.

## 3. Exact proposed files/modules

All paths below are relative to D:/Projects/harness/hermetrix-harness. This is a future change list; only this proposal is created now.

### NEW — runtime/proof code

| Path | Responsibility |
|---|---|
| cmd/hermetrix-h1/main.go | Fixture-only entry point; explicit isolated-root checks; no runServe composition. |
| internal/agentplatform/contract.go | Small manual wire types, nullable fields, ACK, embedded schema registry/version dispatch. |
| internal/agentplatform/validate.go | Strict decoding, semantic conditions, scope/identity/criteria/digest/signature checks. |
| internal/agentplatform/canonical.go | Single JCS encoding boundary and digest/signature input construction. |
| internal/agentplatform/binding.go | Trusted node-local binding lookup and canonical repository/worktree verification. |
| internal/agentplatform/intake.go | Transactional assignment dedup and task linking. |
| internal/agentplatform/observe.go | Serialized observation, read-candidate filtering and record-only selection. |
| internal/agentplatform/projection.go | Existing CAS/artifacts projection, truthful RunUpdate, atomic outbox staging. |
| internal/agentplatform/outbox.go | Bounded delivery, persisted ACK and exact-byte recovery. |
| internal/agentplatform/fixture/receiver.go | In-process, authenticated fixture receiver with independent durable SQLite receipt state; no dispatch dependencies. |
| internal/product/project_binding.go | Narrow exported read-only root/confinement helper calling existing private resolver functions. |
| internal/taskengine/create_tx.go | Shared task-creation implementation that can participate in the caller's SQL transaction. |
| internal/store/agent_platform.go | Additive schemaV49 definitions; no Pi schema. |

### NEW — schema/fixture artifacts

Under internal/agentplatform/schemas/: manifest.json, TaskAssignment.json, AccessScope.json, AssignmentAuthorizationClaim.json, EvidenceRef.json, RunUpdate.json, ProgressFacts.json, VerificationEvidence.json, ManagedEnvelope.json, Signature.json, Timestamp.json and Ack.json.

Under internal/agentplatform/testdata/: assignment-read.json, assignment-none.json, readonly-claim.json and contract-vectors.json. Negative fixtures can be generated by targeted mutation in tests instead of maintaining many copied JSON files. Fixture signing keys are test-only; no production credentials are copied.

### NEW — tests

- cmd/hermetrix-h1/main_test.go
- internal/agentplatform/contract_test.go
- internal/agentplatform/binding_test.go
- internal/agentplatform/intake_test.go
- internal/agentplatform/observe_test.go
- internal/agentplatform/projection_test.go
- internal/agentplatform/outbox_test.go
- internal/agentplatform/safety_test.go
- internal/agentplatform/fixture/receiver_test.go
- internal/taskengine/create_tx_test.go
- internal/product/project_binding_test.go
- internal/store/agent_platform_test.go

### MODIFY

| Existing path | Bounded change |
|---|---|
| internal/taskengine/service.go | Keep Create behavior/API; delegate common validated insertion to the transaction-capable helper. |
| internal/store/store.go | Register next additive migration and advance CurrentSchemaVersion after the migration succeeds. |
| go.mod / go.sum | Only if a verified RFC 8785 encoder dependency is required. Existing jsonschema/v6 is already available. Dependency selection/version pinning occurs during implementation, not by inventing an encoder here. |

### DO NOT TOUCH

Task decision semantics in internal/taskengine/decision.go; internal/taskcoord/decision.go and service.go; effect/lease/recovery behavior in internal/taskengine/execution.go; Context Compiler and StepPacket; provider/MCP/agent execution; workspace mutation and command execution; existing blob storage behavior; cmd/hermetrix/main.go; web/UI/Discord; Pi repositories/configuration; production databases/workspaces.

No H1 endpoint, automatic background worker or managed capability advertisement is enabled in the normal application.

## 4. Data model

Reuse the existing Hermetrix SQLite connection and existing CAS. All new Platform keys are scoped by a trusted configuration value platform_id, never by an untrusted claimed peer name alone.

Four additive client tables:

| Table | Columns and constraints |
|---|---|
| agent_platform_bindings | binding_id TEXT PK; platform_id, node_id, repository_id, worktree_key TEXT NOT NULL; project_id FK projects; owner_principal_id; canonical_root; git_common_dir; git_worktree_dir; repository_fingerprint; worktree_fingerprint; binding_revision INTEGER >=1; enabled INTEGER; created_at, updated_at. UNIQUE(platform_id,node_id,repository_id,worktree_key). worktree_key is an internal lookup key: empty string represents absent Platform worktree; never emitted as an invented ID. |
| agent_platform_assignments | platform_id, assignment_id, assignment_revision INTEGER >=1 composite PK; platform_task_id; platform_run_id; node_id; agent_id; binding_id FK; binding_revision; harness_task_id FK durable_tasks; payload_digest; immutable_envelope BLOB; accepted_claim BLOB; access TEXT CHECK IN ('none','read'); intake_state CHECK IN ('projected','observed'); created_at. H1 UNIQUE(platform_id,assignment_id), UNIQUE(platform_id,platform_task_id), UNIQUE(platform_id,platform_run_id), UNIQUE(harness_task_id). These explicitly limit H1 to one accepted revision and one task projection per logical identity. |
| agent_platform_streams | platform_id, platform_run_id composite PK; assignment_id, assignment_revision composite FK to assignment; next_sequence INTEGER >=1, default 1; acked_sequence INTEGER >=0, default 0; updated_at. This is a transport cursor, not a run lifecycle or a Harness run. |
| agent_platform_outbox | platform_id, platform_run_id, sequence composite PK/FK; observation_key; idempotency_key; payload_digest; immutable_envelope BLOB; projection_artifact_id FK artifacts; created_at; delivery_status; attempts INTEGER >=0; next_attempt_at nullable; last_error nullable; acked_at nullable. UNIQUE(platform_id,idempotency_key), UNIQUE(platform_id,platform_run_id,observation_key). Index(delivery_status,next_attempt_at,created_at). |

Immutable assignment/outbox columns cannot be rewritten by delivery code. Enforce through narrowly scoped updates and DB triggers rejecting changes to committed identity/payload/evidence columns. Mutable delivery bookkeeping is not inside the signed payload. No automatic deletion of ACKed evidence/outbox records in H1.

Existing artifacts stores each projection's ID, project/owner, kind=platform_projection, MIME application/json, blob_ref, checksum, byte_size and private/export-deny metadata. It does not store another copy of the evidence payload. The outbox references the artifact; the blob remains in the existing CAS.

The fixture receiver has its own receiver.db outside the workspace, with:

- received_updates(platform_id, platform_run_id, sequence PK scope, payload_digest, immutable_envelope, received_at), UNIQUE(platform_id,idempotency_key).
- receive_cursors(platform_id, platform_run_id PK, acked_sequence).

Those fixture tables emulate future Pi receipt persistence only; they are not Pi migrations and never reach a production database.

## 5. Contract mapping and types

Use manual Go structs for this small subset, validated against embedded Draft 2020-12 schemas and explicit semantic checks. Do not rely on Go zero values to stand in for absent/null run, worktree, generation or plan fields. Preserve exact incoming bytes for audit and canonical JSON for comparison; reject duplicate JSON object keys and unsupported numeric forms before hashing.

The effective schema registry is derived from the locked base plus managed amendments. manifest.json records source hashes, source sections, message/revision pairs, effective schema hashes and fixture profile. No max-version guessing or network schema resolution.

H1 fixture manifest proposes TaskAssignment revision 2 from the base and RunUpdate revision 3 from the managed fixture, with the amendments compiled into the effective profile. These are fixture-supported pairs, not a claim that a live Pi registry has published them. A future real peer must advertise exactly matching supported definitions/pairs; otherwise fail version negotiation.

Supporting wire types are necessary even though the primary flow has only TaskAssignment and RunUpdate: ManagedEnvelope, Signature, Timestamp, ProgressFacts, EvidenceRef, VerificationEvidence, ACK and the none/read subset of AssignmentAuthorizationClaim. This is not implementation of distributed write authorization.

| Platform field | Harness mapping / validation |
|---|---|
| task_id | platform_task_id cross-reference; never supplied as Task.ID. |
| title | CreateTaskInput.Title after nonempty/schema validation. |
| goal | CreateTaskInput.Objective; do not rewrite original_request. |
| original_request | Preserve verbatim in existing durable task input. |
| acceptance_criteria | Existing []taskengine.Criterion, stable IDs preserved; nonempty, unique, maximum 50; no index-generated IDs. |
| constraints | Existing requirement constraints; accepted unsupported context is not silently fetched. |
| node_id / agent_id | Must equal configured local node and managed-client identity. |
| repository/worktree | Local binding only; check top-level, AccessScope and credential effective targets agree. |
| assignment_id / assignment_revision | Immutable Platform cross-reference; no arithmetic relation to Task.Revision. |
| platform_run_id | Transport stream identity only. No task_runs insert. |
| access_scope | Closed enum/objects; H1 accepts only none/read; non-null write authority rejected. |
| authorization credential | Separate renewable read/none credential, verified in fixture trust space; not inside immutable task requirement digest. |
| context reference requiring ContextPack | H1 returns a local unsupported-feature result before task creation; no full ContextPack implementation. |
| contract_version / schema_revision | Exact registered pair; unsupported input rejected before writes. |

JCS must be RFC 8785, including UTF-16 key order and numeric rules; encoding/json output is not asserted to be a general JCS implementation. Golden vectors cover Unicode, escaping, number boundaries, duplicate keys and signature-value exclusion.

Fixture profile uses locally generated test Ed25519 keys, explicit trusted key IDs and the locked signature domains. Key material and trust manifests live only in the fixture root. This tests authentication without a real Pi and cannot mint a production write grant. No unsigned network ingress or production authentication bypass is proposed.

## 6. Assignment intake

Proposed ordering:

1. Enforce byte/count bounds before decoding; validate exact schema/version, allowed field policy and required identity/content.
2. Recompute payload_digest over locked canonical payload; verify enclosing signature, sender key/trust identity and separate none/read credential integrity/bindings. Enforce credential validity time for new admission; authenticated exact already-admitted history follows the replay rule below. Never use a payload-supplied public key as trust.
3. Reject access=write and every non-null authority scope/generation, even if its fixture signature is valid. Reject node/agent/target mismatches, invalid criteria or unsupported required features.
4. Resolve trusted local binding and pin its binding_revision.
5. Under the serialized intake mutex, look up the accepted assignment revision/digest.
6. Same revision and digest: return the existing task and observation/outbox identity; create nothing and do not rerun decision selection.
7. Same revision, different digest: reject immutable-payload conflict; leave accepted state untouched.
8. Different revision or a second assignment for an already-bound Platform task/run: explicit H1 unsupported-supersession/reassignment result, with no state change. Full supersession is later work under the existing contract.
9. For a new valid input, transaction A creates the existing task and cross-reference/cursor atomically. Commit before reporting accepted.
10. If projected but not yet observed after a crash, resume local observation staging only. If already observed, resume outbox delivery only.

Local H1 restriction codes are adapter diagnostics; map outbound errors into the contract's structured error categories. Do not extend the locked wire taxonomy casually.

Identical already-admitted history must not create fresh authority when credentials later expire. H1 may return its durable receipt for an authenticated exact replay; expiration prevents new admission, not redispatch of previously stored observations. No replay path grants write authority.

## 7. Node-local repository/worktree binding

Trusted local registration maps (platform_id,node_id,repository_id,optional worktree_id) to binding_id → projects.id → Project.RootPath. Pi IDs never become filesystem paths.

For the locked H1 none/read profile, use null/absent wire worktree identity as specified; resolve it through an explicit local repository-default binding, not the current directory. Do not invent a Platform worktree ID to populate a field. Validate any supplied worktree identity against the effective schema first; unknown/mismatched bindings always fail closed. Unit tests also cover explicit worktree binding lookup so a future permitted profile cannot fall back to another checkout.

Binding verification:

- Load project under the configured local owner principal; no ownership fallback.
- Use the existing canonical project-root resolver through the new read-only helper. Compare canonical paths and filesystem identity, not case-sensitive string prefixes.
- Read Git metadata locally, without invoking git or any shell. Support a .git directory or a .git pointer plus commondir for a registered linked worktree. Canonical Git/common/worktree metadata must match the trusted registration. A linked worktree may legitimately reference a pre-registered Git directory outside its code directory; do not treat arbitrary unregistered pointers as equivalent.
- Repository/worktree fingerprints identify the local registration and Git directory identity, not mutable HEAD contents. A normal branch/commit change does not remap a repository; a different checkout, symlink/junction target or Git directory does.
- Reject missing directories, unknown registrations, ambiguous default bindings, project-root drift, reparse-point changes and identity mismatches. No clone, checkout, fetch, mkdir or repair.
- Revalidate immediately before accepting/observing. Pin binding_revision to detect local reconfiguration. H1 performs no workspace content operation, so it cannot turn a later path race into a write.
- AccessScope.path_prefixes are advisory and cannot widen the existing confinement boundary. Incoming absolute foreign paths never override a binding.

Fixture setup creates a tiny local repository/worktree layout before the measurement baseline. The incoming task cannot change that setup.

## 8. Atomic task cross-reference

The existing Service.Create starts its own transaction; calling it and then inserting a cross-reference is not crash-safe. A crash between them would leave an orphan task and allow replay to create another.

Propose CreateInTx(ctx, tx, input) as a narrow taskengine API, returning the minted local task ID/revision without committing. Both it and existing Create delegate to the same validation/insertion logic. This is transaction composition, not a second task engine.

Transaction A:

~~~text
check assignment/idempotency uniqueness
→ validate project/principal using the same transaction
→ Task Engine inserts draft + requirement revision using local identity.New IDs
→ insert Platform assignment cross-reference
→ insert Platform transport cursor
→ COMMIT
~~~

Store.Open sets MaxOpenConns(1). The helper MUST NOT call s.Get, OwnerPrincipalID through s.store.DB, or another DB-level query while holding tx; use tx-bound queries and fetch the normal Task only after commit. Existing Create must retain ownership, egress and criteria behavior in regression tests.

The incoming revision need not be 1; the first locally accepted assignment can carry a higher Platform revision while the newly created Harness task starts at local revision 1. No replacement or fabricated Harness run is created. H1 is not allowed to attach arbitrary pre-existing user tasks containing effects/plans.

## 9. Decision-only boundary

Existing DecisionEngine.Decide returns DecisionResult only; it has no executor capability. Preserve this boundary.

H1 algorithm:

1. Ask the existing Task Engine for DecideNextAction at the exact committed revision; retain its actual CompactState and generated CandidateAction list.
2. Before filtering, stop observation as blocked if unresolved effects or unexpected non-H1 task state are present. Never hide a reconciliation/finish gate by removing its candidate.
3. Keep only existing candidates with Risk=read, RequiresPolicy=false and Type in the H1 allowlist inspect_file/search_repository/retrieve_memory. Do not manufacture a candidate or change candidate metadata.
4. Call the existing RuleDecision.Decide on that subset. For a new unplanned draft this selects the existing retrieve_memory candidate through its established fallback. The ordinary unrestricted ask_planner recommendation can be recorded for comparison, not executed.
5. Check selected ID belongs to the supplied subset; serialize a local observation receipt with selection, backend and measured latency.
6. Return data. There is no dispatch callback, executor field, tool bridge or next-action switch after selection.

Even retrieve_memory is NOT called. H1 records the recommendation only. access=none does not authorize reading repository content; selecting an unexecuted recommendation does not widen it.

Production H1 construction binds taskengine.RuleDecision explicitly behind the existing DecisionEngine interface. It must not accept an arbitrary plugin/provider-backed engine. Do not call taskcoord.ShadowDecision, BonsaiDecision.Decide, an admitted-provider selector, AutoPlan or agent.RunTurn: they can perform provider requests.

The H1 controller receives a narrow task view exposing only needed reads/decision observation; task creation stays in intake. Keep concrete execution-capable task/product/provider objects out of the controller. Constructors must not offer a hidden dispatch option.

## 10. Factual RunUpdate projector and evidence

H1's fresh local task remains draft without plan/run/attempts. A selected recommendation is not executed work and does not complete the task.

| RunUpdate fact | Initial H1 value / source |
|---|---|
| platform_run_id | Persisted accepted assignment cross-reference. |
| sequence | Allocated only in the outbox staging transaction. |
| harness_run_ref | Null/absent; no local run exists. |
| execution_status | accepted; never completed merely because H1 demonstration or delivery succeeded. |
| phase | initializing. |
| completed_steps / total_steps | 0 / 0 from the actual unplanned task. |
| attempt_count | 0 because no committed step attempt exists; do not export task-wide budget counts. |
| current_step_id | null. |
| plan_revision | Absent while active local plan revision is 0; do not invent revision 1. |
| failed_checks / verification | 0 and absent/empty for this fresh task; no RecordValidation call and no pass inferred from a selected candidate. |
| authority fields | Null/absent for none/read. |
| projection_ref | Existing artifacts/CAS reference to a durable immutable source snapshot. |
| last_action | Omit; a proposed candidate must not be reported as an executed action. |
| created_at | Fixed observation time, serialized with required UTC millisecond precision. |

H1 does not generalize arbitrary active-task status projection. Unexpected runs/attempts/plans or validations on its isolated task fail the narrow fixture precondition. Future broader projectors must implement the already-locked active-plan attempt and fail-only/current-subject semantics.

Coherent source: one data-root owner, one serialized H1 event loop, no production web server/background writers, and only H1-created tasks. Hold the H1 observation mutex across state reads and staging; verify the exact task and binding revisions. Under that ownership model, the existing multi-query CompactState reads see an unchanged committed source. This is intentionally not a claim that a revision check alone makes concurrent production reads atomic; later shared-runtime integration needs a transaction-scoped reader.

Create snapshot bytes containing source task/requirement/plan revisions, factual counters, actual candidate set and recorded selection, plus observation provenance. It may reference local evidence IDs; it must not contain keys, credentials or full workspace content. Avoid self-reference: snapshot bytes do not include their own digest or the resulting projection_ref.

Persist with existing store.Blobs.Put; construct EvidenceRef with type=artifact, local artifact identity, origin node and content_digest=sha256:<blob_ref>. Then transaction B atomically inserts:

- existing artifacts metadata with private/export-deny defaults;
- outbox immutable envelope referencing the artifact;
- sequence cursor increment;
- assignment intake_state=observed.

Use a deterministic local observation_key for initial assignment observation. A concurrent/retried stage sees the existing key and returns its envelope. No new seq/time/signature is generated for a committed replay.

CAS-before-SQL can leave an unreferenced blob after a crash; it must never leave a committed reference to unwritten content. Do not implement a new CAS/GC system in H1. Existing artifacts representation remains the metadata anchor. A projection produced before a failed SQL commit is not considered delivered/committed.

## 11. Durable outbox, fixture receiver and ACK

The outbox is authoritative for sent bytes. Sender first commits transaction B, then calls a small Send(ctx, immutableEnvelopeBytes) → ACK fixture interface. Transport code has access to outbox records and fixture transport only, not the task engine or decision controller.

Receiver steps:

1. Validate envelope/schema/version/digest/signature and expected sender in fixture trust space.
2. In one receiver transaction, lookup (platform_run_id,sequence).
3. Same digest: no-op; different digest: SEQUENCE_CONFLICT, no overwrite/ACK advancement.
4. Insert a new immutable receipt if absent; advance only through contiguous received sequences.
5. Commit receive state before returning {platform_run_id, acked_seq}.

Sender validates authenticated receiver/context, matching PlatformRun and 0 ≤ acked_seq ≤ highest committed local sequence. Unknown-run or ahead-of-stream ACKs are rejected. Stale lower ACKs are harmless no-ops. Persist the monotonically increasing ACK cursor and delivery records in one transaction.

Sequences are allocated and incremented with outbox insertion in one transaction; crashes must not reserve holes. The cursor never resets with a process/local-run restart. Out-of-order receiver tests use 1,3,2 and must return ACK 1,1,3.

On crash/timeout/ACK loss: reopen the fixture roots and resend stored bytes above acked_sequence. Do not rebuild payloads or reread/reselect task decisions. Retry scheduling is bounded, with injected clock for tests; exhaustion retains the unACKed record. Delivery failure does not change accepted execution_status or fail the durable task.

Local fixture transport is an in-process call into a separate durable DB. It exercises byte validation, ordering, dedup and receipt-before-ACK, not real Pi HTTP/MCP, TLS, routing or credentials. Real transport is explicitly deferred; no Kafka/Redis/broker and no HTTP listener are needed for H1.

## 12. Pi dependencies

| Dependency | Classification | H1 treatment |
|---|---|---|
| Locked schemas, digest/signature/ACK semantics | FIXTURE_OK_FOR_H1 | Already supplied; embed effective definitions and known vectors. |
| Node/agent/repository/worktree IDs | FIXTURE_OK_FOR_H1 | Isolated fixture namespace with explicit local mapping; never authoritative production IDs. |
| Assignment issuance | FIXTURE_OK_FOR_H1 | Signed local fixture and read/none credential. |
| Key discovery/trust | FIXTURE_OK_FOR_H1 | Local test trust manifest; no real Pi keys required. |
| RunUpdate receiver + durable ACK | FIXTURE_OK_FOR_H1 | Separate fixture SQLite receiver. |
| Exact live Pi supported revision pairs | NOT_NEEDED_UNTIL_LATER | Required before replacing fixture transport, not before H1 proof. |
| Real Pi URL, scoped credential, authentic peer keys, P2 receiver | NOT_NEEDED_UNTIL_LATER | Prerequisites for real transport acceptance. |
| Node/Agent/Session/Run registries and live managed capability advertisement | NOT_NEEDED_UNTIL_LATER | H1 does not register or advertise to production Pi. |
| Distributed write grants/fencing, HANDOFF/CONSULT | NOT_NEEDED_UNTIL_LATER | No implementation and no fake grants. |
| KnowledgeCandidate/full ContextPack/Pi knowledge routing | NOT_NEEDED_UNTIL_LATER | No dependency or retrieval calls. |

REQUIRED_REAL_PI: none for H1. No result from this proof may be reported as successful communication with the Raspberry Pi. Pi P2 may remain unstarted.

## 13. Test plan

These are proposed tests, not tests executed during this planning task.

| Test group | Required cases |
|---|---|
| Wire/authentication | Correct read/none; bad version/revision; duplicate JSON keys; altered payload/digest; wrong key/sender/node/agent; invalid/expired claim for new admission; mismatched effective target; unknown scope capability; missing/duplicate criteria; bounds; non-UTC timestamp. |
| Scope negatives | write assignment rejected before any local task; none/read with scope/generation rejected; required unsupported context rejected; no path authoritative from payload. |
| Binding | Unknown repository/worktree; absent/ambiguous default; different node/owner; root/path/Git identity mismatch; Windows case/junction/symlink aliases; project root drift; valid linked-worktree metadata; no command invocation. |
| Intake atomicity | Same revision/digest returns same task; changed digest conflicts; second revision/reassignment rejected under H1 subset; concurrent duplicate yields one task; crash/rollback between core insert and mapping leaves neither. |
| Decision | Existing real state/candidates; draft yields read-only subset recommendation; selected ID is supplied; non-read/policy candidate cannot be selected; unresolved effects produce blocked observation; no planner/provider calls. |
| Projection | No invented run/plan/attempt/pass; immutable artifact hash; required source binding; no key/path leak; crash before/after CAS and SQL commit; duplicate observation key creates one seq/artifact reference. |
| Delivery | Duplicate, conflict, ACK loss, receiver crash before commit, crash after commit before ACK, sender restart, 1/3/2 gap, wrong-run/ahead/stale ACK, bounded retry exhaustion, exact-byte replay. |
| Recovery safety | projected intake resumes observation once; observed intake resumes only delivery; restart never creates a local run, attempt, effect or provider request. |
| Migration | Fresh root; populated v48 copy; FK/integrity check; repeated Open idempotent; interrupted migration rollback; existing task/evidence unchanged. |

Use existing tests as regression anchors: TestDecisionStateRejectsStaleRevisionAndKeepsActionsBounded, TestDecisionRequiresReconciliationBeforeAnyNewWork, TestDecisionUsesLatestCheckStatus, TestRecoveryMakesDispatchedEffectUncertainAndBlocksReplay, TestExpiredLeaseCannotPlanDispatchOrCompleteEffects, TestMigrationV43ToV48PreservesPopulatedDatabase and TestPutIsContentAddressedAndIntegrityChecked. Preserve their behavior; do not weaken them.

## 14. Safety proof and negative-call detection

Layered proof, not only row counts:

1. **Capability boundary:** the H1 observation/delivery constructors receive no executor, shell, provider, MCP client, workspace mutator or production coordinator. Only the trusted composition point owns task creation and persistence.
2. **Source guard test:** inspect resolved Go calls in H1 production packages/composition and reject references to PlanEffect, DispatchEffect, BeginRun, BeginStepAttempt, StartCommand, WriteProjectFile(s), workspace Apply, providers.StreamChat, MCP ExecuteCapability, agent turns, os/exec and production recovery/dispatch entry points. Allow only the documented read-only product helper. Reject a concrete execution-service escape into observation/delivery.
3. **Fail-on-call canaries:** test adapters expose traps for forbidden methods that immediately fail the test. Any future constructor/facade expansion that wires a dispatcher/provider/tool must hit these traps; add negative tests that deliberately invoke each trap to prove detection works. No silent no-op fake dispatcher.
4. **Database backstop:** in the scratch DB, install test-only rejecting triggers for inserts/updates/deletes on task_runs, task_step_attempts, task_effect_intents, background_jobs, task_planner_runs, task_code_proposals and file_mutation_intents during measured H1 processing. This catches indirect writes even when a helper path escapes a direct-call guard.
5. **External boundary counters:** provider/tool/command/workspace-write invocation counters stay zero. Tests must fail on an attempted forbidden call even if it returns before creating a row.
6. **Workspace receipt:** snapshot every file's relative path, type/symlink target and SHA-256 before/after, including Git metadata; compare names, additions/deletions and bytes. Do not use Git status as the only proof.
7. **Exact replay receipt:** assert decision invocation count unchanged during outbox replay, not merely effect count unchanged.

State counts alone cannot prove that a forbidden method was never attempted; call-boundary guards and canaries are required alongside database/filesystem receipts. An ordinary read-only recommendation does not bypass this safety boundary.

## 15. Migration and rollback

Propose additive schemaV49 in the existing migration transaction; reserve the actual next number only when implementation begins, since other work may advance schema 48. New tables reference existing projects/tasks/artifacts; no rewrite of existing task state or IDs.

Migration is tested only on disposable roots and copied v48 fixture databases. Do not run the H1 binary against the live data root. No Pi migration.

Rollback for H1 is stop the fixture command and retain its receipt directory for inspection, then recreate a fresh fixture root if needed. For a copied pre-migration test database, restore the whole pre-migration snapshot together with its CAS. Do not delete individual cross-reference rows and leave tasks orphaned; do not rewrite PRAGMA user_version to fake a downgrade.

Current migrate writes CurrentSchemaVersion at the end and is not an automatic downgrade tool. Never open the upgraded fixture root with an old binary as a rollback procedure. Source rollback alone does not undo a migrated DB.

No background delivery survives outside the fixture process. The normal application remains without an H1 entry point or enabled worker.

## 16. Implementation sequence after proposal approval

1. Freeze the effective schema/fixture manifest and fixture trust profile; reuse existing JSON Schema library; select and test a proper JCS encoder. Establish failing boundary/negative tests first.
2. Add migration and disposable-root composition. Seed project/binding/keys outside measured intake; verify no production root can open.
3. Extract transaction-shared Task Engine creation with existing Create regression tests. Add atomic dedup/linking and crash tests.
4. Add node-local canonical binding and none/read validation; reject every write assignment.
5. Wire existing read-only state/candidates and RuleDecision through the record-only path; verify all forbidden-call traps.
6. Stage projection in existing CAS/artifacts and atomic outbox transaction. Verify crash boundaries and truthful accepted/initializing payload.
7. Add durable fixture receiver, ACK validation, bounded replay/restart tests.
8. Run the isolated demonstration and capture receipts. Stop before real Pi transport or automatic action execution.

Proposed verification commands after implementation: targeted go test for agentplatform/fixture/command/taskengine/store/product, race tests for the new adapter, and existing decision/effect-recovery regressions. Execute broader checks only for changed shared code. No H1 code or tests exist from this proposal, and none are claimed passing now.

## 17. Runnable demonstration and acceptance criteria

Proposed future command:

~~~text
go run ./cmd/hermetrix-h1 demo --fixture-only
~~~

The command must create a new temporary demo directory and report its path. Under it: sender data root, receiver DB, fixture trust/key material, a tiny prepared workspace, and receipts. The workspace and persistence directories are siblings. Fixture setup is complete before the before/after measurement begins.

Demonstration:

1. Load signed read assignment and separate valid read credential.
2. Validate → bind local root → atomically create task and cross-reference.
3. Build actual CompactState; filter existing read candidates; RuleDecision selects retrieve_memory without invoking it.
4. Commit immutable source artifact, accepted/initializing RunUpdate and sequence 1.
5. Receiver durably accepts → ACK 1; sender persists ACK.
6. Re-submit identical assignment: same task, same initial observation, no additional sequence.
7. Simulate ACK loss/restart and replay the exact envelope: one receiver receipt, same ACK, no decision re-execution.
8. Send changed digest for same sequence/revision: reject; accepted state unchanged.
9. Produce an evidence receipt describing the flow, counters, identities/digests and negative-case results.

H1 passes only when all are true:

- Exactly one local task/requirement projection for the accepted assignment; Platform and Harness IDs remain distinct.
- No task_runs or task_step_attempts created; harness_run_ref remains absent/null.
- effect_count_before == effect_count_after.
- background_job/command_count_before == background_job/command_count_after.
- planner/provider/tool/workspace-write invocation counts are all zero.
- Workspace file set, link targets and all bytes are unchanged.
- Projection artifact and outbox are durable and hash-verifiable; task remains draft/accepted, not falsely completed.
- ACK is highest contiguous sequence; duplicate replay creates no new receipt/effect/task/decision.
- All specified write/identity/path/digest negatives fail closed without partial task creation.
- Receipt labels transport=fixture, receiver=local, real_pi_contacted=false, candidate_executed=false.
- Restarts preserve sequence, provenance and committed bytes.

Observability is deliberately small: structured events for validation outcome/duration, binding result/revision, task cross-reference, decision latency/backend/selected candidate, projection artifact digest, sequence/delivery attempt/ACK and replay/conflict. Include correlation IDs; redact credentials, raw requests and absolute paths from wire/ordinary logs. Selected-candidate details remain local evidence, not Pi-required fields. Aggregate counters can be derived from the fixture receipt; no metrics server or new telemetry platform.

## 18. Risks and open questions

No unanswered Pi dependency blocks this proposal. The following are explicit implementation decisions/limits, not contract revisions:

| Item | Proposed H1 position |
|---|---|
| Live Pi schema revision registry and real credentials | Defer until real transport. Fixture manifest is explicit and isolated; never assume compatibility from a revision maximum. |
| RFC 8785 encoder | No general JCS implementation found in the inspected code. Verify a library and golden vectors during step 1; existing packet hashes are not substitutes. |
| none/read optional worktree representation | Use the locked null/absent profile and an explicit trusted repository-default binding. Never invent IDs or reinterpret a foreign path. |
| Concurrent production state | Out of H1. Serialized isolated root makes the initial snapshot coherent; future shared-runtime projection needs a transaction-scoped reader. |
| Task Create transaction extraction | Small shared-code risk; owner/criteria/egress checks and one-connection behavior must remain identical. |
| CAS/SQL crash gap | Safe unreferenced CAS object may remain; no committed outbox may reference missing evidence. Retain for normal future maintenance; no new GC in H1. |
| Real local model proof | Bonsai performs provider calls, so excluded here. RuleDecision proves the existing interface/pipeline; it does not prove model quality or inference integration. |
| Existing arbitrary tasks / supersession / renewal | H1 rejects unsupported new lifecycle operations rather than silently changing old tasks. Full lifecycle support is later implementation of the locked contract. |
| Windows filesystem identity | Test junctions/symlinks/linked worktrees and case handling. H1 never runs git or performs a workspace action to repair a binding. |
| Meaning of success | Successful proposal review and later successful H1 fixture flow are not task completion, real Pi integration, write authorization or production readiness. |

This document is ready for review as an implementation plan. Only this proposal was written; no H1 code, Harness runtime change, Pi change, deployment or external action was performed.

H1_PROPOSAL_STATUS: READY_FOR_REVIEW
