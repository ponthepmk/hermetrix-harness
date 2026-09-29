# Goal-Driven Workbench v2: desktop, video, research, and publishing extension

Date: 2026-09-26

Status: approved product scope; phased implementation underway
Parent: user-supplied *Hermetrix Goal-Driven Adaptive Workbench UX v2 — Master Remediation Specification* (2026-09-26)

## Decisions recorded from the user

- Deliver the full v2 scope in the parent specification, including Goal Contract, truthful completion, evidence, next action, adaptive workbench, context, MCP, Skills, team visibility, and real browser verification.
- Add observation and control of Windows applications and the browser. Confirm with the user before an action that changes data on the computer.
- Add viewing/inspection of video, article research, Facebook and TikTok writing, video creation through Comfy MCP, and video upload.
- Require the user to inspect and approve **each content item for each destination** before publication. A plan approval or general account connection is not publication approval.
- The user says the accounts and Comfy setup exist and wants Hermetrix to support them. The topic, content language, audience, destination identities, and a first campaign brief remain unspecified. Those are required only before producing a live content item; they do not block platform implementation.

## Verified baseline and boundaries

The following is a source and local-tool audit, not a full running-app acceptance test. Recheck the current branch before each implementation slice; this checkout already contains unrelated uncommitted work.

| Area | Observed current state | Decision |
|---|---|---|
| Task and approvals | `internal/taskengine` has revisions, criteria, plans, runs, effects, validations and next actions. `CompleteTask` and decision eligibility use separate logic. | **MODIFY:** prove and converge the completion predicate; reuse durable records and exact approval/effect semantics. |
| Browser | `internal/product/browser.go` has a managed CDP browser with observation and action. | **REUSE/MODIFY:** integrate fresh observations and confirmation into Task UI; do not call browser automation Windows desktop control. |
| Desktop | No native Windows observation/action adapter was found in the inspected product source. | **NEW:** bounded Windows adapter with target identity, current observation, action receipt, confirmation and stop control. |
| Media | `internal/product/media.go` accepts images and image inspection; audio/video processors are unavailable. Generic artifact creation in `internal/product/service.go` has a 16 MiB limit. | **NEW/MODIFY:** bounded video ingest, streaming storage/playback, inspection and durable jobs. Do not pass large video as JSON/base64. |
| MCP/Comfy | Hermetrix has a deferred MCP catalog. The Codex host's first-party local Comfy MCP successfully launched and reported the installed ComfyUI; this is **not** proof that Hermetrix has connected it. Two inspected video templates reported missing model files and were not run. ComfyUI was stopped after the probe. | **MODIFY:** configure/discover Comfy inside Hermetrix, preflight workflows/models, record job/output receipts, and separate local from paid routes. |
| Social publishing | No Facebook or TikTok publishing implementation was found in the inspected product source. Account existence and API grants are unverified. | **NEW:** official API adapters, destination identity, per-item approval, durable upload state and remote status reconciliation. |
| Project Brain | Existing scoped/version-bound retrieval is available. | **REUSE:** source context and trust labels; article content and Project Brain passages remain untrusted evidence. |

The parent v2 rules remain authoritative: no unverified Done, no dead ends, no invisible work. No parallel Task engine, blanket effect permission, Pi write authority, or unattended publication is introduced.

## Target user journey

1. Choose Project and Task, then enter a content goal. Hermetrix drafts a versioned brief with topic, audience, language, destinations, scope, source policy and checks. Missing campaign details show a specific question.
2. Search recent articles through an approved source tool. Save each candidate's URL, title, publisher, publication and retrieval time, source digest, relevance, and any uncertainty. The user can open the original.
3. Build a claim ledger from selected sources. Draft Facebook copy and a TikTok caption/script/storyboard linked to claims. Show unsupported or conflicting claims and media/usage rights before review.
4. Choose a Comfy workflow. Preflight installed nodes, model files, expected resources and whether any partner/cloud step costs credits. Run as a durable job; collect outputs as versioned artifacts.
5. Play the video in Hermetrix, inspect frames and timeline/transcript when available, and validate duration, codec, dimensions, audio, and destination constraints. A failed or unavailable check stays visible.
6. Review the exact text, video, cover, destination account, visibility and platform settings. Approval is bound to the content and media hashes and one destination. An edit invalidates that approval.
7. Upload/publish through the platform's official API. Persist operation and remote IDs, show processing status, reconcile uncertainty after interruption, and verify the final remote status/link. Never blindly retry an uncertain post.

