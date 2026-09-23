# Managed Execution Contract v1 — Final Lock Review

Reviewed: 2026-09-22. Scope: the corrected B3/B5/B6 surgical revision only; B1/B2/B4 and I1–I8 were checked for regression, not reopened.

**Result: the remaining B3/B5/B6 contract defects from the previous focused review are resolved. No contract-level blocker remains within this review's scope.** This report supersedes the earlier lock review of the unchanged September 21 artifacts; it does not claim that managed execution has been implemented or deployed.

~~~yaml
B3: PASS
B5: PASS
B6: PASS
Mechanical validation: PASS
B1/B2/B4 regression: PASS
I1-I8 regression: PASS
~~~

Mechanical PASS means the scoped contract checks, fixture shapes and explicit normative predicates passed using the review profiles documented below. It does not mean the Markdown snippets are a complete standalone generated validator package or that live cryptographic/distributed integration was tested.

## 1. Mandatory artifact gate

SHA-256 was calculated before reading or reviewing the documents. Both matched the user's expected values exactly:

| Input | Expected and actual SHA-256 | Gate |
|---|---|---|
| agent-platform-managed-execution-v1.md | 3f49bd57876fe223ead6ae51362b9dbce50bcdb8b2e7806bb51d50c5174c5179 | MATCH |
| managed-execution-blocker-resolution.md | 45551d6f6ee3296025b7f6fbdf4ea8372ee1b253b752ab2b0b9a47a304f1a53b | MATCH |

Inputs:

- [Corrected managed contract](C:/Users/ZP2E0/Downloads/agent-platform-managed-execution-v1.md).
- [Corrected resolution map](C:/Users/ZP2E0/Downloads/managed-execution-blocker-resolution.md).
- [Previous focused review](D:/Projects/harness/hermetrix-harness/docs/managed-execution-v1-final-harness-review.md).

Numbering in this report follows the user's review request and previous Harness review. The addendum's historical section titles use different B-numbering; those titles do not reopen accepted topics.

Actual Harness: HEAD 08fcac4ab8a7c367da07d773b5d9dbde0f049b1d, including existing working-tree changes. Relevant source was reread. The aggregate of 877 files under internal/ and cmd/, plus go.mod, go.sum and README.md, still matches the prior review:

~~~text
75692b4ba65f0c18d238fbbe761d105a9d4ab91e1df11aad1b8f7e1de9be26fd
~~~

Aggregate method: sort file paths, concatenate each repository-relative POSIX path, NUL, and binary SHA-256 of file bytes; SHA-256 that concatenation.

The inherited base-contract file is no longer present at its previously supplied Downloads path. Its unaffected areas were not rereviewed. The inherited EvidenceRef constraints used for the limited fixture validation were retained from the prior verified inspection, with the addendum's explicit evidence-type extensions. This limitation is not represented as a fresh validation of the full base-contract schema collection.

## 2. B3 — PASS

| Previous defect / requested confirmation | Verified correction |
|---|---|
| ReleaseProof.digest | Removed from required fields and properties; §2.3 explicitly forbids the self-digest. |
| checkpoint.digest | Removed; checkpoint content identity is checkpoint.evidence_ref.content_digest. |
| ReleaseProof identity and lost-ACK replay | §§2.3/3.4 use the enclosing Envelope.payload_digest. Release dedup is scope + old_generation + that payload_digest; an already-processed release re-ACKs without another grant or generation increment. |
| Canonical payload bytes | §§3.5/4.2 specify SHA-256 over RFC 8785/JCS canonical payload bytes, without a payload self-digest/signature. The payload does not need to know the envelope digest before serialization. |
| RunUpdate namespace | §3.3 specifies exactly (platform_run_id, sequence, payload_digest), stable across local run A → B → C. The generic RunUpdate.run_id is removed. |
| Local provenance | harness_run_ref is explicitly provenance/cross-reference only and MUST NOT drive sequencing, dedup or ACK. |
| Consistent replay rules | §4.4, §8.5, the affected crash-table rows and I4 use the PlatformRun-based identity. Highest-contiguous ACK and a sequence starting at 1 without recovery rebasing are explicit. |
| Historical authority neutrality | §§3.2/3.4/3.6 preserve authenticated historical acceptance without restoring old authority. |
| No effect replay | I4 still expressly forbids re-executing Effects. Changing message identity does not authorize redispatch. |

No previous B3 defect remains. Inherited same-sequence/same-digest no-op and same-sequence/different-digest conflict semantics remain applicable to the corrected PlatformRun namespace.

Actual Harness compatibility: [RecoverExpiredRun](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:224) marks dispatched effects uncertain and does not replay them. [DispatchEffect](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:525) remains a separately guarded transition using local lease/token/generation and plan/step state. Historical delivery processing can remain outside that path. No live replay was executed.

## 3. B5 — PASS

