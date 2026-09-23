# Managed Execution Contract v1 — Focused Final Harness Review

Reviewed: 2026-09-22
Scope: the six blockers in the supplied focused-review request only. No architecture re-audit, implementation, H2, Harness-core modification, Pi modification, or deployment.

**Result:** the resource authority, quiescent-release and assignment-lifecycle designs are now sufficiently specified to implement. Remaining contract issues concern replay identity/digest bytes, factual observation semantics, and contradictory or undefined wire shapes. Missing adapter code and narrow release/recovery APIs are **not** counted as contract blockers.

## Inputs and numbering

- [Managed Execution addendum](C:/Users/ZP2E0/Downloads/agent-platform-managed-execution-v1.md): 699 lines.
- [Blocker resolution map](C:/Users/ZP2E0/Downloads/managed-execution-blocker-resolution.md): 202 lines.
- Current Hermetrix working tree at D:/Projects/harness/hermetrix-harness; HEAD 08fcac4ab8a7c367da07d773b5d9dbde0f049b1d, including pre-existing modified/untracked source.
- The base contract was consulted only for inherited managed transport, claim schemas and preserved decision/lease invariants. Other base-contract areas were not reopened.

The addendum and the review request use different B-numbering. **This report uses the user's focused-review numbering throughout.**

| This report / request | Addendum location |
|---|---|
| B1 — HANDOFF / release / exclusivity | §2, §8.3–8.4, §9 |
| B2 — Stable scope / fencing | §1 |
| B3 — Idempotency / ACK / replay | §§3–4, §8.5, inherited base §§9/21 |
| B4 — Assignment / PlatformRun lifecycle | §5 |
| B5 — Factual update / verification | §6 |
| B6 — Normative schema / wire profile | §7 and the normative message shapes |

Input SHA-256:

~~~text
agent-platform-managed-execution-v1.md
739a86bdd1fa9754ac69d5921e4187171e071dd86ad4bfdca480642b96966d89

managed-execution-blocker-resolution.md
50d3f351bbe260e96058d1c9037c1806ce140b3b252e7235a0d8ddce7fbda6fd
~~~

## Blocker result

| Blocker | Contract Status | Harness Impact | Implementation Needed | Remaining Contract Issue |
|---|---|---|---|---|
| B1 | CONTRACT_RESOLVED_IMPLEMENTATION_PENDING | Narrow release/quiescence/recovery integration | Stop dispatch, enumerate/drain write-capable jobs, reconcile receipts, checkpoint, invalidate lease, durably retain ReleaseProof | No separate release-order design blocker. The strict §2 gate governs all recovery scenarios; §9 row 6 cannot authorize remaining write-capable effects. Proof wire identity is covered under B3/B6. |
| B2 | CONTRACT_RESOLVED_IMPLEMENTATION_PENDING | Adapter at resource binding and effect authorization | Resolve canonical resource scope; enforce generation plus local lease; serialize grants across assignments | None in the resource-based fencing design. Missing code is implementation work. |
| B3 | CONTRACT_NOT_RESOLVED | Outbox, canonical encoder, receiver dedup/ACK | Implement durable immutable delivery and historical re-ACK | ReleaseProof's required inner digest has no defined hash domain; run_id versus platform_run_id dedup identity is not fixed. |
| B4 | CONTRACT_RESOLVED_IMPLEMENTATION_PENDING | Assignment/claim cross-reference adapter and release integration | Sequential local runs under one PlatformRun; revoke/drain before supersession; separate renewable claim | None in lifecycle design. The addendum's explicit separation overrides the old embedded-claim/1:1-run assumptions. |
| B5 | CONTRACT_NOT_RESOLVED | Factual projector, evidence adapter | Project committed task/run/step/validation facts without changing core decisions | No real local-run reference exists for the specified unplanned accepted state; attempt and check-count scopes remain undefined; source-cursor comparison/binding needs a wire rule. |
| B6 | CONTRACT_NOT_RESOLVED | Wire registry and semantic validator | Compile amended schemas and implement stated semantic checks | Read-only/no-scope rule contradicts required RunUpdate scope; Signature wire type is undefined; release/claim integrity fields need one unambiguous envelope/binding profile. |

## B1 — HANDOFF / Release / Exclusivity

**CONTRACT_RESOLVED_IMPLEMENTATION_PENDING**

Addendum §2.2, lines 117–139, now requires the correct order:

