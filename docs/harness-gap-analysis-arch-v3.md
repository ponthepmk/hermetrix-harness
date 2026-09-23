# Agent Harness — Final Gap Analysis and Platform Integration Contract Discovery

**Status:** Contract discovery only
**Repository inspected:** `hermetrix-harness`
**Inspection date:** 2026-09-21
**Scope boundary:** This report does not implement Platform Router behavior, new shared schemas, a new Decision Engine, MCP changes, or autonomous execution.

## Executive conclusion

The current Harness is substantially ahead of the assumptions in the proposed Architecture v3. It already has two execution paths:

1. a conversational agent loop owned by `internal/agent.Service`, with immutable session contracts, context compilation, direct and deferred tools, approval pauses, budgets, loop detection, durable events, and turn recovery; and
2. a durable software-task path owned by `internal/taskengine.Service` plus `internal/taskcoord.Service`, with requirements, plans, steps, run leases, attempts, effect intents, proposals, verification, independent review, checkpoints, failure fingerprints, bounded escalation, progress derivation, and crash recovery.

It also already contains the proposed decision primitives: `CompactState`, `CandidateAction`, `DecisionEngine`, deterministic `RuleDecision`, model-backed `BonsaiDecision`, shadow evaluation, benchmark admission, and a read-only admitted path. These should be reused. Creating parallel versions would split authority and evidence.

The main Platform integration gap is not agent reasoning. It is a narrow distributed-control contract around the existing durable task engine:

- stable external identities for node, agent, platform session, repository, workspace, and worktree;
- a local binding from Platform IDs to a Harness-owned absolute root path;
- idempotent assignment ingestion;
- durable, sequenced run updates with acknowledgement and replay;
- structured escalation requests routed through Pi;
- a structured ContextPack projection; and
- candidate-only knowledge submission with Pi-side admission.

The recommended boundary remains:

```text
Pi Platform: who / where / tier / task / project / repository / worktree / permissions
       |
       v
TaskAssignment
       |
       v
Harness: state reduction / next action / tools / evidence / local recovery
```

Pi must not send an authoritative foreign-node filesystem path. Harness should resolve `worktree_id` through a local binding and reject assignments whose node or binding does not match the local installation.

## 1. Current Harness architecture map

### Process composition and entry points

The executable entry point is `cmd/hermetrix/main.go:41`. `runServe` at `cmd/hermetrix/main.go:76` builds the process graph:

```text
cmd/hermetrix.main
  -> runServe
     -> store.Open (SQLite + migrations + blob store)
     -> inference gate/scheduler
     -> skills + learning + curator
     -> taskengine.Service
     -> product.Service
     -> context.Compiler
     -> providers.Service
     -> qualification.Service
     -> capabilities.Catalog
     -> mcp.Service
     -> tools.Registry
     -> agent.Service
     -> taskcoord.Service
     -> web.Server
     -> optional Discord bridge
```

Startup recovery is explicit. `runServe` invokes product job recovery, agent approval recovery, agent turn recovery, learning recovery, and durable task recovery at `cmd/hermetrix/main.go:160`, `:253`, `:259`, `:265`, and `:271`.

### Main modules

| Module | Current responsibility | Key code |
|---|---|---|
| `internal/agent` | Interactive session/turn loop, context assembly, tool calls, approvals, streaming | `service.go:398`, `:691`, `:1009`, `models.go` |
| `internal/taskengine` | Authoritative long-lived task, run, attempt, effect, validation, decision and checkpoint state | `service.go`, `execution.go`, `decision.go`, `progress.go`, `packet.go` |
| `internal/taskcoord` | Bounded planner/worker/apply/verify/review orchestration over task-engine authority | `service.go:157`, `:294`, `:423`, `:607`, `:705`, `:924` |
| `internal/worker` | Proposal-only planner, file selector, code worker and reviewer model boundaries | `planner.go`, `file_selection.go`, `proposal.go`, `review.go` |
| `internal/context` | Token-budgeted context selection, deduplication, spilling, compaction and integrity reporting | `compiler.go:46`, `types.go` |
| `internal/tools` | Core tool schemas, registry, path confinement, execution and receipts | `registry.go`, `definitions_runtime.go` |
| `internal/mcp` | Streamable HTTP and stdio MCP clients, discovery, catalog, invocation and pooled sessions | `service.go`, `client.go`, `stdio.go`, `pool.go` |
| `internal/providers` | OpenAI-compatible, Anthropic and Gemini adapters; streaming and usage accounting | `models.go`, `service.go:237`, adapter files |
| `internal/inference` | Local-resource scheduling, usage ledger and immutable role presets | `scheduler.go`, `presets.go` |
| `internal/product` | Projects, workspace files, commands, artifacts, memories, terminals, browser and portability | `service.go`, `workbench.go`, `models.go` |
| `internal/learning` / `internal/skills` | Evidence digest, candidate creation, review, replay, promotion and rollback | `learning/models.go`, `skills/service.go` |
| `internal/web` | HTTP API, auth/boundary middleware, UI and SSE/stream endpoints | `server.go`, `auth.go`, `boundary.go` |

### Persistence

SQLite is the authoritative local metadata store. Schema creation and migrations are in `internal/store/store.go`. Relevant tables include:

- agent: `agent_sessions`, `agent_events`, `agent_event_parts`, `context_snapshots`, `step_bindings`, `tool_approvals`;
- task: `durable_tasks`, requirement/plan revisions, `task_steps`, `task_checkpoints`, `task_validations`, `task_runs`, `task_step_attempts`, `task_effect_intents`;
- orchestration: planning decisions, failures, escalations, planner runs, code proposals/reviews, decision shadow/benchmark/admission records;
- product/evidence: `projects`, `artifacts`, `background_jobs`, `memories`, terminal/browser records;
- integration: MCP server/tool/resource/prompt catalogs, provider profiles, runtime fingerprints, inference presets and usage ledger.

Large bodies are stored by reference in the blob/CAS subsystem and represented by durable artifact records. SQLite uses foreign keys and transactional state transitions. This is already an evidence store foundation; a second database is unnecessary.

## 2. Identity and lifecycle map

All internal generated IDs are strings created by `identity.New(prefix)` in `internal/identity/id.go:11`, using a type prefix plus 16 random bytes encoded as hex. The prefix is descriptive, not a namespace shared with Pi.

