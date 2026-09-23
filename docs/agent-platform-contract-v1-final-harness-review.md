# Agent Platform Contract v1 — Final Hermetrix Harness Compatibility Review

Date: 2026-09-21
Scope: independent contract review against the current working tree. No implementation, H2, migration, deployment, service restart, or production modification.

The final contract is **not ready to lock**. The reconciliation correctly describes several improvements, but does not establish that every earlier finding was resolved. Required schema fields, release/authorization semantics, and recovery rules still differ from its claims.

The architecture remains viable. This review does not require a replacement Task Engine, DecisionEngine, Context Compiler, or evidence store. Contract defects are distinguished from adapters which simply have not been implemented yet.

## 1. Inputs and actual implementation evidence

| Input | Identity |
|---|---|
| Final contract | [agent-platform-contract-v1.md](C:/Users/ZP2E0/Downloads/agent-platform-contract-v1.md), 1,099 lines |
| Reconciliation | [agent-platform-contract-v1-reconciliation.md](C:/Users/ZP2E0/Downloads/agent-platform-contract-v1-reconciliation.md) |
| Previous review | [agent-platform-contract-v1-harness-review.md](D:/Projects/harness/hermetrix-harness/docs/agent-platform-contract-v1-harness-review.md), especially §§4–14 and §16 |
| Harness | D:/Projects/harness/hermetrix-harness; HEAD 08fcac4ab8a7c367da07d773b5d9dbde0f049b1d; existing modified/untracked source was included |

Input SHA-256:

~~~text
final contract:
ebed8f2dea6bd2b4464e75e6a1a039a92ce1ad2dcff2faf37dcf6a968ba790e4
reconciliation:
a37bc458f251955cbd5f46012e0d3ec495107437755dc5b7b814bbafd9d829e9
~~~

Pi source/database was not supplied. Its production SHA/schema/deployment assertions are not independently verified here. The reconciliation's “21 findings” regroup earlier feedback; it is not a ledger of every required change. Its validation section also mentions JSON examples absent from the supplied final file.

Resolution vocabulary: RESOLVED / PARTIALLY_RESOLVED / NOT_RESOLVED / NEW_CONFLICT.
Disposition: ACCEPT / ACCEPT_WITH_CHANGE / REJECT / NOT_NEEDED_FOR_HARNESS.
Action: REQUIRED CHANGE / RECOMMENDED CHANGE / OPTIONAL IMPROVEMENT / NO CHANGE.
Core impact: UNCHANGED / ADAPTER_REQUIRED / CORE_CHANGE_REQUIRED. A narrow new lifecycle API in Task Engine counts as core integration, even without a redesign.

| Evidence ID | Actual source and observation |
|---|---|
| H1 | [models.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/models.go:32), [service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:26): Criterion has ID/Description; Create needs nonblank title/objective/request/actor, validates local project ownership, and mints its own task ID. |
| H2 | [service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:646): nonempty criteria, unique nonblank IDs/descriptions. [planner.go](D:/Projects/harness/hermetrix-harness/internal/worker/planner.go:67): 32-item criteria/constraints/unknowns limits and 64 KiB serialized planner input. |
| H3 | [service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:88): immutable requirement revisions and optimistic revision checks invalidate the active plan. |
| H4 | [execution.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:144): BeginRun mints run/token; RenewRunLease at 205; RecoverExpiredRun at 224. [store.go](D:/Projects/harness/hermetrix-harness/internal/store/store.go:1601): one running run per **task**, not per worktree. |
| H5 | [execution.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:450): PlanEffect and DispatchEffect at 525 check live run/token/local generation, step/plan. Observe/Reconcile at 557/564 are separate from dispatch. |
| H6 | [service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:413): Checkpoint does not release a lease. RecoverInterrupted at 457 abandons planned effects and marks dispatched effects uncertain, without replay. |
| H7 | [commands.go](D:/Projects/harness/hermetrix-harness/internal/product/commands.go:59): StartCommand creates an asynchronous durable job with context detached from request cancellation. [taskcoord/service.go](D:/Projects/harness/hermetrix-harness/internal/taskcoord/service.go:1178): reconciliation refuses to finish a queued/running command job. |
| H8 | [progress.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/progress.go:32), [decision.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:112): observational projections from durable records; neither is a Platform outbox nor an atomic multi-table source snapshot. |
| H9 | [service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:337): validation needs task/check/subject revision; pass requires evidence. CompleteTask at 378 checks active-requirement evidence. |
| H10 | [types.go](D:/Projects/harness/hermetrix-harness/internal/context/types.go:35), [compiler.go](D:/Projects/harness/hermetrix-harness/internal/context/compiler.go:46): Fragment supports provenance/trust/version/priority/metadata. [packet.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/packet.go:24): StepPacket has no ContextPack slot; hash covers checkpoint/evidence fields; 128 KiB ceiling. |
| H11 | [learning/service.go](D:/Projects/harness/hermetrix-harness/internal/learning/service.go:76): StageTrigger accepts the source SQL transaction; DrainPending creates idempotent learning jobs. This is a useful sibling durability pattern, not an existing RunUpdate transport. |
| H12 | [product/models.go](D:/Projects/harness/hermetrix-harness/internal/product/models.go:191), [product/service.go](D:/Projects/harness/hermetrix-harness/internal/product/service.go:338): artifact blob/checksum/lineage. Job and effect rows have mutable lifecycle state. Events, validations, proposals and receipts provide evidence. |
| H13 | [learning/models.go](D:/Projects/harness/hermetrix-harness/internal/learning/models.go:30): Digest contains receipts, VerifiedBy, artifacts/redactions. [skills/service.go](D:/Projects/harness/hermetrix-harness/internal/skills/service.go:388): promotion is separate. |
| H14 | [decision.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:77), [taskcoord/decision.go](D:/Projects/harness/hermetrix-harness/internal/taskcoord/decision.go:49): DecisionEngine/Bonsai select without authority to execute. |
| H15 | [product/models.go](D:/Projects/harness/hermetrix-harness/internal/product/models.go:5), [registry.go](D:/Projects/harness/hermetrix-harness/internal/tools/registry.go:198): paths resolve locally. [auth.go](D:/Projects/harness/hermetrix-harness/internal/web/auth.go:23): installation authentication is not a Platform claim verifier. |
| H16 | [mcp/pool.go](D:/Projects/harness/hermetrix-harness/internal/mcp/pool.go:185): automatic reconnect/retry is for catalog listing, never tools/call. |

## 2. Re-check every previous required change

This table follows the **15 required changes in previous §16**, including omitted subparts.

| Prior finding | Verified final content | Resolution | Remaining action |
|---|---|---|---|
| R01 Task identity/content, criteria, egress | S3 adds task_id/title/request and structured criteria, but accepts empty/duplicate IDs; egress mapping absent. | PARTIALLY_RESOLVED | Nonempty/unique criteria; explicit local-only default and separate audited remote/export approval. A new egress field is not essential if this restriction is explicit. |
| R02 Required key/digest and canonicalization | S3/S4/S5/S8 still omit both from required. “Canonical JSON” lacks encoding/self-exclusion rules. | NOT_RESOLVED | Required arrays, exact bytes/domain and duplicate receipt rules. |
| R03 Verifiable expiring fenced claim | S10 adds identity/scope/generation/expiry/integrity; write target/equality/local dispatch checks incomplete. | PARTIALLY_RESOLVED | Bind effective target/scope and verify at effect boundary. Crypto selection may remain open. |
| R04 Target equality/write worktree | Three duplicated target locations; all permit missing write worktree. | NOT_RESOLVED | One authoritative target or explicit equality; nonblank worktree for write. |
| R05 Factual progress/source/cancelled | Structured progress/UI percent added; source revision and cancelled mapping absent. | PARTIALLY_RESOLVED | Source snapshot identity, full status/phase/count semantics. |
| R06 Delivery independent | §8 corrected; §9 still permits NO_PROGRESS/STUCK on retry exhaustion; delivery state sits in immutable S4. | PARTIALLY_RESOLVED | No fabricated task escalation; separate live delivery bookkeeping. |
| R07 Receipt/ACK/direction | Two-field ACK prose and re-PUSH added; no ACK/acceptance schema or ACK query response contract. | PARTIALLY_RESOLVED | Endpoint ownership, ACK lookup, atomic sequence/cursor, receipt/rejection. |
| R08 HANDOFF/fencing | Phases added; pending effects may precede regrant; status alone is release; new assignment changes fence scope. | PARTIALLY_RESOLVED; NEW_CONFLICT | Quiescence or reconciliation-only transfer; stable resource scope and release proof. |
| R09 Escalation identity/evidence | Adds assignment revision/generation only; optional evidence, unbounded context, no correlated release proof. | PARTIALLY_RESOLVED | Mandatory correlation key and bounded evidence bundle can replace many top-level Harness-specific fields. |
| R10 Evidence immutability/type/retrieval | Digest-bound snapshots required; enum still omits effects/fallback; retrieval protocol incomplete. | PARTIALLY_RESOLVED | Explicit artifact subtype fallback and scoped digest-checking resolver. |
| R11 Context pin/provenance/bounds | Pack digest/revision added; assignment pins ID only; priority promised but rejected; authority precedence unspecified. | PARTIALLY_RESOLVED | Immutable selection binding; knowledge/policy separation; align schema/prose. |
| R12 Verification status/subject/evidence | S7 unchanged: evidence-free pass, no check/subject; blocked/unknown unmapped. | NOT_RESOLVED | Exact mapping and revision-bound proof or normative evidence bundle. |
| R13 Candidate verification/provenance | Inert and nullable run/task refs; singular verification/minimal provenance remain. | PARTIALLY_RESOLVED | Per-check records or lossless bundle; reject unsupported verified claims. Array syntax alone is not a blocker. |
| R14 Version/closure contradiction | Generic ignore-unknown removed; negotiation added. All revisions accept any integer ≥1; range/extension encoding undefined. | PARTIALLY_RESOLVED | Original contradiction RESOLVED; exact revision/schema/dependency registry remains. |
| R15 Safe errors/retries | Conflict/stale errors added; INTERNAL universally retryable; uncertain outcome absent. | PARTIALLY_RESOLVED | Separate proven pre-dispatch failures from uncertain mutations; preserve denial and reconciliation. |