~~~text
stop new dispatch
→ enumerate every in-flight write-capable effect
→ completion OR verified process-tree termination OR verified rollback
→ disposition receipts
→ durable checkpoint
→ invalidate local authority
→ integrity-bound ReleaseProof
→ Pi validates proof and released generation
→ durable generation increment
→ regrant
~~~

§2.4 expressly forbids an ordinary proof or a new ordinary writer while a write-capable effect remains uncertain. RECONCILIATION_ONLY applies to the resource, not just one assignment. §2.5 requires the proof before regrant. These are substantive corrections to the previous contract.

Actual Harness fit:

- [product/commands.go](D:/Projects/harness/hermetrix-harness/internal/product/commands.go:59) creates asynchronous command jobs; the execution context is detached from the request. Closing a request or updating a run row does not prove the process stopped.
- [taskcoord/service.go](D:/Projects/harness/hermetrix-harness/internal/taskcoord/service.go:1178) reconciles an existing command by operation ID and refuses queued/running jobs. It does not relaunch them.
- [taskengine/execution.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:450) separates effect planning/dispatch from observation/reconciliation.
- [taskengine/service.go](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:413) writes checkpoints without releasing a lease. Recovery at line 457 marks dispatched effects uncertain and pauses runs.

A future release API must join these facts into the specified lifecycle. The absence of that API does not invalidate the contract.

Interpretation of the recovery table is important: §9 row 6 permits remaining “async effects” after acquisition, but does not explicitly permit remaining **write-capable** effects. Read together with the unconditional §2 prohibition, only non-write-capable activity or delayed observations may remain. A new writer while an old write-capable command is still active would violate §2; Platform-boundary rejection is not an alternative to quiescence.

Similarly, §9 row 1 cannot be used to bypass current local lease/recovery checks merely because the table assumes a still-valid lease. Its permitted action is to resume quiescence, not resume ordinary dispatch. RECONCILIATION_ONLY recovery must return through the §2 proof/validation gate before any ordinary grant; the abbreviated state diagram is not an exemption.

These constraints admit a safe implementation without changing DecisionEngine or replaying effects. Byte-level ReleaseProof deficiencies are reported once under B3/B6, not as evidence that the quiescence design failed.

## B2 — Stable Authority Scope / Fencing

**CONTRACT_RESOLVED_IMPLEMENTATION_PENDING**

§1.2–1.3, lines 49–94, establishes:

- authority_scope_id identifies the exclusive writable resource through its canonical worktree registration.
- Assignments, revisions, agents, PlatformRuns and consultant assignments targeting that resource share its scope.
- Generations are monotonic per scope and persisted by Pi before a grant.
- Distinct isolated worktrees may have independent scopes.
- Stale generations cannot authorize new writes, effects or authority-sensitive mutations.
- Historical authenticity is separate from current authorization.

The adapter must honor the opening “actual exclusive writable resource” requirement when binding registry identities to local roots. It must not treat aliases to the same writable resource as independent isolated worktrees merely because two strings differ. Establishing/rejecting such bindings is implementation of the resource identity invariant.

Current [PlanEffect/DispatchEffect](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:450) already gate local run token, expiry, LeaseGeneration, plan and step revisions. The Platform grant can be checked at this boundary without modifying action selection. Local LeaseGeneration remains local; it is not overwritten by authority_generation.

The existing [one-live-run index](D:/Projects/harness/hermetrix-harness/internal/store/store.go:1601) is per task. The future scope adapter must provide the specified cross-task/resource exclusivity. That missing adapter is not a remaining contract issue.

The contract requires rejection of stale new actions; it does not require offline write availability. A fail-closed managed authorization boundary can therefore implement the rule safely when freshness cannot be established.

## B3 — Idempotency / ACK / Replay

**CONTRACT_NOT_RESOLVED**

Resolved design:

- §4.3 requires idempotency_key, payload_digest and signature for persisted managed envelopes.
- §4.2 selects JCS/RFC 8785 and specifies payload_digest self/signature exclusion and the signed envelope fields.
- §4.4 removes mutable delivery metadata from immutable RunUpdate bytes.
- Inherited base §§9/21 specify sequence per PlatformRun, same-sequence/different-digest conflict, highest contiguous ACK, durable receive before ACK, sender outbox, after_sequence and re-PUSH, bounded retries and pending retention.
- §3.3 explicitly makes the generation-12 release replay after a generation-13 grant an authority no-op with mandatory re-ACK.
- §9 row 4 requires historical RunUpdate acceptance/ACK without restoring old authority.
- §10 I4 explicitly prohibits effect re-execution.