| Requested identity | Exists? | Current name/type | Created by | Stored/lifecycle | Main users |
|---|---|---|---|---|---|
| `node_id` | Missing | None | N/A | N/A | No module |
| `agent_id` | Partial, ambiguous | Team member `ID string`; authenticated principal; no execution-agent identity | Product/team creation or server config | Team rows/principal context; persistent for team, request-bound for principal | `internal/product`, `internal/web`, `internal/identity` |
| `session_id` | Existing locally | `agent.Session.ID string` | `agent.CreateSession` | `agent_sessions`; from creation until retained/deleted; persistent | agent events, approvals, snapshots, artifacts, inference ledger |
| `run_id` | Existing, multiple domains | `taskengine.Run.ID`, product team run ID, background job ID | respective service | SQLite; persistent | task execution, teams, jobs |
| `task_id` | Existing, multiple domains | durable `taskengine.Task.ID`; team task ID; worker task ID | task engine/team coordinator/caller | durable task tables or team tables | task engine, coordinator, web, learning |
| `project_id` | Existing locally | `product.Project.ID string`; referenced by `taskengine.Task.ProjectID` | `product.SaveProject` | `projects`; persistent | product, agent session, task engine, artifacts |
| `workspace_id` | Missing as an identity | “workspace” currently means a local root or migration package, not a row/entity | N/A | No authoritative workspace identity | product portability and UI wording only |

### Identity collisions and required namespacing

`run_id` and `task_id` are semantic names used by more than one subsystem. They are safe inside current tables but unsafe as unqualified distributed identifiers. Platform contracts must specify an ID domain, or use explicit fields such as `platform_run_id` and `harness_run_id` in local mappings.

`agent_id` must not be inferred from:

- the authenticated `principal`, which identifies the installation/user trust subject;
- a provider profile, which identifies a model endpoint/configuration; or
- an `AgentTeam` member, which is a product-level role definition.

Recommended mapping rules:

- Pi owns `node_id`, `agent_id`, `workspace_id`, `repository_id`, `worktree_id`, Platform `session_id`, `assignment_id`, and Platform `run_id`.
- Harness owns local `project_id`, local agent `session_id`, durable task ID, local task run ID, attempt ID, effect operation ID, event ID and artifact ID.
- A new local binding record should cross-reference the two domains. Existing IDs should not be overwritten or forced equal.

## 3. Agent loop trace

### Interactive path

The concrete loop is owned by `internal/agent.Service`:

```text
HTTP/Discord input
  -> Service.RunTurn                         service.go:398
  -> validate immutable SessionContract
  -> acquireTurn (session lease + user event) service.go:531
  -> initialize frozen skill selection
  -> InferenceGate.RunForeground
  -> runAgentLoop                            service.go:691
       -> load durable events
       -> compileTurn                        service.go:1800
       -> context.Compiler.Compile           context/compiler.go:46
       -> freezeStep + context snapshot      service.go:2013
       -> providers.Service.StreamChat       providers/service.go:237
       -> if tool calls:
            executeToolCalls                 service.go:1009
            persist tool call/result events
            pause for approval if required
            continue next model step
       -> otherwise completeTurn + assistant event
```

Termination occurs when the model returns no tool calls and a usable assistant result, an approval pause is reached, a budget/timeout/loop guard fires, provider execution fails, or the context is invalid.

Default session budget is created in `internal/agent/service.go:207`: 12 model steps, 24 tool calls, 600 seconds, and cumulative tokens equal to six context profiles. `RunTurn` applies the wall-clock deadline at `:462`. `runAgentLoop` enforces model-step, tool-call and cumulative-token limits at `:707`, `:866`, `:870`, and around provider usage. A third identical non-poll tool call is rejected by the signature loop detector near `:883`.

There is no generic automatic retry of an interactive model step. Provider failures fail the turn. Incomplete streamed provider evidence is recorded as `provider_incomplete` but is not executed. Tool results, including failures, are returned to the model as durable events; the model may choose a different action on the next step. Effectful tool calls are never blindly replayed.

### Durable software-task path

The more suitable Platform integration path is:

```text
Task create/revise
  -> AutoPlan / CreatePlan
  -> BuildNextStepPacket
  -> BeginRun (lease)
  -> BeginStepAttempt
  -> SelectFiles
  -> Propose (model has no filesystem authority)
  -> explicit proposal decision
  -> Apply under RunAuthority
  -> Verify using frozen checks
  -> independent Review
  -> CompleteAttempt / CompleteStep / CompleteTask
  -> learning candidate enqueue after verified completion
```

The task engine owns authoritative state; the coordinator executes bounded phases. The planner and worker are already separated. The planner can only submit a plan. The proposal worker receives caller-curated file content and can only submit structured changes. Apply, test and review remain separate gates.

The model controls tool selection in the interactive loop, but it does not own durable task state transitions or effects in the software-task path. This distinction should remain.

## 4. State and context analysis

### Existing state equivalents

There is no single cross-path `AgentState`, but most required data already exists in structured form.

| Desired field | Existing source | Form/durability |
|---|---|---|
| Goal | `Task.Objective`, `OriginalRequest`; current user goal fragment | Structured/persistent |
| Current step | `Task.Plan.Steps`, `StepAttempt`; `CompactState.CurrentStep` | Structured/persistent |
| Task | `taskengine.Task` | Structured/persistent |
| Repository | `Project.RootPath`, `StepPacket.ProjectID` | Structured locally; no repository identity |
| Changed files | `worker.Result.Changes`, code proposal/rollback artifacts | Structured/persistent after proposal |
| Git state | Command output only if explicitly queried | Conversation/evidence-derived; no first-class branch/HEAD/worktree state |
| Test state | `Validation`, verification bundle, command job/artifact | Structured/persistent |
| Errors | step/attempt/run errors, failure records, agent events | Structured/persistent |
| Tool history | `agent_events`, receipts and effect intents | Structured/persistent |
| Last action/result | latest event/effect/attempt; `CompactState` candidates | Derived from durable records |
| Context | context snapshots + compiled report | Structured/persistent per model step |
| Model | provider profile/revision, preset, runtime fingerprint | Structured/persistent bindings |
| Available tools | frozen session `ToolBindings`; capability catalog | Structured/persistent snapshot/catalog |

`taskengine.CompactState` already exists in `internal/taskengine/decision.go`. It is a bounded, evidence-referenced projection containing task/revision, current step, completed steps, failed checks, evidence references, unresolved effects, criteria, checkpoint, consecutive failures and completion eligibility. It is derived from authoritative task records and is explicitly not a second source of truth.

Do not create another `CompactState`. Extend this projection only when a contract-locked action requires an additional fact.

### Context construction

`agent.compileTurn` (`internal/agent/service.go:1800`) converts durable session events and frozen contract data into typed `context.Fragment` values. The compiler receives system/policy, project/selected skills, user goal/conversation, tool call/result pairs, approval decisions, open work and checkpoints. Multipart content is attached to the most recent user message after compilation.

