# Modern workbench, interaction feedback and flow verification

User request: improve the real Hermetrix UI with a modern appearance, clear
click feedback, loading states and easier workflows. Work stayed in the
existing vanilla frontend and preserved the Easiiot pilot data and approval gates.

## Implemented

- Shared slate/mint tokens, segmented navigation, clearer focus rings, polished
  buttons/cards/settings surfaces and restrained entrance/press animations.
- Real request ledger: button spinner, loading status, long-request elapsed
  message, success and dismissible failure. Background polls stay quiet.
- Bootstrap loading and 30-second foreground read timeout; writes are not
  automatically aborted or retried. Overlapping failures are not masked.
- Plans puts evidence awaiting acceptance first, with descriptive actions;
  completed work is tucked into a disclosure. Mobile summaries are compact.
- Responsive spacing and touch targets; reduced-motion CSS; anchor offsets
  keep Task section navigation from hiding the result heading.

## Reproduced defects and fixes

1. Bootstrap could restore Chat after the user had already opened a workspace.
   A navigation-generation check now preserves the user's newer choice.
2. Opening a Task while overview evidence was still loading left its detail
   displaying loading after the response arrived. The overview callback now
   repaints the currently selected detail. With a 200 ms evidence delay the
   real-browser regression failed before the fix and passed afterward.
3. Returning to the overview still remembered the old detail on reload.
   Surface memory now records overview explicitly; browser reload verifies it.
4. Creating a new Task form accessed `selected.id` without a selected Task.
   Live console evidence exposed the exception. The optional access is fixed;
   the browser regression checks the form produces no error event.

## Validation and evidence

- Node UI suite: **125/125 passed**, `.tmp-ui/motion-node.txt`.
- Go web suite with Chrome required passed; final log
  `.tmp-ui/motion-web-tests-final.txt`. The completion-flow regression was also
  rerun after the final null-selection fix: `.tmp-ui/motion-restore-after.txt`.
- Application build and whitespace checks passed. Preview executable was
  rebuilt and relaunched against the preserved pilot data at
  `http://127.0.0.1:18801/`.
- Live UI: mobile/desktop Task navigation, result anchor, overview reload,
  new form, empty required input, settings and recovery were inspected.
- Separate read-only localhost fixture delayed a model-test response 9 seconds,
  returned 503, then succeeded on an explicit retry. Loading and failure were
  visibly shown; controls recovered. A double activation on retry produced one
  request: two total receipts (failure + retry), not three. The fixture never
  forwarded mutations and was stopped after verification.
  `.tmp-ui/motion-qa-receipts.json` records the simulated responses.
- 390/768/1440 px views had no document horizontal overflow. Measured mobile
  Plans primary buttons were 44–45 px high. The DOM visual audit found no
  sub-12 px Plans text or missing image alt attributes; visible sampled text
  passed contrast checks. This is a focused audit, not a full WCAG certification.

Screenshots: `.tmp-ui/motion-desktop.jpg`, `motion-mobile.jpg`,
`motion-tablet.jpg`, `motion-mobile-results.jpg`, `motion-loading.jpg` and
`motion-error.jpg`. The latter two show the isolated fixture, not a real model
outage. The visual-runtime helper was unavailable; read-only DOM measurements,
console inspection and actual screenshots supplied the visual evidence.

Easiiot Task revision 31 remains awaiting actual user acceptance (16/17 gates).
No acceptance, replay, deployment, commit or unrelated draft cleanup was done.
This UI work does not establish one-shot autonomous coding quality or complete
the earlier scheduler/escalation recommendations. Streaming chat retains its
existing response-status path; the new ledger wraps the common API helper.