Additional earlier findings, including recommendations and required sub-findings:

| Previous location/finding | Resolution | Assessment |
|---|---|---|
| §4.4 nullable local session | RESOLVED | platform_session_id nullable; durable work need not create interactive Session. Separate AgentSession wire schema unnecessary. |
| §4.5 AgentRun lifecycle/release/ACK | PARTIALLY_RESOLVED | Generation/delivery concepts present; acceptance, terminal lifecycle and run cardinality incomplete. Pi table names are not Harness requirements. |
| §§6.1,7 claim expiry/disconnected policy | PARTIALLY_RESOLVED | S10 expiry can supersede optional S1 expiry. Stop-new-writes/renewal behavior still needs definition. |
| §§6.2,10 effect/checkpoint/context/review/proposal coverage | PARTIALLY_RESOLVED | Snapshot rule fixed; define artifact subtype projection. Separate enum values not all essential. |
| §6.2 retrieval endpoint authorization/availability | PARTIALLY_RESOLVED | Origin scope stated; allowlist/discovery/digest verification/unavailable semantics incomplete. |
| §6.3 input size/count bounds | NOT_RESOLVED | No bounded envelopes or negotiation. Exact Hermetrix 32-item limit need not apply to every agent. |
| §6.3 unknowns/expected VCS revision | NOT_RESOLVED | Still recommendations; not independent blockers. |
| §6.4 exporting all local ProgressSnapshot fields | PARTIALLY_RESOLVED | Factual subset is sufficient in principle. No need to export every internal counter/budget. |
| §6.4 needs_escalation/evidence delta | PARTIALLY_RESOLVED | Define observational derivation and snapshot/delta semantics. |
| §§6.5,8 advice identity/timeout | NOT_RESOLVED | CONSULT ownership clear, but correlation/result and no-transfer-on-timeout need definition. Mandatory key can replace escalation_id. |
| §6.6 null context project scope | NOT_RESOLVED | Define null as global/projectless or reject it; no new scope object required. |
| §6.6 source version/admission/digest | PARTIALLY_RESOLVED | path@version/pack digest help; trust/provenance coverage incomplete. Immutable source lookup may carry extra admission details. |
| §6.7 fail evidence requirement | NOT_RESOLVED | Earlier review overstated code: H9 requires evidence for pass, not every fail. This review does not invent a local fail-evidence requirement. |
| §6.8 provenance closure | PARTIALLY_RESOLVED | Candidate closed, EvidenceRef intentionally open. Open non-authoritative metadata is acceptable. |
| §6.8 related_refs/conflict authority | RESOLVED | Pi owns admission/conflicts; supersedes/conflicts hints optional. |
| §6.9 version advertisement | PARTIALLY_RESOLVED | Supported revisions/algorithms added but optional and range syntax undefined. |
| §6.9 expiry/instance/digest/readiness | NOT_RESOLVED | Can be implementation policy with authenticated refresh/fail-closed readiness, not separate lock blockers. |
| §7 transactional sequence/cursor | NOT_RESOLVED | Durable storage stated; atomic projection/gap semantics absent. |
| §7 retain until ACK | PARTIALLY_RESOLVED | Sender retention stated; receiver lifetime+7-day dedup needs replay expiry/tombstones. |
| §8 local-generation increment on release | PARTIALLY_RESOLVED | Pi generation correctly Pi-owned. Local token/state invalidation suffices; counters need not advance identically. |
| §§13–14 digest, UTC, refs, bounds | PARTIALLY_RESOLVED | sha256 prefix fixed; canonicalization/format/registry/limits remain. |
| §14 golden fixtures/schema digests | NOT_RESOLVED | Recommended interoperability evidence; syntax parsing does not establish semantic conformance. |
| §17 canonical MCP alias | NOT_RESOLVED | Optional catalog simplification. |
| §17 candidate admission receipt | NOT_RESOLVED | Useful duplicate/admission visibility; no promotion authority. |
| §17 “thin adapter only” wording | NOT_RESOLVED | §27 repeats it despite no live-run release API. |
| §18 routing budgets, trace IDs, retention hints, typed display commands | NOT_RESOLVED | Optional improvements, not prerequisites. |
| §19 identity, local paths and decision no-change findings | RESOLVED | Preserved, subject to distinct run-cardinality/resource-fencing conflicts below. |

## 3. TaskAssignment projection and revisions

Disposition: **ACCEPT_WITH_CHANGE**.

| Required information | Final state | Harness mapping |
|---|---|---|
| task_id | Required | Platform cross-reference; Create mints its own task ID. |
| title/original_request/goal | Required | Title/OriginalRequest/Objective; reject whitespace-only values. |
| acceptance_criteria[{id,description}] | Required array without minItems/ID uniqueness | Valid entries directly map to Criterion. AC- prefix is compatible but unnecessary; empty/duplicate criteria fail H2. |
| assignment_id/assignment_revision | Required | Separate adapter key; never local Task.Revision/RequirementRevision. |
| platform_run_id | Required | Cross-reference; lifetime issue below. |
| repository_id/worktree_id | Worktree optional/null | Resolve owner-authorized local project/root; write requires unambiguous worktree. |
| access_scope | Required | Intersection with local policy/approvals/root/lease. |
| authority_generation | Required | Persist in binding, never overwrite local LeaseGeneration. |
| authorization_claim | Required | Verify envelope/target binding, not merely its signature. |

Actor can derive from authenticated node/agent-to-local-principal mapping; never trust an arbitrary body identity as local owner. H1 defaults EgressPolicy to local_only and requires explicit actor/reason for remote_allowed. Thus the old demand for a mandatory egress wire field is narrowed: preserve local-only baseline and prohibit implicit remote/export permission; a field is needed only if v1 must request that authority.

Same assignment revision plus same verified digest can return the existing durable projection without Create. Different digest must fail closed. Create itself always mints a new ID: an adapter inbox/mapping and transactional or recoverable acceptance receipt are needed.

Unresolved lifecycle rules:

1. §6 immutable revision conflicts with non-material updates without a revision bump if any digested metadata/issued_at/claim expiry/signature changes. Define immutable execution payload versus renewable authorization envelope, or newly identify each changed envelope. Never exclude authority from integrity.
2. Higher revision “new projection” must quiesce/revoke old write authority before replacement work begins. §13 advances generation only on transfer/revocation. Lower stale revisions must not recreate tasks.
3. §4 mandates 1:1 Platform run ↔ local run. H4/H6 can pause a local run and later BeginRun with a new local ID. Choose sequential local-run cross-refs under a Platform execution, or a protocol obtaining a new Pi run before resume. Disconnected recovery cannot invent Pi-owned IDs.

## 4. RunUpdate from committed facts

Disposition: **ACCEPT_WITH_CHANGE**.