`context.Compiler.Compile` (`internal/context/compiler.go:46`) then:

1. validates the selected context profile;
2. charges exact serialized direct-tool schema cost;
3. deduplicates fragments;
4. preserves causal call/result pairs;
5. spills oversized tool results to CAS and replaces them with artifact references/previews;
6. allocates system, tools, skills/project, pinned and active slices;
7. compacts dropped active evidence into a verified checkpoint;
8. checks token ledger and causal integrity; and
9. returns selected fragments plus a detailed `Report`.

The current interactive path does not indiscriminately send full terminal history or repository source. It sends the selected event history, selected skills/project instructions, direct tool schemas, and the results the agent previously requested. Source enters through read tool results; terminal output enters through `workspace.run` results; MCP results enter through deferred tool receipts. Large results are spilled.

Growth pressure remains in long sessions because every step re-derives context from the full durable event list before selection, tool call/result evidence accumulates, and direct tool schemas are charged on every request. The compiler already limits provider-facing growth. The future insertion point for a Platform ContextPack is `agent.compileTurn` as one typed, provenance-bearing fragment or, for durable tasks, `StepPacket` construction. The insertion point for state reduction is `taskengine.BuildCompactState`, not inside provider adapters.

## 5. Evidence and result handling

| Evidence kind | Current storage | Has stable ID? | Independently retrievable? |
|---|---|---:|---:|
| Core tool call/result | `agent_events` with metadata; large body may spill to CAS | Yes, event ID and tool call ID | Events yes; spilled blob via artifact reference |
| Terminal/command output | `background_jobs` result/output and artifacts; terminal sessions keep output state | Yes | Yes through product APIs |
| Test output | command jobs + verification bundle artifact + `Validation.EvidenceRefs` | Yes | Yes |
| Logs/errors | event metadata, run/attempt/step error fields, failure rows | Usually | Yes for durable task/agent records |
| Diff/generated patch | `code_proposal` artifact with change preimages/hashes; rollback artifact | Yes | Yes |
| MCP result | tool result event/receipt, bounded to 2 MiB; large agent context may spill | Yes as event/call | Yes through session events; no global result index |
| Context sent to model | `context_snapshots` plus `step_bindings` | Yes | Yes locally |
| Side effect | `task_effect_intents` with operation ID and receipt | Yes | Yes |

Results are not only appended to an in-memory conversation. Agent events, snapshots, step bindings, task effects, validations and artifacts survive session completion and process restart. Volatile losses include active HTTP/SSE consumers, in-memory MCP pending elicitations, live subprocess/PTY handles, and any provider response that fails before it can be durably recorded.

Future `EvidenceStore` work should be a thin index/projection over existing artifact, event, validation, job and effect stores. It should not duplicate payloads. Contract `evidence_refs` should use typed opaque references such as `artifact:<id>`, `event:<id>`, `job:<id>`, `validation:<id>` and `effect:<operation_id>`.

## 6. Tool and MCP analysis

### Core tool abstraction

`internal/tools/registry.go` defines:

- `Definition`: name, description, JSON schema, revision, effect and approval flag;
- `Registry`: workspace-confined definitions and optional deferred capability catalog;
- `Receipt`: call ID, tool/revision/effect, status, output/error, duration and metadata;
- `ApprovalPlan` and `ApprovalGrant`: exact argument hash and effect binding.

Direct tools include workspace list/read/search/write, skill search/view/manage, context search, deferred tool search/describe/call, command execution and browser control. `Registry.For(root)` scopes the same definitions to a project root. File resolution evaluates symlinks and confines access to that root. Writes require an expected preimage hash and persisted approval. Runtime command execution uses an allowlist and no shell.

The current abstraction can support the proposed actions without a tool-system redesign:

| Candidate action | Existing mechanism | Gap |
|---|---|---|
| `inspect_file` | `workspace.read_file` | Map semantic action to existing tool |
| `search_repository` | `workspace.search_files`, file selector | None beyond action mapping |
| `run_test` | `workspace.run` or frozen coordinator verification | Prefer durable coordinator for software tasks |
| `edit_code` | proposal/apply path; interactive `workspace.write_file` | Durable tasks should use proposal/apply, not raw writes |
| `retrieve_memory` | product memory retrieval/skills/context search | No single direct agent memory action |
| `ask_planner` | `taskcoord.AutoPlan`, planner run records | Not exposed as a generic direct tool |
| `finish` | assistant completion or task completion gate | Already a state transition, not a tool |

Candidate actions should remain a decision vocabulary above tools. Do not register each as a second low-level tool unless it needs an independent effect/policy boundary.

### MCP

MCP support is existing and reusable:

- transports: Streamable HTTP and local stdio (`internal/mcp/models.go`);
- multiple server profiles persisted in `mcp_servers`;
- discovery for tools, resources and prompts, with pagination and catalog revisions;
- deferred `tool_search` -> `tool_describe` -> exact-revision `tool_call` flow;
- input and output JSON Schema validation;
- pooled stdio sessions with idle reaping;
- server-initiated sampling and elicitation through `internal/agent/mcpbridge.go`;
- credentials from environment or the local vault;
- conservative effects when annotations are untrusted;
- no automatic replay of `tools/call`, resource reads, prompt gets, or effectful failures;
- automatic reconnect/retry only for catalog listing operations after a broken stdio connection.

Missing for Pi integration are OAuth, per-call Platform identity propagation, assignment/run/task headers, resource subscriptions, and a distributed acknowledgement protocol. Static bearer token support exists; a Pi MCP server can be connected today, but it sees the credential/server connection rather than a cryptographically scoped agent/run identity.

Kanban, Second Brain and Infrastructure MCP can be consumed as separate MCP server records. Their tools/resources/prompts will appear in the shared deferred capability catalog. Before production use, Pi should define effect annotations, approval expectations, idempotency behavior and identity claims for each service. Infrastructure mutations must remain behind explicit policy and effect receipts.

## 7. Repository and worktree behavior

`product.Project` stores `ID`, `Name`, and absolute local `RootPath` (`internal/product/models.go:5`). `SaveProject` resolves and validates the root (`internal/product/service.go:170`, `:603`). File, command and terminal operations resolve project-relative paths under that root. The tool registry is similarly rebound per project by `Registry.For(root)`.

Current first-class state:

- repository/current workspace root: project `RootPath`;
- command working directory: project-relative input resolved locally;
- Git branch/HEAD: not persisted as structured state;
- worktree: not modeled;
- repository identity: not modeled;
- workspace identity: not modeled.

Git can be invoked through the command tool, so branch and HEAD may appear in evidence, but they are not part of authorization or stale-state validation.