Durable sender allocation, ACK persistence, receiver duplicate records and crash-consistent release processing remain implementation requirements. This review does not require a particular SQL transaction layout or an existing outbox implementation to accept those semantics.

Two actual wire/identity issues remain:

**B3-a — Undefined ReleaseProof digest.** The proof schema at lines 146–178 requires a payload field named digest; checkpoint has another digest. Replay at lines 253–258 keys on the proof digest. §4.2 defines only envelope payload_digest, excluding payload_digest and signature, not the proof's digest field.

Consequently, independent implementations cannot know whether ReleaseProof.digest is:

1. the envelope payload_digest, which creates a circular definition if also included inside the hashed payload;
2. a separate hash excluding digest/signature; or
3. some other proof/checkpoint hash.

Required correction: define one proof identity/hash domain. For example, remove the redundant inner proof digest and use the enclosing payload_digest for proof dedup; alternatively specify the exact separate canonical object and exclusions. Also make checkpoint.digest an explicit equality alias of evidence_ref.content_digest or define its distinct purpose. No crypto algorithm choice is required.

**B3-b — Dedup run identity.** New RunUpdate requires run_id, platform_run_id and harness_run_ref (lines 320–326). §10 I4 keys replay on run_id, while retained base §21 keys it on platform_run_id. Neither meaning nor equality is assigned to run_id.

With local-run-A → local-run-B under one PlatformRun, using local run_id versus PlatformRun produces different dedup namespaces. Required correction: explicitly define the authoritative key as (platform_run_id, sequence, payload_digest) across every local incarnation and remove or bind the extra run_id accordingly. This is identity correctness, not a naming preference.

The critical lost-ACK scenario is otherwise **resolved**:

~~~text
Pi commits proof for scope S / generation 12
→ records the already-applied transition
→ generation becomes 13
→ ACK lost
→ exact authenticated proof replay
→ find original processed proof
→ return its ACK, do not increment/regrant again
~~~

The same historical processing path must never call PlanEffect, DispatchEffect, StartCommand, a provider, MCP tools/call, or workspace apply.

## B4 — Assignment / PlatformRun / Recovery Lifecycle

**CONTRACT_RESOLVED_IMPLEMENTATION_PENDING**

§5.1 explicitly replaces 1:1 Platform/local run identity with sequential local incarnations under a stable Pi-owned PlatformRun. A recovery does not require Pi to mint another PlatformRun or Harness to invent one.

§5.2 requires the old assignment revision to lose new-effect authority before replacement execution, including quiescent release when it owned a write scope. §5.3 separates immutable execution requirements from a newly issued renewable claim, preserving assignment payload_digest across renewal.

For a same-scope write transfer, the mandatory generation increment in §1.3/§2.5 still applies. The parenthetical “new generation if the scope changed” in §5.2 is not permission to skip the required increment for a same-scope HANDOFF. Its condition is not an “only if” exception.

Current [BeginRun](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:144) can create new local IDs; [recovery](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:457) preserves uncertain outcomes instead of replaying. The future mapping can append a new local run after prior authority is retired, while retaining one PlatformRun and its sequence.

Renewal must update the authorization record, not mutate the task/requirement projection. The older base schema's embedded claim must be replaced when compiling the addendum's effective wire schema; that implementation task does not undo the explicit lifecycle design.

## B5 — Factual RunUpdate / Verification Evidence

**CONTRACT_NOT_RESOLVED**

Resolved:

| Requirement | Normative specification and Harness fit |
|---|---|
| cancelled | §6.1 reports cancelled and last committed facts, no further dispatch. |
| Pre-first-attempt counts | accepted / initializing / zero attempts; zero steps when unplanned. |
| Active plan | plan_revision identifies the active plan; counts rebase, sequence does not. |
| Skipped steps | Excluded from completed and total. Adapter counts raw Step.State instead of copying CompactState.CompletedSteps, which includes skipped. No core change needed. |
| Snapshot vs delta | Full ProgressFacts snapshot in every update. |
| Evidence-backed pass | §6.2 requires check_id, subject_revision, and evidence or a lossless immutable bundle; evidence-free pass MUST be rejected semantically. |
| Outcome mapping | pass/fail/blocked/unknown/not_applicable explicit; only pass is success. |
| progress_percent | Retained base rule remains optional/UI-only. It need not be emitted; no scheduling/safety/completion dependence. |