| Field | Actual source | Needed interpretation |
|---|---|---|
| phase | Task.State plus committed adapter lifecycle | Define task vs step phase. Transport is not execution phase. |
| completed_steps | Active-plan completed/skipped steps | Existing compact/packet counts include skipped; define wire meaning. |
| total_steps | Active plan length | Null/absent before plan; no divide by zero; rebase on plan revision. |
| current_step_id | Step.ID | Local opaque ref, distinct from Step.Key; null when none. |
| attempt | Durable attempts | Define ordinal/count and task/run/step scope. Zero before first attempt; S4 rejects zero when sent. |
| failed_checks | Revision-scoped validations | Define current vs historical and whether blocked checks count; CompactState currently includes fail/blocked. |
| blocked_reasons | PauseReason/ProgressSnapshot/reconciliation | Real execution blockers only. |
| execution_status | Committed task/run/adapter state | Complete mapping, especially cancelled and pre-run phases. |
| evidence_refs | Immutable snapshots/receipts | Define snapshot vs delta and source identity. |
| sequence | New outbox | Not task revision, event sequence or lease generation. |
| authority_generation | Accepted assignment binding | Historical generation of observed work, not newly current generation. |
| progress_percent | Optional mathematically derived count | UI-only/null/omitted. Never affects scheduling, safety, completion or handoff. |

A feasible map is draft/ready/running/verifying → working with distinct phase; waiting_for_input → needs_input; paused → blocked with reason; completed/failed directly. **Cancelled has no defined map**: add it or preserve explicit cancellation phase/reason without inventing failure. Escalated/handed_off must derive from durable adapter facts, not Task.State string guessing.

Empty/omitted progress can be a heartbeat if defined; it does not meet a promise of complete factual progress on every update. Source snapshot identity/revision or a normative immutable projection EvidenceRef is missing.

H8's sequential queries are not one coherent transaction. Not every effect/validation changes Task.Revision. A consistent projector/source cursor is necessary; Task.Revision alone is not a universal commit sequence. This belongs in an additive projector after the contract defines its guarantee.

## 5. Execution and transport independence

Disposition: **ACCEPT_WITH_CHANGE**; primary separation **RESOLVED**.

Working + Pi unreachable preserves committed execution state. Delivery failure must not invoke FailAttempt, mark a task failed, consume confirmed-failure budgets or fabricate NO_PROGRESS/STUCK. §9 still permits those escalations following retry exhaustion: restrict them to independent execution evidence or use transport-only health.

S4 puts delivery_status inside immutable (run,sequence,digest) content. Updating in_flight→failed→delivered on the same outbox item changes its digest. Put live delivery bookkeeping outside the immutable message, or explicitly define that wire value as an immutable sample with separate live health. Receiver metadata must not change sender-digested bytes.

Disconnected inference can continue within local policy. New effects stop when applicable authority expires/cannot be established under the chosen offline model. Connectivity failure itself remains distinct from execution failure.

## 6. ACK/replay and crash recovery

Disposition: **ACCEPT_WITH_CHANGE**.

SQLite and H11 are compatible building blocks. There is no existing Platform RunUpdate outbox, assignment inbox or highest-contiguous ACK mechanism in the inspected code.

Required rules:

1. Commit immutable update, sequence and source cursor with source facts, or use a recoverable projector that cannot lose committed observations.
2. Never publish before commit. Avoid permanent gaps from rolled-back reservations; never reuse a committed/published sequence. Reuse of an uncommitted reservation is not itself wrong.
3. Pi persists payload/digest/dedup outcome before ACK. Receiving 1 and 3 yields ACK 1. Define acked_seq=0, response schema and ACK lookup.
4. Persist validated ACK before pruning; reject ACK beyond sender high-water mark and decreasing ACK.
5. Pi GET exposes only Pi-received data. Missing sender-only updates must be re-PUSHed from local outbox; define a separate origin pull endpoint only if actually needed.
6. Retain unacknowledged records; define receiver tombstone/replay expiry after lifetime+7-day dedup retention.
7. Exact duplicate returns original receipt; changed digest conflicts. Replay never invokes effects/provider/tool calls.

**NEW_CONFLICT — old-generation replay:** Pi receives release at generation 12, loses the ACK, then activates 13. §9 requires retry of the exact generation-12 release; §13 rejects every stale update. This can strand the old durable outbox.

Specify ordering: authenticate historical origin/bytes; allow exact committed duplicate ACK and legitimate historical observations without restoring authority; reject stale **new effect requests**. If release arrives above a sequence gap, either wait for its contiguous prefix or allow delayed authenticated history after transfer.

| Crash window | Required result | Existing fit |
|---|---|---|
| Before source/outbox commit | Nothing published/inferred | SQLite pattern available |
| Source commit before interrupted projector | Recover via durable cursor | New projector needed |
| Pi commit, ACK lost | Re-PUSH identical bytes, original ACK | Contract correction needed |
| Local release commit, message unsent | Durable release/outbox; no reacquisition | New release integration |
| New generation, old release retry | Historical ACK, no old writes | Current contract conflicts |
| Effect dispatch, receipt lost | Uncertain; reconcile original operation/job | H5/H6/H7 preserve no replay |

This is source-based failure analysis, not an executed Pi/Harness crash test.

## 7. Two-phase HANDOFF — critical gate

Disposition: **REJECT as specified**. Resolution: **PARTIALLY_RESOLVED with NEW_CONFLICT**.

An allowed failure trace:

1. Old worker has generation 12/live local lease and dispatches workspace.run.
2. H7 executes asynchronously; the command can still mutate the worktree.
3. §12.A.3 allows the in-flight effect to be “durably recorded as pending.”
4. Worker checkpoints/releases lease and emits handed_off/generation 12. S4 requires no checkpoint, local run ref or quiescence evidence.
5. Pi considers release valid and grants generation 13.
6. New worker writes while the previously authorized old command keeps running. Recording status/generation does not terminate that process.

There is overlap between an operation legitimately dispatched under old authority and newly authorized work. The abstract single-writer invariant does not supply the missing quiescence condition.

REQUIRED CHANGE:

- Stop scheduling and new effect dispatch for the released resource.
- Await completion or verified process-tree termination/rollback of write-capable operations. Recorded uncertainty alone is not quiescence.
- Unresolved outcomes permit only reconciliation responsibility with new writing disabled; not ordinary write regrant.
- Durably bind checkpoint, release state, lease invalidation and release outbox so restart cannot claim a release that did not occur or reacquire released authority.
- Define release proof bound to assignment revision, stable scope, old generation, local run, checkpoint digest and effect-disposition evidence. A conditional RunUpdate with typed EvidenceRef can carry it.
- Pi verifies this proof/exclusivity before regrant. Expiry/unreachability alone does not prove an already launched process stopped.

Checkpoint is not release. RecoverExpiredRun rejects a live lease and can renew an expired one; it is not a handoff primitive. There is no explicit live-run ReleaseRun transition.

**Resource-scope NEW_CONFLICT:** generation keyed by (assignment,worktree) changes scope when §12 chooses a new assignment ID. CONSULT also permits a separate consultant assignment. Independent claims for the same physical write target can each appear valid; H4 uniqueness is only per task. Define stable resource authority/exclusivity across assignment IDs/revisions/consultants. Separate isolated worktrees may be separate scopes.

## 8. Fencing and local leases

Disposition: **ACCEPT_WITH_CHANGE**.

Keep Platform authority_generation and local LeaseGeneration distinct. Carry the Platform value in assignment binding, outgoing observations/escalations and managed effect authorization linked to the existing operation ID.

H5 is the correct effect boundary: enforce verified Platform scope/generation/expiry alongside local run/token/lease/plan/step checks. DecisionEngine semantics do not change. H5 currently cannot compare Pi generation; H15 is not a claim verifier.

§13 enforcement only “where the Platform boundary can enforce it” is insufficient: local workspace.apply/run do not inherently traverse Pi. Specify local enforcement and revocation knowledge. Possible implementations include online authorization with in-flight accounting, or bounded local grants with transfer waiting for verified release. A cached generation cannot guarantee immediate disconnected revocation.

No distributed authority logic needs to move into DecisionEngine. Expiry/revocation must block new effects and recovery must not widen authority.

## 9. AssignmentAuthorizationClaim

Disposition: **ACCEPT_WITH_CHANGE**.

S10 requires assignment/revision/node/agent/Platform run/scope/generation/issue/expiry/integrity. Repository is already mandatory inside AccessScope: a second top-level repository field need not independently be required. Worktree remains optional everywhere.

The verifier must compare envelope and claim assignment/revision/node/agent/run/generation and effective target/scope. A valid signature on one scope does not authorize a different unsigned envelope.

Required semantics:

- Trusted issuer/profile and negotiated nonempty integrity mechanism; reject unknown/unverifiable/tampered claim.
- Bind local repository/worktree/project ownership; no default/current-directory fallback for writes.
- Validate time order/expiry/skew and current grant eligibility.
- Recheck authorization at new effect dispatch, not only acceptance.
- Define signature input encoding/integrity-value exclusion and envelope digest scope.
- Define renewal as distinct authorization event or explicitly unsupported. Editing expiry/signature in the immutable assignment otherwise changes digest.

