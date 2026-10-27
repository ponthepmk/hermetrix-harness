# Dev integration — 2026-10-27

The user authorized committing and pushing the prepared Harness implementation to
`dev`. This integrates dev base `e0b8f13c7e6f` with feature checkpoint
`bfaa09fdc22d`, retaining the six separated Harness commits and the
remote dev changes. Publication uses a normal push; the final remote commit receipt
is reported separately. No application install or installed database was changed.

## Integration behavior

Both branches had a different migration 74. The merged store uses schema 81:
the existing idempotent desktop migration adds its observation columns, followed
by the existing dev retry and approval-mode tables. An actual database created
by the original dev schema 74 opened successfully with the merged schema 81,
preserving an isolated sentinel. Standing tests also cover schema 80 reopen and
idempotent legacy desktop migration. These checks use disposable local data.

Turn admission retains idempotency, expected-outcome checks and queue claims in
distinct argument positions. A final tool request returns a receipt-based partial
summary while preserving the single audit-report exception and tool-call caps.
Notebook compaction, automatic retry, queue, stop and steer remain present.

The retained compact composer exposes the new approval controls through its Tools
disclosure. Text attachments and automatic-retry notices are integrated into the
existing footer. Token progress measures the latest turn; cumulative chat totals
remain separate. Browser assertions account for the dev workspace-bound chat
reusing three turns. The IDE selects an admitted profile before sending, retains
explicit project and target consent, and avoids retriggering mobile compose mode
after a completed send. Global dev capacity selection remains unchanged.

## Verification

The exact merged source snapshot passed 1,459 Go test events with
zero failures and 23 skipped tests; vet and build passed.
All 159 UI unit tests and the Mac ARM64 cross-build passed. A cross-build does
not establish Mac runtime behavior. Skipped integrations are enumerated in the
[portable validation](../validation/2026-10-27-dev-integration.json).

Actual Chrome fixtures exercised mobile reading and tool/queue/token disclosures,
pointer and touch submission, text attachments, unsaved editor context, explicit
project consent creating two files, and an approved browser origin with held-key
movement. The 390px screenshot was inspected. Earlier failing native merge drafts
and mismatched branch assertions were corrected before the final complete run.

Three bounded read-only Bonsai reviews approved the core, composer controls and
IDE scopes with zero findings. This is the same model through a separate saved
reviewer profile, not model diversity or an independent security audit. One larger
UI packet was refused before inference by the existing input ceiling; fresh smaller
packets passed without increasing any model budget. New semantics and tests were
native authored; integration also projects exact existing source spans.

## Git contents and continuation

Generated applications, working project folders, new raw captures, local databases
and model files remain on disk and ignored. The tracked manifests remain the root
Harness Go module and its existing frontend vendor package. Existing tracked
historical evidence is retained. No business-project source is added by this work.

The broader self-improvement and marketplace goal remains ACTIVE. The Project Brain
pilot establishes one-lesson retrieval/answer-use improvement, not general learning
transfer. Continue from the Project Brain measurement and full execution contract.
The shared authoring database is still schema 80; do not blindly rerun old authoring
or migration scripts after switching the checkout to schema 81. The earlier serving
approval rejection still prohibits retrying another port, binary or route.
