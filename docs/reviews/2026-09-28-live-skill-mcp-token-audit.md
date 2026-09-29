# Live Hermetrix Skill, MCP, token, and CMS Task audit

Date: 2026-09-28 (Asia/Bangkok)

This record separates observed Hermetrix actions from model claims and test fixtures. No social content was published and no Project Brain page was written by the live model prompts.

## User Preview: actual Bonsai, Skill, and Pi Project Brain

The development Preview at `http://127.0.0.1:18737/` initially had zero registered Skills and zero MCP servers. The separately installed Hermetrix data had a Project Brain profile, but Preview did not inherit it. A read-only Project Brain connection and `project-brain-evidence` Skill were registered in Preview. The credential was transferred between local vault and local Preview API without printing it.

Session `session_a77542b7027f46bcd801fdf74ee466ea` used the `Bonsai main 64k` provider, the `certified-64k` context profile, and one user prompt. Durable history shows five model steps and six tool calls: `skill_search` once, `skill_view` once, `tool_search` twice, `tool_describe` once, and `tool_call` once. All six succeeded. The final call made a real read-only `get_context` request to Pi Project Brain scoped to `hermetrix-harness`. It returned `reference/bind-a-board-project-before-adding-hermetrix-planning-tasks-2223d9b30c12.md` at `sha256:7f18e3102f70215acca3ef3238014e4970fa4f66cea6d4721b146c099a43f35b`. The model cited the path and version in its answer. It did not call a file write or Project Brain write action.

The inference ledger records 25,041 input plus 1,437 output tokens, 26,478 total for that turn. The largest individual request had 7,098 input tokens. The latest context compiler snapshot had capacity 65,536, predicted input 8,029, zero spilled or dropped tokens, and 45,219 free tokens after reserve. The Chat bar's 26,478/393,216 value is cumulative use over the turn's possible model steps, **not** a single 64k context window. The visible Preview Chat showed this session, its tool calls, token bar, and 5/12 model steps. This proves discovery, invocation, result handling, and token accounting for this prompt; it does not prove that all future tasks will stay within context or complete within the step budget.

## Isolated CMS prompt and browser actions

On isolated staging port 18739, a CMS audit session `session_9b47f0ba69303aa7463e70ca14790231` used a synthetic local read-only MCP fixture and a test Skill. Those are **not Pi Project Brain**. Its first prompt made 12 model requests and 21 tool calls: workspace reads 9, commands 3, file listings 2, Skill search/view 1 each, tool search/describe/call 1 each, workspace search 1, and browser 1. Twenty receipts succeeded. The browser call failed because Chrome did not become ready; the local CMS `node test.js` command passed API CRUD, draft/published behavior, Thai content, and root-page checks. On the final step, Bonsai emitted literal `<tool_call>` text and the old staging binary incorrectly marked the turn complete. A second prompt asked for a concise summary using existing evidence: one model request and no tools. Its model prose is not independent proof of the synthetic MCP fixture's claims.

The two CMS prompts consumed 97,830 input + 4,314 output, then 12,954 input + 1,520 output tokens: 116,618 in total across 13 model requests. The maximum first-turn input request was 13,347, below 65,536. The second turn's context compiler snapshot showed 65,536 capacity, predicted input 13,971, 4,001 spilled, zero dropped, and 39,277 free tokens after reserve. Spilling shows context management occurred; it does not by itself prove the retained evidence was sufficient for every task.

The browser failure was traced to a relative Chrome profile directory on Windows. `internal/product/browser.go` now resolves that profile path to absolute before launching Chrome. In rebuilt staging on port 18743, direct browser open/read succeeded. A real Bonsai prompt in `session_42e79baa0f45e4a81f06b7cfc55d78c9` then made one successful browser tool call in two model requests and reported the actual local CMS page title, `บทความ - CMS ตัวอย่าง`. The successful prompt used 7,498 input + 164 output tokens. An earlier prompt in that cloned staging failed because the test data clone lacked blobs; the clone was repaired before this successful attempt. This browser fix is present in the rebuilt staging binary; the existing 18737 Preview process has not been restarted with the new binary.

`internal/agent/service.go` now forbids tool-call XML on the last model step and rejects a final answer beginning with `<tool_call>` instead of reporting a false completion. `TestFinalModelStepRejectsPlainTextToolCall` passed. This fix is in source and the rebuilt staging binary, not yet in the running 18737 Preview binary.

## CMS Task outcome and action counts

The isolated CMS sample passed `node test.js` and the earlier real desktop/mobile browser journey recorded in [the CMS user-flow review](2026-09-27-cms-user-flow-e2e.md). The Hermetrix Task itself is **not complete**. Task `task_0e908c5b77221ada45f97c86f94d417a` shows four of nine current steps complete. The delete step failed and four later steps remain pending. Several final sample changes were applied manually after Task proposal failures, so a passing sample is not evidence that the Task engine generated and verified all of them.

Historical Task ledger counts across **all plan revisions** are 99 observed effect intents: 51 `provider.propose`, 19 `workspace.run`, 15 `provider.review`, and 14 `workspace.apply`. There are 33 code proposals: 19 rejected, 12 verified, one applied, and one post-review rejected. The 47 step records across revisions are 12 completed, 31 pending, three failed, and one blocked. These historical totals must not be read as actions in the single live Preview prompt. The normal empty-project Task flow still fails with `project has no eligible source files`. Later delete-step proposals failed the unique-preimage rule or hit output limits. The Task cannot be certified finished until those paths work without manual source seeding or manual final edits.

## Verification and Project Brain record

- `go test ./internal/agent -run 'TestFinalModelStep' -count=1` passed.
- `go test ./internal/product -run 'Test.*Browser' -count=1` passed.
- `go test ./internal/agent ./internal/product -count=1` passed.
- `go build -o .tmp-ui/hermetrix-live-audit-v2.exe ./cmd/hermetrix` passed.
- `git diff --check` passed.
- Actual model-to-browser and model-to-Pi tool calls were observed in durable session histories, not inferred from a model summary.

The issue and evidence were captured in Pi Project Brain as raw draft `raw/2026-09-28-hermetrix-live-skill-mcp-token-and-cms-task-audit-2026-09-28.md`, commit `9d22a04`. It has not been promoted to curated knowledge.

The isolated Pi test server was stopped. Its encrypted test-data clone under `C:\Users\ZP2E0\AppData\Local\Temp\hermetrix-pi-live-e2e-20260928` remains because automatic approval review rejected the recursive cleanup command with `blocked by policy`. No plaintext credential was printed in this report or tool output.