Selecting HMAC vs Ed25519 is **not** required. Structural schemas cannot verify signatures/current generation or arbitrary nested equality; normative semantic rules and fixtures are needed.

## 10. EvidenceRef

Disposition: **ACCEPT_WITH_CHANGE**; snapshot and payload ownership **RESOLVED**.

| Harness source | Wire representation | Condition |
|---|---|---|
| Events | event | Canonical immutable event snapshot/digest |
| Artifacts | artifact | Content blob/checksum; separate mutable access metadata or snapshot it |
| CAS | cas_object | Normalize checksum; verify fetched bytes |
| Jobs | job | Immutable observation/terminal snapshot, not changing job row |
| Validations | validation | Check/subject/outcome/receipt retained |
| Effect receipts | artifact subtype effect_receipt or new enum | Explicit fallback required; enum currently omits effects |
| Test outputs | test_output/artifact | Frozen output plus execution receipt; output alone is not pass |
| Diffs/proposals | diff/artifact | Immutable diff/preimage/revision provenance |
| Checkpoints/context snapshots/reviews | artifact subtype | Explicit fallback avoids forcing many enum additions |

origin_node_id/evidence_id/content_digest provides a suitable binding. Ambiguous run_id “Platform or local” needs a kind/namespace or separate refs. Provenance/locator never grants access.

Pi need not own paths, copy private payloads or mirror CAS. Metadata/summaries also follow export policy. Define allowlisted origin discovery, scoped retrieval, digest verification and unavailable/denied outcomes; endpoint_ref cannot authorize arbitrary network access.

## 11. ContextPack into Compiler and StepPacket

Disposition: **ACCEPT_WITH_CHANGE**.

| Pack field | Existing representation | Core redesign? |
|---|---|---|
| architecture/conventions/patterns | Selected project-information Fragment with provenance/version/bounded content | No |
| decisions | Knowledge/evidence Fragment; never CandidateAction or policy | No |
| constraints | Knowledge by default; authoritative constraints only through separately verified requirement revision | No |
| failures/solutions | Project/evidence/checkpoint material | No |
| trust | Numeric source score mapped to local Trust label/metadata; no automatic policy authority | No |
| relevance | Local ranking/priority hint or semantic metadata | No |
| evidence_refs | Artifact receipts/digest-bound selected-context refs | No |
| content_revision/digest | Immutable selected-pack binding/local snapshot | No |
| priority | Promised in prose/reconciliation, **absent and rejected in S6** | Fix contract, not Compiler |

H10 Compiler retains token budgeting, spill, deduplication and integrity. Trust is a string in Fragment; numeric remote trust must be mapped conservatively, not blindly promoted to KindPolicy.

StepPacket has no ContextPack slot. An adapter can freeze selected context as a local artifact and put its digest-qualified reference in existing Checkpoint.EvidenceRefs before BuildNextStepPacket; the checkpoint is hashed. Coordinator input assembly then retrieves exactly that immutable selection. Alternatively, an additive typed packet field could be introduced with hash/version handling, but is not necessary to represent this information.

Assignment currently pins only context_id. Define immutable ID semantics or explicit id/revision/digest, and durably freeze the selection before inference/restart. Define whether content_digest covers only sections or also provenance/freshness/scope; authenticate trust-affecting data outside that digest.

No field inherently requires replacing Compiler or StepPacket. Large packs must be bounded/selected, never forced wholesale into the 128 KiB authoritative packet. Null project scope needs a stated global/projectless meaning.

## 12. KnowledgeCandidate and verification

Disposition: **ACCEPT_WITH_CHANGE**; candidate-only authority **RESOLVED**.

H13 supplies inert candidates from receipts, VerifiedBy, artifacts, decisions and redactions; task validation/review evidence can augment this. Existing learning jobs may require SessionID, but a wire candidate can be projected from task receipts without fabricating an interactive session.

Harness requires no Platform promote/activate/global dedup/global conflict resolution. Pi/Second Brain remains authority. Existing local skill admission/promotion controls remain independent.

S7 accepts a pass with no evidence/check/subject, unlike H9's passing validation and revision-scoped completion. blocked/unknown cannot be sent; partial/not_applicable have no normative map. S8 accepts only one VerificationEvidence.

An array is not the only solution: define per-check records **or** an immutable verification-bundle EvidenceRef preserving check IDs, subject revisions, outcomes and receipts. Unverified proposals may remain inert, but cannot become verified knowledge merely because the sender says pass. tests/command are display/suggested checks, never executable admission instructions.

Same candidate/key with different digest must conflict. A new key must not silently mutate immutable candidate content. Distinguish Platform run refs from local IDs and reject disagreeing duplicated provenance.

## 13. Simple Agent independence and Core APIs

Architecture: **ACCEPT**. Incomplete operation specifications: **ACCEPT_WITH_CHANGE**.

§§1–2 separate Simple from Managed. S8 allows no run/task fields; the mechanical simple-candidate test succeeds without TaskAssignment, HANDOFF, fencing, local outbox or Harness session. Platform credentials/attribution are not Harness semantics. Core-only capability advertisement must not imply managed.run.

| API/record | Classification | Assessment |
|---|---|---|
| project.get_context | ACCEPT_WITH_CHANGE | Distinguish legacy retrieval response from structured ContextPack. |
| project.get_architecture | NOT_NEEDED_FOR_HARNESS | Managed pack covers it; optional Simple API remains useful. |
| project.get_constraints | ACCEPT_WITH_CHANGE | Separate knowledge from authoritative policy/precedence. |
| work.get_task | ACCEPT_WITH_CHANGE | Stable Platform task/criteria IDs; reading work does not grant managed authority. |
| work.get_status | ACCEPT | Read-only status is compatible. |
| work.update | ACCEPT_WITH_CHANGE | Pi work-state mutation, not files; expected version/conflict and duplicate receipt. |
| work.report_result | ACCEPT_WITH_CHANGE | Structured result/revision-bound evidence; Pi accepts work-state transition. |
| knowledge.search | ACCEPT | Existing MCP client can consume it. |
| knowledge.get | ACCEPT | Retrieved content remains data. |
| knowledge.retrieve_context | NOT_NEEDED_FOR_HARNESS as duplicate alias | Legacy/Simple alias can remain; returned shape must be clear. |
| knowledge.submit_candidate | ACCEPT_WITH_CHANGE | Inert/idempotent/evidence-backed submission. |
| AgentSession | NOT_NEEDED_FOR_HARNESS as standalone wire schema | Pi internal with nullable opaque local ref. |
| AgentRun | ACCEPT_WITH_CHANGE as Pi record | Correct lifecycle/cardinality; no second local run engine. |
| Assignment acceptance receipt/ACK | ACCEPT_WITH_CHANGE | Managed boundary shapes still absent. |
| Advice/result | ACCEPT_WITH_CHANGE | Correlation to request/run/revision and immutable evidence; recipient operational. |

The final file contains service mappings, not full Core API request/response schemas. Absent API schemas cannot be certified. Existing Core MCP may remain operational independently while managed contracts are revised.

## 14. Path ownership and AccessScope

Paths: **ACCEPT**. Effective write scope: **ACCEPT_WITH_CHANGE**.

Pi owns identities; Harness owns Project.RootPath/local resolution. Assignment carries no authoritative foreign absolute path. KnowledgeRef path@version is a knowledge identifier, not filesystem authority.

AccessScope, permissions and claim objects are closed; unknown permission/deploy is mechanically rejected. path_prefixes is explicitly advisory and must not be advertised as an enforced restriction. Operations retain local confinement/policy.

Node/repository/worktree must bind to an owner-authorized local project; unavailable/ambiguous binding rejects. Egress/export is independent of read/write. Ordinary coding must not bypass the production deployment boundary.

## 15. Schema/versioning and mechanical validation

Disposition: **ACCEPT_WITH_CHANGE**.

Only document extraction and in-memory schema/SQLite probes were executed. Python jsonschema 4.17.3 / Draft202012Validator was available; no packages installed.

~~~text
JSON fenced blocks:             11
JSON Schemas:                   10
Draft 2020-12 meta-schema valid: 10
Non-schema JSON examples:        1 (structured error)
Synthetic instance cases:       36
Accepted by schema:             25
Rejected by schema:             11
~~~

These are observations, **not “36 compatibility tests passed.”** Some accepted instances should be refused, and some rejected fields are required/claimed by the prose.

Unmodified sibling-ID validation first failed with this installed resolver:

~~~text
RefResolutionError:
unknown url type: 'agent-platform/v1/agent-platform/v1/AccessScope'
~~~

