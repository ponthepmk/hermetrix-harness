# Workspace tabs — actual local rollout, 2026-10-03

The mobile pane dropdown has been replaced with a persistent strip of opened
workspace tabs. The strip scrolls horizontally; the add and active-tab close
buttons stay outside the scrolling area. Add opens a chooser for Chat, Tasks,
Code, Review, Files, Git, Tooling, Terminal, Browser, Documents, Team, Output,
Debug and Notes. Choosing an existing module activates it rather than creating
a duplicate. Notes can be opened independently. Code becomes available after
opening a file. Existing IDE/sidebar and wide resizable-pane behavior remains.

The compact pane header is removed to avoid repeating the selected tab. Controls
have at least 44 px targets. Arrow keys, Home and End move selection and focus;
the selected tab is revealed inside the strip. Escape closes the add chooser.
Selection is saved with the layout. Existing draft capture and independent
note storage preserve content when switching tabs. Opening a module retains
its existing API, loading, error and approval behavior.

## Verification

- Required-Chrome `go test -json ./...`: exit 0, 35 packages passed,
  1,059 passing test/subtest events, zero failures. Thirteen environment-dependent
  tests skipped; seven packages contain no tests. Skips include external model,
  Comfy, live voice and opt-in browser integration checks; they are not live
  integration claims.
- Node UI tests: 135/135 passed, zero skipped. `go vet ./...` and build passed.
- Browser tests exercise add/activate/reopen/close, separate notes, unsent
  assistant draft, selected-tab persistence on reload, keyboard navigation,
  pointer-reachable menus and Code format/save/run/test/debug workflows.
  Widths 375, 390, 414, 768, 1024 and 1440 were checked for horizontal page overflow
  and reachable tab controls.
- Manual in-app browser checks on preview 18808 and main 18801 confirmed actual
  clicks and keyboard selection. At 390 px the tab strip had 511 px content
  inside 262 px, while the document stayed exactly 390 px. Main 1440 px layout
  stayed 1440 px and a selected off-screen pane was revealed after its scroll
  animation completed. Captured main console errors/warnings were empty.
- An obsolete dropdown test was updated to the chooser flow. An IDE test was
  reading a previous test receipt because its timing contained `10`; it now
  waits for `node ./main.js` and exact output `10`. Actual formatted-buffer
  saving passed without changing editor logic.

## Runtime and preservation

UI assets are embedded in the executable. A consistent SQLite copy was tested
on an owned preview before main rollout. The copied database migrated from
schema 73 to 74 with integrity `ok`; original Task, step, acceptance, effect,
provider, session, skill-version, approval and job rows were retained. Opening
Git in the non-Git preview fixture added one failed Git-status command job;
this was a preview UI action, and did not alter an original job or main data.

Main **http://127.0.0.1:18801/** now serves the tested executable, schema and
expected schema 74, PID **153584**. `start-easiiot-harness.ps1 -CheckOnly` passed.
The launcher retains the same data, actual Easiiot workspace and explicit local
reviewer. The first executable copy encountered a Windows release-time file
lock; the old version restarted intact. A second cutover waited for process
exit and handle release, verified the copied executable hash, and succeeded.

Before the successful cutover there were no leased agent turns, queued/running
background jobs, pending approvals or unsettled effects. All nine compared
tables remained byte-for-byte equal after rollout: 6 durable Tasks, 10 steps,
1 acceptance, 46 observed effects, 3 providers, 37 sessions, 2 skill versions,
4 tool approvals and 26 jobs. The secrets-file hash was unchanged; integrity
remained `ok`. Easiiot Task revision 31 remains `verifying`, 16/17 gates,
awaiting user acceptance. No Task acceptance or effect replay occurred.

The owned preview was stopped and its browser tab closed. Main, existing 18804
and 18806 remain running. The user's existing tabs were preserved; a separate
main tab was kept as a deliverable and temporary viewport overrides reset.

Receipts and rollback copies are preserved under
`.tmp-ui/workspace-tabs-20261003/`: `main-cutover.db`, `main-cutover.exe`,
`cutover-before.json`, `after-preservation.json`, `preview-preservation.json`,
`core-tests-final.jsonl`, `node-tests.txt`, `vet.txt`,
`main-task-completion.json`, `main-tabs-mobile.png`, `main-tabs-desktop.png`.
Final startup log: `.tmp-ui/easiiot-18801-639265964889956698.error.log`.

This milestone completes the workspace-tab request. Delivery recovery, durable
Stop/Queue/Steer, live model-driven desktop and real Discord remain governed by
the separate main repair plan. The local reviewer remains offline and the
self-improvement lab remains paused. No public deployment or commit was made.
