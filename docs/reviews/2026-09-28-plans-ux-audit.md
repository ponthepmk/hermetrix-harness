# Plans UX and CMS task flow audit — 2026-09-28

## Scope and observed problem

The Plans screen used a task dropdown and a Kanban board for the same selection. On the CMS Pilot task, the immediate action was below goal text, plan history, and 32 completion gates. A project with an existing task could not open a blank task form from the overview because the renderer selected the first old task when `selectedDurableTask` was null.

## Change

- The overview groups tasks into needs attention, in progress, and completed. Each card shows its current phase, completed steps, and next step.
- Task detail places the actionable control first, followed by phase, goal, plan, and result/evidence. The duplicate task dropdown and board are removed from this flow.
- Approved plan steps and the full completion gate list are collapsed; progress counts remain visible.
- A failed plan step names the failed step and labels the next action as a fresh repair attempt. The raw engine pause reason remains available under technical details.
- A dedicated new-task selection opens a blank form even when the project has earlier tasks.

## Verification

- Built `cmd/hermetrix` and ran an isolated staging server at `127.0.0.1:18745` against a SQLite backup of the CMS test data. The user's Preview data was not edited by this UI test.
- In the browser, the CMS Pilot overview displayed a blocked step and `4/9`; its detail displayed “เริ่มรอบแก้ไขขั้นนี้” before the status, goal, plan, and evidence. The 32 gate rows were collapsed.
- At a 390×844 viewport, document scroll width was 390 px. The action and controls remained in a single column; the browser viewport was reset afterward.
- Clicked “เริ่มงานใหม่ใน CMS Pilot”, entered an objective and acceptance criterion, submitted, then confirmed a new task detail with “ไปสร้างแผน” and an overview containing both tasks. This was a staging database task, not a production task.
- `node --test internal/web/ui/tasks.test.js`: 31/31 passed. Targeted task API, cockpit contract, plan review, and primary request browser Go tests passed. `go test ./internal/taskcoord ./internal/product` passed.

## Remaining failures and remedy

- CMS Pilot is still `4/9`. Step 5 (“ลบบทความหลังบ้านและเก็บสถานะ UI”) has a failed provider proposal with an observed receipt. Restart it as a fresh bounded attempt and review the proposed change before apply; do not replay the recorded effect.
- An empty project has no eligible source manifest. `taskcoord.SelectFiles` rejects it and the current worker/result/apply/rollback contracts assume every changed file already exists. New-file support needs an explicit absent-file preimage, restricted path selection, reviewed write intent, and safe rollback on failed verification. Simply bypassing the manifest check would leave unsafe or misleading completion state.
- The full `go test ./internal/web` suite was not green: unrelated browser workflow and UI contract tests failed in this mixed working tree. The targeted Plans/task tests above passed. Do not treat the CMS workflow or the full suite as complete.

Pi Project Brain raw draft: `raw/2026-09-28-plans-ux-and-cms-execution-audit-2026-09-28.md` (captured via MCP; draft, not indexed).