Relative IDs are not inherently invalid, but the document lacks a canonical retrieval-base/registry policy. To continue, only $id values were normalized **in memory** to https://review.invalid/agent-platform/v1/<Name> and all schemas registered locally. No remote retrieval or supplied-document edit occurred. Internal $defs and nested sibling refs then resolved.

The installed FormatChecker lacked its optional date-time checker; malformed time initially passed despite supplying FormatChecker. The final 36-case run registered explicit regex/calendar validation using datetime for the tested RFC3339 forms. This rejects malformed date/time but allows RFC3339 offsets. It is a fixture checker, not a complete proposed production implementation. UTC-Z enforcement is additional.

| Case | Result | Meaning |
|---|---|---|
| Minimum instance of all ten schemas | Accepted | Includes empty criteria and evidence-free pass; structural success is not operational conformance. |
| S3/S4/S5/S8 without key/digest | Accepted | Contradicts §21 mandatory mutation envelope. |
| Duplicate criterion IDs/different descriptions | Accepted | H2 rejects; uniqueItems alone would not enforce ID uniqueness. |
| Write assignment with no worktree anywhere | Accepted | Target unbound. |
| Envelope repository / claim node mismatch | Accepted | Normative semantic equality needed. |
| schema_revision 999 | Accepted structurally | Negotiation must reject unsupported revision. |
| Non-UTC RFC3339 timestamp | Accepted | date-time is broader than UTC Z. |
| Invalid timestamp with explicit checker | Rejected | Deliberate configuration necessary. |
| progress.attempt=0 | Rejected | Define pre-first-attempt representation. |
| progress={} | Accepted | Heartbeat vs full snapshot unspecified. |
| execution_status=cancelled | Rejected | No published terminal mapping. |
| handed_off without release evidence | Accepted | No conditional release shape. |
| RunUpdate.source_revision | Rejected | Define field or immutable projection-ref convention. |
| EvidenceRef.type=effect_receipt | Rejected | Artifact fallback or enum extension needed. |
| VerificationEvidence.outcome=blocked | Rejected | Exact mapping needed. |
| Candidate verification array | Rejected | Singular field needs lossless bundle alternative. |
| Context entry priority | Rejected | Prose/reconciliation mismatch. |
| ContextPack $defs + EvidenceRef | Accepted after registry normalization | Resolvable with explicit registry. |
| Unknown AccessScope field / deploy capability | Rejected | Permission closure works. |
| Empty integrity alg/value | Accepted structurally | Must fail verifier; tighten shape. |
| supported_schema_revisions value [-1,999,0] | Accepted | Range semantics neither defined nor constrained. |
| Simple candidate, no managed IDs | Accepted | Client separation possible. |
| Unknown claim field | Rejected | Claim closure works. |

Normative version corrections:

- Removing generic unknown-field forward compatibility is correct.
- Dispatch validation by exact negotiated message revision, not one minimum:1 schema for every version.
- Define whether revision arrays are ranges or enumerations; reject empty/reversed/negative representations.
- S1/S2/S7/S10 have revision labels but no instance field: acceptable only if their exact revisions are pinned by parent schema/registry.
- Extensions must be revision-declared fields or advertised feature identifiers. No generic extensions container exists today.
- Open EvidenceRef.provenance is allowed; ContextPack/Candidate provenance are closed. Prose must distinguish them.
- Define canonical digest/signature bytes, total-size bounds and timestamp assertions. Node-specific limits can be negotiated rather than universal.

Appendix A audits every declared property, including nested ones. Signatures, current authority, cross-object equality, unique IDs by key and immutable evidence require semantic validation beyond JSON Schema.

## 16. Preserve Harness core

| Subsystem | Classification | Impact |
|---|---|---|
| CompactState | UNCHANGED | Keep bounded local derived state; wire projection separate. |
| CandidateAction | UNCHANGED | No wire command authority. |
| DecisionEngine | UNCHANGED | Selects only; authorization outside engine. |
| RuleDecision | UNCHANGED | Baseline retained. |
| BonsaiDecision | UNCHANGED | Local selector/shadow/evaluation retained. |
| Context Compiler | ADAPTER_REQUIRED | Fragment/Request mapping; algorithms unchanged. |
| StepPacket | ADAPTER_REQUIRED | Frozen context bound through hashed checkpoint/evidence; typed field optional. |
| Local leases | CORE_CHANGE_REQUIRED for full HANDOFF | Explicit guarded live-run release API; no replacement of model. |
| Effect intents | ADAPTER_REQUIRED | Managed authority gate linked to existing operation; preserve states/identity. |
| Effect no-replay | UNCHANGED | Updates/retries never dispatch. |
| Checkpoints | ADAPTER_REQUIRED | Existing structure; release transaction binds it; checkpoint alone never release. |
| Recovery | CORE_CHANGE_REQUIRED for full HANDOFF | Persist/recognize release so recovery cannot reacquire it; retain uncertain-effect behavior. |
| Evidence/CAS | ADAPTER_REQUIRED | Snapshot/resolver, reuse stores. |
| Task/requirement/plan semantics | ADAPTER_REQUIRED | Cross-reference/revision wrapper, not ID/revision replacement. |

**Significant finding:** final §27's “no core change / thin adapter” claim overstates current capability. Narrow release/recovery integration is required for full HANDOFF. The observation-only slice below avoids it. No core redesign is warranted.

## 17. Residual open choices

| Choice | Blocks lock by itself? | Assessment |
|---|---|---|
| Claim HMAC/Ed25519/other mechanism | No | Bound inputs/trusted issuer/profile/rejection semantics required; algorithm choice can be negotiated later. |
| Tier-A routing recipient | No | Pi policy; correlation and no-transfer semantics must still be fixed. |
| progress_percent formula | No | Optional/null/UI-only; never operational. |
| Claim lifetime numeric default | No | Duration is policy, but expiry/renewal/revocation **semantics** are contract-level. |
| Metrics storage | No | Observational implementation detail. |

Other blockers exist; they cannot be dismissed as these operational preferences.

## 18. Pi P1 safety boundary

Disposition: **ACCEPT_WITH_CHANGE** in migration wording; logically independent.

Workspace → Platform Project → Platform Service can proceed without Node, Agent, Session, Run, TaskAssignment, managed tables or Harness integration. Existing Kanban Project stays distinct with nullable reference. This is not a Pi migration test or deployment authorization.

Clarify §27: reserve nullable **columns/logical references** to future nodes/agents now; defer active FK constraints until parent registries exist. Existing repository/workspace/project FKs are different.

An isolated in-memory SQLite check with foreign_keys=ON, default_node_id REFERENCES nodes(id), no nodes table, and insertion omitting that nullable field produced:

~~~text
no such table: main.nodes
~~~

Thus the literal active-FK interpretation is unsafe for SQLite; this is not evidence about Pi's actual migration runtime. If “nullable FKs” means placeholders, say so. No Node/Agent table must be added to solve P1.

Reconciliation mentions session/run reserved columns, but the actual §27 list has none. They are not needed for minimal P1 and should not be invented to satisfy the summary.

## 19. Compatibility matrix and implementation preview

| CONTRACT AREA | HARNESS SUBSYSTEM | COMPATIBILITY | REQUIRED ADAPTER | CORE CHANGE? | BLOCKER? |
|---|---|---|---|---|---|
| TaskAssignment | Task/requirements | ACCEPT_WITH_CHANGE | Validator/binding/inbox/receipt | ADAPTER_REQUIRED | Yes: criteria/target/idempotency/lifecycle |
| RunUpdate | Task/run/attempt/validation/progress | ACCEPT_WITH_CHANGE | Consistent projector | ADAPTER_REQUIRED | Yes: source/terminal/immutable bytes |
| ACK/replay | SQLite/outbox pattern | ACCEPT_WITH_CHANGE | Dedicated outbox/cursor/ACK | ADAPTER_REQUIRED | Yes: crash/replay/old generation |
| CONSULT | Escalation/evidence | ACCEPT_WITH_CHANGE | Correlated advice | ADAPTER_REQUIRED | Correlation/transport semantics; recipient no |
| HANDOFF | Lease/effect/checkpoint/recovery | REJECT as specified | Quiescence/release proof | CORE_CHANGE_REQUIRED: narrow lifecycle API | Yes: overlapping writes possible |
| Fencing | Dispatch/local lease | ACCEPT_WITH_CHANGE | Stable scope/managed gate | ADAPTER_REQUIRED; release integration above | Yes: scope/offline enforcement |
| Authorization claim | Auth/principal/scope | ACCEPT_WITH_CHANGE | Verifier/expiry/binding | ADAPTER_REQUIRED | Yes: equality/renewal; algorithm no |
| EvidenceRef | Events/artifacts/CAS/jobs/validations/effects | ACCEPT_WITH_CHANGE | Snapshots/scoped resolver | ADAPTER_REQUIRED | Projection/retrieval convention incomplete |
| ContextPack | Compiler/StepPacket | ACCEPT_WITH_CHANGE | Frozen fragments/hashed refs | ADAPTER_REQUIRED | Pinning/prose/authority mismatch |
| KnowledgeCandidate | Learning/evidence | ACCEPT_WITH_CHANGE | Inert verified projection | ADAPTER_REQUIRED | Verification semantics incomplete |
| AccessScope | Root/local policy | ACCEPT_WITH_CHANGE | Grant intersection | ADAPTER_REQUIRED | Write target/equality |
| Capability discovery | MCP/registry | ACCEPT_WITH_CHANGE | Revision/algorithm negotiation | ADAPTER_REQUIRED | Exact encoding/registry incomplete |
| Schema/versioning | Wire validation | ACCEPT_WITH_CHANGE | Registry/semantic validator | ADAPTER_REQUIRED | Required fields/normative shapes |
| Simple Core MCP | MCP client | ACCEPT | Existing Core client | UNCHANGED | No hidden Harness dependency established |
| P1 identities | None | ACCEPT_WITH_CHANGE | None | UNCHANGED | Independent; clarify future FK placeholders |

