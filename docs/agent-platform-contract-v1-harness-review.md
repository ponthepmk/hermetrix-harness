# Agent Platform Contract v1 — Hermetrix Harness Review

**Reviewed document:** `agent-platform-contract-v1-draft.md` dated 2026-09-21
**Harness repository:** `hermetrix-harness`
**Review scope:** Contract and implementation fit only. No implementation, migration, deployment, or H2 work was performed.
**Classification vocabulary:** `ACCEPT`, `ACCEPT_WITH_CHANGE`, `REJECT`, `NOT_NEEDED_FOR_HARNESS`.

## 1. Executive decision

The architecture boundary is correct and the draft is close, but it is not ready to lock. The Harness can reuse its durable task engine, context compiler, evidence stores, learning pipeline, decision abstractions, and no-replay effect state machine. No parallel task, session, decision, or evidence system is needed.

The contract requires revision before implementation because several wire-level details do not yet map safely or losslessly:

1. `TaskAssignment` has no Platform `task_id`, title, original request, stable criterion IDs, or explicit Harness egress policy. These are required by `taskengine.Create`.
2. `TaskAssignment.acceptance_criteria` is optional and is an array of strings, while Harness requires at least one `{id, description}` criterion.
3. assignment authorization is described as a short-lived claim, but the schema carries only `credential_ref` and expiry. That is not a verifiable claim or fencing token.
4. repository/worktree identity is duplicated between `TaskAssignment` and `AccessScope` without a same-value invariant; write assignments do not require `worktree_id`.
5. `RunUpdate.progress` is a synthetic scalar. Harness progress is a structured receipt-derived snapshot and cannot truthfully produce a normalized percentage.
6. transport retry exhaustion is incorrectly described as task `blocked`/`NO_PROGRESS`. Delivery failure is not execution failure and may occur while valid disconnected work continues.
7. the ACK/replay endpoint direction and ACK schema are incomplete.
8. HANDOFF lacks a two-phase prepare/release/ack protocol. Revoking a Pi claim does not end the live Harness run lease.
9. `EscalationRequest` lacks `escalation_id`, failure signature/count, attempted actions, and checkpoint/authority-release evidence.
10. `EvidenceRef` claims effects are covered but has no `effect` type; several referenced records are mutable and therefore cannot safely retain one payload digest without an immutable snapshot rule.
11. compatibility text says consumers ignore additive fields while every schema uses `additionalProperties: false` and `contract_version` is fixed to `1.0.0`.
12. common idempotency/digest fields are optional in schemas despite being mandatory in the prose, and canonical digest calculation is underspecified.

These are contract corrections. They do not require replacing Harness core subsystems.

## 2. Actual Harness implementation used for this review

| Concern | Actual implementation |
|---|---|
| Durable task contract | `internal/taskengine/models.go`, `internal/taskengine/service.go:26` |
| Run lease and effect authority | `internal/taskengine/execution.go:144`, `:205`, `:224`, `:450` |
| Checkpoint/recovery | `internal/taskengine/service.go:413`, `:457`; `execution.go:224` |
| Compact state and decision | `internal/taskengine/decision.go:77`, `:97`, `:112` |
| Progress/failure budgets | `internal/taskengine/progress.go:32`; `orchestration.go:162` |
| Planner/worker coordinator | `internal/taskcoord/service.go:157`, `:294`, `:423`, `:607`, `:705`, `:924` |
| Bonsai decision | `internal/taskcoord/decision.go:49`, `:70`, `:164` |
| Context compilation | `internal/agent/service.go:1800`; `internal/context/compiler.go:46` |
| Authoritative step handoff | `internal/taskengine/packet.go:20`, `:41` |
| Evidence stores | agent events, artifacts/CAS, jobs, validations, effects, proposals, context snapshots |
| Learning evidence/candidates | `internal/learning/models.go`; `internal/skills/models.go`; `skills/service.go:34`, `:388` |
| Workspace root | `internal/product/models.go:5`; `product/service.go:170`; `tools/registry.go` `Registry.For` |
| Auth and credentials | `internal/web/auth.go`; `internal/web/boundary.go`; `internal/secrets/vault.go` |

The nine JSON Schema documents and the error example are syntactically valid JSON. This review concerns semantic and implementation compatibility.

## 3. Architecture and section-level classification

