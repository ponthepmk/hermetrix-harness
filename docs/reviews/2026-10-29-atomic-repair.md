# Optional atomic repair and fresh native repair — 2026-10-29

State: Full large-project goal ACTIVE. Code published: 025e5cbe3d4500e4c931de99551c3e3e230d55ce.

## Failure and causal boundary

The previous fresh scope-v4 writer added X-Response-Deadline in handleReady and replaced doRequest instead of repairing http.Server.WriteTimeout. It exhausted the unchanged three-attempt/two-plan allowance. Its prompt used15474tokens, far below the qualified65536context: context exhaustion was not the observed failure.

Restoring a missing companion through an explicit changes item still invited unnecessary edits. Hypothesis: an optional one-span root form lets the host restore missing historical companions without asking the model to emit their changes. This is a protocol capability, not a guarantee that a model selects the causal span. The new native observation did NOT adopt the root form, so the hypothesis is not established as a native quality/efficiency gain.

## Source behavior

- Recipe2 accepts either root `repair:{path,target_id,new}` or the existing explicit `changes` form, exclusively. Null or empty changes cannot accompany repair; missing/null replacement is rejected. Explicit empty string remains a valid deletion when the resulting syntax is valid.
- The host derives exactly that model-authored target replacement, then restores every missing historical companion from its verified frozen draft. Existing companions are untouched. Complete output retains actual current/absent preimages and normal independently rederivable DraftEditProof. All checks/review remain mandatory.
- Recipes0/1 keep their old requests and target identities. Recipe2 IDs bind the recipe and exact historical source. New coordinator tasks prefer2 only within the existing context fit; fallback is1, then0, then the original context. No schema migration, provider default, write authority, output or goal budget was enlarged.
- Multi-edit and ordinary changes remain available for genuinely broader work. A target catalog is syntax metadata, not a proven diagnosis; hashing a quote does not prove causality.

## Acceptance evidence

| Gate | Actual evidence | Result |
|---|---|---|
| Grammar and authority | Exclusive JSON-schema/runtime forms, stale/no-op/unknown/malformed targets, legacy requests, re-signed tampering, selected writable preimages | Passed |
| Companion outcome | Node required both absent sum.js/helper.js; failed first check and rollback; root repair restored both; readonly test unchanged; SQLite persisted/rederived two-file proposal; post-test reviewer called once | Passed, inference transport was a fixture |
| Regression | Frozen affected worker/coordinator/engine suite: 460pass/1skip/0fail; final schema and companion additions checked separately | Passed within affected isolated candidate |
| Tooling | Whole candidate vet/build; final vet includes added test | Passed |
| Native repair | Five actual Bonsai calls plus one explicitly recorded initial failing source response;20actions/2attempts/1plan; same3attempt/2plan/600second cap | Completed component |
| Independent observation | Reopened native DB, rederived proposal/proofs, checked current complete selected-source scope1 and actual7Go PASS records | Passed, zero inference/source writes |
| Code review | Four bounded production worker-review4096/reasoning0/thinkingfalse packets, role ledger reconciled | Four approvals, same physical model |

Full suite used v1 frozen source. Final v3 has identical production hashes; only the added schema test and real companion lifecycle test differ, and both separately passed. Review covered the same production hashes plus v2 schema test; the later companion test is additional outcome evidence. Review inputs explicitly disclosed that the full affected suite was still running then. Review preparation first exceeded its own32000byte packet ceiling; no model request was sent. Splitting supporting context kept all four packets29535/29397/28717/28280bytes without changing source or validator.

## Native outcome and limitations

The actual new writer selected NewHealthServer.WriteTimeout=10seconds, which addresses the observed5second-handler/5second-server EOF. It used the explicit two-named-edits form and also added a100ms success delay to FakePing in main_test.go. That extra helper change was unnecessary; assertions were not removed or weakened and the readonly deadline regression/module hashes remained unchanged. This is NOT evidence that the native model now always chooses minimal edits.

The ordinary loop passed all7actual Go cases and a fresh same-model post-test review. Proposal `4c45ebb778f5ac91f69d5803631077e9edfd325745715060357e31961e13684e` is verified and independently observed. This trial has one initial recorded failing source fixture, not a second recorded repair answer. No host reduction, Codex-authored application semantic source, standalone server or deployment was needed. It is fresh repair success for this seeded component, not unseeded marketplace completion, fair A/B, a general speed/token saving or learning transfer.

## Full objective and next work

Detailed requirements/DAG planning, relevant context+Skills, bounded sustained execution, verified repair/recovery, reverse engineering/problem analysis and the complete marketplace/Compose/Kubernetes/business/UI/learning outcome remain in scope and incomplete. Stop repeating this passing deadline case. Carry the protocol into a distinct next real project step and observe repair choice/companion behavior there; full business journeys and learning benefit still need independent evidence.

Projects/raw calls/DB copies remain under ignored `.tmp-ui/atomic-repair-20261029`; only scoped Harness source/tests and curated documentation belong in Git. Shared architecture/Skills/UI/learning/schema82 work is preserved. Serving18816 rejection is not bypassed; only owned headless tests ran. See [machine-readable validation](../validation/2026-10-29-atomic-repair.json).