Conditional implementation preview, **analysis only**: no implementation now because lock blockers remain.

~~~text
Versioned schemas/types + golden fixtures
  → TaskAssignment structural/semantic/claim validator
  → node-local repository/worktree/project binding
  → durable assignment inbox + task cross-reference + acceptance receipt
  → existing CompactState/DecisionEngine read-only observation
  → consistent committed RunUpdate projection
  → immutable durable outbox + transactional sequence/cursor
  → Pi durable ACK lookup + replay/conflict handling
  → STOP BEFORE automatic external action dispatch
~~~

Do not start effects, provider calls, tools, workspace mutation, automatic planning, HANDOFF or promotion from this slice. A local task projection and a read-only decision do not authorize dispatch.

Later acceptance tests: concurrent duplicate assignments, changed digest/stale revision, mismatched scopes, missing bindings, source/outbox crash windows, ACK gaps/loss, historical replay and unchanged effect counts during replay. Full handoff/process quiescence tests belong to later lifecycle work.

## 20. Actual contract blockers and verdict

Only contract blockers are listed, excluding algorithm/recipient/percentage preferences and optional audit/catalog improvements.

1. **Unsafe release/exclusivity:** pending write effects may precede regrant; no required release proof; assignment-keyed fencing lacks stable resource exclusivity across new assignments.
2. **Ambiguous authority enforcement:** write worktree/equality incomplete; local writes are not inherently Pi-gated; expiry/revocation/offline enforcement and immutable-claim renewal undefined.
3. **Idempotency/recovery contradictions:** optional key/digest, unspecified canonical bytes, stale-generation replay rejection, mutable delivery state in immutable messages, incomplete ACK/projector/retention rules.
4. **Assignment/run lifecycle mismatch:** empty/duplicate criteria allowed; replacement revision retirement unspecified; mandatory 1:1 Platform/local run lacks recovery protocol.
5. **Factual observation/verification gaps:** cancelled/source snapshot lacks faithful mapping; verification permits unsupported pass claims and lacks check/subject/bundle semantics. No requirement to export every internal counter.
6. **Incomplete normative schema profile:** context pin/priority/provenance mismatch, evidence subtype/retrieval convention and exact revision/dependency/extension semantics remain undefined.

P1 registry work is separable subject to FK-placeholder clarification. This does not make the managed contract ready to lock.

The final verdict is repeated after the field audit so it is the last line of the deliverable.

## Appendix A. Every JSON field

The following inventory is generated from the ten supplied schemas, with review classifications and mapping notes. “Required” means required by its immediate parent when that parent is present; it does not imply every ancestor is required. Referenced object internals are audited under their own schema, not duplicated under every use. Pure annotations/defaults do not perform normalization, authorization or state changes.

### AccessScope — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| access | Yes | NO CHANGE | Closed none/read/write enum maps to grant intersection, never bypasses local lease/policy. |
| capabilities | No | NO CHANGE | Closed unique grant enum rejects deploy/unknown strings; coarse grant is not local execution authority. |
| repository_id | Yes | REQUIRED CHANGE | One effective signed target; define equality with claim/envelope. Nonblank worktree required for write; nullable repository-level reads may remain. |
| worktree_id | No | REQUIRED CHANGE | One effective signed target; define equality with claim/envelope. Nonblank worktree required for write; nullable repository-level reads may remain. |
| path_prefixes | No | NO CHANGE | Advisory only; no security boundary derives from these paths. Local confinement still applies. |
| granted_by | No | RECOMMENDED CHANGE | Audit attribution/time, not bearer authority; bounded normalized value. |
| granted_at | No | RECOMMENDED CHANGE | Audit attribution/time, not bearer authority; bounded normalized value. |
| expires_at | No | REQUIRED CHANGE | S10 mandatory expiry can be authoritative; define effective expiry intersection if S1 also supplies one. Null must never widen managed write lifetime. |

### EvidenceRef — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| evidence_id | Yes | NO CHANGE | Opaque immutable evidence identity at originating node; origin binding survives Platform run changes. |
| type | Yes | REQUIRED CHANGE | Current enum omits effects/checkpoints/reviews; explicitly map immutable subtype snapshots to artifact or extend enum. |
| origin_node_id | Yes | NO CHANGE | Opaque immutable evidence identity at originating node; origin binding survives Platform run changes. |
| run_id | No | RECOMMENDED CHANGE | Ambiguous Platform-or-local identity; namespace/type or separate refs avoids guessing. |
| content_digest | Yes | REQUIRED CHANGE | Correct sha256 prefix; define exact bytes for record snapshots and require retrieval verification. |
| created_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| locator | No | NO CHANGE | Optional opaque locator, never filesystem/network authority or proof by itself. |
| provenance | No | NO CHANGE | Open metadata allowed only as non-authoritative attribution; identity and digest remain binding. |
| provenance.agent_id | No | NO CHANGE | Opaque attribution/local source refs; no local principal or automatic trust. |
| provenance.harness_ref | No | NO CHANGE | Opaque attribution/local source refs; no local principal or automatic trust. |
| provenance.notes | No | OPTIONAL IMPROVEMENT | Bound optional display metadata; avoid private content leakage. |
| retrieval | No | REQUIRED CHANGE | Scoped origin resolver/discovery and unavailable/digest failure behavior required; method/endpoint must not authorize arbitrary URL access. |
| retrieval.method | Yes | REQUIRED CHANGE | Scoped origin resolver/discovery and unavailable/digest failure behavior required; method/endpoint must not authorize arbitrary URL access. |
| retrieval.endpoint_ref | No | REQUIRED CHANGE | Scoped origin resolver/discovery and unavailable/digest failure behavior required; method/endpoint must not authorize arbitrary URL access. |

