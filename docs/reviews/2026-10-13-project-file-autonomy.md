# Project file autonomy — 2026-10-13

The user wants Hermetrix to carry out authorized work without repeated refusals. The optional scope question had no reply during implementation; the stated recommended scope was ordinary work in the selected project. This delivery covers **file creation and edits**, with explicit session consent. It does not promise that model responses, other effectful tools or provider errors can never refuse/fail, and does not override Codex execution-policy denials.

## Behavior

Workspace chat now offers **สร้างและแก้ไฟล์อัตโนมัติในโปรเจกต์นี้**. Selecting it and sending starts a new session that freezes the project, canonical folder, authenticated owner, reason and permission revision. Existing sessions keep their original contract. Turning it off starts a new manual session on the next send. Personal and audit sessions cannot obtain this permission.

The ordinary model/tool loop executes sequential file writes without a human pause. Each file still receives a persisted exact approval, decision and terminal receipt using the existing durable approval machinery. Hash/absent checks, root containment, symlink checks, Stop, replay prevention and uncertain-write recovery remain enforced. Project-folder changes invalidate the authorization. Other browser, desktop and MCP grants remain independent. No new database migration or expanded model tool schema was needed.

The model receives a runtime fragment explaining that authorized file work should proceed directly, use actual tools and receipts, and reread conflicts. The UI identifies automatic actions as using this chat's file permission, rather than misleadingly showing a new manual approval.

## Verification

- Agent integration used real temporary project files with deterministic HTTP model output: two writes in one turn, no approval pause, both files present, two exact executed approval records, receipt IDs and recorded owner consent. Reapproving either recorded effect fails.
- Changed project root, existing-file hash conflict, traversal and cancellation before dispatch produce failed receipts without writing unwanted files or replacing the owner's existing contents.
- Actual Chrome UI exercised checkbox → new consent session → two real files → zero pending approvals → checkbox off → new manual session. Desktop1440 and portrait390 controls fit. Captured images were visually inspected.
- Node143/143 passed; targeted agent checks, targeted browser checks, vet and build passed.
- Full `go test -p 1 ./... -count=1` ran with required real browser E2E. It had **one failure** in existing native desktop observation freshness: `TestNativeDesktopActionUsesExactApprovalAndDoesNotReplay` reported a changed observation at preparation. Isolated recheck passed, including native pointer, screenshots and replay rejection. No freshness guard or test assertion was weakened. The broad run is not labeled green.

Evidence: `.tmp-ui/project-writes-20261013/` contains browser-final.txt, project-writes-390.png, project-writes-1440.png, go-all.txt, native-confirm.txt, ui-final.txt, vet.txt and before-deploy.db.

## Runtime and original game status

Read-only discovery on2026-10-13 found the prior Hermetrix PID18980 and Bonsai8088 no longer running; only Tailscale's existing18810 forwarding listener remained. The new file-permission binary was started normally for UI deployment at `http://127.0.0.1:18810/`: PID22292, `.tmp-ui/hermetrix-project-writes-20261013.exe`, same `.tmp-ui/chat-controls-release-data`, `.tmp-ui/chat-controls-fixture` and reviewer provider_715d3d90f2972caf9e4385ad7275663c. Loopback HTTP200 and served new permission control confirmed. Schema76, integrity ok, all nine protected-table hashes identical before/after deployment. The app open request was queued in Codex, not confirmed visually active there.

No confirm-idle action, forced scheduler release, model restart, original failed turn replay, remote gateway change, Task acceptance or Project Brain action was performed. Bonsai remains offline. The previous generic `blocked by policy` response establishes an execution-layer denial; the exact policy class/reviewer and which part of the combined recovery command was denied remain unknown. Do not assert that repeating a chat approval alone will resolve it.

Last Light Keeper remains **unfinished**: designer brief and actual Hermetrix-authored package.json only. This permission improvement is not evidence that the game is playable. Original model-authoring/gameplay acceptance still must be completed through Hermetrix after a legitimate resolution of the recovery and runtime conditions.
