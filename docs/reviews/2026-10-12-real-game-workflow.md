# Real game workflow — preparation and keyboard repair

## Requested result and current outcome

The user asks Codex to design/direct a genuinely playable story game with a real 3D character, have Hermetrix implement it through its own tools, inspect the screen, and repair harness failures until the full workflow succeeds. This request is **not complete**. Hermetrix completed a real planning turn and wrote package.json through workspace.write_file; no server or playable game exists yet.

The proposed game is **ผู้พิทักษ์แสงสุดท้าย / Last Light Keeper**: a third-person island adventure with an articulated humanoid, an engineer NPC, three recoverable storm cores, sentinel combat, health/death/restart, checkpoints/save, pause and a lighthouse/rescue ending. Designer-authored specification is `D:/Projects/hermetrix-last-light/GAME_BRIEF.md`; the project also contains Hermetrix-authored package.json (282 bytes, SHA256 1e771bc6f32bed4257d7d67dca2f2af1734e0483a56d35201f3d9b77df1104a7). Project `project_ac381315d2fb5bee469d0cde21a1607f` binds that isolated root. Three.js0.186.0 is available from npm; no Unreal installation was found in the standard checked locations. No game source was secretly authored by Codex.

## Grounding and prerequisite

On2026-10-12 the local18810 harness, original Bonsai8088 and TLS gateway18811 processes were stopped, while Tailscale's old18810 forwarding route still existed. The latest2026-10-11 startup log confirmed the exact previous executable/data/workspace/reviewer. The harness was restored with that same data and reviewer, without replaying user actions, accepting Tasks, restoring an unrelated instance, coupling to Project Brain, or resuming the paused self-improvement lab.

The initial Bonsai start rejection was resolved by the user's explicit "เปิด bonsai ได้" authorization. Bonsai8088 started successfully (PID3436) and the real plan completed in session session_5398ea21f9939aa71d8b4a84552ebf9f using provider provider_83e80036904bd79a2caae0a8e9e99653, local_only, browser v6 and an explicit qualification exception (not certification).

The foundation stream was interrupted after the package write receipt; the continuation failed with `read provider stream: context canceled`. The foreground director process no longer existed. HTTP runTurn and decideApproval intentionally pass r.Context() to the agent; losing the client cancels inference. The next server turn failed before inference with `cancelled_runtime_idle_unconfirmed`. A separately launched director prevents future foreground-client interruptions but does not clear quarantine. Read-only /slots confirms slot0 is_processing=false. The existing confirm-idle API verifies all slots before releasing quarantine; the attempted confirm-idle plus continuation command was rejected by automatic approval review with generic `blocked by policy`. No alternate route, retry, direct scheduler mutation, restart or UI workaround has bypassed that rejection. Specific recovery confirmation is pending. A prior attempted model restart to correct CORS was also rejected; model remains running. Never claim the game is ready or call canceled streams completed. Director reports now isolate current-stage events, avoiding stale planning answers on failed construction stages.

Bonsai currently has no verified image capability. Screenshot capture and DOM/accessibility observation must not be described as model pixel vision. For actual vision, select and genuinely verify an appropriate model; do not invent capability flags or transmit other/private windows. The game brief explicitly distinguishes these paths.

## Reproduced and fixed browser-tool gap

Product supported a few immediate keyboard taps, but the agent's frozen browser tool did not expose `key`; held movement was absent. The real Chrome regression reproduced that `duration_ms` was ignored and a changed duration was accepted after approval (`keyboard-before.txt`).

Browser v6 now exposes bounded keyboard taps/holds using the existing tool, preserving the global fourteen-tool waist and calibrated compact16k schema budget. Supported keys are w/a/s/d/e/q, arrows, Space, Shift, Enter, Escape and Tab; no modifier shortcuts or arbitrary text. A fresh element ref and observation hash, exact approval, scoped project/tab and one-use native receipt are still required. The duration0..3000ms is included in the existing exact text digest without a migration; legacy tap/text hashes remain compatible. Legacy browser bindings cannot acquire key authority. Focusable canvas/application elements appear in observed refs, and key focus must reach the exact reviewed element before input.

A held key dispatches one keyDown, releases on normal completion and attempts neutral release with a separate bounded cleanup context on cancellation. It never replays keyDown. Cancellation retains uncertain-effect evidence and refuses subsequent input until reconciliation. Existing Stop/process/approval boundaries were not weakened.

The command runner's Windows Job Object deliberately kills descendants when a command exits. A detached Node child cannot be used as a fake durable preview. The brief directs Hermetrix to test its authored server in an owned bounded command, then have the director make an explicit serving handoff after acceptance, preserving the Job Object safety guarantees.

## Validation and serving state

- Real Chrome held key moves the fixture, releases exactly once, rejects changed duration and replay.
- Real Chrome cancellation releases w, leaves the receipt uncertain, and blocks automatic subsequent input.
- Agent bridge passes duration to preparation/execution, rejects legacy binding/unapproved dispatch, and preserves frozen approval.
- First validation found a schema-budget increase; description was compressed rather than increasing the budget or tool count. A whole-suite exact-payload check then exposed JSON escaping in the `<` description; wording was corrected, leaving the check intact. Failed reproductions remain saved.
- Final `go test -p 1 ./... -count=1` passed with real Chrome/network E2E enabled, including actual Windows mouse input. Node141/141, vet and build passed. Changed-file diff checks passed; whole-worktree diff check retains unrelated pre-existing whitespace reports in the generated formatter bundle, which was not modified.

Active local harness: `http://127.0.0.1:18810/`, PID18980, `.tmp-ui/hermetrix-game-flow-20261012.exe`, schema76, same `.tmp-ui/chat-controls-release-data`, `.tmp-ui/chat-controls-fixture` and reviewer `provider_715d3d90f2972caf9e4385ad7275663c`. No new migration. All nine protected table hashes were identical across replacement; SQLite integrity ok, zero active turn leases. No credentials were copied. The separate main18804 and gateway/Tailscale configuration were not changed.

Evidence: `.tmp-ui/game-flow-20261012/keyboard-before.txt`, `keyboard-confirm.txt`, `key-focus-confirm.txt`, `payload-confirm.txt`, `core-final.txt`, `ui-tests.txt`, `vet-final.txt`, `before-runtime.db`, `before-runtime.json`, `after-runtime.json`. Earlier executable is retained as rollback.

## Resume

1. Resolve the pending specific recovery confirmation; do not bypass the rejected confirm-idle/continuation action. Bonsai has already been explicitly authorized and started; do not ask again to start it.
2. Preserve the existing game session and package write receipt. After authorized recovery, confirm all model slots idle using the existing scoped API; do not replace the quarantine safety check with a forced reset.
3. Ask Hermetrix to read the brief and plan the module contracts, then implement in bounded complete-file stages through real write/run tools. Approve only exact writes/commands within the game root and intentional gameplay actions; no existing Task effects or Brain changes.
4. Observe each receipt, reproduce any actual failure, fix the causal harness issue and repeat that exact path. Do not replace game authoring with Codex source generation.
5. Play Start→dialogue→movement/combat→three cores→lighthouse→ending through real inputs, verify death/restart/save/pause/mobile and 3D rendering, make a persistent serving handoff, and save screenshots plus model-authorship receipts before declaring completion.