Remaining issues:

**B5-a — Pre-run reference.** §6.1 lines 455–458 explicitly supports accepted with no plan. RunUpdate nevertheless requires a string harness_run_ref. In actual Harness, task creation produces a draft without a local run; BeginRun rejects a task without an active plan. There is no committed local run ID to report at that stage.

Specify null/absence until a real run exists, or define a separate accepted-assignment observation shape. Do not require inventing a local run, creating a fake run record, or using an undocumented empty-string convention.

**B5-b — Attempt and check-count meaning.** attempt is only constrained to a nonnegative integer with zero before the first attempt. It is not defined as an ordinal/count or scoped to step/local run/PlatformRun/task. failed_checks has no current-subject/history rule or blocked-check inclusion rule.

These distinctions matter: [TaskProgress](D:/Projects/harness/hermetrix-harness/internal/taskengine/progress.go:32) counts task-wide attempts, while [CompactState](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:112) builds a check set and includes failed/blocked observations. Neither should be chosen silently as wire truth.

Required correction: define one count/ordinal population and its reset behavior across plan/local-run recovery; define which current check observations contribute to failed_checks and how blocked checks are represented. No additional internal budget counters need to be exported.

**B5-c — Source binding.** lines 469–473 call projection_source_revision an opaque monotonic string that lets Pi detect gaps/out-of-order observations, or alternatively a projection EvidenceRef. A genuinely opaque token cannot be ordered/gap-tested by an independent receiver without an ordering rule; the alternative field/location is not defined.

Choose an ordered source cursor with scope/comparison semantics, or an immutable projection-reference identity and rely on RunUpdate.sequence for ordering. State how one source representation is mandatory for a factual snapshot. A coherent committed projector is future implementation work; the interoperable source interpretation is contract work.

Verification outcomes map safely after those projection corrections:

| Outcome | Harness / trust behavior |
|---|---|
| pass | Existing ValidationPass with matching check/subject and evidence. |
| fail | Existing ValidationFail; failure receipts recommended. |
| blocked | Existing ValidationBlocked, reason required. |
| unknown | Existing ValidationUnknown, never success. |
| not_applicable | External annotation with reason; does not satisfy a Harness acceptance criterion as pass. Preserve as evidence without adding a local success state. |

[RecordValidation](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:337) already requires a check and subject revision and rejects evidence-free pass. No redesign is needed.

## B6 — Normative Schema / Wire Profile

**CONTRACT_NOT_RESOLVED**

Normative semantic validation is legitimate. A rule does not fail this review merely because the displayed JSON Schema omits its conditional or because generated validator code does not exist yet.

| Requested check | Finding |
|---|---|
| 1. Nonempty criteria | Resolved by §7.1 minItems:1 mandate. |
| 2. Unique criterion IDs | Resolved by explicit semantic validation and revision immutability. |
| 3. Concrete write worktree | Resolved by §7.2 plus resolvable scope. |
| 4. Effective target equality | §7.3 requires matching effective node/repository/worktree across assignment, derived scope and claim. The adapter must include nested AccessScope in resolving that effective target, never authorize a conflicting nested target. |
| 5. ContextPack immutable pin | Resolved by id/schema revision/content revision/digest tuple in §7.4. |
| 6. Context priority alignment | Resolved by choosing the schema as authoritative: no mandatory priority field inferred from old prose. Local compiler priority mapping remains adapter work. |
| 7. Effect/checkpoint/review evidence | §7.7 normatively adds the three evidence categories. Compile them into the effective EvidenceRef discriminator; no parallel evidence store is required. |
| 8. Exact revision registry | Resolved by required known (message_type, schema_revision) mapping. |
| 9. Supported revisions | Resolved by explicit pairs and mutual support, not ambiguous ranges. |
| 10. Bounds | Resolved: 256 KiB payload, 100 evidence refs, 50 criteria, 1000 steps/dispositions. Smaller Harness planner/packet limits remain admission constraints, not a reason to rewrite core. |
| 11. UTC | Resolved by UTC Z with millisecond precision and explicit rejection. |
| 12. Verified knowledge evidence | Resolved by §7.6 proof requirement; unverified claims remain inert. |
| 13. Required key/digest | Resolved for managed envelopes by §4.3. |
| 14. Digest/signature profile | General payload JCS rule resolved; ReleaseProof digest and Signature/claim representation still incomplete (B3-a and B6-b). |

