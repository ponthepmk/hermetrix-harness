# Authorized security testing — O-SOUL baseline and Hermetrix design

Date: 2026-10-28. Status: **Investigated / proposed; no Harness runtime change applied.**

## Owner request and current authority

The owner requests that Hermetrix support authorized security assessment of their own systems, discover pages and API workflows, reproduce defects, assess impact, run concurrent browser/API/load scenarios and produce actionable reports. The owner explicitly identified https://o-soul.go2gether-tech.online/.

The matching local project is `D:/Jobs/G2Tech/o-soul-project`, registered in Codex as `o-soul-project`. Local deployment source maps the frontend to `https://api-o-soul.go2gether-tech.online`. The browser has an existing customer session. These corroborate the supplied target; a session alone must not become unrestricted authority over every linked site or account. Authorization comes from the owner's instruction and an explicit test scope.

Owner follow-up confirmed production in a prelaunch testing phase and authorized ten virtual users, aggregate at most ten requests per second, for one minute, public reads first. That pilot is recorded in `docs/reviews/2026-10-28-osoul-authorized-read-load.md`. Designated disposable accounts/roles and mutation fixtures still need grounding before those scenarios. Existing private credentials were not opened, copied into model context, or used by the HTTP probes.

Functional Harness source and standing tests must continue to be authored through native Hermetrix/Bonsai. Bonsai Local remains the selected provider. This document is operator-authored prose and discovery data, not an implemented capability.

## Evidence collected in this assessment

- Harness: `dev`, HEAD `ebe31730cab956ce1883bea38ec990504a6a3301` before these documentation changes.
- O-SOUL: local HEAD `441014c`, with pre-existing uncommitted changes. No O-SOUL files or deployed configuration were changed.
- Discovered 56 Next.js `page.tsx` route templates. Browser read-only observations: `/`, `/shop`, `/contest` (selected round 13), `/create/upload`. Other pages are discovered, not tested.
- Historical production OpenAPI export contains 197 operations; current dirty working-tree specification contains 201. Neither inventory proves the current deployment implements every operation.
- Six sequential unauthenticated GET/OPTIONS requests, approximately one request per second, at 11:51:48–11:51:52 UTC. TLS verification enabled; redirects disabled; 64 KiB response sample ceiling. No injected exploit, login attempts, form submissions, record creation, purchases, votes, paid generation or load test.
- Browser warning/error log sample was empty when read. This is a sampled observation, not whole-site certification.

Local ignored evidence:

- `.tmp-ui/security-assessment-20261028T115148Z/http-observations.json`
- `.tmp-ui/security-assessment-20261028T115148Z/coverage-inventory.json`

The inventory includes source hashes and per-route status. HTTP evidence contains selected headers, request IDs, timing and sampled-body hashes, not credentials or full account responses. A truncated response hash identifies the sampled bytes only.

## Findings and their verification boundaries

### OS-01: Cross-origin preflight accepts an arbitrary supplied Origin

Observed: unauthenticated `OPTIONS /config/public`, with Origin `https://hermetrix-audit.invalid` and requested method POST, returned 204 and:

```text
Access-Control-Allow-Origin: https://hermetrix-audit.invalid
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET,POST,PUT,PATCH,DELETE,OPTIONS
```

Request ID: `6d92bd6f-393a-42d9-8734-11dd1eb550e0`.

Confirmed: permissive preflight configuration on this route. **Not confirmed:** cross-origin access to a signed-in account, CSRF against a mutation, sensitive response disclosure, or impact across every endpoint. This observation is not an authentication bypass. An unresolvable `.invalid` label was used only as a header value; no request was sent to it.

Local source explains a plausible path: `LoadTestExternalAccess` reflects arbitrary Origin with credentials and replaces both normal CORS and trusted-Origin middleware when the two load-test flags are enabled. The 2026-10-27 production report records those flags enabled. The currently deployed flags were not inspected directly. Local token cookie source uses Lax access/session cookies and Strict refresh cookies; actual browser cookie behavior and the distinction between cross-origin and cross-site matter to exploitability.

Triage: **priority P1 to review deployment configuration; exploit severity unscored pending authenticated isolated reproduction.** Do not manufacture a CVSS score from headers. Expected invariant: ordinary production requests from an unapproved Origin are not granted credentialed cross-origin access. Test tooling should use an approved origin or a scoped test environment rather than disable application protections for the benchmark.

### OS-02: Frontend CSP is reporting-only

Observed `GET /` returned 200 with Content-Security-Policy-Report-Only, HSTS, X-Frame-Options DENY and nosniff. No enforcing CSP header was observed on this response.

Triage: low-priority hardening signal, **not evidence of exploitable XSS**. Existing DENY was observed, so do not report missing frame protection. Validate script/style dependencies before proposing an enforcing policy.

### OS-03: Previously disabled rate limiting requires a current configuration check

