# Chat and Workspace usability verification — 2026-10-01

Branch: dev. Changes are uncommitted.

## Changes

- Chat panes default to 720 px, other utility panes to 360 px. Existing user-resized widths remain respected; the pane host supports horizontal scrolling.
- Workspace composer uses two textarea rows. Model selection and quick actions are collapsed. Idle provider notices no longer occupy a permanent row. Desktop toolbar is shorter; mobile keeps a full-width input.
- Review shows the most recent answer associated with a review/check/continuation request. Model replies are displayed as reports, without independently claiming a full audit passed. Context and contract diagnostics are collapsed.
- Markdown tables render as escaped HTML tables with horizontal scrolling. File rows use a smaller, consistent text size.
- Raw request identifiers are behind disclosure controls. Fallback progress reports and common tool names use readable action descriptions. Legacy fallback receipt lines are translated during rendering.
- Sending explicitly follows the newest message. Streaming follows only when the reader remains at the bottom. Re-rendering preserves the reading position in both chat views.
- Failed sends expose retry controls. Retry refreshes the session first: active requests are left running; committed failed work continues from saved evidence; uncommitted requests retain drafts and attachments; an existing completed answer is not resent automatically.
- Context progress uses the estimated single-request prompt. Actual usage across multiple model requests is labelled separately instead of being compared to a single input window.

## Verification

- Node UI tests: 126 passed.
- Real Chromium regressions: long-answer visibility at desktop/mobile/short viewport sizes, reading-position preservation, forced following after submission, shared Workspace composer, and simulated HTTP 503 followed by the visible retry button. The retry fixture verified exactly one committed user message and an empty draft after success.
- Targeted agent regression tests passed for empty-output/evidence-summary handling.
- Opened the actual desktop server at 127.0.0.1:7331 and inspected the original easiiot_project chat and Workspace. At a 1280 × 720 viewport the chat pane measured 720 px wide, transcript approximately 277 px high, composer approximately 173 px high. Two existing report tables rendered as tables.
- Pressed Enter in the actual Workspace composer with the existing 9arm Gateway / qwen3.8-27b-fp8 session. Requested a UI-only response with no tools or edits. Received HERMETRIX-UI-OK; scroll distance from the bottom was less than 2 px.
- Screenshots: /private/tmp/hermetrix-workspace-usability-final.png. Test logs: /private/tmp/hermetrix-usability-ui.log, /private/tmp/hermetrix-usability-final.log, /private/tmp/hermetrix-usability-retry.log.

The live-provider check verifies sending and displaying a reply. It does not prove the project's review is complete or that every provider/network failure will recover. Automatic replay of mutating tools is deliberately avoided.