Two concrete wire issues remain:

**B6-a — Read-only contradiction.** §1.2 lines 67–69 says none/read assignments MUST NOT require or create authority_scope_id. New RunUpdate lines 320–329 unconditionally requires authority_scope_id and authority_generation. §5.3 also describes every claim as bound to that scope without a read-only variant.

A managed read-only assignment cannot meet both contracts without inventing a write scope or an undocumented sentinel. Make write authority fields conditional/nullable for non-writing managed assignments, or explicitly exclude non-writing assignments from this protocol. No change to Simple/Core MCP is requested.

**B6-b — Signature and claim/proof envelope shape.** Every managed envelope requires Signature, but neither supplied addendum nor base defines that referenced wire type. The base has an embedded integrity object with alg/value; the new proof also has its own optional signature and required digest, while §2.5 requires verifying proof digest + signature.

Specify whether the enclosing signed Envelope alone supplies proof authenticity or whether a nested proof signature is required; define Signature's serializable representation/encoding and its relationship to the separate claim integrity profile. The claim must have an explicit canonical signed input with its own integrity value excluded or encapsulated, rather than an undefined self-inclusive “all bound fields” object.

This does **not** require choosing HMAC or Ed25519 now. A negotiated algorithm profile is acceptable. It does require both peers to know which bytes and which fields carry the proof so independent implementations can verify the same message.

Timestamp's referenced schema is also absent, but its wire grammar is sufficiently specified by §7.11 to generate a validator; that missing schema artifact alone is **not** a blocker. The distinction is the absence of Signature wire semantics, not the absence of a source-code file.

Best-effort evidence retrieval means identity can remain indexed when payloads are offline. A digest is a content commitment, not proof that a test passed. The adapter must preserve §6.2 proof/subject binding and existing trust checks; it must not create a passing Harness validation from arbitrary hash text.

## Safety Invariants

PASS here means a normative obligation that a conforming implementation can enforce, not that a running Pi/Harness integration has been tested. Wire defects above still prevent locking the complete contract.

| Invariant | Result | Basis |
|---|---|---|
| I1. At most one ordinary write authority per scope | PASS | §1 resource scope and §2 quiescent verified regrant. No exception for remaining write-capable effects in §9. |
| I2. RECONCILIATION_ONLY grants no new ordinary writes | PASS | §2.4 explicitly blocks the whole scope. |
| I3. Historical acceptance never restores stale authority | PASS | §3.2–3.4 and mandatory re-ACK/no-op scenario. |
| I4. RunUpdate replay never replays an Effect | PASS | §10 I4 explicitly prohibits re-execution. Adapter delivery stays separate from existing dispatch. |
| I5. Renewal never mutates execution requirements | PASS | §5.3 two separate objects and unchanged assignment payload digest. |
| I6. Sequential local runs under one PlatformRun | PASS | §5.1 explicit model; no Pi ID invention on recovery. |
| I7. DecisionEngine remains selection-only | PASS | Addendum §6.1/§11 preserves base boundary; current DecisionEngine/Bonsai have no execution authority. |
| I8. Pi authority does not replace local safety | PASS | §2.2 explicitly distinguishes lease from Platform generation; both gates remain necessary. |

## Harness Core Impact

| Subsystem | Classification | Work, without redesign |
|---|---|---|
| CompactState | UNCHANGED | Derived local decision input remains intact. |
| CandidateAction | UNCHANGED | Wire observations do not become executable candidates. |
| DecisionEngine | UNCHANGED | Selection only. |
| RuleDecision | UNCHANGED | Existing deterministic behavior. |
| BonsaiDecision | UNCHANGED | Existing bounded local selector/shadow behavior. |
| Context Compiler | ADAPTER_REQUIRED | Map selected immutable pack to existing Fragments, preserving trust and budgets. |
| StepPacket | ADAPTER_REQUIRED | Bind frozen pack through hashed checkpoint/evidence references; no replacement needed. |
| Local leases | NARROW_CORE_CHANGE_REQUIRED | Guarded release transition integrated with quiescence and durable proof. |
| Effect intents | ADAPTER_REQUIRED | Managed grant verification around existing dispatch and receipt enumeration. Preserve local intent/no-replay machinery. |
| Checkpoints | ADAPTER_REQUIRED | Reuse structure; bind checkpoint digest and release proof durability. |
| Recovery | NARROW_CORE_CHANGE_REQUIRED | Recognize preparing/released authority and recover exact proof delivery without enabling released effects. |
| Evidence/CAS | ADAPTER_REQUIRED | Immutable snapshot projections and scoped resolution from existing stores. |