| Previous defect / requested confirmation | Verified correction |
|---|---|
| Pre-local-run reference | §4.4 makes harness_run_ref optional and nullable. accepted/initializing observations do not require a fabricated local run. Empty/fake/synthetic run IDs are expressly forbidden. |
| Attempt population | §§4.4/6.1 define attempt_count as committed execution attempts associated with the current active plan revision for the PlatformRun projection. Before attempts, it is zero. |
| Rebase/recovery | Only changing the active plan revision rebases the count to that plan's committed attempts. Changing/recovering a local run alone does not rebase it. Sequence remains continuous. |
| Failed-check population | Only current fail observations for the active subject/plan revision count. blocked observations and superseded-subject checks are excluded; blocked facts remain in reasons/evidence. |
| Source binding | Every factual snapshot MUST have projection_ref pointing to an immutable committed projection snapshot. The opaque monotonic projection_source_revision convention is removed. |
| Ordering versus facts | sequence orders transport; projection_ref identifies the committed factual source. The receiver is not asked to order opaque evidence identities. |
| Verification pass | §6.2 still requires check identity, subject revision and evidence or an immutable lossless verification bundle. A digest alone does not establish that a check passed. |

Actual Harness fit:

- [BeginRun](D:/Projects/harness/hermetrix-harness/internal/taskengine/execution.go:144) requires an active plan before inserting a real local run. The corrected nullable reference represents the earlier accepted state without changing this gate.
- [TaskProgress](D:/Projects/harness/hermetrix-harness/internal/taskengine/progress.go:32) currently counts task-wide attempts. The managed projector must select the specified plan/PlatformRun population from committed facts; it must not copy the task-wide budget counter blindly.
- [BuildCompactState](D:/Projects/harness/hermetrix-harness/internal/taskengine/decision.go:112) includes blocked observations in its local FailedChecks list. The managed fail-only projection is an adapter calculation; CompactState and DecisionEngine remain unchanged.
- [RecordValidation](D:/Projects/harness/hermetrix-harness/internal/taskengine/service.go:337) already rejects passing validations without check/subject identity and evidence.

No new task state, artificial run, core progress counter or decision semantic is required by these corrections. Building the coherent committed snapshot is future implementation work.

## 4. B6 — PASS

| Requested confirmation | Verified result |
|---|---|
| none/read needs no fake authority | §§1.3/4.4/5.3/6.3 consistently permit null/absent scope and generation; non-null write authority on none/read is rejected. |
| write requires real authority | §6.3 requires concrete worktree, scope, generation and a valid claim. The condition is expressly applied to TaskAssignment, RunUpdate and AssignmentAuthorizationClaim. |
| Assignment context for RunUpdate | Appendix A3/A4 explicitly uses the associated assignment's access_scope.access to determine write/read semantics. A nullable update field does not let the sender reclassify an assigned write run as read-only. |
| Signature wire representation | §6.4 defines alg, key_id and value, all required; value is base64url-encoded signature bytes. Algorithm/profile remains negotiated. |
| Envelope signing domain | The current §6.4 ManagedEnvelope binds idempotency_key, message_type, contract_version, schema_revision, sender_node_id, payload and payload_digest through UTF8(JCS(...)); signature itself is excluded. |
| ReleaseProof signature | Inner signature removed. Proof authenticity comes from the enclosing managed envelope; there is no separate proof signing domain. |
| Claim integrity domain | §6.4 defines the independent canonical claim input excluding its integrity/signature value. It binds assignment/revision, node/agent, PlatformRun, effective repository/worktree, access scope, applicable scope/generation and validity period. |
| Reference strategy | Short sibling references resolve against registered schema IDs without duplicating agent-platform/v1/. Signature is now defined. Timestamp's wire grammar is explicitly specified in §7.11. |

Effective-profile reading used for this review:

- §6.4 is the current surgical managed-envelope profile, and §12 explicitly identifies its addition of sender_node_id. The earlier §4 envelope outline is read with that refinement; the effective signature input includes sender_node_id. It is not a second negotiated signature domain.
- The addendum's explicit conditional-authority prose overrides the earlier unconditional base requirements. Compiling those semantic conditions into a validator is implementation work.
- The inherited claim stores integrity as alg/value. Excluding the integrity value leaves the immutable bound fields and integrity metadata available for canonicalization; no signature value is part of its own input.
- Timestamp is still not delivered as a separate JSON schema block, but its UTC/millisecond representation is specified sufficiently to generate the dependency. As already stated in the previous focused review, that missing generated schema artifact alone is not a lock blocker.

No algorithm choice, existing managed adapter, or production verification endpoint is required to lock these semantics.

## 5. Independent mechanical validation

The installed Python jsonschema Draft202012Validator was used. All probes ran in memory. No source, contract or production data was modified.

Two levels were kept separate: the printed schema fragments, and the effective normative contract including explicit semantic requirements. The latter governs the lock decision.