**Recommendation: option B.** Pi sends `repository_id`, `worktree_id`, and target `node_id`; Harness resolves them through a local, owner-controlled binding to an absolute root. The binding should include at least node, repository, worktree, local project, resolved root, expected VCS kind, optional expected remote fingerprint, state and revision. The absolute path must never be accepted from a TaskAssignment as authority.

At assignment acceptance and before each write/apply, Harness should verify:

- assignment `node_id` equals this installation;
- the binding is active and owned by the authenticated principal;
- the resolved root still exists and stays within the bound location;
- optional repository fingerprint/remote matches;
- optional expected HEAD/branch constraints match if the assignment requires them.

## 8. Bonsai integration

The local model is represented by a normal `providers.Profile`, usually using the OpenAI-compatible adapter and a local base URL. Provider profiles bind model, context window, output cap, resource group, runtime fingerprint, revision and measured token calibration (`internal/providers/models.go`).

| Capability | Current status |
|---|---|
| Endpoint/protocol | Configurable OpenAI-compatible HTTP; native Anthropic/Gemini also exist |
| Model configuration | Persistent versioned profile with immutable per-dispatch validation |
| Context handling | Context profiles + compiler + token calibration + immutable step snapshot |
| Reasoning configuration | Immutable inference presets support disabled/bounded mode and token cap |
| Structured output | Tool/function schema plus strict JSON decode; no general response-format field |
| Logprobs | Missing from common request/response and adapters |
| Candidate scoring | Missing as logits/logprob scoring; current Bonsai chooses one supplied ID through structured tool output |
| Streaming | Supported; OpenAI-compatible SSE and adapter-normalized deltas |
| Retries | No general provider-call retry in normal execution; worker errors mark retryability for caller policy |
| Timeout | Caller context, turn deadline, MCP timeout and scheduler budgets |
| Resource control | Inference scheduler/gate serializes constrained local resources and persists usage |

One Bonsai server can support logical DECISION, WORKER and PLANNER profiles. The role distinction is already expressed by immutable inference presets (`internal/inference/presets.go`) and bounded prompts/contracts, while a provider profile points to the same endpoint/model. The inference scheduler can serialize calls sharing a local resource group. Three model processes are unnecessary unless measurement later shows incompatible KV-cache, latency, memory or model-version requirements.

Logical profiles do not create reviewer independence. Independent review currently requires a distinct eligible provider identity/configuration, enforced by the coordinator. Reusing the same server under three role labels must not be represented as independent evidence.

## 9. DecisionEngine readiness

Status is **EXISTING**, with model activation deliberately constrained.

- `CompactState`, `CandidateAction`, `DecisionResult`, `NextActionDecision`, `DecisionEngine` and `RuleDecision` are in `internal/taskengine/decision.go`.
- `Service.DecideNextAction` produces a read-only recommendation and requires the caller to use existing policy/revision gates.
- `taskcoord.BonsaiDecision` in `internal/taskcoord/decision.go:49` selects one bounded candidate using a local provider and structured `submit_decision` output.
- `ShadowDecision` compares the model with the deterministic baseline without changing task state.
- decision benchmarks and admission policies are persisted.
- `DecideAdmittedReadOnly` in `internal/taskcoord/decision_benchmark.go:91` is the only admitted model path and falls back safely.

What is partial is integration into one bounded execution driver. The Decision Engine has no execution authority by design. That is correct. The next slice should call the existing read-only decision path, then dispatch the selected action through existing task revision, run lease, effect and approval gates.

## 10. Retry, progress and failure analysis

| Condition | Current behavior |
|---|---|
| Tool fails | Durable failed receipt/result is returned; interactive model may choose again. Effectful calls are not auto-replayed. |
| Tests repeatedly fail | Verification rolls back, fails the attempt/step, records normalized confirmed failure and can trigger bounded planner escalation. |
| Same model action repeats | Interactive loop rejects third identical non-poll tool signature. Durable task path counts equivalent failure signatures. |
| No useful progress | `ProgressSnapshot.NoProgress` after two consecutive equivalent confirmed failures. No generic semantic progress score. |
| Context becomes large | Deduplication, spill, slice selection, verified compaction, hard overflow errors and integrity checks. |
| Model loops | Max 12 model steps by default, 24 tool calls, cumulative-token cap, 600-second wall clock, identical-call guard. |
| Model cannot solve | Turn/attempt fails; task may pause and escalate; bounded attempts and planner escalations prevent endless execution. |

`internal/taskengine/progress.go` defines 20 total attempts, two planner escalations and two consecutive equivalent failures. Progress is derived from durable receipts and cannot mutate execution. `BudgetExhausted`, unresolved effects and blocked reasons are explicit.

The primary missing behavior for distributed use is a durable outward progress stream. Worker `ProgressEvent` heartbeats are in-process callbacks and contain phase/time only. Platform `RunUpdate` must be created from committed local state, never directly from an ephemeral callback.

## 11. Planner and escalation analysis

Planner support is **EXISTING**:

- bounded `worker.PlanWithProviderService` accepts task facts, repository manifest, allowed executables and optional escalation evidence;
- `taskengine.PlannerRun` persists planned/dispatched/observed/failed/rejected/uncertain states;
- `taskcoord.AutoPlan` binds task/provider/preset revisions and records the plan artifact;
- failure normalization and escalation records are in `taskengine/orchestration.go`;
- escalation is capped at two and pauses the task.

Stronger-model/fallback support is **PARTIAL**. Provider profiles can point to different tiers, and planner/reviewer provider IDs are explicit. There is no Platform-aware tier router, distributed handoff protocol, or formal CONSULT result.

Recommended behavior:

- **CONSULT:** Harness stays run owner, snapshots `CompactState` plus evidence references, submits an `EscalationRequest` to Pi, receives bounded advice as a new artifact/context input, and re-decides locally. Advice has no execution authority.
- **HANDOFF:** Harness checkpoints and releases/ends local authority. Pi creates a new assignment/run owner for Tier-A. The returned result is imported as evidence under explicit provenance. Two workers must never hold the same write lease.

Harness should not directly select or call Tier-A for Platform escalation. Pi owns who/where/tier. Existing local planner/reviewer calls may remain for local bounded task execution because they are part of the Harness implementation, not Platform routing.

## 12. Persistence and crash recovery

Execution can resume, with qualifications:

- session events, contracts, context snapshots, step bindings and approvals persist;
- durable tasks, plan revisions, runs, attempts, effects, checkpoints, validations, proposals and artifacts persist;
- startup recovery marks interrupted turns and repairs approval states;
- expired task-run leases can be renewed when no effect crossed dispatch;
- a dispatched effect without an observed receipt becomes `uncertain`, pauses the task and requires reconciliation;
- planned but undispatched effects are abandoned;
- `ExecutionSnapshot` reconstructs latest task/run/attempt/proposal/effects;
- checkpoints record completed/pending steps, evidence, unresolved effects, next action and prerequisites.