Supporting source: [DecisionEngine](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:77), [BonsaiDecision](D:/Projects/harness/hermetrix-harness/internal/taskcoord/decision.go:49), [context.Fragment](D:/Projects/harness/hermetrix-harness/internal/context/types.go:35), [StepPacket](D:/Projects/harness/hermetrix-harness/internal/taskengine/packet.go:24).

Narrow release/recovery APIs equivalent to PrepareRelease, QuiesceEffects, ReleaseRunAuthority and RecoverReleasedRun are acceptable future work. Their absence is not in the remaining blocker list.

## Mechanical checks and limits of verification

Executed read-only extraction and in-memory schema checks using Python jsonschema 4.17.3 with explicit Draft202012Validator:

- Six JSON schema blocks parsed and passed Draft 2020-12 meta-schema validation. The snippets omit $schema; the draft was selected explicitly.
- Symbolic dependency inventory found Signature and Timestamp absent from both supplied addendum and base schema collections.
- Eleven targeted instance probes: nine structurally accepted, two rejected.
- Pre-first-attempt zero counts, all five verification outcomes and failed DeliveryRecord were representable.
- Negative attempt and empty Envelope were rejected.
- Evidence-free pass, blocked/not_applicable without reason, and total_steps=1001 were structurally accepted. **They are still forbidden by explicit semantic clauses.** These results demonstrate validator implementation work, not additional contract blockers.
- An executing snapshot without source/plan/current/check fields was structurally accepted; B5 specifies the remaining meaning/binding questions rather than treating every optional field as inherently unsafe.
- Structural inspection confirmed the unconditional RunUpdate authority_scope_id requirement and the additional required ReleaseProof.digest.

Full reference-resolved message validation could not be certified without the missing wire-type definitions. No placeholder Signature schema was invented to produce a passing result.

No Harness runtime tests, live handoff, command cancellation, Pi endpoint calls or deployments were performed. Source inspection supports implementation fit; it is not proof of a deployed distributed safety property.

Final artifact checks passed: six blocker rows, eight invariants, required sections, existing local evidence links, balanced code fences and one verdict. Both input hashes remained unchanged. A before/after deterministic hash of 877 files under internal/ and cmd/, plus go.mod, go.sum and README.md, also matched:

~~~text
75692b4ba65f0c18d238fbbe761d105a9d4ab91e1df11aad1b8f7e1de9be26fd
~~~

Existing working-tree changes were retained; the new review document is the only artifact written in this review.

## Implementation Work After Lock

This is analysis only; no code was implemented.

1. Compile negotiated managed types, envelope/signature profiles and semantic validators from the corrected contract.
2. Add assignment acceptance/cross-reference storage, separate claim records and node-local resource binding; preserve local task/run IDs.
3. Add a committed factual projector, immutable outbox, receiver deduplication and persisted ACK/replay handling. The existing [learning StageTrigger](D:/Projects/harness/hermetrix-harness/internal/learning/service.go:76) demonstrates transaction-bound outbox staging, but is not the managed transport itself.
4. Validate duplicate messages, conflicting digests, lost ACK after generation advance, sequential local recovery and unchanged effect counts during replay.
5. Separately implement the narrow release/quiescence/recovery lifecycle, including verified process-tree outcomes and reconciliation-only scope handling.
6. Only subsequently authorize effect-dispatch integration under its own tested grant/lease checks. An observation-only adapter does not automatically start effects.

## Remaining contract changes only

1. **B3:** define ReleaseProof/checkpoint digest domains and use one PlatformRun-based dedup/sequence identity across local recoveries.
2. **B5:** support an accepted observation before a local run exists; define attempt/check-count scope and source snapshot ordering/binding.
3. **B6:** reconcile managed read-only messages with absent write scope; define Signature and the enclosing-versus-nested proof/claim integrity representation.

All three are protocol interpretation or wire-contract issues. None asks for missing Harness implementation to be completed before lock. B1, B2 and B4 need implementation, not a new architecture review.

MANAGED CONTRACT VERDICT: NEEDS_REVISION