| Draft area | Classification | Harness finding |
|---|---|---|
| Architecture boundary | `ACCEPT` | Pi routes who/where/tier/identity; Harness decides and executes the next local action. |
| Client classes | `ACCEPT` | The existing MCP catalog supports simple clients and the managed path can be additive. |
| Ownership matrix | `ACCEPT_WITH_CHANGE` | Effective write authority is the intersection of Pi grant and Harness local lease/policy. Pi cannot be the sole real-time authority while the node is disconnected. |
| Identity model | `ACCEPT` | Platform and Harness IDs remain separate and use explicit cross-references. |
| Absolute path rule | `ACCEPT` | Matches local `Project.RootPath` and `Registry.For(root)` confinement. |
| Core MCP operations | `ACCEPT_WITH_CHANGE` | Useful, but aliases and policy/knowledge provenance need tightening; see §5. |
| Managed execution concept | `ACCEPT_WITH_CHANGE` | Maps to durable tasks, but assignment, ACK, handoff and status contracts need changes. |
| Knowledge exchange concept | `ACCEPT_WITH_CHANGE` | Fits learning evidence, but candidate verification/provenance fields are incomplete. |
| Evidence model | `ACCEPT_WITH_CHANGE` | Reuse existing stores; require immutable evidence snapshots and complete type mapping. |
| Auth/trust model | `ACCEPT_WITH_CHANGE` | Per-node credentials are correct; assignment claim is not yet a verifiable/fenced contract. |
| CONSULT semantics | `ACCEPT_WITH_CHANGE` | Ownership is preserved correctly; correlate advice to an immutable escalation and evidence record. |
| HANDOFF semantics | `REJECT` as currently sequenced | It can grant a second writer before Harness has durably ended its local lease. Use two-phase handoff. |
| Outbox/PUSH/ACK/replay | `ACCEPT_WITH_CHANGE` | Fits SQLite recovery, but endpoint direction, ACK shape and delivery-failure semantics must be fixed. |
| Idempotency | `ACCEPT_WITH_CHANGE` | Correct intent; required fields and canonical digest rules are missing. |
| JSON Schema strategy | `ACCEPT_WITH_CHANGE` | Correct draft/version, but reference resolution, bounds and compatibility behavior need correction. |
| Error model | `ACCEPT_WITH_CHANGE` | Add stale revision, expired claim, uncertain outcome and evidence unavailable; restrict retry semantics. |
| Capability discovery | `ACCEPT_WITH_CHANGE` | Add supported contract/schema versions, expiry and dynamic qualification distinction. |
| Compatibility section | `REJECT` as written | `ignore unknown fields` conflicts with `additionalProperties: false` and exact `1.0.0`. |
| P0 safety invariants | `ACCEPT_WITH_CHANGE` | Preserve Pi production gate and Harness local effect gate; document their intersection. |
| Metrics | `ACCEPT` | Observational and non-authoritative. |
| Pi implementation mapping | `ACCEPT_WITH_CHANGE` | Harness cannot verify Pi internals, but contract-facing mapping is coherent after schema corrections. |
| Hermetrix mapping | `ACCEPT_WITH_CHANGE` | Mostly accurate; ContextPack/StepPacket and HANDOFF require additive core integration points, not only an outbox adapter. |

## 4. Managed contracts

### 4.1 TaskAssignment — `ACCEPT_WITH_CHANGE`

It maps to the existing durable task type; no new task type is required. The adapter can create a local `taskengine.Task`, store Platform/local cross-references, resolve a Pi worktree identity to a local `product.Project`, and retain assignment scope separately.

The current schema cannot call `taskengine.Create` without invention. `CreateTaskInput` requires title, objective, original request, actor and at least one stable criterion. It also carries `Unknowns`, `EgressPolicy` and an explicit remote-egress approval.

**REQUIRED CHANGE**

- Add required Platform `task_id`.
- Add required `title` and `original_request`, or state a deterministic mapping. Explicit fields are preferred because `goal` is not a safe substitute for both.
- Make `acceptance_criteria` required, `minItems: 1`, and use objects `{id, description}` with unique stable IDs.
- Require `idempotency_key` and `payload_digest`.
- Replace `claim.credential_ref` with a non-secret `claim_id` plus a verifiable/fenced claim mechanism. Credentials belong in authenticated transport or an opaque signed token, not a body field that appears to identify a local secret.
- Add an authority/fencing generation bound to assignment, worktree and access scope. A stale claim must fail after HANDOFF.
- Require `worktree_id` when `access_scope.access == "write"`.
- Remove duplicate repository/worktree authority or explicitly define that top-level values and scope values MUST be equal. Prefer one authoritative location.
- Define `egress_policy`, defaulting to `local_only`. `remote_allowed` must carry reviewed actor/reason or a Pi policy reference that Harness can audit.
- Add maximum sizes/counts. The existing planner accepts at most 32 constraints, 32 unknowns and 32 criteria; decision state supports at most 64 criteria; StepPacket is capped at 128 KiB.

**RECOMMENDED CHANGE**

- Add optional `unknowns` to map to `CreateTaskInput.Unknowns`.
- Replace `knowledge_context_ref` string with a pinned object containing `context_id`, `version`, and `content_digest` to prevent retrieval drift.
- Add optional expected VCS identity such as commit SHA. It is a constraint, not an absolute path.
- Clarify that Platform `project_id` maps to a local Harness project ID and is never written into local `Task.ProjectID` without lookup.
- Define assignment acceptance/decline receipt with local opaque task/run refs and a safe rejection code.

**OPTIONAL IMPROVEMENT**

- Retain `complexity` and `risk` as routing hints; Harness must derive effective policy locally.
- Add an optional deadline/budget only if Pi intends to bound local execution. It must not weaken Harness budgets.

**NO CHANGE**

- Separate `platform_session_id` and `platform_run_id`.
- `node_id`, `agent_id`, `repository_id`, and local worktree resolution.
- No absolute path field.

### 4.2 RunUpdate — `ACCEPT_WITH_CHANGE`

RunUpdate can be generated from committed Harness state. Status comes from `Task.State`, run/attempt/effect states, and proposal/review state. Progress comes from `TaskProgress`, which is derived from durable receipts. Sequence belongs to the local outbox, not `CompactState`.

**REQUIRED CHANGE**

- Replace scalar `progress` with a structured object. At minimum support `phase`, `task_revision`, `attempts`, `failed_attempts`, `confirmed_failures`, `planner_escalations`, `unresolved_effects`, `remaining_attempts`, `remaining_planner_escalations`, `no_progress`, `budget_exhausted`, and `blocked_reasons`.
- Add `cancelled` to status or publish an exact mapping from all Harness task states: draft, ready, running, waiting_for_input, paused, verifying, completed, failed, cancelled.
- Require `payload_digest`. `idempotency_key` may be required or deterministically derived as `platform_run_id:sequence`; choose one rule.
- Define that an update is appended only after its source state transaction commits. Include `source_revision`/`task_revision` so the projection is auditable.
- Separate transport state from task status. Exhausted delivery retries must set local delivery state such as `delivery_degraded`; it must not turn a working task into `blocked` or generate `NO_PROGRESS`.

**RECOMMENDED CHANGE**

- Remove `needs_escalation` from the authoritative payload because it is derived from status/progress and an `EscalationRequest`. If retained, mark it explicitly derived.
- Bound `last_action` and define it as an observational label plus optional operation/evidence reference, never a `CandidateAction` or instruction.
- Add optional `harness_task_ref`, `attempt_ref`, `lease_generation` and safe `error_code`.
- Define whether each update contains all current evidence refs or only newly added refs. Delta-only is smaller but requires explicit semantics.

