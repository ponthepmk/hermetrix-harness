# Large repository context qualification — 2026-10-28

## Current64k continuation — 28 October

The historical16k trials below remain frozen. The owner now authorized Bonsai64k.
Native L2, L7 named-prefix coverage, and qualified64k planner policy are applied;
latest exact QA1466pass/20conditional skips/0fail, vet/build0. See
[commerce64 runtime and real-project continuation](2026-10-28-commerce64-runtime.md)
for current profiles, evidence and pending marketplace status. L4/L5/L6 remain
unapplied. Source metadata coverage does not prove full task completion.

The first actual reverse-engineering trial on a faithful1,236-file/46,965,500-byte
Harness repository snapshot failed before planning. The unchanged Bonsai16,384
window received a prompt of24,106 tokens. Its planner user JSON alone was41,767
bytes. `repositoryBodyBudget` assumed four characters per token; dense file paths
and SHA256 values violated that assumption. The saved profile field did not prove
that the rendered request fit.

## Applied repair

Bonsai authored the helper repair and regression tests. The helper now conservatively
limits metadata body bytes to the input allowance after the existing2,048 framing
reservation, rather than multiplying that allowance by four. Ranked selection,
required writable paths, explicit refusal, snapshot hashes and truncation receipts
are preserved. This reduces candidate breadth; later stages still need focused
discovery. The fixed framing reserve is not a universal token bound for every adapter.

The two new tests use actual filesystem fixtures containing1,200 dense filenames.
Against original production code they failed independently: planner41,646 bytes,
selector42,858 bytes, allowed10,752. Both passed with the candidate. Existing
all-pins-or-refuse and late-writable-path full-lifecycle tests remain in place.

The exact two-file candidate passed the complete repository suite:1,464 passing
test events,20 conditional skips,zero failures; vet/build passed. A separate saved
Bonsai reviewer approved with zero findings. This is the same model, not diversity.
The product write API applied two native source/test files with two receipts and
zero rollback. Targeted actual-source tests passed after apply. No installed app,
installed database, model profile, context allocation, commit or push changed.

Source: [budget helper](../../internal/taskcoord/repository_context.go),
[regressions](../../internal/taskcoord/repository_context_dense_test.go).

## Actual model results and remaining failures

A fresh candidate trial using the same repository and original task passed planning
with6,011 prompt/1,426 output tokens and file selection with3,193/248. The worker
then used13,810/32 and returned empty changes, rejected as `invalid change count`.
Both trials are terminal at an idle waiting goal, with zero generated reports.
No invalid proposal was applied or salvaged, and all prepared source hashes in
both workspaces remained unchanged. This does not establish large-job completion.

The selector named15 reference paths, but omitted the report destination. The frozen
contract and step instructions explicitly name `analysis/context-architecture.md`;
the worker task has neither that path in Files nor an AbsentFiles declaration.
That is a confirmed gap in carrying an authorized new-file target into generation.
It is a plausible explanation for the empty change response, not yet proof of sole
causation. The worker request also shows that character-based fitting can still
underestimate actual token use outside the repaired metadata paths.

The L4 native candidate binds only exact current-step destinations already in the
frozen writable scope. Its regression fails against the old coordinator and passes
against the candidate: with all model transport stubbed, the actual filesystem,
Node acceptance command, and goal lifecycle create the omitted destination and
complete the fixture. This is a regression result, not actual Bonsai completion.
The helper, coordinator patch and tests remain unapplied. The candidate full suite
recorded 1,480 passing test events and one failed managed-browser test caused by
Windows denying deletion of a Crashpad file during TempDir cleanup. An isolated
rerun passed; the complete suite has not yet been requalified. Native review did
not approve, citing the lack of actual end-to-end native completion and full QA.

A fresh actual Bonsai trial v3 included the target-binding candidate. Its worker
request now has both the explicit empty destination and host-observed AbsentFiles
entry. Planning used 6,011 prompt/1,253 output tokens; selection used 3,143/282.
Generation then failed with HTTP400: 17,361 prompt tokens exceed the unchanged
16,384 window. The step required approximately 18 reference sources; its rendered
worker body was 47,546 bytes. All 1,241 prepared file hashes remain unchanged,
profiles remain unchanged, and zero reports were generated. The run is terminal
with a waiting goal; do not replay its identity.

The L5 native candidate extends the conservative byte envelope to worker input.
A dense Thai regression fails with the old guard and passes with the candidate;
the existing large-source and omitted-target lifecycle fixtures also pass. The
curation fixture preserves its original below-framing-reserve case as explicit
refusal and adds the same optional-reference data at an existing 8,192 test
envelope, preserving citation checks. The candidate count change was authored by
Bonsai. Candidate DATA is mounted only during qualification and the guarded
original eight-row worktree fixture has been restored. A first compatibility run
incorrectly relied on Go source overlays for runtime os.ReadFile DATA and failed
the row-count check; correcting the operational mount made all four targeted
tests pass. No product or standing-test assertion was weakened.

Offline replay of the actual v3 source task with the L5 guard refuses with
`proposal source fit exhausted`, even at minimum 256-byte views. The guard prevents
this known oversized dispatch but cannot make a broad step executable. Neither
L4 nor L5 is approved or applied. Next inspect budget-aware planning/decomposition:
split source-grounded analyses into dependent, bounded artifact updates, preserve
requirements and explicit destinations, and report missing coverage honestly.
Silently hiding references or enlarging the profile is not a completion proof.
Then run a fresh actual native trial and independently verify report citations.

## Goal and evidence boundaries

The full goal remains ACTIVE: detailed planning, relevant skills/context, bounded
decomposition, autonomous implementation/testing/repair and reverse engineering.
Realistic business changes, recovery, held-out transfer, trusted promotion and the
full marketplace Compose/Kubernetes delivery remain required. This repair is one
observed obstruction removed, not a substitute for those requirements.

Raw evidence remains ignored under RUNROOT/L1-20261028-native-context-audit-v1,
v2 and v3, L4-20261028-target-binding-v1 and L5-20261028-worker-fit-v1. Native
trial, generation, review and apply identities are consumed. Reconcile their exact effects
before further work; do not reset deadlines, reuse output paths or blindly replay
failed provider operations. The installed serving approval rejection still applies.
See the [portable validation](../validation/2026-10-28-large-repository-context.json).
