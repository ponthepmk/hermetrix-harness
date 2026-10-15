# Hermetrix workbench design and interaction contract

The product is an operations workbench: users choose a workspace, describe an
outcome, review a plan, inspect changes and evidence, and explicitly accept the
result. Keep the next useful action close to the state that explains it.

## Shared presentation

`internal/web/ui/interaction.css` is the final presentation layer over the
existing shell. Use semantic tokens: background `#101317`, surface `#181d23`,
raised surface `#222932`, text `#edf2f7`, muted `#b0bbc7`, accent `#8ae2d4`.
Primary controls use dark text on the mint accent; errors and blocked states
also carry words and icons. Preserve the existing native controls and Thai
product vocabulary. Do not introduce a new component framework for polish.

Plans prioritizes current evidence awaiting user acceptance, then blocked work,
then other ongoing work. Completed work is a disclosure. Acceptance readiness
comes from the completion API, never from a guessed Task state.

## Interaction

- Click and focus feedback is immediate; press motion lasts 240 ms and color
  transitions 140 ms. Respect `prefers-reduced-motion`.
- Loading reflects actual pending work. Bootstrap has its own operation;
  explicitly initiated API calls share a request ledger. Idle polling is quiet.
- Show a spinner and an elapsed message for long requests. Do not manufacture
  progress percentages or claim that a request receipt completes the whole Task.
- Keep overlapping work pending until all requests settle. A failure survives
  a later successful request in the same overlapping group.
- Prevent another activation of a pending control. Existing handlers retain
  ownership of validation, disabled state and permission checks.
- Foreground reads are bounded at 30 seconds. Mutations are not automatically
  aborted or replayed: a delayed response does not prove a write failed.
- Errors retain the surrounding UI and a recovery explanation. A user can
  explicitly try again. Never submit Task acceptance through the feedback layer.
- Late evidence must repaint the currently selected Task even if the request
  began on the overview. Bootstrap restoration must respect newer navigation.
- Returning to the overview must survive reload. Preserve drafts and existing
  workspace arrangements when changing surfaces.
- Workspace navigation uses a persistent strip of opened tabs with horizontal
  overflow confined to the strip. Keep add/close reachable outside it; the add
  chooser activates an existing module and creates independent notes. Compact
  layouts show one pane without a duplicate pane header. Preserve drafts and
  selected-tab state; support arrows, Home/End and Escape with visible focus.
- Mobile composition must remain stable through pointer down/up when focus
  leaves a textarea. Hold compose mode until explicit dismissal or submit
  completion. Compact panes use one content row; feedback overlays must not
  intercept primary actions. Verify Send with native pointer/touch events and
  a persisted response, including error/draft recovery.

- Prioritize the transcript and draft. Main Chat starts at one draft line
  (48 px minimum), with growth capped at18dvh/144px and internal scrolling.
  Tools, token details and queued messages use compact44px disclosures in a
  footer. Queue attention remains visible when collapsed; never cancel pending
  messages automatically to reduce clutter. Expanded menus scroll within the
  viewport and close on outside pointer/Escape. Keep Send visible on short
  screens and preserve the next draft while an earlier turn finishes.
- At1080px and below use one52px header row: truncated workspace name, current
  surface menu, chat drawer and settings. The surface menu retains Chat, Plans,
  Workspace, Search and hardware status; hardware details stay collapsed by
  default. Tablet and mobile use one active Workspace pane and a drawer rather
  than reserve transcript width for the session list. Conversation text is16px
  with1.7 line height. Keep technical errors and actor IDs secondary to a human
  explanation and recovery action. Background completion feedback must not
  cover the draft or its controls.

## Responsive verification

Replies use semantic headings, nested lists, quotes, tables with internal
horizontal scrolling, and escaped fenced code. Mermaid diagrams render in an
isolated viewer, then appear as SVG images at their own readable size; never
stretch narrow diagrams to the full tablet width. Offer a native expansion
dialog with focus restoration and an inspectable source fallback. Completed
approval previews collapse; pending approval previews remain visible. The
app header carries one current conversation title (project title in Workspace
and Plans), with no second Chat title bar.

Use at least 390, 768 and 1440 px widths for shell and Plans changes. Mobile
primary controls default to 44 px height. The user explicitly requested
Workspace tabs and text Send buttons: use 36 px height
and 4 px vertical padding for those controls, with 32 px secondary assistant
controls. Chat tool targets and icon Send retain their existing size. Compact summary cards leave room
for the next action. Keep sticky section links from covering anchor headings.
Inspect screenshots, document overflow, text contrast, console errors and real
loading/error recovery. Supporting checks are the Node UI suite and Go web
suite with `HERMETRIX_REQUIRE_BROWSER_E2E=1`.

Measured evidence and known limits: `docs/reviews/2026-10-02-ui-motion-and-flow.md`.