### TaskAssignment — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| assignment_id | Yes | REQUIRED CHANGE | Stable mapping key and immutable revision; supersession/claim-refresh behavior must retire old authority without local revision conflation. |
| assignment_revision | Yes | REQUIRED CHANGE | Stable mapping key and immutable revision; supersession/claim-refresh behavior must retire old authority without local revision conflation. |
| platform_session_id | No | NO CHANGE | Nullable Platform ref; no requirement to fabricate an interactive Harness Session. |
| platform_run_id | Yes | REQUIRED CHANGE | Cross-ref only; resolve 1:1 cardinality versus new local runs after recovery. |
| agent_id | Yes | REQUIRED CHANGE | Nonblank authenticated target; match signed claim and trusted node-local principal mapping. |
| node_id | Yes | REQUIRED CHANGE | Nonblank authenticated target; match signed claim and trusted node-local principal mapping. |
| workspace_id | No | NO CHANGE | Nullable Platform metadata; resolve local ProjectID through binding, never copy foreign project ID into local task. |
| project_id | No | NO CHANGE | Nullable Platform metadata; resolve local ProjectID through binding, never copy foreign project ID into local task. |
| service_id | No | NO CHANGE | Nullable Platform metadata; resolve local ProjectID through binding, never copy foreign project ID into local task. |
| repository_id | Yes | REQUIRED CHANGE | Verify complete effective target/scope/claim/generation equality and current eligibility. Write target mandatory; lease remains separate. |
| worktree_id | No | REQUIRED CHANGE | Verify complete effective target/scope/claim/generation equality and current eligibility. Write target mandatory; lease remains separate. |
| task_id | Yes | NO CHANGE | Pi task identity as cross-ref; Task Engine creates independent local ID. |
| title | Yes | NO CHANGE | Maps to Title/OriginalRequest/Objective; preserve request and reject whitespace-only content semantically. |
| original_request | Yes | NO CHANGE | Maps to Title/OriginalRequest/Objective; preserve request and reject whitespace-only content semantically. |
| acceptance_criteria | Yes | REQUIRED CHANGE | Add minItems:1 and semantic unique IDs; reject oversize assignments against advertised node limits. |
| acceptance_criteria[].id | Yes | REQUIRED CHANGE | String/AC- shape fits Criterion; enforce uniqueness by ID, stable within revision. uniqueItems is insufficient alone. |
| acceptance_criteria[].description | Yes | NO CHANGE | Direct Criterion.Description; nonblank bounded text. |
| goal | Yes | NO CHANGE | Maps to Title/OriginalRequest/Objective; preserve request and reject whitespace-only content semantically. |
| constraints | No | RECOMMENDED CHANGE | Maps to requirement constraints; local planner limit 32 and byte budget. Do not truncate authority silently. |
| complexity | No | NO CHANGE | Non-authoritative routing hints; local risk/policy still governs. |
| risk | No | NO CHANGE | Non-authoritative routing hints; local risk/policy still governs. |
| knowledge_context_ref | No | REQUIRED CHANGE | ID-only pointer needs immutable-ID guarantee or revision/digest pin before inference and recovery. |
| access_scope | Yes | REQUIRED CHANGE | Verify complete effective target/scope/claim/generation equality and current eligibility. Write target mandatory; lease remains separate. |
| authority_generation | Yes | REQUIRED CHANGE | Verify complete effective target/scope/claim/generation equality and current eligibility. Write target mandatory; lease remains separate. |
| authorization_claim | Yes | REQUIRED CHANGE | Verify complete effective target/scope/claim/generation equality and current eligibility. Write target mandatory; lease remains separate. |
| issued_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| idempotency_key | No | REQUIRED CHANGE | Present but optional; require for mutation or publish deterministic derivation, identity scope and conflict/receipt rules. |
| payload_digest | No | REQUIRED CHANGE | Present but optional; require and define canonical bytes, self-field exclusion and immutable envelope coverage. |

### RunUpdate — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| platform_run_id | Yes | REQUIRED CHANGE | Cross-ref/source identity; specify stable Platform lifetime over local recovery. Local ref should be required for release proof. |
| harness_run_ref | No | REQUIRED CHANGE | Cross-ref/source identity; specify stable Platform lifetime over local recovery. Local ref should be required for release proof. |
| assignment_id | Yes | REQUIRED CHANGE | Bind to accepted historical projection. Authenticate replay without reviving stale authority. |
| assignment_revision | Yes | REQUIRED CHANGE | Bind to accepted historical projection. Authenticate replay without reviving stale authority. |
| authority_generation | Yes | REQUIRED CHANGE | Bind to accepted historical projection. Authenticate replay without reviving stale authority. |
| sequence | Yes | REQUIRED CHANGE | Monotonic per Platform run, starting 1; atomic allocation/cursor, conflicts and ACK gaps defined independently from local revision. |
| execution_status | Yes | REQUIRED CHANGE | Map every committed Harness state including cancellation. handed_off requires durable release, not a bare string. |
| delivery_status | No | REQUIRED CHANGE | Mutable delivery health cannot change bytes/digest of existing sequence; separate metadata or immutable sampled meaning. |
| progress | No | REQUIRED CHANGE | Define heartbeat/partial/full snapshot and source scope/revision; nested fields are all optional now. |
| progress.phase | No | RECOMMENDED CHANGE | Define task/step phase vocabulary; derive from committed state. |
| progress.completed_steps | No | RECOMMENDED CHANGE | Use active plan; define skipped inclusion, no-plan/null, revision changes and completed<=total consistency. |
| progress.total_steps | No | RECOMMENDED CHANGE | Use active plan; define skipped inclusion, no-plan/null, revision changes and completed<=total consistency. |
| progress.current_step_id | No | NO CHANGE | Opaque local Step.ID, nullable; do not confuse with Step.Key. |
| progress.attempt | No | REQUIRED CHANGE | Define count vs ordinal and task/run/step scope; allow zero or specified omitted/null before first attempt. |
| progress.failed_checks | No | RECOMMENDED CHANGE | Current relevant subject/check count; clarify fail vs blocked and historical evidence. |
| progress.blocked_reasons | No | NO CHANGE | Bounded execution observations from durable state; transport failures are not task failures. |
| progress_percent | No | NO CHANGE | Optional/null 0..100 UI-only; no safety/scheduling/admission dependence. |
| last_action | No | NO CHANGE | Observational label only, never a CandidateAction or dispatch instruction. |
| needs_escalation | No | RECOMMENDED CHANGE | Define derived observation; never cause an effect or override actual EscalationRequest/state. |
| evidence_refs | No | REQUIRED CHANGE | Define snapshot/delta/source revision binding; release must conditionally require proof. |
| occurred_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| idempotency_key | No | REQUIRED CHANGE | Present but optional; require for mutation or publish deterministic derivation, identity scope and conflict/receipt rules. |
| payload_digest | No | REQUIRED CHANGE | Present but optional; require and define canonical bytes, self-field exclusion and immutable envelope coverage. |

### EscalationRequest — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| platform_run_id | Yes | NO CHANGE | Managed identity/authority cross-refs; bind request to accepted projection, never local ID replacement. |
| assignment_id | Yes | NO CHANGE | Managed identity/authority cross-refs; bind request to accepted projection, never local ID replacement. |
| assignment_revision | Yes | NO CHANGE | Managed identity/authority cross-refs; bind request to accepted projection, never local ID replacement. |
| authority_generation | Yes | NO CHANGE | Managed identity/authority cross-refs; bind request to accepted projection, never local ID replacement. |
| reason | Yes | REQUIRED CHANGE | Stable reason code fine without rename; NO_PROGRESS/STUCK needs execution evidence, not delivery retries. |
| mode | Yes | REQUIRED CHANGE | CONSULT non-transfer resolved; HANDOFF intent is not release/regrant proof. |
| evidence_refs | No | REQUIRED CHANGE | Require failure evidence and define bounded failure/checkpoint/actions bundle. Correlate advice/release using stable mandatory key. |
| required_capability | No | RECOMMENDED CHANGE | Optional routing hint; advertisement/readiness and grant checks still required. |
| context | No | RECOMMENDED CHANGE | Described bounded but no limit; bounded rationale only, not unrestricted transcript/authority. |
| requested_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| idempotency_key | No | REQUIRED CHANGE | Present but optional; require for mutation or publish deterministic derivation, identity scope and conflict/receipt rules. |
| payload_digest | No | REQUIRED CHANGE | Present but optional; require and define canonical bytes, self-field exclusion and immutable envelope coverage. |

### ContextPack — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| context_id | Yes | REQUIRED CHANGE | Freeze immutable identity/revision/digest; define exact content/provenance digest coverage and assignment selection pin. |
| content_revision | Yes | REQUIRED CHANGE | Freeze immutable identity/revision/digest; define exact content/provenance digest coverage and assignment selection pin. |
| content_digest | Yes | REQUIRED CHANGE | Freeze immutable identity/revision/digest; define exact content/provenance digest coverage and assignment selection pin. |
| project_id | Yes | REQUIRED CHANGE | Required-but-null has no declared scope semantics; define global/projectless or disallow null. |
| task_ref | No | RECOMMENDED CHANGE | Declare Platform task identity explicitly; optional scope, not local task lookup. |
| sections | Yes | NO CHANGE | Closed structured container; empty sections may be valid retrieval result if documented. |
| sections.architecture | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.decisions | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.constraints | No | REQUIRED CHANGE | Knowledge constraint is not policy; define source/precedence so remote content cannot override assignment/local authority. |
| sections.patterns | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.failures | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.solutions | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.conventions | No | NO CHANGE | Bound/select as provenance-bearing Fragments; packet binds frozen references. Core compiler unchanged. |
| sections.evidence_refs | No | RECOMMENDED CHANGE | Lazy scoped retrieval and bounded count; immutable ref mapping in S2. |
| provenance | Yes | REQUIRED CHANGE | Bind source versions/trust/provenance to immutable pack or resolver; path@version convention needs stable semantics. No automatic policy trust. |
| provenance.sources | Yes | REQUIRED CHANGE | Bind source versions/trust/provenance to immutable pack or resolver; path@version convention needs stable semantics. No automatic policy trust. |
| provenance.sources[].ref | Yes | REQUIRED CHANGE | Bind source versions/trust/provenance to immutable pack or resolver; path@version convention needs stable semantics. No automatic policy trust. |
| freshness | Yes | NO CHANGE | Closed observational freshness metadata. |
| freshness.generated_at | Yes | RECOMMENDED CHANGE | Validate UTC time; generation time is not source verification time. |
| freshness.max_source_age_days | No | RECOMMENDED CHANGE | Specify selection bound vs observed age and nonnegative value; not independent trust proof. |
| $defs.entryList[].ref | Yes | REQUIRED CHANGE | Stable knowledge version reference, not authoritative local file path; bind resolver/digest convention. |
| $defs.entryList[].title | No | NO CHANGE | Display/context text; bounded selection preserves compiler budget. |
| $defs.entryList[].summary | Yes | NO CHANGE | Display/context text; bounded selection preserves compiler budget. |
| $defs.entryList[].confidence | No | NO CHANGE | Optional qualitative metadata; does not confer authority. |
| $defs.entryList[].trust | No | NO CHANGE | Optional score maps to conservative local labels/priority/metadata. Missing priority field requires prose/schema correction. |
| $defs.entryList[].relevance | No | NO CHANGE | Optional score maps to conservative local labels/priority/metadata. Missing priority field requires prose/schema correction. |

