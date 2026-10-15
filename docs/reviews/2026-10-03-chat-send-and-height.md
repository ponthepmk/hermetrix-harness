# Chat send and full-height correction — 2026-10-03

The user's tall mobile screenshots showed a composer ending above a large blank
area, and tapping Send left the draft without a submitted message. This was
reproduced with actual browser clicks before editing.

## Causes and correction

1. Compact layout hid the pane header, but a more specific CSS rule still
   allocated two grid rows. At 390×1200 the chat body ended at 922.44 px while
   its pane ended at 1200 px; computed rows were 623.66 and 277.56 px. The compact
   grid now explicitly uses one row, including the existing IDE override.
2. The mobile compose layout depended on textarea focus. Pointer down could
   blur it, restore the header and move Send before pointer up/click. Compose
   mode is now held by the assistant section until Done, Escape or submission
   completion. Moving focus to Send no longer changes the layout.
3. The floating request-status notice could cover Send. A pointer hit-test
   returned its dismiss button instead of Send. Notice text now allows pointer
   events through, and mobile positioning reserves the right edge for Send;
   its dismiss control remains interactive.

The submit button shows preparing/responding states and `aria-busy`, and the
assistant status remains visible during composition. Existing failure guidance
and draft retention remain in use. Changed embedded assets have a new cache
version. The audited session/turn/approval pipeline is unchanged.

## Evidence

- A new real Chrome regression first failed on pointer-down movement. It uses
  native mouse down/up and touch start/end, rather than DOM `click()`, then
  verifies the submitted message, streamed response, exact request count and
  cleared draft. It covers 390×1200, 390×550, 414×844, 768×900 and 1440×900,
  full compact height, model failure/draft retention, and the sibling Chat
  composer. Resize assertions wait for rendered frames, avoiding interaction
  with a previous breakpoint's controls.
- Full required-Chrome Go suite: 35 packages passed, 1,060 passing test/subtest
  events, zero failures; external/environment-dependent skips remain as in the
  preceding workspace-tab review. Node135/135, vet and build passed. After the
  final loading-label ordering and test resize correction, the pointer/touch,
  GoalComposer and mobile workspace browser tests passed together (25.520 s)
  and Node135/135 was repeated successfully.
- Manual preview clicks submitted a real turn. Initial preview failures came
  from missing referenced blobs in the database-only copy; the blob tree was
  restored at its correct path. A subsequent turn reached Bonsai8088 and
  recorded its actual connection refusal, displayed persistent Thai recovery
  guidance, and retained the text. This is an offline-model recovery proof,
  not a successful Bonsai response.
- Main **18801** was tested with the existing explicitly chosen Qwen profile
  in an empty synthetic project, without opening or attaching any file. The
  egress dialog recorded the test purpose. A real Send click in Workspace
  returned **WORKSPACE-SEND-OK** (3,520 tokens); the sibling Chat button returned
  **CHAT-SEND-OK** (3,458 tokens). The persisted session has four message events
  and **zero tool calls**. This is live send/response evidence, not coding or
  Task-completion evidence.
- Main at390×844: chat bottom and pane bottom both844 px, document/viewport
  both390 px, successful-send draft empty, captured console warnings/errors
  empty. Screenshots retained for both main composers and the tall preview.

## Rollout and preservation

Main now uses the tested final executable, **PID81924**, schema/expected schema74;
launcher `-CheckOnly` passed. Startup log:
`.tmp-ui/easiiot-18801-639265983342493480.error.log`.

The automatic approval review rejected a batch that stopped a preview and
copied model credentials, reporting only `blocked by policy`. No step of that
batch executed. The credential-copy action was not repeated. A separate preview
was created without credentials; main was subsequently updated by an approved
owner-checked executable replacement using its original credentials in place.

Before cutover there were no leased turns, queued/running jobs, pending approvals
or unsettled effects. Immediately afterward all nine compared tables and the
secrets hash matched the consistent backup; SQLite integrity was `ok`. The
live proof then intentionally added one empty test project and one test session
(`session_568c6e5dcaad9f3e743c26c9a973243e`); model usage calibration changed through
normal operation. All37 original session rows remained unchanged, and Task,
step, acceptance, effect, skill-version, tool-approval and job tables still
matched their backups. The vault hash remained equal. No original Task was
accepted or replayed.

Receipts/backups: `.tmp-ui/send-fix-20261003/`, including `reproduce.txt`,
`pointer-hit.txt`, `core-tests.jsonl`, `browser-final.txt`, `node-tests-final.txt`,
`vet.txt`, `main-cutover.db`, `main-cutover.exe`, `after-preservation.json`,
`main-proof-preservation.json`, `main-reply-proof.json`,
`main-workspace-reply.png`, `main-chat-reply.png`, `preview-full-height.png`.
Preserve failed preview receipts as diagnosis evidence.

Owned previews18808/18809 were stopped and their tabs closed. Main18801 and
existing18804/18806 remain running. User tabs/drafts were not reloaded or
submitted. Main deliverable tab19 was kept and viewport overrides reset.
Existing tabs must load the new assets before they receive the fix; copy an
unsent draft before manually refreshing. Bonsai8088 remains offline. The
self-improvement lab remains paused, and the separate main repair plan is
otherwise pending. No public deploy or commit was made.