**OPTIONAL IMPROVEMENT**

- Add `delivery_attempt` only to local transport logs, not to the run update payload.

**NO CHANGE**

- `platform_run_id`, `assignment_id`, monotonic `sequence`, `occurred_at`, and opaque `harness_run_ref`.
- Replaying RunUpdate must never invoke a Harness action or effect.

### 4.3 EscalationRequest — `ACCEPT_WITH_CHANGE`

The modes fit the architecture, but the request lacks the durable failure identity already available in `task_step_failures` and `task_step_escalations`.

**REQUIRED CHANGE**

- Add required `escalation_id`.
- Rename `reason` to stable `reason_code`, or formally define that it is the code.
- Require non-empty `evidence_refs` for failure-based escalation.
- Add `failure_signature`, `failure_count`/ordinal, `attempted_actions`, and `checkpoint_ref`.
- Require `idempotency_key` and `payload_digest`.
- For HANDOFF add `authority_release_state` with a state machine, not a boolean assertion.
- Add `TRANSPORT_UNAVAILABLE` only if delivery escalation has a separate route; do not report it as `NO_PROGRESS`.

**RECOMMENDED CHANGE**

- Replace free-form `context` with a bounded `rationale_summary`; advice inputs should otherwise be evidence or ContextPack references.
- Add `task_id`, task revision and local run/lease generation cross-reference.
- Add `knowledge_refs` as bounded stable refs if consultation depends on prior knowledge.

**OPTIONAL IMPROVEMENT**

- Add a requested response deadline for CONSULT, with no implication that timeout transfers authority.

**NO CHANGE**

- Pi chooses the Tier-A recipient.
- CONSULT and HANDOFF remain distinct.

### 4.4 AgentSession — `NOT_NEEDED_FOR_HARNESS` as a standalone wire schema

Pi may keep a thin platform session record. Harness only needs the Platform session reference in TaskAssignment and may return an optional local `harness_session_ref`. A durable software task does not necessarily create an interactive `agent.Session`, so the cross-reference must remain nullable.

**REQUIRED CHANGE**

- Do not require a Harness session for every Platform run.
- If an AgentSession API is exposed to Harness, define its schema and state transitions; otherwise document it as Pi-internal persistence, not a Managed Execution wire contract.

### 4.5 AgentRun — `ACCEPT_WITH_CHANGE` as a Pi persistence concept

Pi needs this record for ownership, last acknowledged sequence and cross-reference. Harness does not need a separate AgentRun payload if TaskAssignment, assignment receipt, ACK and RunUpdate carry the necessary identifiers.

**REQUIRED CHANGE**

- Define run states including assignment offered/accepted/rejected, working, blocked, consulting, handoff_preparing, handed_off, completed, failed and cancelled.
- Store authority generation/fencing state and the last contiguous acknowledged sequence.
- Never infer local lease release from Pi status alone.

## 5. Core MCP API classification

| API | Classification | Required Harness-facing treatment |
|---|---|---|
| `project.get_context` | `ACCEPT_WITH_CHANGE` | Return either explicitly labelled legacy evidence pack or versioned ContextPack; do not call both the same shape. |
| `project.get_architecture` | `NOT_NEEDED_FOR_HARNESS` for managed v1 | ContextPack supplies it. It may remain useful for simple clients. |
| `project.get_constraints` | `ACCEPT_WITH_CHANGE` | Keep knowledge constraints separate from authoritative Platform policy and Harness local policy. Never merge without provenance labels. |
| `work.get_task` | `ACCEPT_WITH_CHANGE` | Return stable Platform task/criterion IDs and access summary; managed assignment remains authoritative for execution. |
| `work.get_status` | `ACCEPT` | Read-only reconciliation is compatible. |
| `work.update` | `ACCEPT_WITH_CHANGE` | This updates Pi work state, not repository files. Require idempotency and expected revision; preserve Write Gate behavior. |
| `work.report_result` | `ACCEPT_WITH_CHANGE` | Structured terminal result and evidence refs; do not reduce evidence to unstructured `ai_notes` only. |
| `knowledge.search` | `ACCEPT` | Existing deferred MCP catalog can consume it. |
| `knowledge.get` | `ACCEPT` | Treat returned content as provenance-bearing data, not policy. |
| `knowledge.retrieve_context` | `NOT_NEEDED_FOR_HARNESS` if identical to `project.get_context` | Keep one canonical managed operation; retain alias only for legacy/simple clients and avoid duplicate model-visible tools. |
| `knowledge.submit_candidate` | `ACCEPT_WITH_CHANGE` | Candidate-only, idempotent, no promotion authority; Pi independently validates and deduplicates. |

**RECOMMENDED CHANGE**

- Add a scoped evidence retrieval operation or define it normatively. `EvidenceRef.retrieval` currently points at a mechanism whose request, authorization, digest verification and unavailable response are unspecified.
- Publish effect and idempotency annotations through MCP. Harness treats untrusted annotations conservatively and never automatically replays `tools/call`.

## 6. Field-by-field schema review

### 6.1 AccessScope — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `access` | `NO CHANGE` | Maps to none/read/write at repository level. |
| `repository_id` | `REQUIRED CHANGE` | Keep in one authoritative location; currently duplicates TaskAssignment. |
| `worktree_id` | `REQUIRED CHANGE` | Require for write; keep nullable for repository-level read only. |
| `path_prefixes` | `REJECT` as advisory permission | Harness does not enforce this today, and Pi must not appear authoritative for paths. Remove, or redefine as normalized repository-relative requested scope and require local enforcement before it affects authority. |
| `granted_by` | `RECOMMENDED CHANGE` | Require for write grants; provenance/audit only. |
| `granted_at` | `RECOMMENDED CHANGE` | Require for write grants. |
| `expires_at` | `REQUIRED CHANGE` | Require for managed write authority; null may be allowed only for no/read if policy permits. |
| missing `egress_policy` | `REQUIRED CHANGE` | Add local_only/remote_allowed mapping or an explicit scoped capability. |
| missing authority generation | `REQUIRED CHANGE` | Needed to fence stale writers across HANDOFF. |