Lost on crash are live stream subscribers, in-memory sampling/elicitation waits, process handles and uncommitted transient provider output. The system intentionally refuses to infer whether an effect completed across an uncertain boundary.

Pi should persist Platform assignment/routing state, node/agent/worktree registry, Platform session/run state, update acknowledgements and escalation ownership. Harness should persist local task execution state, leases, attempts, effects, evidence bodies, context snapshots, local path bindings and the outbound update outbox. Pi should store evidence references and selected summaries, not duplicate every local blob by default.

## 13. Authentication and trust boundary

HTTP authentication uses one configured bearer token or a signed, 12-hour, strict same-site cookie (`internal/web/auth.go`). The authenticated principal is inserted into request context, and claimed `actor` fields in bounded JSON mutations must match it. Host allowlisting, same-origin checks and cross-site mutation rejection are in `internal/web/boundary.go`.

Secrets are stored outside SQLite in `secrets.json`. On Windows they are protected by current-user DPAPI (`internal/secrets/format_windows.go`); POSIX uses owner-only file permissions. Provider and MCP records store secret references/readiness, not token values. MCP HTTP uses a server credential; stdio receives a minimal environment plus configured credential.

Current identity is installation/user scoped, not per node/agent/run. Tool receipts carry session/turn/call context internally, and durable effects carry task/run/attempt/operation identity, but MCP wire calls do not carry a standard Platform identity envelope.

Pi should trust:

1. a mutually authenticated/scoped node credential;
2. Platform-issued assignment claims bound to `assignment_id`, `node_id`, `agent_id`, access scope and expiry;
3. monotonic update sequence and idempotency keys; and
4. Harness evidence only when its node/run/operation provenance and digest are intact.

Do not trust JSON `actor`, a provider profile name, a local absolute path, or an MCP tool annotation as identity. Use a per-node credential with scoped agent claims rather than a fleet-wide shared key.

## 14. Answers to Pi questions Q1–Q7

### Q1 — Worktree/path ownership

Harness should resolve `worktree_id` locally. Pi sends stable repository/worktree/node IDs and optional expected VCS revision constraints. Harness maps them to local `Project.RootPath`. This matches the existing root-confined product/tool architecture and prevents Pi from becoming authoritative for paths on another OS or node.

### Q2 — RunUpdate transport

Use a hybrid with Harness push plus Pi pull/replay:

- Harness transactionally writes an update to a local outbox after the underlying state commit.
- It pushes unacknowledged updates when connected.
- Pi acknowledges the highest contiguous sequence.
- Pi can poll/replay from `after_sequence` after reconnect or suspected loss.
- Duplicate `(run_id, sequence)` is accepted idempotently.

This preserves disconnected local work and crash recovery without a broker.

### Q3 — Escalation ownership

Harness sends `EscalationRequest` to Pi Coordinator/Router. Pi chooses Tier-A, node and mode. CONSULT returns advice while Harness retains ownership. HANDOFF requires a checkpoint and ownership transfer before another worker can mutate. Direct Tier-A selection in Harness would violate the stated routing boundary.

### Q4 — ContextPack

Expose a structured, versioned ContextPack. Existing Second Brain `get_context` may populate it, but should not itself be the cross-system contract unless it already guarantees the required schema, provenance, bounded size and version. Harness needs facts separated into architecture, decisions, constraints, patterns, failures, solutions and evidence refs so they can receive different trust and context priorities.

### Q5 — Authentication

Use per-node scoped credentials, with assignment-scoped `agent_id` and permissions. A shared fleet key has too large a blast radius and cannot support revocation/audit. Per-agent long-lived secrets are unnecessary if Pi issues short-lived signed assignment claims to an authenticated node.

### Q6 — Session identity

Keep Platform Session and Harness agent session independent with cross-references. Harness session semantics include a frozen provider/tool/skill contract and local event history. Forcing equality would couple Second Brain lifecycle to model/tool cache epochs and local recovery. Store `platform_session_id` on the binding and retain local `harness_session_id`.

### Q7 — Knowledge submission

Add `knowledge.submit_candidate` with Pi-side admission. Harness already proves the safe pattern internally: a verified digest becomes an inert candidate, then lint/security/replay/review/promotion gates decide admission. Directly composing `capture`, `record_lesson`, and `promote` risks bypassing a single atomic candidate boundary or granting Harness promotion authority. Existing operations may implement the new endpoint behind Pi, but the contract should expose one candidate submission and never direct promotion.

## 15. Proposed five contracts

Classification meanings: **REQUIRED** is transmitted in v1; **OPTIONAL** may be transmitted; **DERIVED** is computed by the receiver and not authoritative on the wire; **REMOVE** should not be in v1.

### 15.1 TaskAssignment

| Field | Class | Rationale |
|---|---|---|
| `contract_version` | REQUIRED | Exact schema discriminator, e.g. `platform-harness/v1` |
| `assignment_id` | REQUIRED | Idempotent ingestion and audit identity |
| `session_id` | REQUIRED | Platform session cross-reference; never reused as Harness session ID |
| `run_id` | REQUIRED | Platform run identity used by updates/escalation |
| `task_id` | REQUIRED | Pi/Kanban task identity |
| `workspace_id` | REQUIRED | Platform workspace authorization boundary |
| `project_id` | REQUIRED | Platform project identity; mapped to local project |
| `node_id` | REQUIRED | Must match receiving Harness installation |
| `agent_id` | REQUIRED | Assigned logical worker identity |
| `repository_id` | REQUIRED for repository work; OPTIONAL otherwise | Stable repository binding |
| `worktree_id` | REQUIRED for repository writes; OPTIONAL for read-only/non-code work | Local path lookup key |
| `access_scope` | REQUIRED | Bounded capabilities/effects and expiry; deny by default |
| `goal` | REQUIRED | Bounded objective |
| `acceptance_criteria` | REQUIRED | Stable criterion IDs plus descriptions |
| `constraints` | OPTIONAL | Explicit task constraints; empty is valid |
| `complexity` | OPTIONAL | Routing hint; Harness must not treat as authority |
| `risk` | OPTIONAL | Routing/policy hint; effective policy is derived from access scope/local policy |
| `context_ref` | OPTIONAL | Reference to a versioned ContextPack |
| absolute local path | REMOVE | Harness-local authority, never Pi input |
| Harness task/session/run IDs | DERIVED | Created/mapped locally after acceptance |

Minimum additional mechanics fields: `issued_at`, `expires_at`, `idempotency_key`, and optional `expected_repository_revision` for assignments that require a specific Git state.

