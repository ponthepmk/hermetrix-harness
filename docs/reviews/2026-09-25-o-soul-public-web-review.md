# O-SOUL public website review — 2026-09-25

Target: https://o-soul.go2gether-tech.online/

Scope: anonymous, read-only inspection of routes linked by the site, public JavaScript bundles loaded by the site, one HEAD request for response headers, and two direct API GET checks. No account credentials, form submissions, purchases, uploads, fuzzing, or permission bypass attempts were used. A name in the client bundle is a candidate endpoint, not evidence that it is reachable without authentication.

## Confirmed observations

| Surface | Anonymous result |
| --- | --- |
| `/`, `/shop`, `/contest`, `/login` | Pages rendered. Shop products and Contest entries appeared after client loading completed. |
| `/create`, `/create/upload`, `/studio`, `/my`, `/cart`, `/orders` | Client redirected to `/login?next=<original path>` after checking account state. This shows a UI gate; server-side authorization was not tested. |
| `GET https://api-o-soul.go2gether-tech.online/contest/rounds` | HTTP 200. Response had one round with `phase: "open"`, `acceptingEntries: true`, and an end date of 2026-09-30. The settled Contest page displayed an open round, consistent with the API. |
| `GET https://api-o-soul.go2gether-tech.online/admin/health` | HTTP 401 with `{"code":"UNAUTHORIZED","error":"unauthorized"}`. This one admin endpoint denied an anonymous request. |

The Contest page briefly displayed a waiting state during hydration. After it settled, the page displayed the open round and four entries. The brief initial state is not counted as a defect.

## Findings to address

1. **Login inputs lack accessible names (confirmed).** At `/login`, the email and password inputs have placeholders but no associated `<label>`, `aria-label`, `aria-labelledby`, or `id`. The browser accessibility tree exposes both as unnamed text fields. Add visible labels tied to stable input IDs; keep placeholders as hints. This affects screen-reader and voice-control use.
2. **Contest year in the title may be stale (content check).** The displayed round name is “O-SOUL Pixel Master 2025 — FREEDOM”, while the public API reports its start and end in 2026 and the page says it closes 30 September 2026. Confirm whether “2025” is an intentional series name; otherwise update the round title/copy.
3. **CSP is report-only (hardening observation).** The web response sends `content-security-policy-report-only`, with `script-src 'self' 'unsafe-inline'`. This policy reports violations but does not block them. XSS was not tested, so this is not a demonstrated exploit. After evaluating violations and app compatibility, deploy an enforced CSP and reduce inline-script allowance.

## API and source inventory

The public client bundle at `/_next/static/chunks/6994-a0e4c049dd413160.js` names the API origin `https://api-o-soul.go2gether-tech.online` and paths in the auth, contest, cart, account, design, community, and admin groups. Examples include `/auth/login`, `/auth/me`, `/contest/rounds`, `/contest/entries`, `/cart/items`, `/my/projects`, and `/admin/health`. The login page also links to `/auth/line/start`. The bundle contains a separate same-origin `POST /api/pixel-lab/cartoon` call. Only the two direct API GET routes in the table above were requested; the other API paths were found in source or links and were not tested.

The site header includes HSTS, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and a strict-origin referrer policy. These are configuration observations, not a full security assessment.

## Reproduction

- Open `/login` without an account. Inspect its two inputs and their accessibility names; both are empty.
- Open `/contest`, wait for client loading, and compare its round title/end date with anonymous `GET /contest/rounds`.
- Read the web origin's response headers for the report-only CSP.
- Request `GET /admin/health` without credentials to reproduce the 401 for this specific endpoint.

Further authorization, business-logic, and API mutation testing requires a test account and a defined test environment/scope. This review does not infer access to backend source code from client JavaScript.