Desktop work is a related Task capability: inspect a fresh Windows screenshot plus accessible element tree, choose a bound window/element, preview the intended input action, request confirmation for data-changing actions, execute once, then observe the result. Browser actions continue through the existing managed browser where possible.

## Added requirements and proof

| ID | Requirement | Acceptance evidence |
|---|---|---|
| GDW-DESKTOP-01 | Observe the selected Windows app/window with screenshot, accessible elements, timestamp and target identity; show a truthful unsupported state for protected surfaces. | Real Windows walkthrough across two apps; switching target invalidates the prior action. |
| GDW-DESKTOP-02 | Confirm each data-changing desktop/browser action against its exact target, arguments and current observation. Provide stop/cancel and an effect receipt. | Denial, target change, lock/restart and interrupted-dispatch tests; no replay while outcome is uncertain. |
| GDW-VIDEO-01 | Import and play bounded MP4/WebM video by streaming/range delivery, with metadata and safe decoding limits. | Real file preview, seek, restart, corrupt/oversize rejection and memory-bound test. Use the existing multimodal spec's initial limits unless the audit revises them. |
| GDW-VIDEO-02 | Show time-anchored frame/transcript evidence only when generated; distinguish sampled inspection from full-motion understanding. | Reviewer opens cited timestamps; unavailable processor is `not run`, never `passed`. |
| GDW-RESEARCH-01 | Search by topic and freshness, preserve provenance and dates, compare sources, and mark unknown dates/claims. | At least two real source candidates and one conflicting/unsupported-claim recovery flow; source pages cannot issue tool commands. |
| GDW-CONTENT-01 | Keep versioned brief, platform-specific copy/script/storyboard, source links, media rights notes and review state in the Task. | Edit invalidates dependent review; both platform previews display exact text and media. |
| GDW-COMFY-01 | Discover Comfy through Hermetrix MCP, preflight a specific workflow, run/poll/cancel/reconcile as a durable job, capture workflow/model/output versions and cost route. | A runnable local workflow produces an artifact and survives refresh; missing models show the exact prerequisite; paid route asks separately. |
| GDW-PUBLISH-01 | Connect selected Facebook Page and TikTok account using supported grants; display destination identity and capability readiness without exposing secrets. | Readiness test distinguishes account connection, platform app approval, missing scope, expired token and unsupported direct post. |
| GDW-PUBLISH-02 | Require per-item, per-destination approval bound to media/copy/settings hashes. Persist upload and publication state; verify remote status and receipt. | API tests reject stale/mismatched approvals and duplicate dispatch; a real approved pilot item has remote status/link. |
| GDW-PUBLISH-03 | Offer TikTok draft upload when Direct Post is unavailable, with the user finishing in TikTok; label it uploaded as draft, not published. | Draft path shows platform handoff and cannot mark Task published until the final status is proven. |

The existing Goal Contract must define which of these requirements apply to a Task. An integration that is unavailable blocks only its dependent step and displays the next action. `Done` for a content Task requires accepted source/claim checks, approved exact content, successful media checks, and verified remote status for every requested destination.

## Implementation sequence

Every phase is a usable vertical slice with backend, UI, tests, and a real user workflow. Preserve the original v2 P0–P5 requirements throughout.