### 6.2 EvidenceRef — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `evidence_id` | `NO CHANGE` | Platform evidence identity may differ from local object ID. |
| `type` | `REQUIRED CHANGE` | Add immutable receipt/snapshot coverage for `effect`, `checkpoint`, `context_snapshot`, `proposal`, and `review`, or state their required projection to `artifact`. Draft §20 incorrectly says effects are covered by the current enum. |
| `owner.node_id` | `NO CHANGE` | Required owner is correct. |
| `owner.agent_id` | `NO CHANGE` | Nullable supports system-produced evidence. |
| `provenance.platform_run_id` | `RECOMMENDED CHANGE` | Also allow assignment/task refs; make known fields closed rather than `additionalProperties: true`. |
| `provenance.harness_ref` | `NO CHANGE` | Correct opaque local reference. |
| `digest` | `REQUIRED CHANGE` | Standardize on `sha256:<hex>` or document why this schema omits the prefix. Define canonical bytes for JSON records. |
| `locator` | `REJECT` unless opaque token is defined | It risks leaking/storing a local path. `harness_ref` plus retrieval is sufficient. |
| `retrieval.method` | `RECOMMENDED CHANGE` | Define one scoped retrieval protocol. `cas` is a storage kind, not necessarily a remote transport. |
| `retrieval.endpoint_ref` | `REQUIRED CHANGE` | Specify discovery, scope check, expiry and digest verification; never accept arbitrary caller URLs. |
| `created_at` | `RECOMMENDED CHANGE` | Make required for immutable evidence. |

Evidence must reference immutable bytes. Existing artifacts/CAS/events/validations qualify. Jobs qualify only as terminal snapshots. Effects are mutable through planned/dispatched/observed/uncertain/reconciled; expose an immutable receipt/snapshot or include a revision-specific projection.

### 6.3 TaskAssignment — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Resolve exact-version versus `1.x` compatibility contradiction. |
| `assignment_id` | `NO CHANGE` | Required external idempotency/audit identity. |
| `platform_session_id` | `NO CHANGE` | Correctly nullable and separate. |
| `platform_run_id` | `NO CHANGE` | Correctly separate from Harness run. |
| missing `task_id` | `REQUIRED CHANGE` | Pi/Kanban task identity is essential. |
| `agent_id` | `NO CHANGE` | Platform-owned assignment identity. |
| `node_id` | `NO CHANGE` | Must match local node registration. |
| `workspace_id` | `RECOMMENDED CHANGE` | Required when Pi work belongs to a workspace; nullable only for explicitly projectless work. |
| `project_id` | `RECOMMENDED CHANGE` | Require for repository work and map through local binding. |
| `service_id` | `OPTIONAL IMPROVEMENT` | Useful Pi routing metadata; Harness does not require it locally. |
| `repository_id` | `REQUIRED CHANGE` | Keep once and ensure local binding; no path. |
| `worktree_id` | `REQUIRED CHANGE` | Conditional requirement for writes. |
| `access_scope` | `REQUIRED CHANGE` | Add fencing and egress; eliminate duplicate identity ambiguity. |
| `goal` | `NO CHANGE` | Maps to Objective. Bound length. |
| missing `title` | `REQUIRED CHANGE` | Required by Harness task creation. |
| missing `original_request` | `REQUIRED CHANGE` | Required and retained separately from normalized goal. |
| `acceptance_criteria` | `REQUIRED CHANGE` | Required non-empty array of stable `{id, description}` objects. |
| `constraints` | `RECOMMENDED CHANGE` | Default empty; max 32; bounded strings. |
| missing `unknowns` | `RECOMMENDED CHANGE` | Maps directly to requirement revision. |
| `complexity` | `OPTIONAL IMPROVEMENT` | Hint only. |
| `risk` | `OPTIONAL IMPROVEMENT` | Hint only; cannot lower local/Pi policy. |
| `knowledge_context_ref` | `REQUIRED CHANGE` | Pin context ID, version and digest. |
| `claim.credential_ref` | `REJECT` | Not proof of authorization and may expose secret-store naming. |
| `claim.expires_at` | `REQUIRED CHANGE` | Keep but bind cryptographically/opaquely to assignment, scope and fencing generation. |
| `issued_at` | `NO CHANGE` | Require UTC `Z` and define skew tolerance. |
| `idempotency_key` | `REQUIRED CHANGE` | Make required. |
| `payload_digest` | `REQUIRED CHANGE` | Make required and define canonicalization/exclusion rule. |

### 6.4 RunUpdate — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Align version policy. |
| `platform_run_id` | `NO CHANGE` | Correct routing identity. |
| `harness_run_ref` | `NO CHANGE` | Opaque cross-reference. |
| `assignment_id` | `NO CHANGE` | Required correlation. |
| `sequence` | `NO CHANGE` | Outbox-owned monotonic sequence starting at one. |
| `status` | `REQUIRED CHANGE` | Add cancelled and normative Harness mapping. |
| `progress` | `REJECT` as 0–1 scalar | Replace with structured factual progress. |
| `last_action` | `RECOMMENDED CHANGE` | Bound it and optionally attach operation/evidence ref; keep observational. |
| `needs_escalation` | `RECOMMENDED CHANGE` | Remove or mark derived. EscalationRequest is authoritative. |
| `evidence_refs` | `RECOMMENDED CHANGE` | Define snapshot versus delta semantics and maximum count. |
| `occurred_at` | `NO CHANGE` | Event time; sequence remains ordering authority. |
| `idempotency_key` | `REQUIRED CHANGE` | Require or formally derive. |
| `payload_digest` | `REQUIRED CHANGE` | Make required. |
| missing source revision | `REQUIRED CHANGE` | Add committed task/source revision. |

