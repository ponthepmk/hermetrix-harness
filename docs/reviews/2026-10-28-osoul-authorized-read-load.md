# O-SOUL: authorized prelaunch production read-load pilot

Status: **One measured public-API read pilot completed; full security/flow coverage and Harness tester implementation remain incomplete.**

The owner confirmed this is production in a testing phase, not yet open to customers, and selected ten virtual users, an aggregate ceiling of ten requests per second and a one-minute window. This permission does not authorize a larger or continuous stress test or arbitrary production mutations.

## Actual run

- Run: `osoul-load-20261028T122047Z`, 2026-10-28, 12:20:47 UTC / 19:20:47 Bangkok.
- Target: the API origin mapped by the matching local O-SOUL project, `https://api-o-soul.go2gether-tech.online`.
- Four GET endpoints: `/products`, `/config/public`, `/shipping-methods`, `/contest/rounds`.
- Ten virtual guest-user slots, persistent HTTPS connections, aggregate pacing, no credentials, cookie reuse, registration, writes, votes, purchases or generation calls. No redirects, retries or proxy-identity spoofing.
- Sixty-second admission window; total elapsed including final cleanup 60.015 seconds.
- 549 requests dispatched, average 9.15 per second, maximum ten starts in any rolling second. All ten guest slots were used. Maximum **observed in-flight requests was two**, so this does not demonstrate ten simultaneous requests or authenticated users.
- All 549 received HTTP 200 headers. 548 response bodies completed bounded decompression and JSON-container validation. One final request was stopped by the runner's approved-window deadline after HTTP 200 headers, before its body was validated. Preserve the raw summary's `errors=1`; classify it as one runner cancellation and zero other observed errors, not a fully passed response or a server failure.
- A later explicit `GET /health/ready` at 12:24:37 UTC returned 200 with `{"status":"ready"}`. This proves readiness at that observation, not absence of all side effects.

| Endpoint | Requests | p50 | p95 | p99 | Body-check boundary |
|---|---:|---:|---:|---:|---|
| `/products` | 138 | ~62 ms | ~78 ms | ~125 ms | One final runner cancellation; 137 fully checked |
| `/config/public` | 137 | ~78 ms | ~125 ms | ~156 ms | 137 fully checked |
| `/shipping-methods` | 137 | ~47 ms | ~78 ms | ~110 ms | 137 fully checked |
| `/contest/rounds` | 137 | ~62 ms | ~94 ms | ~110 ms | 137 fully checked |

These are client timings from one read workload; the timer's implementation and resolution are recorded in the validation JSON. A 200 response and parseable JSON do not verify business correctness. No application/DB/resource metrics, privileged routes, cart/checkout mutations, browser load, multi-role isolation, race scenarios or exploit reproductions were covered. This is not maximum capacity, an SLA, a comparison with the older 250-user benchmark, or an all-flow/zero-vulnerability certification.

## Evidence

- Tracked aggregate and classification: `docs/validation/2026-10-28-osoul-read-load.json`.
- Ignored local request-level evidence and operational driver: `.tmp-ui/osoul-load-20261028T122047Z/` (`scope.json`, `requests.json`, `summary.json`, `analysis.json`, `followup-health.json`, `run_read_smoke.py`). Raw responses and secrets were not retained. The tracked aggregate records evidence hashes.
- Earlier six-request CORS/header observations and coverage inventory: `.tmp-ui/security-assessment-20261028T115148Z/`.
- Design and scope limits: `docs/redesign/2026-10-28-authorized-security-testing.md`.

The permissive credentialed CORS preflight observation remains a configuration finding; no signed-in cross-origin disclosure was reproduced. The older report's disabled-rate-limit setting still requires a current deployment check; this pilot did not brute-force requests to infer it or change the protection.

## Harness work in parallel

Native Bonsai generation is building historical failed-source context for the large-project repair loop. Actual retained API input verified that a repair worker received compiler diagnostics but only empty/absent source after rollback. Early compound candidates truncated or failed Go checks and were not applied. A narrowly scoped taskengine lookup candidate compiled with the existing taskengine suite; five exact native-SQL replay cases on the retained database matched only the correct log/task/step. This is not yet an end-to-end behavior test or an applied Harness fix.

A temporary immutable temperature-0.7 preset stopped one observed generation loop, but its code failed compilation. No provider or existing default preset was changed and no performance/general-quality improvement is claimed from that single trial. Subsequent candidates use smaller functions and the existing default preset, with real Go checks before application. Runtime changes still require native tests, exact QA, review and guarded apply.

Next: implement and verify the scoped tester authority and runner lifecycle from the design, designate disposable accounts/fixtures before mutation scenarios, and connect browser/API/security jobs under one enforced aggregate budget. Preserve the full large-project/context/reverse-engineering objective; this pilot does not complete it.

## Native repair-context checkpoint for continuation

Ignored work directory: `.tmp-ui/goal-20261023/live-20261026-001100/C64-20261028-historical-source-v1/`. Read `checkpoint.json` first. Every existing task/output identity is consumed; use fresh identities for further calls. Latest check found zero reserved/dispatched/uncertain inference rows; no model job is left running.

Pending accepted candidates, all **unapplied**:

- `engine-v3`: `internal/taskengine/historical_proposal.go`; ancestry is `engine-v2` → `engine-v3`. Correct exact `artifact:` JSON-member lookup and QueryRowContext release before GetCodeProposal (store has only one open connection). Existing taskengine tests passed with this overlay. Exact native SQL replay matched the retained actual proposal only for correct task/step/log; wrong task, step, substring and double prefix did not match.
- `excerpt-v2`: `internal/taskcoord/historical_source_excerpt.go`; ancestry `excerpt-v1` → `excerpt-v2`. Pure exact-path/max2-files/8192-total-byte/UTF8 prefix collector with full hashes kept distinct from excerpts.
- `orchestrator-v4`: `internal/taskcoord/historical_failure.go`; ancestry `orchestrator-v3` → `orchestrator-v4`. Validated loader and evidence wrapper, preserving terminal-log identity and nil guards. These three candidates compiled together via `production-candidate-overlay.json`; `[no tests to run]` is compile evidence only.

Do not use rejected monolithic `helpers-v1`, `context-v1`, `context-temperature07-v1`, `context-repair-v2` or rejected `excerpt-tests-v1` output as source. The last test request hit the output cap. Split the seven frozen test cases into smaller native calls rather than dropping them or repeating the same compound request. Scope, Thai-byte boundaries, combined-file budget, empty source, file count, complete-content hashes and input immutability all remain required.

Still required before apply: native service hook (enrich at most one diagnostic per packet), native boundary tests, direct Go replay of retained failed source, full rollback/repair workflow observation, exact affected-scope QA/vet/build, native review and ancestry-validated guarded apply. No product Go or standing-test files have been written into the worktree.

The temperature pair was verified equal after normalizing only temperature and the required fresh task identity. Temperature0.1 repeated stubs to8192;0.7 completed1116tokens but failed Go QA. New immutable experimental preset `C64-worker-temperature07-context-v1@1` lives only in the isolated runtime data. Existing profiles/default presets remain unchanged. Native provider service applies the selected preset's generation cap, so the Task field4096 was not the effective request cap; actual recorded cap was8192. Preserve actual request/preset identity and do not claim that declared Task cap was enforced.