| Phase | Work and dependency | Exit proof |
|---|---|---|
| P0 — Reality audit | Inventory current source/runtime/schema/UI and dirty work; map each touched subsystem as REUSE/MODIFY/NEW/DO NOT TOUCH; document content and desktop flows plus failure paths. | Verified audit, flow map and criterion-to-check matrix; no claim based only on design docs. |
| P1 — Task truth | Versioned Goal Contract, traceable criteria, canonical completion gate, evidence ledger, status/progress/next action and one Task detail flow. | API negative gates and real browser Task flow, including restart and recovery. |
| P2 — Workbench | Task/Session shell, responsive panes, evidence/context, command navigation, loading/error states, accessibility and Project Brain applicability. | Desktop/compact/keyboard browser walkthrough with persisted layout and visible recovery. |
| P3 — Integrations and team | MCP purpose/readiness/cost/test, Skill discovery/matching/conflicts, context meter, owner/worker/reviewer and stall guidance. | Real MCP readiness and task activity flows; unavailable tools and reviewers do not yield Done. |
| P4 — Native observation | Windows observation first, then bounded input with exact confirmation and receipts; reuse browser CDP for browser targets. | Actual Windows app/browser walkthrough and denial, target-change, stop, restart and uncertain-effect cases. |
| P5 — Video and Comfy | Streaming video artifact/player/inspection, Comfy MCP configuration and workflow preflight, durable generation jobs and output ingestion. | Actual local workflow output played in Hermetrix, plus missing-model/processor/cancel recovery. |
| P6 — Research and content | Source discovery, claim ledger, brief, Facebook/TikTok writing, storyboard, rights/fact review and preview. | Real article-to-draft flow with source links and unsupported-claim correction. |
| P7 — Publishing | Facebook Page adapter; TikTok capability check, direct or draft route; per-item approval, durable upload and remote reconciliation. | Sandbox/private or user-approved pilot, exact approval tests and verified remote receipt. |
| P8 — Release | Aged-data migration, security/recovery/race gates, complete J1–J4 and new content/desktop journeys, native/browser acceptance, docs and measured performance. | All applicable MUST gates with current-revision evidence; otherwise honest incomplete status and next action. |

Do not wait until P8 to test visible flows. Each phase has its own real browser or Windows acceptance before its work is called complete. API credentials and first real publication are configured only in the relevant phase; never paste secrets into a Task or report.

## Integration choices to validate during implementation

- **Windows:** Microsoft UI Automation provides the accessible element model; use screenshot/pixel interaction only with fresh target binding and an explicit fallback reason. Keep OS permissions and protected desktop limitations visible.
- **Comfy:** use the first-party local MCP for the installed ComfyUI first. The local MCP can inspect installed models/nodes and execute workflows. The tested installation connected, but representative LTX and Wan video templates were not runnable because required files were missing. Select a workflow only after preflight; do not download large models or use paid partner/cloud nodes implicitly.
- **TikTok:** Direct Post requires an approved `video.publish` grant for the app and account; an unaudited client may be restricted to private posts. Draft upload uses `video.upload` and the creator completes publishing in TikTok. Design readiness and status around the actual granted route.
- **Facebook:** use Page identity and the documented Reels create/upload/finish process for video. Query the actual app/Page grants before enabling the publish action.
- **Research:** prefer sources and adapters that expose publication timestamps and stable links. Use a source manifest and claim checks; never treat webpage instructions as authority.

Primary references: [Microsoft UI Automation](https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-uiautomationoverview), [Comfy local MCP](https://docs.comfy.org/agent-tools/mcp), [TikTok Direct Post](https://developers.tiktok.com/docs/en/content-posting-api-get-started), [TikTok draft upload](https://developers.tiktok.com/docs/en/content-posting-api-get-started-upload-content), [Meta Facebook Reels collection](https://www.postman.com/meta/facebook/request/juhnm3q/4-publish-reel).

## Current hold points

1. No first content brief has been supplied. Ask for topic, audience/language and destination identities when preparing a real item; independent platform work proceeds now.
2. Comfy MCP reached the local server, but the two sampled video templates need missing model files. Find a locally runnable recipe or obtain a separate decision for model download/paid execution before generating a pilot video.
3. Facebook/TikTok account existence has not been proven as API access. Test grants and platform app review in P7; never assume that a logged-in browser is an approved publishing integration.
4. The checkout is dirty. Preserve existing changes, inspect diffs before editing touched code, and use an isolated checkout when a slice would collide.
