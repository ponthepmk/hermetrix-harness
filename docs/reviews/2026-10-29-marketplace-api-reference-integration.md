# Marketplace API: primary documentation failure and reviewed source integration

Date: 29 October 2026 (Asia/Bangkok). Status: **API health foundation integrated and independently qualified; full marketplace and autonomous repair remain unfinished.** Application source stays outside Harness Git at `D:/Projects/harness/hermetrix-marketplace`.

The actual application now contains three native Bonsai Go files. Seven behavioral tests pass against those files, with no source replacement overlay, skips or fake output; API vet/build also pass. A separate headless probe uses the actual dedicated PostgreSQL and confirms database identity, liveness 200 without a DB acquire, and readiness 200 with a real pool acquire/Ping. No main process or standalone server was launched. The PostgreSQL probe adds only its temporary test file through an overlay.

## What failed before this result

The published requirement-escalation repair (`3109f2d`) allowed the existing task to advance from requirement 1 to 2 while preserving obsolete requirement history. A fresh bounded trial supplied installed Go primary documentation: 11,345 bytes to the actual worker, exact SHA verified, and a 3,315-rune planner preview. The reference was readonly and operator prepared, rather than autonomously retrieved or selected as a skill.

That real trial still exhausted its four attempts: 30 actions, 10 actual calls, two plans and four compiler-failed apply/rollback cycles, retaining zero Go files. All responses completed as structured tool calls; truncation was not the observed cause. Correct planner API instructions and a delivered reference did not prevent the worker from repeating variable-scope, signal-constant and Ping function/type mistakes. The original exhausted goal, deadline, counters and failure artifacts remain intact.

## Narrow repair experiment

The last protocol-accepted, applied and rolled-back native source was exported through verified proposal/input/artifact loading. It was repaired only in a disjoint `draft-api` namespace, never passed off as current absent application source. Thirteen native calls, manually decomposed by the operator, fixed narrow regions instead of repeatedly regenerating both absent files.

The work covered variable scope, valid SIGTERM import, function adapters, outgoing HTTP request construction and JSON 404 routing. A production-server timeout probe exposed another defect: the five-second response deadline expired alongside the five-second readiness handler, producing EOF instead of 503 JSON. Native code increased WriteTimeout to ten seconds. The final native regression fails on the old five-second version and passes on the corrected version, asserting a bounded Ping, one invocation and an actual 503 JSON response using the configured server.

A later source review found that main could return as soon as ListenAndServe returned ErrServerClosed, before graceful Shutdown completed. Native `shutdown-v13` adds completion synchronization and forced Close on shutdown error. This was inspected against installed net/http documentation and retains passing tests; main/OS-signal process behavior has **not** been exercised. Same-model technical review v2 approved all three exact files with zero findings.

This experiment demonstrates supervised narrow repair feasibility. It does not demonstrate an automatic draft repair controller, automatic skill use, learned transfer, faster execution or lower total cost.

## Actual integration and a disclosed acceptance miss

A new scoped CodingOperation integrated the reviewed source references through normal planning, file selection, proposal, guarded apply, actual commands and post-test review. It did not replay or reset the exhausted original task. The worker received all three full readonly source references and exactly eight relevant files; writable authority stayed at the three actual Go paths, whose current preimages were absent.

The native goal reported completion after **four calls, eleven actions, one attempt and one plan**, and its proposal reached verified. However, the independent exact-byte check failed: main.go lacked one final newline. The original integration driver exited 1 and its result/log are retained; this is an observed miss in literal-copy fidelity, not a compiler failure. The native completion gate did not detect this requirement discrepancy.

The operator ran standard gofmt, verified that its output differed by exactly that final LF and matched the previously reviewed native file, then wrote it through the product API with the real current preimage. The separate normalization has one write receipt, zero rollbacks, no semantic edits and no rewrite of the native goal/proposal history. This was a supervised mechanical correction, not an autonomous repair result.

Independent checks on the resulting actual source pass all seven tests, vet/build and real PostgreSQL acceptance. All three source hashes now match the reviewed native references. A final read observer confirms the old exhausted task/goal is unchanged, its original 30 actions/four attempts/two plans and deadline remain, and no inference is reserved/dispatched/uncertain. REQUEST, PLAN, module files and readonly references are unchanged.

| Actual source | SHA-256 |
|---|---|
| api/cmd/server/main.go | cc6a2292197e2bf3616aeef8dfd08fa19d8b70e6bc2604ea35f918487f67c920 |
| api/cmd/server/main_test.go | 0388303a2682d210e179d702827dc34bcfa01cd0adfe781a51bc974acf1f1ad6 |
| api/cmd/server/deadline_test.go | 9312772329108528a8b6795c4052daaf5d366cf9e8dc87b018f32790ebf0b981 |

## Implications for Harness

The measured failure is not simply a context-capacity problem: selected primary documentation reached the worker and complete responses still compiled incorrectly. A promising next change is an explicit draft repair mode that edits one verified failed diagnostic in a separate namespace while retaining actual current preimages. Host materialization must record draft provenance and bind the ordinary proposal to actual source state; it must not pretend a failed draft is the current file or relax absent-file/view guards. This design is **not implemented**.

Literal-copy/formatting requirements also need an observable acceptance check if used again; a generic review and green behavioral tests did not prove byte equality. Do not hide the original failure with the later mechanical receipt.

Continue with bounded native business slices and real transaction/integration tests, while retaining complete buyer/seller/admin/payment/shipping/live/report/Thai UI/Compose/Kubernetes coverage. Actual DB outage/recovery, main/OS-signal behavior, media/cache/queue and end-to-end user journeys remain pending. Same physical model is used for authoring/review, and the prior unrelated Windows desktop full-suite failure is still open. No whole-repository test-suite, production-ready or whole-goal completion claim is made.

Evidence: [validation and artifact hashes](../validation/2026-10-29-marketplace-api-reference-integration.json). Raw/native evidence remains ignored under `.tmp-ui/goal-20261023/live-20261026-001100/{C64-20261028-marketplace-api-primary-docs-v1,C64-20261028-primary-prototype-repair-v1,C64-20261029-marketplace-api-reference-integration-v1}`. Private PostgreSQL credentials were never supplied to the model, project, logs or Git. No Harness source changed in this milestone; earlier runtime repairs are separately published.