### 6.5 EscalationRequest — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Align version policy. |
| missing `escalation_id` | `REQUIRED CHANGE` | Required durable identity. |
| `platform_run_id` | `NO CHANGE` | Correct. |
| `assignment_id` | `NO CHANGE` | Correct. |
| `reason` | `RECOMMENDED CHANGE` | Rename to `reason_code`. Add failure/transport distinctions. |
| `mode` | `NO CHANGE` | CONSULT/HANDOFF separation is correct. |
| `evidence_refs` | `REQUIRED CHANGE` | Require non-empty for failure escalation. |
| `required_capability` | `NO CHANGE` | Hint to Pi; no target selection. |
| `context` | `RECOMMENDED CHANGE` | Bounded rationale only; no unbounded transcript. |
| `requested_at` | `NO CHANGE` | Correct. |
| `idempotency_key` | `REQUIRED CHANGE` | Make required. |
| `payload_digest` | `REQUIRED CHANGE` | Make required. |
| missing failure/checkpoint/action fields | `REQUIRED CHANGE` | Add signature, ordinal/count, attempted actions and checkpoint ref. |

### 6.6 ContextPack — `ACCEPT_WITH_CHANGE`

ContextPack can enter existing compilation without replacing it:

- agent path: validate/digest-check, turn entries into provenance-bearing fragments before `context.Compiler.Compile`;
- task path: bind a pinned ContextPack reference/projection into `StepPacket` before calculating `CanonicalPacketHash`;
- remote architecture/decisions/patterns remain untrusted project knowledge, never `KindPolicy` merely because Pi supplied them;
- local policy and assignment constraints retain higher authority.

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Align version policy. |
| `context_id` | `NO CHANGE` | Immutable identity. |
| `project_id` | `REQUIRED CHANGE` | Required-but-null is ambiguous. Use non-null for project packs or explicit scope object. |
| `task_ref` | `RECOMMENDED CHANGE` | Rename to `task_id` and state it is Platform-owned. |
| `sections.architecture` | `NO CHANGE` | Project knowledge fragment, not policy. |
| `sections.decisions` | `NO CHANGE` | Preserve provenance/version. |
| `sections.constraints` | `REQUIRED CHANGE` | Mark constraint authority/source; knowledge constraints cannot override assignment/local policy. |
| `sections.patterns` | `NO CHANGE` | Select by relevance. |
| `sections.failures` | `NO CHANGE` | Evidence-bearing historical input. |
| `sections.solutions` | `NO CHANGE` | Evidence-bearing historical input. |
| `sections.conventions` | `NO CHANGE` | Project knowledge. |
| `sections.evidence_refs` | `RECOMMENDED CHANGE` | Bound count and retrieve lazily. |
| `provenance.sources` | `REQUIRED CHANGE` | Add source kind/version, admission state/time and digest; `path@version` alone is too informal. |
| `freshness.generated_at` | `NO CHANGE` | Correct. |
| `freshness.max_source_age_days` | `RECOMMENDED CHANGE` | This reports selection policy, not proof each source is fresh; include oldest source time or per-entry time. |
| `version` | `NO CHANGE` | Monotonic version. |
| missing `content_digest` | `REQUIRED CHANGE` | Assignment must pin and Harness must verify the immutable pack. |

Add `maxItems`, `maxLength` and total encoded-size limits. The compiler has token budgets, but wire decoding still needs byte/list bounds.

### 6.7 VerificationEvidence — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `kind` | `NO CHANGE` | Maps to test/build/manual/review/reproduction claims. |
| `command` | `RECOMMENDED CHANGE` | Display/evidence only; never execute a command received inside knowledge. Prefer executable + args if structure is useful. |
| `scope` | `RECOMMENDED CHANGE` | Define repository/worktree/subject revision, not free-form authority. |
| `outcome` | `REQUIRED CHANGE` | Align with Harness pass/fail/blocked/unknown or publish an exact mapping; `partial` is ambiguous. |
| `evidence_refs` | `REQUIRED CHANGE` | Require at least one for pass/fail claims. Harness refuses passing validation without evidence. |
| `observed_at` | `NO CHANGE` | Correct. |
| missing subject/check identity | `REQUIRED CHANGE` | Add check ID and subject revision so evidence cannot close a newer task revision. |

### 6.8 KnowledgeCandidate — `ACCEPT_WITH_CHANGE`

It can wrap `learning.Digest` and existing verification/artifact refs. Submission must create an inert Pi candidate. It must never call Harness `PromoteCandidate`, Pi promotion, or mutate shared knowledge directly.

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Align version policy. |
| `candidate_id` | `NO CHANGE` | Client-generated idempotent source ID; Pi may return its own admitted record ID. |
| `project_id` | `RECOMMENDED CHANGE` | Prefer explicit `{scope_kind, scope_ref}` to support project/global/repository scopes. |
| `kind` | `NO CHANGE` | Broad enough for current learning output. |
| `title` | `NO CHANGE` | Bound length. |
| `problem` | `NO CHANGE` | Bound length and treat as untrusted content. |
| `solution` | `RECOMMENDED CHANGE` | Require for solution/pattern/convention/lesson; allow null for failure-only candidate. |
| `diff_ref` | `OPTIONAL IMPROVEMENT` | Useful for code-derived learning. |
| `tests` | `RECOMMENDED CHANGE` | Duplicates verification and can be model claims; treat as suggested checks or remove. |
| `verification` | `REQUIRED CHANGE` | Use an array; a verified task can have tests, build and independent review. Require evidence for admitted trust. |
| `provenance.agent_id` | `NO CHANGE` | Required attribution, not automatic trust. |
| `provenance.platform_run_id` | `RECOMMENDED CHANGE` | Require for managed-run candidates; allow null only for simple-agent submissions. |
| `provenance.evidence_refs` | `REQUIRED CHANGE` | Require non-empty for verified claims. Add node, assignment/task and local source refs. |
| `related_refs` | `RECOMMENDED CHANGE` | Useful hint but insufficient for conflict checks; Pi must search independently. Add optional supersedes/conflicts hints without trusting them. |
| `submitted_at` | `NO CHANGE` | Correct. |
| `idempotency_key` | `REQUIRED CHANGE` | Make required. |
| `payload_digest` | `REQUIRED CHANGE` | Make required. |

