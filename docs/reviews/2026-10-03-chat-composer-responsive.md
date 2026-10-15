# Responsive bottom Chat composer — 2026-10-03

The user requested the token meter near the bottom, a horizontal scrolling
skills/files/tools row, and a composer height suited to responsive screens.
At live main18801, 390×844, the old tools row wrapped to 87 px and token usage
occupied 53 px beneath the top heading. This turn changes the main Chat only;
the Workspace token meter already sits near its own composer.

## Implementation

- Move the existing measured session token meter into the bottom composer,
  above the textarea. Preserve the usage details toggle state across renders.
- All tool buttons, including voice, share one horizontally scrolling row.
  Each target is 44 px tall; native keyboard focus reveals the last control.
  Disabled vision attachment remains disabled for unsupported models.
- The textarea starts at 64 px, grows with the draft, and is capped at
  `clamp(64px,22dvh,180px)`. Longer text scrolls within the input. The 44 px Send
  button remains outside the scrolling tools row. Attachments and expanded
  usage details have bounded scrolling areas.
- Measured sizing uses a literal CSS custom property, as required by the
  existing frontend CSP contract. Asset cache version is composer-responsive-2.
  The session, turn, approval and model-selection pipeline is unchanged.

## Verification and attempt log

- Expanded the existing real Chrome pointer/touch regression across 390×550,
  390×844, 390×1200, 768×900 and 1440×900. It checks token placement, one tool
  row, keyboard access to the last tool, bounded long drafts, visible Send,
  exactly one model request and persisted replies. Workspace native Send and
  error/draft retention checks remain included.
- The first targeted run found an older test explicitly requiring all tools
  to fit without horizontal overflow. Updated that obsolete expectation to
  the requested single scrolling row; the targeted suite passed (25.656 s).
- The first complete web suite caught direct style.height sizing violating
  the repository contract; replaced it with the established custom-property
  mechanism. That run also had a transient MCP directory search failure.
  Its independent regression passed twice without changing the MCP product
  or test. Original failure logs are retained, not overwritten.
- Node UI tests: 135/135 passed. Build, web vet and whitespace checks passed.
  Complete final browser-required Go web suite passed (84.771 s). Main18801
  serves the same tested binary, schema74, PID96372. Final cutover hashes
  show all nine tables and the vault unchanged; SQLite integrity is ok.

## Evidence

`.tmp-ui/composer-responsive-20261003/` contains source rollback copies,
SQLite cutover backup, table/vault preservation hashes, test outputs and live
mobile/short/desktop screenshots. Nine protected tables retained all rows,
including 38 original sessions; SQLite integrity is ok and credentials remain
in place with the same hash. No new model request, Task acceptance, file
attachment, project file change or provider selection was made by live QA.
Owned QA tabs preserve user tabs and drafts. This turn does not resolve the
separate Delivery/Docker, offline reviewer, Discord or self-improvement backlog.

Final live QA on composer-responsive-2: long draft height180 px with internal
scroll and Send visible at390×844; desktop form760 px wide, page overflowfalse.
Console error inventory was empty. Final images are main-mobile-final.png and
main-desktop-final.png. New main tab20 is the deliverable; existing user tabs
were not reloaded, viewport overrides were reset, and test drafts were cleared.

## Follow-up: compact button padding

The user requested smaller top/bottom padding for Chat, Plans, Review and
Send. Live Workspace mobile measurements before the change were44 px height
and8 px padding per side, assistant helper/model buttons40 px. The final
presentation layer now uses4 px vertical padding and36 px height for main
view navigation, Workspace tabs/add/close and assistant text Send. Secondary
helper/model/menu buttons use32 px. Font sizes, horizontal scrolling, token
placement and the submission pipeline are preserved. The circular Chat Send
and its tool row already use zero vertical padding and keep their size.

Required-Chrome final targeted checks passed13.293s: static cockpit/CSP contract,
mobile tab add/switch/close/picker, native pointer/touch Send and failed-draft
recovery, and Chat/Workspace conversation entry. Updated the existing tab
size assertion to the user's36 px target; no new style-only test was added.
Node135/135, build and diffcheck passed. Live main18801 now serves
compact-buttons-2, schema74, PID163020. All nine protected tables and the
vault hash are unchanged; SQLite integrity is ok. Evidence/rollback copies
are in `.tmp-ui/compact-buttons-20261003/`.

Live QA found the legacy `.compact` class using !important padding, leaving
secondary helper/model/add/close padding at8 px despite smaller minimum
height. Scoped final overrides reduce their vertical padding too; the close
icon gets a matching36 px height/line-height. Repeated the targeted browser
suite after this correction. No product-wide .compact rule was changed.

Final live measurements at430×1244: view navigation, Workspace tabs/add/close
and assistant Send all36 px high with4 px top/bottom padding; helper/model
buttons32 px with4 px padding. Real Review-tab and Plans-page clicks worked.
At1440×900, navigation/tabs/Send remain36 px with no page overflow. Console
error inventory was empty. final-mobile.png, final-desktop.png and
live-metrics.json are saved. Deliverable main tab21 retained; viewport reset
and user tabs/drafts untouched. No new model request or Task acceptance in
live QA; native Send responses were verified by the local browser fixture.
