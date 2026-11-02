# Platform development candidate — G2–G7 execution record

Handoff: 2026-11-02, Asia/Bangkok. Tests were run across the preceding evening. The user authorized continued development through the final stage; production mutations remain separately gated. Godot remains deferred.

**Disposition: development candidate packaged and verified; production release NOT qualified.** Do not interpret the presence of G7 artifacts as passing every dependency/acceptance criterion. Exact gate statuses and blockers are in ../PLATFORM-RELEASE-STATUS.json.

## Changes completed in development

- Bound automatic server continuations to three attempts across recovery; manual new requests reset their allowance. Client connectivity retries also stop after three attempts and every ordinary chat request now has a retained idempotency key.
- Exposed exhausted status clearly instead of silently scheduling indefinite work.
- Classified refused project path escapes as HTTP403; an HTTP regression proves the outside canary is not disclosed.
- Retained the voice capture epoch/socket guards, added restoration of the previous transcript after a rejected unconfirmed partial, and requested verbose Whisper speech evidence. High no-speech-probability segments are dropped without blanket rejection of English/Thai speech.
- Added owner-scoped scheduled/event task intake with atomic draft/receipt persistence, stable creation/event keys, revision guards, overlap suppression, bounded due batches, elapsed-interval coalescing, pause and history. New task templates are local-only and require review; unattended execution is deliberately not enabled by intake.
- Added the matching Maintenance UI using existing application components. Real-browser creation/pause/reload passed and screenshots were checked at390/768/1440.
- Froze reduced host-owned child budgets for team work, divided a finite aggregate token allowance among children, disabled independent child auto-continuation, and refused to call incomplete child outputs completed. Existing DAG/cancellation/approval/recovery behavior remains covered by tests.
- Added additive schema83, API-addition OpenAPI and complete method/path inventory, threat model, runbook and checksum-verified development packaging. The new development launcher requires OS sandbox support by default; Windows trusted-development opt-in is explicit.

## Verification receipts

| Verification | Result |
|---|---|
| Full Go regression after backend changes |1654 passed test events,26 skips,0 failures;38 passing packages |
| Final Node UI regression |183 passed,0 failed |
| Targeted real browser after final UI changes | Automation create/pause/reload, Goal controls, voice-interruption controller, path refusal and Whisper confidence contract passed |
| Automation concurrency/race tests | Passed |
| Synthetic installed-binary migration |50→83, bilingual history, paused task, integrity/foreign keys, reopen and restore passed |
| Brain exact committed source, isolated Linux fixtures |186 passed,0 failures |
| go vet ./... | Passed |
| Development ZIP | Extracted; all11 checksummed files verified; binary-reported hash/schema matches manifest; no databases, secrets/logs or test files included |

The full Go pass preceded the final small client retry-key adjustment; affected Node/browser workflows were rerun afterward. This is not a claim that a live model or physical microphone was exercised by those tests. Skipped integrations remain unverified.

Brain source archive came from committed HEAD2730d21176293097a4fe7dbd6c2eb7cad7a1ecfc via read-only SSH git archive. No Pi files were installed/modified. Initial Windows execution correctly failed because the service uses fcntl. On WSL with fixtures on the Windows-mounted filesystem,183 passed and three restore/permission tests failed due to mount rename/mode behavior. Repeating the unchanged source on native Linux temporary storage passed all186. No lock/permission assertion or production code was weakened.

The browser test initially found that pause sent the response-only next_run_at field into a strict input API. The UI now sends exactly the editable contract and the original workflow passes. Node VM fixtures were updated to supply the real Web Crypto API after adding idempotency keys; product code was not weakened to satisfy the test environment.

## Release identity

Binary SHA256: d9f982fdeb9215f3c00383d8432855e25e8ebf74a06489ac411fbc5db2bf76bc

Input ledger digest: dfc69db74a3cf46420189ebe2dac229e3ebbaaba412f4c22c606810f4d02f5cc

ZIP SHA256:5d3d9a2be4d8c698a4829d570f31ccd0991fc644793f9f14ede01661a6e6f0d5

Base commit b3de3a855b83562291555ea24b7c61133f3e6495, explicitly dirty. The package includes pre-existing checkout changes identified by the input ledger, not only this turn's delta. No automatic commit/push was performed; test files remain local under the user's commit constraint.

Artifact: D:/Projects/harness/hermetrix-harness/.hermetrix-g1-staging/release-2ec0d50735054dc890366bfdb26ba326/hermetrix-development-candidate.zip

Raw synthetic receipts/screenshots: .hermetrix-g1-staging/g2-g7-evidence/. Sanitized package receipt: ../validation/g2-g7-package.json.

## Unfinished acceptance — not hidden behind passing tests

1. Automatic approval review rejected launching the local LLM runtime with "blocked by policy". The model process was not started by that command. A currently running endpoint was requested from the user; none had been supplied when packaging finished. Actual GPU/context/latency and representative multi-file model-driven coding acceptance are therefore pending.
2. Physical microphone/loudspeaker echo and natural bilingual interruption remain unqualified. PCM/Whisper contract/controller tests do not prove acoustic echo cancellation.
3. Windows untrusted-code filesystem/network isolation is not supplied by a Job Object. The new launcher fails closed by default; opting into trusted development does not close that security gate.
4. Specifications00–22 remain unavailable. The implemented scope follows the user goals and approved G0 plan; exhaustive Full Scope conformance cannot be signed off without those inputs.
5. Real installed-data upgrade/restore, credential rotation, source/config changes on Pi and deployment have not been performed or approved by this candidate. No managed Platform production executor is claimed from the fixture-only adapter.

## Rollback and continuation

Installed app/data and Pi services remain unchanged; staged services owned by the acceptance runs were stopped. To discard this candidate, stop only its own process and retain artifacts; no production rollback is needed. For a later approved upgrade, use the runbook's consistent full backup/restore procedure, never an old executable against migrated data.

Next dependent work is live-model and acoustic acceptance, specification reconciliation and real-data staging authorization. The saved gate ledger and exact input/executable hashes allow continuation without repeating already-passing unit/browser/Brain checks unless the code changes.