### 6.9 CapabilityAdvertisement — `ACCEPT_WITH_CHANGE`

| Field | Result | Finding |
|---|---|---|
| `contract_version` | `REQUIRED CHANGE` | Advertisement should list supported version range/schema IDs, not only its own envelope version. |
| `agent_id` | `NO CHANGE` | Platform identity. |
| `node_id` | `NO CHANGE` | Platform node identity. |
| `client_class` | `NO CHANGE` | Simple/managed distinction is useful. |
| `capabilities` | `RECOMMENDED CHANGE` | Add `uniqueItems`; distinguish coarse advertised capability from assignment-time qualification/readiness. |
| `supported_modes` | `NO CHANGE` | Correct. |
| `knowledge_kinds` | `OPTIONAL IMPROVEMENT` | Useful routing metadata. |
| `advertised_at` | `NO CHANGE` | Correct. |
| missing expiry/instance/revision | `REQUIRED CHANGE` | Add `advertisement_id` or client instance, `expires_at`, supported contracts/schema revisions and capability revision/digest. |

## 7. Transport, ACK and replay

Classification: `ACCEPT_WITH_CHANGE`.

SQLite plus the existing startup recovery model is suitable for a local outbox. A future outbox row should contain Platform run, sequence, payload digest, payload, delivery state, attempts, next-attempt time, acknowledged time and source local revision. It must be appended in the same transaction as the source mutation or by a durable projector whose cursor makes missed commits recoverable.

**REQUIRED CHANGE**

1. Identify endpoint ownership:
   - Harness PUSHes to a Pi endpoint;
   - Pi returns/persists a defined ACK;
   - after reconnect, Harness queries Pi for `acked_seq` and replays its local outbox;
   - if Pi also pulls, define a separate authenticated Harness outbox endpoint. A Pi-hosted `GET /runs/.../updates` cannot retrieve an update Pi never received.
2. Add an ACK schema with `platform_run_id`, `acked_seq`, receiver timestamp and optional expected next sequence.
3. Define sequence allocation transactionally and never reuse a sequence after rollback/restart.
4. Delivery retry exhaustion changes delivery health only. It must not mutate task progress to blocked and must not fabricate `NO_PROGRESS`/`STUCK`.
5. Define disconnected execution policy relative to claim expiry. After write authority expires, Harness must stop new writes even if local inference can continue read-only.
6. Define retention until ACK plus a safety interval. The current “run lifetime + 7 days” is acceptable for idempotency records but must state what happens to unacknowledged updates after that period.

**NO CHANGE**

- No broker.
- Highest contiguous ACK.
- Duplicate same sequence/digest is a no-op; conflicting digest is an error.
- Update replay is data delivery only and never replays effects.

## 8. CONSULT and HANDOFF

### CONSULT — `ACCEPT_WITH_CHANGE`

The original Harness run, lease and write authority remain unchanged. Advice returns as immutable evidence or a pinned ContextPack addition. The Decision Engine may consider it on the next local decision, but Pi cannot select or execute a `CandidateAction`.

Required additions are an `escalation_id`, advice/result correlation, evidence digest, and a clear statement that consultation timeout/rejection does not alter authority.

### HANDOFF — `REJECT` until two-phase fencing is specified

Current Harness has renewable local `Run.LeaseToken`, `LeaseGeneration` and `LeaseExpiresAt`. It has recovery for expired leases, but no explicit live-run “release for handoff” transition. Pi claim revocation alone cannot prove local execution stopped, especially during disconnection.

Required protocol:

```text
1. Harness -> Pi: EscalationRequest(HANDOFF, escalation_id)
2. Pi -> Harness: prepare_handoff / stop-renewing authority
3. Harness:
   - stop scheduling new effects
   - wait for or reconcile every dispatched effect
   - abandon safe pre-dispatch intents
   - persist checkpoint
   - transition local run to paused/handed_off terminal ownership state
   - invalidate local lease token and advance fencing generation
4. Harness -> Pi: HandoffReleaseReceipt
   - checkpoint/evidence refs
   - local run ref and final lease generation
   - unresolved effects must be zero, or Pi must route only reconciliation
5. Pi durably revokes old claim
6. Pi grants a strictly higher authority generation to the new assignment
7. New worker begins
```

Pi must not grant the new writer before step 4/5 is durably complete. If the old node is unreachable, Pi waits for claim/lease expiry and uses a fencing generation that every write gate validates. This preserves the Harness no-replay invariant and prevents split-brain writes.

## 9. ContextPack insertion mapping

Classification: `ACCEPT_WITH_CHANGE`.

| ContextPack section | Agent compiler mapping | Durable StepPacket mapping |
|---|---|---|
| architecture | `KindProjectInstruction`, untrusted/provenance-bound | selected summary/ref in packet context extension |
| decisions | project instruction or active evidence based on task relevance | decision refs relevant to step |
| constraints | assignment constraint only if authority says so; otherwise project knowledge | requirement revision for authoritative constraints; knowledge ref otherwise |
| patterns | selected project knowledge | optional selected context refs |
| failures | active evidence/checkpoint material | validation/failure evidence refs |
| solutions | selected project knowledge | optional selected context refs |
| conventions | project instruction | optional selected context refs |
| evidence_refs | artifact receipts, fetched lazily | bounded references, not payloads |

