# Versioned repair targets and observed native limits

The full large-project objective remains active. This component makes historical
source repair more precise and rejects some malformed edits before apply. It has
not demonstrated successful autonomous API repair, a complete marketplace,
positive learning transfer, or complete website/security coverage.

## Protocol and boundaries

Recipe version 1 derives target IDs from the immutable historical path, source
hash, syntax kind, symbol and byte span. The model submits only an ID and new span
text. The host rederives offsets, source output and proof. The catalog offers
nonoverlapping syntax spans and text fallbacks, bounded to 32 targets and 8,192
encoded bytes. It describes syntax, not the verified cause of a failure.

The existing two-draft/8,192-source-byte limit, selected writable paths, current
workspace preimages, credential checks, raw proposal limits and inference budget
still apply. Missing historical companions require an explicit change, including
`retain_draft:true` for unchanged code. A summary cannot substitute for a file.
If the catalog does not fit the existing context budget, the coordinator falls
back to complete legacy drafts or the original bounded task; it never clips them.

For version 1, a repair cannot introduce Go syntax failure into previously
parseable Go. This guard covers both named and exact-text edits, including
persisted proof rederivation. It emits positions and fixed guidance, not parser
messages that may quote source. Malformed historical source remains repairable.
Parsing is only a structural check; actual compiler, behavioral checks and review
remain mandatory. Historical version-zero canonical/replay semantics remain.

The final instructions and tool descriptions agree that named edits replace only
the complete selected span. A Go TypeSpec excludes the enclosing `type` keyword.
Unchanged companion tests should be retained rather than regenerated. Earlier
candidate instructions incorrectly said draft paths accepted ONLY exact edits or
retention before later recommending target edits; the final candidate removes
that contradiction. This is a confirmed protocol inconsistency, not proof that
it alone caused every model failure.

## Retained negative evidence

- Developer packet v1 chose a plausible timeout edit but invented an unsupported
  database API and omitted a test companion. Real independent checks failed.
- Packet v2 selected overlapping parent and child spans and was rejected. The
  catalog was subsequently flattened, rather than salvaging the response.
- Ordinary native API v4 exhausted 3 attempts/2 plans: 24 actions, 7 actual calls
  and 1 explicit source fixture. Whole files were pasted into smaller spans;
  real Go checks failed and source was rolled back.
- A bounded-debug packet changed the correct timeout but inserted a function into
  a TypeSpec, producing `type func ...`. Replaying its exact captured response
  succeeds structurally on the old candidate but is rejected by the syntax guard
  before apply on the new candidate. The replay makes no model request.
- Ordinary native API v5 exhausted the same 3-attempt/2-plan/600-second budget:
  17 actions, 7 actual calls and 1 explicit fixture. The seeded check failed and
  rolled back; two subsequent syntax regressions were rejected before apply and
  recovered within the normal goal lifecycle. No successful native repair claim.
- After instruction consistency was corrected, the existing bounded-debug preset
  reached its unchanged 2,524-token generation ceiling (`finish_reason=length`).
  This response remains rejected; it was not repaired, salvaged or replayed as a
  successful inference. This does not support automatically routing repair work
  to that preset. Default provider, role, context and output settings are unchanged.
- The final normal-worker packet was complete and structurally valid. It selected
  the correct write-timeout target, but also changed `main` to use nonexistent
  `pgxpool.NewConfig` and replaced a working method test with a duplicate deadline
  test lacking imports. Independent compilation of exactly those model changes
  failed. No operator correction was made to obtain a pass.

These are development/component trials on the same physical Bonsai model. Their
initial source fixture comes from earlier native source. They are not unseeded
project-generation trials, a fair performance comparison, or heldout learning
evaluation. Consumed task/operation identities and original results remain intact.

## Verification checkpoint

The isolated candidate excludes unrelated shared-worktree architecture, Skills,
learning, UI and schema-82 changes. The syntax-guard snapshot (qualified-v2)
passed 431 test events/one conditional skip across worker/coordinator/engine.
Final qualified-v3 changes only the source-form instructions, tool descriptions
and one contract test; its full worker suite (186 events), focused coordinator
checks (9), and repository-wide vet/build pass using the isolated overlay.

Six valid production-role-bound Bonsai component approvals cover all thirteen
source files and changed hunks. The first review preparation used an incorrect
region-ID hash; the validator stopped that component before inference. Corrected
preparation retained the already-approved component and reviewed the other five,
without weakening validation or repeating the approved source review. Actual
HTTP parameters and the reconciled inference ledger match the immutable reviewer
preset. This is same-model review, not independent model diversity.

The qualified source is committed and pushed to `dev` at
`5aab261378b479930165a912e613a7f81cef4cb5`. No installed application or hosting
deployment was performed. Read the associated validation record for terminal
evidence and exact source/log hashes. Raw trials and generated projects stay in ignored `.tmp-ui`;
no generated marketplace or trial project belongs in Harness Git.

## Next acceptance

An operator-run counterfactual enumerated each of the final model's three named
edits separately, mechanically retaining all other historical source. Exactly
one of three cases passed the original seven actual Go tests: the write-timeout
edit. The other two failed compilation. Module files and the readonly deadline
regression were unchanged, and no new model request or actual project write took
place. This is evidence that unnecessary additional edits destroy an otherwise
working repair. It is not a production reducer or autonomous goal completion.

The next core step is bounded reduction and verification of complete, structurally
valid, test-failed proposals, with explicit derivation/provenance and unchanged
goal/check/review budgets. Do not salvage incomplete, truncated or invalid tool
responses. First verify the reducer inside the ordinary durable goal lifecycle,
including actual checks, failure/rollback/reopen, native source ancestry and review.
Then verify a complete normal native repair before claiming improved autonomous
completion. Continue the full requirement and
context/Skill selection, dependency decomposition, reverse engineering, real
marketplace journeys and learning-transfer acceptance. Guards and passing
component tests are supporting mechanisms, not substitutes for those outcomes.