### 15.2 RunUpdate

| Field | Class | Rationale |
|---|---|---|
| `contract_version` | REQUIRED | Version discriminator |
| `run_id` | REQUIRED | Platform run identity |
| `sequence` | REQUIRED | Monotonic per run, used for dedupe/replay |
| `status` | REQUIRED | Small enum mapped from committed Harness state |
| `progress` | OPTIONAL | Structured counters/phase; never an invented percentage |
| `last_action` | OPTIONAL | Candidate/action ID and local operation reference |
| `evidence_refs` | OPTIONAL | New evidence since prior update |
| `needs_escalation` | DERIVED | Prefer explicit status plus EscalationRequest; may remain a UI convenience |
| `timestamp` | REQUIRED | UTC event creation time |
| `idempotency_key` | DERIVED | Deterministically `run_id:sequence` |
| full logs/blobs | REMOVE | Transfer by evidence retrieval policy, not every update |

Add optional `harness_run_id`, `task_revision`, `lease_generation`, `error_code`, and `blocked_reasons`. These are useful for reconciliation and are already structured locally.

### 15.3 EscalationRequest

| Field | Class | Rationale |
|---|---|---|
| `contract_version` | REQUIRED | Version discriminator |
| `escalation_id` | REQUIRED | Idempotency/audit key |
| `run_id` | REQUIRED | Platform run owner |
| `reason_code` | REQUIRED | Stable machine-readable reason |
| `mode` | REQUIRED | `CONSULT` or `HANDOFF` |
| `evidence_refs` | REQUIRED | Escalation without evidence is not actionable |
| `attempted_actions` | REQUIRED | Bounded action IDs/operation refs, not prose transcript |
| `knowledge_refs` | OPTIONAL | Relevant ContextPack/Second Brain references |
| `required_capability` | OPTIONAL | Capability/tier need; Pi remains selector |
| target model/node | REMOVE | Router-owned decision |
| failure signature/count | REQUIRED | Reuses current normalized failure/escalation evidence |

Add `task_id`, `task_revision`, `checkpoint_ref`, `requested_at`, and for HANDOFF `authority_release_state`.

### 15.4 ContextPack

| Field | Class | Rationale |
|---|---|---|
| `context_id` | REQUIRED | Immutable pack identity |
| `version` | REQUIRED | Monotonic content version/schema binding |
| `project_id` | REQUIRED | Scope |
| `task_id` | OPTIONAL | Omit for project-wide context |
| `architecture` | OPTIONAL | Bounded verified facts |
| `decisions` | OPTIONAL | Decision records and rationale summaries |
| `constraints` | OPTIONAL | Policy/technical constraints |
| `patterns` | OPTIONAL | Reusable patterns, not instructions with hidden authority |
| `failures` | OPTIONAL | Prior failure signatures and evidence |
| `solutions` | OPTIONAL | Verified solutions |
| `evidence_refs` | REQUIRED | May be empty only for an explicitly empty pack |
| `provenance` | REQUIRED | Source, author/admitter, timestamps and trust |
| raw full conversation | REMOVE | Too large and wrong authority model |

Add `schema_version`, `content_digest`, `created_at`, `expires_at` or freshness policy, and bounded-size metadata. Harness should verify the digest and turn sections into separately prioritized fragments.

### 15.5 KnowledgeCandidate

| Field | Class | Rationale |
|---|---|---|
| `candidate_id` | REQUIRED | Idempotent candidate identity |
| `run_id` | REQUIRED | Provenance |
| `task_id` | REQUIRED | Provenance/scope |
| `type` | REQUIRED | Controlled candidate kind |
| `problem` | REQUIRED | Bounded reusable problem statement |
| `solution` | REQUIRED | Proposed knowledge, still untrusted |
| `rationale_summary` | OPTIONAL | Human review aid |
| `evidence_refs` | REQUIRED | Must point to measured evidence |
| `verification` | REQUIRED | Checks, verdicts and verifier provenance |
| `provenance` | REQUIRED | Node/agent/model/provider/runtime/source revisions |
| promotion decision | REMOVE | Pi/Second Brain owns admission |

Add `contract_version`, `idempotency_key`, `project_id`, `repository_id` when applicable, `created_at`, and optional `supersedes_knowledge_id`.

## 16. Contract mechanics

- Use JSON Schema 2020-12, one schema per message type, with a shared definitions file for IDs, timestamps, evidence refs and access scope.
- `contract_version` is a stable major contract name such as `platform-harness/v1`; add fields compatibly within v1 and use v2 for changed meaning or removed required fields.
- Reject unknown major versions. Ignore unknown optional fields only when `additionalProperties` policy and capability negotiation permit it. Keep security-sensitive objects such as `access_scope` closed.
- Use opaque UTF-8 string IDs with documented maximum lengths. Never infer entity type from a prefix across the Platform boundary.
- Require RFC 3339 UTC timestamps. Ordering comes from sequence/revision, not wall-clock time.
- Assignment ingestion is idempotent on `assignment_id` plus payload digest. Same ID/same digest returns the original mapping; same ID/different digest is a conflict.
- Run updates use a monotonically increasing sequence per Platform `run_id`. Pi stores the highest contiguous sequence, accepts duplicates with identical digest, and rejects conflicting duplicates.
- Escalation and knowledge candidate submission use explicit idempotency keys and immutable payload digests.
- Acknowledge durable receipt, not processing completion. Return accepted ID, digest, status and acknowledged sequence.
- Retry only transport failures/timeouts and only with the same idempotency key. Never automatically repeat an effect whose dispatch outcome is uncertain.
- Use bounded exponential backoff with jitter and a local outbox. No Kafka/Redis/message broker is justified for v1.
- Add a capability/contract discovery endpoint so both sides can advertise supported major/minor schema revisions and optional fields.
- Keep error bodies structured: `code`, safe `message`, `retryable`, `request_id`, optional `expected_revision`; do not return secrets, prompts or raw provider bodies.
- Evidence references are opaque and immutable. Retrieval must re-check assignment scope and owner principal.

## 17. Ownership matrix