The compiler continues to own token budgeting, deduplication, spill, selection, compaction and integrity. `StepPacket` continues to own the canonical hash. A ContextPack version/digest must be part of the hashed packet if used for the step.

## 10. Evidence mapping

Classification: `ACCEPT_WITH_CHANGE`.

| Harness source | EvidenceRef representation | Condition |
|---|---|---|
| Artifact/CAS | `artifact` or `cas_object`, local ID/ref + stored checksum | Direct fit |
| Agent event | `event`, event ID + canonical event digest | Event is immutable |
| Context snapshot | Add `context_snapshot` or represent as artifact | Immutable snapshot only |
| Background job | `job`, terminal job snapshot digest | Do not reference a still-mutating job as immutable evidence |
| Validation | `validation`, validation ID + canonical digest | Direct fit; subject revision retained in retrieval |
| Effect | Add `effect_receipt`; operation ID + immutable observed/reconciled receipt | Never expose mutable planned/dispatched row as final evidence |
| Code proposal/diff | `proposal`/`diff` or artifact | Proposal and preimage hashes retained |
| Review | `review` or artifact | Reviewer/provider provenance retained |
| Screenshot | artifact with MIME/checksum | Direct fit |

Pi may index metadata. Payload retrieval must occur through an authenticated, allowlisted reference resolver and verify the digest. An unavailable origin must produce `EVIDENCE_UNAVAILABLE`; it must never silently convert an unverified reference into accepted proof.

## 11. KnowledgeCandidate mapping

Classification: `ACCEPT_WITH_CHANGE`.

Harness already builds a bounded `learning.Digest` containing goal/constraints, outcome, decisions, tool receipts, `VerifiedBy`, activations, corrections, artifacts and redactions. The learning reviewer may create a `skills.Candidate`, which remains inert until lint/security/replay/review/promotion.

The Platform adapter can submit a projection of this digest plus task verification artifacts. It must not:

- call local or Pi promotion;
- state that a suggested test ran without an evidence receipt;
- treat agent/model identity as trust;
- treat `related_refs` as a completed conflict search; or
- upload private artifact payloads without an explicit export grant.

Pi should return an admission receipt with candidate ID, payload digest and state such as queued/rejected/needs_review/admitted. Admission remains Pi/Second Brain authority.

## 12. Existing Harness decision and effect invariants

Classification: `ACCEPT`.

The adapter must not modify:

- `taskengine.CompactState` as the bounded, derived decision input;
- `taskengine.CandidateAction` vocabulary generation;
- `taskengine.DecisionEngine` interface;
- deterministic `RuleDecision` baseline;
- `taskcoord.BonsaiDecision`, shadow evaluation and benchmark admission;
- the rule that Decision Engine selects but cannot execute;
- task revision checks, run leases, effect intent state machine or approval gates;
- the rule that an uncertain effect is reconciled, never automatically replayed.

`RunUpdate` replay, assignment duplicate handling, consultation results and Pi retries are transport/data operations. None may invoke `PlanEffect`, `DispatchEffect`, workspace apply, tool call or provider request implicitly.

## 13. Error and compatibility corrections

Classification: `ACCEPT_WITH_CHANGE` for errors; `REJECT` for compatibility as written.

**REQUIRED CHANGE**

- Add `STALE_REVISION`, `CLAIM_EXPIRED`, `OUTCOME_UNCERTAIN`, `EVIDENCE_UNAVAILABLE`, and `HANDOFF_NOT_RELEASED`.
- `INTERNAL` must not automatically be retryable for mutations. Retryability depends on whether the server proved that no effect crossed dispatch. Unknown outcome returns `OUTCOME_UNCERTAIN` and requires reconciliation.
- State that `WRITE_GATE_DENIED` remains denied for the same claim, scope, payload and revision. A materially new authorized request is a new operation, not a retry.
- Resolve forward compatibility in one of two ways:
  1. lock v1 to exact `1.0.0`, closed schemas, and negotiate a new schema ID for additions; or
  2. permit defined extension containers/unknown optional fields and test them.

The current combination of exact `const: 1.0.0`, `additionalProperties: false`, “minor versions add enum values,” and “consumers ignore unknown fields” cannot all be true.

For v1, exact version plus closed schemas is the safer choice. Additive capability versions can be introduced as separately advertised schema revisions after both sides implement them.

## 14. Contract mechanics corrections

**REQUIRED CHANGE**

- Define canonical JSON, preferably RFC 8785/JCS, and state that `payload_digest` is calculated with the `payload_digest` field omitted.
- Use one digest representation consistently: `sha256:<64 lowercase hex>`.
- Make idempotency keys and payload digests required on every mutation.
- Use complete resolvable `$ref` URIs or a bundled schema registry. Bare `"AccessScope"` and `"EvidenceRef"` require a resolver policy that is currently unstated.
- Configure validators to enforce `format: date-time`; JSON Schema treats formats as annotations unless the validator enables assertions.
- Require UTC `Z`, define maximum clock skew, and keep sequence/revision authoritative for ordering.
- Add `maxLength`, `maxItems`, `uniqueItems` and total document byte limits.
- Define duplicate receipts: same idempotency key and digest returns the original resource/receipt; a different digest conflicts.
- State acknowledgement durability and transaction boundary.

**RECOMMENDED CHANGE**

- Include schema content digests in capability discovery.
- Provide shared golden valid/invalid fixtures used by both Pi and Harness CI.

## 15. Answers to the nine Harness questions