### VerificationEvidence — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| kind | Yes | NO CHANGE | Verification category; not enough to prove execution by itself. |
| command | No | RECOMMENDED CHANGE | Display only; do not execute knowledge commands. Subject/repo/check binding must come from defined evidence, not free text authority. |
| scope | No | RECOMMENDED CHANGE | Display only; do not execute knowledge commands. Subject/repo/check binding must come from defined evidence, not free text authority. |
| outcome | Yes | REQUIRED CHANGE | Define pass/fail/blocked/unknown mapping; partial/not_applicable cannot silently replace local statuses. |
| evidence_refs | No | REQUIRED CHANGE | Pass requires nonempty receipt proof; preserve check and subject revision in record or normative bundle. |
| observed_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |

### KnowledgeCandidate — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| candidate_id | Yes | REQUIRED CHANGE | Stable immutable submission identity; define same-key/different-digest and same-ID/new-key conflicts. |
| project_id | No | RECOMMENDED CHANGE | Nullable project/global scope needs clear meaning; keep Simple clients independent. |
| run_id | No | NO CHANGE | Optional Platform IDs; no Harness Session or managed assignment requirement for Simple clients. |
| task_id | No | NO CHANGE | Optional Platform IDs; no Harness Session or managed assignment requirement for Simple clients. |
| type | Yes | NO CHANGE | Knowledge taxonomy; does not confer admission/promotion permission. |
| title | No | RECOMMENDED CHANGE | Bound content; solution optional for failure-only candidate; require useful nonblank claims without inventing fixes. |
| problem | Yes | RECOMMENDED CHANGE | Bound content; solution optional for failure-only candidate; require useful nonblank claims without inventing fixes. |
| solution | No | RECOMMENDED CHANGE | Bound content; solution optional for failure-only candidate; require useful nonblank claims without inventing fixes. |
| diff_ref | No | NO CHANGE | Immutable EvidenceRef to diff/proposal snapshot. |
| tests | No | RECOMMENDED CHANGE | Display/suggested checks only; strings are not executed verification receipts. |
| evidence_refs | No | REQUIRED CHANGE | Define per-check records or lossless evidence bundle; current singular evidence-free verification cannot support verified trust. |
| verification | Yes | REQUIRED CHANGE | Define per-check records or lossless evidence bundle; current singular evidence-free verification cannot support verified trust. |
| provenance | Yes | REQUIRED CHANGE | Attribution alone not proof; preserve node/check/subject/evidence via verified refs. Keep Pi admission authority. |
| provenance.agent_id | Yes | REQUIRED CHANGE | Attribution alone not proof; preserve node/check/subject/evidence via verified refs. Keep Pi admission authority. |
| provenance.platform_run_id | No | RECOMMENDED CHANGE | Optional for Simple; must match outer run_id when both provided; never substitute local run. |
| provenance.evidence_refs | No | REQUIRED CHANGE | Require supporting evidence for verified claims; avoid contradictory duplicate evidence lists. |
| related_refs | No | NO CHANGE | Hints for Pi's own global dedup/conflict review; not a completed conflict search. |
| created_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| idempotency_key | No | REQUIRED CHANGE | Present but optional; require for mutation or publish deterministic derivation, identity scope and conflict/receipt rules. |
| payload_digest | No | REQUIRED CHANGE | Present but optional; require and define canonical bytes, self-field exclusion and immutable envelope coverage. |

### CapabilityAdvertisement — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| contract_version | Yes | NO CHANGE | Family identity is separate from assignment/content revisions; never a Harness ID. |
| schema_revision | Yes | REQUIRED CHANGE | Bind exact per-type revision to a registry schema; minimum:1 alone accepts unsupported revisions. Nested schema dependencies need pinning. |
| agent_id | Yes | NO CHANGE | Platform identity, not Harness session; trusted registration/credential binding. |
| node_id | Yes | NO CHANGE | Platform identity, not Harness session; trusted registration/credential binding. |
| client_class | Yes | NO CHANGE | Simple/Managed separation; no managed requirements for Core-only clients. |
| capabilities | Yes | NO CHANGE | Unique closed list, managed.run opt-in; capability is not runtime qualification or an assignment grant. |
| supported_modes | No | RECOMMENDED CHANGE | Only negotiated modes; Simple can omit. Never infer release readiness from this advertisement alone. |
| supported_schema_revisions | No | REQUIRED CHANGE | Define array as supported set or min/max range and validity; require sufficient managed negotiation before sending typed extensions. |
| supported_claim_algs | No | REQUIRED CHANGE | Managed execution needs authenticated mutually supported verifier profile; absent/unknown means no managed write grant. |
| knowledge_kinds | No | OPTIONAL IMPROVEMENT | Optional production/retrieval capability hints, not trust. |
| advertised_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |

### AssignmentAuthorizationClaim — ACCEPT_WITH_CHANGE

| JSON field | Required in parent? | Review action | Mapping / finding |
|---|---|---|---|
| assignment_id | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| assignment_revision | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| node_id | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| agent_id | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| platform_run_id | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| repository_id | No | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| worktree_id | No | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| access_scope | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| authority_generation | Yes | REQUIRED CHANGE | Bind exact effective envelope identity/target/scope/generation; require write worktree and equality or remove duplicates. Separate local lease. |
| issued_at | Yes | RECOMMENDED CHANGE | Timestamp observation/provenance only; assert date-time plus UTC Z and bounds as a wire profile; never use time instead of sequence/fence. |
| expires_at | Yes | REQUIRED CHANGE | Mandatory good; expiry order/renewal/current authorization must be enforced at effect boundary. Duration choice not blocker. |
| integrity | Yes | REQUIRED CHANGE | Closed shape good; negotiated trusted verifier and exact signed input/exclusion needed. Empty or unverifiable values cannot authorize. Algorithm choice may remain open. |
| integrity.alg | Yes | REQUIRED CHANGE | Closed shape good; negotiated trusted verifier and exact signed input/exclusion needed. Empty or unverifiable values cannot authorize. Algorithm choice may remain open. |
| integrity.value | Yes | REQUIRED CHANGE | Closed shape good; negotiated trusted verifier and exact signed input/exclusion needed. Empty or unverifiable values cannot authorize. Algorithm choice may remain open. |

## Appendix B. Review verification boundaries

The review executed schema meta-validation, synthetic instance probes and an isolated in-memory SQLite FK check. It did not execute Harness unit/integration tests, invoke the running app, send messages, contact Pi, simulate live HANDOFF, or exercise production. No such runtime compatibility claim is made.

The schema probes used generated minimal instances plus one-field mutations and nested-reference cases. The supplied error example was JSON-parsed only because no error schema was supplied. Normalized schema IDs and the explicit fixture timestamp checker were test-local, not edits to the contract.

Review artifacts are the only intended repository changes. Existing modifications were retained. No H2 implementation or automatic external action dispatch was performed.

Final document QA passed: all 20 requested review sections are present; all 10 schema inventories are present; local evidence links resolve; fenced blocks are balanced; exactly one final verdict is present. Input contract/reconciliation SHA-256 values remained identical to §1.

Before and after writing the report, a deterministic manifest hash of 877 files under internal/ and cmd/, plus go.mod, go.sum and README.md, was identical:

~~~text
75692b4ba65f0c18d238fbbe761d105a9d4ab91e1df11aad1b8f7e1de9be26fd
~~~

This confirms the reviewed source set was not altered while producing the artifact; it is not a claim that the pre-existing working tree was clean.

Field inventory: 172 declared JSON properties audited across 10 schemas.

HARNESS VERDICT: NEEDS_CONTRACT_REVISION