| Concept | Owner | Harness role |
|---|---|---|
| Workspace | Pi Platform | Local binding and enforcement |
| Platform Project | Pi Platform | Map to local `Project` |
| Local Project/root | Harness | Resolve and protect local path |
| Task | Pi/Kanban for intent; Harness for local execution projection | Preserve cross-reference and revisions |
| Node Registry | Pi Platform | Prove local node identity |
| Agent Registry | Pi Platform | Execute assigned identity within scope |
| Platform Session | Pi Platform | Cross-reference only |
| Harness agent session | Harness | Frozen local conversation/model/tool contract |
| Platform Agent Run | Pi Platform | Routing/ownership lifecycle |
| Harness task run/attempt | Harness | Lease and effect authority |
| AgentState | Harness | Derived local operational view |
| CompactState | Harness | Existing bounded decision input |
| Evidence payload | Harness by default | Persist locally and expose scoped references |
| Evidence catalog/reference | Shared | Pi indexes refs; Harness verifies/serves payload |
| Platform Router | Pi | No Harness implementation |
| Decision Engine | Harness | Existing read-only selector |
| BonsaiDecision | Harness | Existing model-backed selector |
| Progress Detector | Harness | Existing derived progress/failure budget |
| Escalation record | Shared | Harness originates; Pi routes/owns resolution |
| ContextPack | Pi/Second Brain | Harness validates, caches and compiles it |
| Knowledge Candidate | Harness originates; Pi stores/adjudicates | Candidate-only submission |
| Knowledge Admission | Pi/Second Brain | Harness receives admitted refs later |
| Production Write Gate | Pi Platform | Harness additionally enforces local effect policy |
| Deployment | Pi Platform | Harness may produce verified artifacts/evidence |

## 18. Existing / Partial / Missing matrix

| Feature | Status | Current implementation | Reuse? | Change needed | Files |
|---|---|---|---:|---|---|
| AgentState | PARTIAL | State spread across task/session/run/effects | Yes | One derived projection/binding, no new authority | agent/taskengine models |
| CompactState | EXISTING | Bounded evidence-referenced task projection | Yes | Add only contract-proven fields | `taskengine/decision.go` |
| CandidateAction | EXISTING | Bounded action ID/type/risk/cost/policy | Yes | Map to dispatcher | `taskengine/decision.go` |
| DecisionEngine | EXISTING | Interface + deterministic baseline | Yes | No duplicate | `taskengine/decision.go` |
| BonsaiDecision | EXISTING | Structured local-model candidate selector | Yes | Keep read-only admission | `taskcoord/decision.go` |
| AgentLoop | EXISTING | Budgeted interactive loop | Yes | Do not rewrite for P1 | `agent/service.go` |
| ToolRegistry | EXISTING | Versioned schema/effect/approval/receipt | Yes | Semantic action adapter only | `tools/registry.go` |
| MCP | EXISTING | Multi-server HTTP/stdio catalog and invocation | Yes | Identity/scoping contract later | `mcp/*` |
| EvidenceStore | PARTIAL | Events + artifacts/CAS + jobs + validations/effects | Yes | Unified reference resolver/index | product/store/taskengine |
| StateReducer | PARTIAL | `BuildCompactState`, context compiler and progress derivation | Yes | Consolidate a read model, not a second state store | `decision.go`, `progress.go` |
| ProgressDetector | EXISTING | Durable receipt-derived snapshot | Yes | Export RunUpdate | `progress.go` |
| FailureBudget | EXISTING | attempts/escalations/equivalent-failure limits | Yes | Bind to Platform updates | `progress.go`, `orchestration.go` |
| Planner | EXISTING | Bounded plan-only model + durable runs | Yes | Platform escalation adapter | `worker/planner.go`, `taskcoord/service.go` |
| Escalation | PARTIAL | Local failure escalation records | Yes | CONSULT/HANDOFF Platform protocol | `taskengine/orchestration.go` |
| ContextPack | MISSING | Existing fragments/memory retrieval, no shared pack | Partial | Validate/cache/compile structured pack | context + product retrieval |
| KnowledgeCandidate | PARTIAL | Internal learning Digest -> Skill Candidate | Yes | Platform candidate contract/outbox | learning/skills |
| Session | EXISTING locally | Immutable contract + event stream | Yes | External cross-reference | `agent/models.go`, `service.go` |
| Run | EXISTING locally | Leased durable task run | Yes | External cross-reference | `taskengine/execution.go` |
| Persistence | EXISTING | SQLite + CAS + migrations | Yes | Add mapping/outbox tables only | `store/store.go` |
| Metrics | PARTIAL | usage ledger, token accuracy, skill metrics, decision benchmarks, task progress | Yes | Platform delivery/SLIs | inference/agent/taskengine |
| Node registry | MISSING | None | No | Local node identity + Pi registration binding | New narrow integration package |
| Repository/worktree binding | MISSING | Project root only | Project reuse | Map Platform IDs to local root | product + integration package |
| RunUpdate outbox | MISSING | SSE/callbacks only | State sources reuse | Durable sequenced outbox/ack/replay | New integration package/store migration |

## 19. KEEP / MODIFY / ADD / DEPRECATE / DO NOT TOUCH

### KEEP

- `taskengine.Task`, revisions, run leases, attempts, effect intents, checkpoints and validations.
- `taskengine.CompactState`, `CandidateAction`, `DecisionEngine`, `RuleDecision`.
- `taskcoord.BonsaiDecision`, shadow evaluation, benchmark and read-only admission.
- `context.Compiler` spill/compaction/integrity pipeline.
- tool revisions, exact argument hashes, approvals and receipts.
- MCP deferred catalog and no-replay behavior.
- provider profiles, runtime fingerprints, immutable inference presets and scheduler.
- artifact/CAS storage and learning candidate/promotion gates.

### MODIFY

- Extend local project lookup through a Platform repository/worktree binding; keep `Project.RootPath` local.
- Add Platform cross-reference fields through mapping tables rather than renaming existing IDs.
- Export task progress/status through a durable RunUpdate projection.
- Let `agent.compileTurn` or `StepPacket` accept a verified ContextPack projection after the schema is locked.
- Extend evidence lookup with a typed reference resolver and access-scope checks.

### ADD after contract lock

- `internal/platformcontract` (or equivalently narrow package): schema DTO validation and version negotiation.
- local node identity record and repository/worktree binding.
- assignment mapping with payload digest/idempotency.
- sequenced RunUpdate outbox, acknowledgement and replay API.
- EscalationRequest outbox/result binding.
- ContextPack cache with digest/provenance and size bounds.
- KnowledgeCandidate adapter over existing learning evidence.

### DEPRECATE

- Any API use that treats caller-supplied `actor` as trusted when authenticated identity is available.
- Any future integration that sends absolute paths from Pi.
- Ambiguous unqualified `run_id`/`task_id` in cross-system logs or payloads.
- Direct promotion of remotely submitted knowledge.

### DO NOT TOUCH in P1

- Core interactive `runAgentLoop`.
- Existing Decision Engine types or candidate semantics without measured need.
- task effect state machine and no-replay recovery behavior.
- approval binding and local write gates.
- MCP transport/pool implementation.
- provider adapter wire protocols.
- skill promotion/replay/security gates.