| Required mechanical check | Result and limits |
|---|---|
| 1. JSON parses | PASS: 14 managed JSON blocks = 8 schemas + 6 JSON fixtures. Resolution map contains no JSON blocks. |
| 2. Draft 2020-12 meta-validation | PASS: all 8 supplied schemas. Draft selected explicitly. |
| 3. References resolve | PASS for the corrected strategy: all 15 managed reference occurrences resolved through a review registry, using ordinary sibling URI resolution. Counts: EvidenceRef 6, Timestamp 5, Signature 2, ProgressFacts 1, VerificationEvidence 1. |
| 4. Pre-local-run fixture | PASS: A1 validates with null reference; a variant omitting local reference and authority fields also validates. Explicit normative predicates accept both. |
| 5. Read-only fixture | PASS: A2 validates without concrete write authority. A4 is rejected by the explicit none/read authority predicate. |
| 6. Write requires authority/claim | PASS: A3 fails the normative missing-worktree/scope/generation/claim checks. Adding worktree/scope/generation still leaves a missing-claim rejection. Each missing authority field was independently probed. This is not a positive cryptographic claim-validity test. |
| 7. Evidence-free verification pass | PASS under §6.2 semantic validation: rejected without evidence/bundle. Missing check_id or subject_revision is also rejected. A proof-bearing structural fixture is accepted. Actual proof execution/content is not inferred from that acceptance. |
| 8. No self-referential input | PASS: ReleaseProof fixture has no inner digest/signature, checkpoint has no second digest; envelope/claim signature-value mutations do not change their respective signature inputs; changing any required bound claim field changes the input. |
| 9. PlatformRun dedup consistency | PASS: the old tuple is absent; the authoritative tuple, provenance-only rule, ACK state machine, replay scenario and I4 agree. This is a contract consistency check, not an implemented transport test. |

**35 targeted normative probes passed.** Direct structural checks of A1–A6 also passed under the registered dependency profiles.

Important validation distinctions:

- Raw RunUpdate schema alone accepts A3/A4; the assignment-conditioned authority predicates reject them as specified. Raw VerificationEvidence alone accepts evidence-free pass; §6.2's predicate rejects it. Raw RunUpdate does not require projection_ref, but §4.4 does; that required-source predicate was exercised. These are explicit rules awaiting validator compilation, not ambiguous contract requirements.
- Relative registry IDs were mounted at an absolute review-only registry root; short refs were unchanged. An initial validator invocation double-applied a relative root ID; normal absolute resource registration resolved that test-harness setup issue without editing the contract or adding aliases for broken qualified refs.
- Timestamp was generated in memory directly from §7.11 and checked for UTC/millisecond grammar and valid date/time. The EvidenceRef validation profile used the previously inspected base constraints plus the addendum's effect_receipt/checkpoint/review extensions. The missing original base file means this is not a fresh full-base schema certification.
- Appendix A6 contains explanatory placeholders for payload, digest and signature. It parses and satisfies structural fields, but is not a real cryptographic test vector. Deterministic-input checks replaced its placeholder payload with A1 and calculated its digest. No signature was asserted cryptographically valid.
- The concrete canonicalization vectors use ASCII keys, ordinary strings and integers, for which compact sorted JSON matches JCS. No general-purpose RFC 8785 encoder, floating-point/Unicode conformance suite or cryptographic implementation was built or certified.

A1's canonical payload SHA-256 for that restricted fixture:

~~~text
d1062d3cebef912cc504b37258f3b8b25436fcaaa30a30913d7eefeae27ecc09
~~~

## 6. Regression checks only

B1/B2/B4 remain:

~~~ini
B1 = CONTRACT_RESOLVED_IMPLEMENTATION_PENDING
B2 = CONTRACT_RESOLVED_IMPLEMENTATION_PENDING
B4 = CONTRACT_RESOLVED_IMPLEMENTATION_PENDING
~~~

The release-proof identity/signature changes do not alter stop → drain/reconcile → checkpoint → local release → proof → Pi increment/regrant. Resource-scoped fencing remains complementary to local leases. Sequential local runs and renewable claims preserve the previously accepted lifecycle.

| Previously accepted invariant | Regression |
|---|---|
| I1 — At most one ordinary writer per scope | PASS: no new grant/overlap exception introduced. |
| I2 — Reconciliation-only grants no ordinary writes | PASS: drain uncertainty still blocks an ordinary release/regrant. |
| I3 — Historical acceptance never restores stale authority | PASS: explicit no-op/re-ACK preserved. |
| I4 — Replay never replays an Effect | PASS: corrected identity does not create dispatch permission. |
| I5 — Claim renewal does not mutate execution requirements | PASS: independent claim integrity retains the separate renewable-credential model. |
| I6 — Sequential local runs under one PlatformRun | PASS: local recovery preserves PlatformRun and sequence; attempt counting follows the active plan. |
| I7 — DecisionEngine remains selection-only | PASS: factual projections do not replace or execute decisions. |
| I8 — Platform authority does not replace local safety | PASS: local leases, effect-intent gates and uncertain-effect recovery remain necessary. |

These are regression findings at the contract/source boundary. No live handoff, asynchronous process termination, remote replay or Pi write was performed.

## 7. Lock decision and stop

There are no remaining B3/B5/B6 contract-level blockers. Remaining schema compilation, semantic validators, coherent projection, durable transport, claim verification and release/recovery integration are implementation work under the accepted contract. None was implemented here.

Only this review document was updated. Harness source, Pi and production were not modified. Review stops here.

MANAGED CONTRACT VERDICT: READY_TO_LOCK