1. **TaskAssignment mapping:** Yes, after required field changes. It maps to `CreateTaskInput` plus a Platform/local mapping record. No new task type is required.
2. **RunUpdate generation:** Yes. Status/progress must be projected from committed Task/Run/Attempt/Effect/Validation records; sequence is allocated by the outbox. Progress must be structured, not 0–1.
3. **Outbox/ACK/replay:** Yes. SQLite is a good fit, provided endpoint direction, transactional enqueue, delivery health and ACK semantics are corrected.
4. **CONSULT:** Yes. Advice is evidence/context and the existing Harness run/lease remains owner.
5. **HANDOFF:** Not yet. There is no current explicit live-lease release method. Contract must require a two-phase release receipt and fencing generation before re-grant.
6. **ContextPack:** Yes. It can become provenance-bearing compiler fragments and a pinned StepPacket extension. It does not replace either subsystem.
7. **EvidenceRef:** Partially. Artifacts/CAS/events/validations fit directly; jobs/effects need immutable terminal snapshot/receipt rules and the enum must cover effects.
8. **KnowledgeCandidate:** Yes after field changes. Existing learning digest is suitable source evidence; `related_refs` is only a hint and is insufficient as Pi conflict admission.
9. **Decision/effect invariants:** Confirmed. They remain Harness-owned and untouched. Transport replay must never dispatch an effect.

## 16. Required changes before lock

1. Add Platform `task_id`, `title`, `original_request`, stable structured acceptance criteria and egress mapping to TaskAssignment.
2. Require idempotency/digest fields and define canonical digest calculation.
3. Replace the assignment `credential_ref` object with a verifiable, expiring, fenced claim contract.
4. Remove repository/worktree duplication or define equality; require worktree for writes.
5. Replace scalar RunUpdate progress with the structured receipt-derived projection and add source revision/cancelled mapping.
6. Separate transport delivery health from execution progress; remove “delivery exhausted → task blocked/NO_PROGRESS.”
7. Define assignment receipt and ACK schema plus endpoint ownership/recovery direction.
8. Specify two-phase HANDOFF with local lease invalidation/release receipt and monotonic fencing generation.
9. Expand EscalationRequest with identity, failure/action/checkpoint and authority-release fields.
10. Make EvidenceRef immutable and complete its enum/retrieval semantics.
11. Pin ContextPack by version/digest and strengthen provenance/authority/size bounds.
12. Align VerificationEvidence with Harness validation status, subject revision and evidence requirement.
13. Change KnowledgeCandidate verification to evidence-backed plural records and strengthen provenance/scope.
14. Resolve strict-schema versus forward-compatibility contradiction.
15. Add safe uncertain-outcome/stale-revision/claim/evidence errors and mutation retry rules.

## 17. Recommended changes

- Keep a single canonical managed context retrieval operation and hide legacy aliases from model-visible catalogs where possible.
- Add optional unknowns and expected VCS revision to TaskAssignment.
- Add Harness task/attempt/lease cross-references to RunUpdate for audit.
- Add advice correlation and a bounded response deadline to CONSULT.
- Add capability advertisement expiry, instance identity, schema versions/digests and readiness distinction.
- Add a candidate admission receipt and independent Pi conflict search.
- Document evidence retention/offline/decommission behavior.
- Change §20 wording from “thin adapter only” to “additive integration layer plus a small explicit handoff transition and ContextPack packet binding.” Core decision and effect machinery remains unchanged.

## 18. Optional improvements

- Add a non-authoritative complexity/risk/budget hint for routing.
- Add source-age details per ContextPack entry.
- Add supersedes/conflicts hints to KnowledgeCandidate.
- Add typed executable/arguments to verification display data, while forbidding execution from knowledge content.
- Add contract trace IDs separate from security/audit IDs.
- Add evidence availability/retention hints that never substitute for retrieval authorization.

## 19. No-change findings

- Platform IDs remain separate from Harness IDs.
- Pi never owns or sends an authoritative local absolute path.
- Pi owns Platform routing; Harness owns local decisions/actions.
- One local project/root remains the Harness filesystem authority.
- Context compiler and StepPacket remain authoritative for their existing roles.
- CompactState, CandidateAction, DecisionEngine, RuleDecision and BonsaiDecision remain untouched.
- Existing durable Task/Run/Attempt/Effect/Checkpoint/Validation state is reused.
- Existing artifact/CAS/event/job/validation stores are reused rather than copied into a second evidence database.
- Knowledge submission remains inert and promotion stays with Pi/Second Brain.
- No automatic effect replay is introduced.
- No Kafka, Redis or message broker is needed.
- Production write/deployment remains behind the Pi P0 safety path; local Harness effects retain their own lease/policy gates.

## 20. Minimum safe revised shapes

These are field requirements for revision, not implementation schemas.

### TaskAssignment minimum

```text
contract_version, assignment_id, task_id, platform_run_id,
agent_id, node_id, workspace_id/project_id as applicable,
repository_id, worktree_id for write,
access_scope + authority_generation + claim_id/expiry,
title, goal, original_request,
acceptance_criteria[{id,description}], constraints[], unknowns[],
egress_policy, pinned_context_ref{id,version,digest},
issued_at, idempotency_key, payload_digest
```

### RunUpdate minimum

```text
contract_version, platform_run_id, assignment_id, sequence,
status, task_revision/source_revision,
structured progress, evidence delta/snapshot semantics,
occurred_at, idempotency_key (or deterministic rule), payload_digest
```

### HANDOFF minimum

```text
EscalationRequest(HANDOFF)
  -> prepare acknowledgement
  -> Harness checkpoint + effect reconciliation + local lease fencing
  -> HandoffReleaseReceipt(checkpoint, evidence, final generation)
  -> Pi revoke old claim
  -> Pi issue new assignment with higher authority generation
```

## 21. Review conclusion

The draft has the correct system boundary and reuse strategy. The required changes are narrow enough to revise the contract without changing the architecture. They are necessary because the present schemas either omit facts the Harness requires, represent derived state inaccurately, or leave an authority gap during HANDOFF.

No Harness implementation should begin until the fifteen required contract changes in §16 are resolved and shared golden fixtures validate on both sides.

HARNESS VERDICT:
NEEDS_CONTRACT_REVISION