## 20. Smallest safe next Harness slice

The safest slice differs from the approximate target because the proposed decision components already exist.

### Slice

Implement one **read-only Platform assignment intake and observation path**:

1. validate a locked `TaskAssignment v1`;
2. verify node/access scope and resolve repository/worktree binding to an existing local project root;
3. idempotently create/cross-reference an existing durable Harness task, without starting it;
4. derive existing `CompactState` and existing deterministic/admitted read-only decision;
5. persist one sequenced `RunUpdate` in a local outbox;
6. expose acknowledgement and replay; and
7. stop before automatic action dispatch.

### Why

This proves identity, ownership, path resolution, idempotency, state mapping, decision reuse and disconnected update delivery before any distributed write authority exists. It creates the narrow waist needed by later CONSULT/HANDOFF and execution work.

### Expected files/interfaces

- new contract DTO/validator package;
- a small Platform integration service;
- additive store migration for node/binding/assignment/update-outbox records;
- HTTP endpoints for assignment intake, update polling/ack and capability discovery;
- adapters calling `product.GetProject`, `taskengine.Create/Get/BuildCompactState`, `DecideNextAction` or admitted read-only decision.

No new model interface, task database, message broker, MCP transport or agent loop is required.

### Backward compatibility

All existing local sessions/tasks continue unchanged. Platform IDs live in additive mapping rows. The feature is disabled until node identity/credential and bindings are configured. Existing APIs remain valid.

### Rollback

Disable the Platform routes/worker, stop outbox delivery, and leave mapping/outbox rows inert. No existing task/event/artifact needs rewriting. Additive tables can remain for audit and be removed only in a later migration if necessary.

## 21. Tests required for the next slice

1. **Schema conformance:** valid minimal assignment, optional fields, unknown major version, closed `access_scope`, size limits and Unicode IDs.
2. **Idempotency:** same assignment/same digest returns same mapping; same ID/different digest conflicts; concurrent duplicate intake creates one task.
3. **Identity:** wrong node rejected; unknown/disabled agent rejected; expired assignment rejected; Platform and Harness session/run IDs remain distinct.
4. **Path ownership:** worktree resolves locally; absolute path in payload rejected; symlink escape rejected; stale/missing binding rejected; optional repository fingerprint mismatch rejected.
5. **Authorization:** read-only scope cannot create write-capable execution; mutation actor comes from authenticated principal; evidence retrieval enforces assignment scope.
6. **State reuse:** created task can produce existing `CompactState`; stale task revision fails; decision remains read-only and cannot dispatch an effect.
7. **RunUpdate ordering:** commit then outbox, monotonic sequence, duplicate delivery, conflicting duplicate, contiguous ack, replay after restart and two concurrent senders.
8. **Disconnected behavior:** updates accumulate locally, execution state remains reconstructable, reconnect delivers in order.
9. **Crash boundaries:** crash before task commit, between task/outbox transaction, after outbox commit before send, after Pi receive before ack.
10. **Security/redaction:** no local absolute path, credential, prompt body or unrestricted artifact content appears in update/error logs.
11. **Compatibility:** existing agent, taskengine, taskcoord, MCP, provider, context and web tests remain green.
12. **Contract fixtures:** shared golden JSON fixtures are validated by both Pi and Harness CI.

## 22. Risks and open questions

1. Pi-side semantics for Workspace versus Project must be made explicit. Harness currently has only Project/root; mapping two Platform scopes into one local row without rules will create authorization ambiguity.
2. Platform Run ownership and Harness task-run leases need a precise transfer rule for HANDOFF. A Platform reassignment must invalidate or wait out local write authority.
3. Repository/worktree bindings need lifecycle events for create, move, archive and delete. A stable ID pointing to a silently replaced directory is unsafe.
4. Evidence retrieval location and retention need agreement. Pi cannot assume every local blob remains online forever; Harness cannot upload private source by default.
5. Access-scope vocabulary must map to existing tool effects and task coordinator phases. Free-form permission strings are insufficient.
6. Model/provider independence must be defined for review. Multiple logical presets on one Bonsai process are useful roles but not independent reviewers.
7. ContextPack freshness and conflict rules need definitions. Newer version does not automatically supersede task-pinned decisions.
8. Clock skew means expiry validation needs a bounded tolerance, while sequence/revision remains authoritative for ordering.
9. Pi MCP services need an idempotency/effect contract before Infrastructure mutations are enabled.
10. Current HTTP auth is suitable for a local UI but not sufficient as fleet node authentication. The transport and credential rotation mechanism must be locked before P1 write capabilities.
11. The Harness has both interactive and durable task paths. Platform assignments should initially target the durable task path; mixing a Platform run into a conversational turn would weaken recovery and evidence guarantees.
12. “Progress” must stay factual. Existing counters and phase/state are reliable; a synthetic percentage is not.

## INFORMATION PI TEAM MUST KNOW BEFORE P1

- Harness already has local `Session`, durable `Task`, leased `Run`, `Attempt`, `Effect`, `Checkpoint`, `CompactState`, `CandidateAction`, `DecisionEngine` and `BonsaiDecision`. Pi contracts must cross-reference these, not replace them.
- `run_id` and `task_id` already exist in multiple local domains. Platform IDs need explicit domain/cross-reference fields.
- Harness has no first-class `node_id`, execution `agent_id`, `workspace_id`, `repository_id` or `worktree_id`.
- The only authoritative filesystem location today is Harness `Project.RootPath`. Pi must send stable IDs; Harness must resolve the local path.
- Platform Session must remain distinct from Harness agent session because the latter freezes provider, tools, skills, qualification and cache epoch.
- The best P1 target is the durable task engine, not the conversational loop.
- Harness already persists evidence in events, artifacts/CAS, jobs, validations, proposals and effect receipts. Use opaque evidence refs instead of creating another evidence database.
- Effects are deliberately never replayed across an uncertain dispatch boundary. Pi retry/ack rules must preserve that invariant.
- One Bonsai server can serve DECISION/WORKER/PLANNER logical presets, but those roles do not constitute independent review.
- Harness should push durable sequenced updates from an outbox; Pi must support acknowledgement and replay/polling.
- Pi owns escalation routing. CONSULT retains Harness ownership; HANDOFF requires explicit lease/authority transfer.
- Context must be a structured, versioned, provenance-bearing ContextPack. A raw `get_context` response or full conversation is not a sufficient contract.
- Knowledge must enter Pi as an inert candidate with evidence and verification. Harness must not receive direct promotion authority.
- P1 should be read-only through decision selection and update delivery. Automatic action dispatch should wait until identity, scope, lease transfer and evidence contracts are proven.