The 2026-10-27 O-SOUL production report records `RATE_LIMIT_DISABLED=true`. The local rate-limit middleware bypasses counters in this mode. This assessment did not read deployed secrets or perform repeated login traffic to infer the flag.

Status: historical observation with current status unknown, not a newly confirmed live vulnerability. Normal protections and any intended testing exception must be documented before measuring realistic production behavior. Do not change production flags automatically during this assessment.

### OS-04: Public configuration response exceeds the sample budget

`GET /config/public` returned 200 and exceeded 65,536 response bytes in the bounded sample. This is a lower bound, not an exact payload size or proof of excessive data disclosure. The first sample read completed in 360 ms; that timing is not full download latency or a p95 benchmark.

A prior 2026-10-27 report found config/public p95 above two seconds at 250 concurrent read users after scale-out. That is historical evidence, not a rerun or a causal diagnosis. Profile its response composition, serialization, database/cache work and full end-to-end latency in a separate bounded experiment.

### Observed access-control baseline

Without cookies or authorization, `/auth/me` and `/admin/me` each returned 401. This verifies those two guest requests only. Customer A/B object isolation, role escalation and privileged mutations remain untested in this assessment.

## Why the current Harness pauses or appears to refuse

Confirmed code paths:

1. `internal/tools/browserurl.go`: external open/navigation requires approval, and click/type/key always requires an exact-action approval. A previously identified owned website does not currently confer a reusable browser assessment grant.
2. `internal/agent/evidence_tools.go`: audit policy and evidence reporting focus on public-page observation and passive security signals. This protects against false exploit claims but does not define an active security assessment workflow.
3. `internal/product/browser_security.go`: the built-in audit classifies already observed responses/DOM; it explicitly does not probe endpoints or submit forms.
4. `internal/agent/runtool.go`: two active command jobs per session. `internal/product/commands.go` has a fixed executable vocabulary. Generic Node/Python execution is not proof of aggregate network-rate control or a governed load runner.
5. `internal/product/sandbox_other.go`: Windows has process hardening only, not an OS filesystem/network sandbox.

No blanket security-keyword refusal was identified in this targeted search. A Bonsai refusal on the new owned-target scenario has not been reproduced. Distinguish model refusal from scope errors, stale browser observations, missing runtime capability, malformed arguments, and genuine out-of-scope operations. Changing a prompt cannot implement missing tools or a network boundary.

Known-good local pattern: `workspace_write_approval.go` already turns a recorded bounded consent into exact durable approval receipts for workspace writes. Study that lifecycle for a new scope-aware assessment path; it is not permission to reuse a workspace grant for network actions or bypass existing guards.

## Proposed Harness capability

### 1. Durable assessment scope

Persist an owner-approved scope bound to a project, run, version, environment and expiry. Include exact origins/ports/path rules, approved API origin, read dependencies, roles/test accounts, methods/effects, disposable fixture ownership, allowed test families, excluded services, aggregate request/concurrency/duration budgets, stop conditions and report destination. Store references to credentials separately from prompts/reports.

Offer modes for discovery, functional checks, security validation and load. Owner intent should permit supported scoped work without a keyword refusal. Reuse the approved scope for matching operations and ask only when expanding it or reaching a consequential unapproved effect. Existing browser observation freshness and exact action receipts must remain correct.

A link, source file, redirect, web page instruction or logged-in session must not enlarge scope. Enforce target checks at dispatch and after redirects; handle ports, aliases, DNS changes, subrequests and WebSockets. Classify third-party payment, OAuth, email/SMS and AI services separately from the owned application.

Return typed outcomes: allowed, missing_scope_information, missing_test_account, capability_unavailable, stale_observation, budget_exhausted, out_of_scope and model_refusal. Report the specific remedy and continue independent in-scope scenarios instead of refusing the entire assessment.

### 2. Route/API/workflow inventory

Merge route templates, API specifications, source handlers, visible navigation and observed browser traffic. Keep source revision and deployed-build provenance separate. A route template is not a completed workflow, and HTTP GET does not establish absence of side effects (auth callbacks are an example).

Track each role and workflow through discovered → planned → exercised → passed/failed/inconclusive/blocked, with evidence references. Resolve parameterized routes only using owned fixtures. Include pagination, filters, empty states, refresh/retry, permissions and asynchronous completion.

Initial O-SOUL families: registration/session/recovery; upload/generation/editor/product placement; private designs; catalogue/cart/checkout/order; payment modes/webhook reconciliation; contest submission/immutable snapshot/voting/likes/comments/follows; notifications; admin moderation/configuration/inventory/shipping/production/reports. Each needs role-specific acceptance; discovering 56 pages does not imply every state has been covered.

### 3. Browser/API/security/load jobs under one scheduler

Run separate job pools for browser workflows, API checks, security validation and load generation. Use a common run ID and timestamps to correlate browser step, HTTP request ID, application logs and available DB/queue/cache metrics.

Support scripts and browser automation through reviewed adapters. Keep aggregate target budgets across all pools so several individually bounded jobs cannot multiply load unexpectedly. A generic script can open arbitrary connections: URL allowlists in prompt text or executable names alone are insufficient enforcement. Prefer a network-restricted worker/container or controlled request broker, with documented browser/OS limitations.

Avoid running every security case during the first load ramp: establish reproducible baselines, then deliberately overlap selected flows to test races. Separate test data per worker except when the scenario intentionally shares an owned resource (for example last-item stock or duplicate checkout). Never automatically replay uncertain mutations.

Proposed stop signals: owner-defined error/latency ceilings, resource saturation, dependency failures and explicit cancellation. Cancellation must stop new dispatch, settle/cancel owned in-flight jobs, retain uncertain effects and preserve evidence.

### 4. Evidence-backed security findings

Use controlled accounts A/B and a least-privilege/privileged test-role matrix for object/role authorization checks. Test sessions, CSRF/CORS, input validation, upload/path handling, injection/XSS hypotheses, sensitive-data handling, idempotency and concurrency with bounded, non-destructive cases on appropriate targets.

Keep suspected, configuration-observed and exploit-reproduced findings separate. Record route/build/role/fixture, expected versus actual behavior, minimal reproduction, redacted request/response evidence, impact/preconditions, confidence, severity rationale, remediation and retest result. Do not infer exploitability from missing headers or infer security from a 200 response.

Export readable Markdown/HTML plus machine-readable findings and coverage. Preserve the full artifact separately and send small evidence references into context, following the useful REA pattern. No claim of complete coverage where scenarios, roles or dependencies remain unknown.

### 5. Load workload with explicit environment authority

Start with smoke, then a capped ramp, then sustained/combined scenarios only within the owner's test window. Select tool adapters after inspecting available tooling; k6/Playwright or the existing O-SOUL performance runner are candidates, not installed Harness capabilities.

The existing O-SOUL mixed production script includes writes, likes, comments, votes and contest submissions and creates Kubernetes jobs. It must not be run merely because it already exists. Production load settings and paid/external operations remain dependent on the owner's scope answer.

Track per-endpoint p50/p95/p99, request rate, timeouts, 4xx/5xx, expected policy rejections, failed business assertions, queue wait/processing, DB pool/locks, Redis, resource consumption and load-generator saturation where observable. Distinguish edge latency from direct-origin capacity; do not silently bypass rate limits or emulate proxy identity to obtain a better number.

## Rollout and acceptance

| Stage | Deliverable | Required observable evidence |
|---|---|---|
| A | Scope model and typed decisions | An approved owned read scenario proceeds; an unrelated host, changed scope, expired grant and third-party page instruction cannot expand it |
| B | Inventory and coverage | Source vs deployed provenance stays separate; parameterized/role flows and unknowns remain visible |
| C | Browser/API runners | At least one complete test-role workflow has correct persisted outcome and correlated evidence; stale observations and uncertain writes are handled |
| D | Security validation | A deliberately vulnerable owned fixture is found and reproduced; patched and secure fixtures are not falsely reported as exploitable |
| E | Concurrent bounded load | Browser/API pools operate together inside an aggregate cap; cancellation, overload stop and owned-fixture accounting work |
| F | O-SOUL pilot | Explicit role/workflow coverage and fresh deployment evidence; no all-flow or zero-vulnerability claim with blocked cases |

For refusal evaluation, use paired legitimate-owned-target and out-of-scope cases with the same qualified Bonsai64k configuration. Measure unnecessary refusals/prompts, actual workflow completion, incorrect allowance, false success, tokens/time and side effects. Do not optimize solely for fewer refusals, and do not mark tool approval as successful execution.

Implementation order: A → B/C → D → E/F, keeping functional Harness changes in separate native-authored commits. O-SOUL source, generated project files, raw responses and private evidence stay outside Harness Git. No commit or push was made for this assessment.

## References

- Local O-SOUL `docs/production-flow-qa-2026-10-27.md`: historical production/load observations; not a current run.
- Local O-SOUL `apps/api/internal/transport/fiber/routes.go`, `middleware/security.go`, `middleware/ratelimit.go`, `handlers/token_auth.go`: current dirty working-tree behavior; not a deployment attestation.
- [OWASP Web Security Testing Guide](https://owasp.org/www-project-web-security-testing-guide/): test planning and risk-oriented coverage.
- [MDN CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/CORS): preflight, credentials and browser behavior.
- [Grafana k6 test types](https://grafana.com/docs/k6/latest/testing-guides/test-types/): smoke, load, stress, soak and other distinct workloads.

## Next action

Environment and initial read-load budget are resolved; the authorized one-minute pilot is consumed and its raw evidence retained. Prepare native Bonsai implementation of Stage A against the existing scope/approval lifecycle; ground disposable accounts/fixtures before mutation scenarios. Recheck current source/runtime identity before using any new run. Do not restart consumed marketplace goal identities, change global approval rules, deploy O-SOUL patches or launch its existing production mixed scripts as a side effect of this design.
