# Browser qualification for requirement and plan review

Date: 2026-10-23. Broad goal remains active. This change stabilizes browser test
synchronization and strengthens actual input/authority checks; product UI JS/CSS
is unchanged. It does not qualify the installed runtime or live model/EA/remote
workflow.

## Reproduction and earliest divergence

`TestPrimaryRequestOpensEditablePlanBeforeExecution` failed twice in its first
three unchanged focused runs. The assertion checks that the pending-plan approval
button remains reachable after desktop-to-mobile emulation and pane remount.
Timeouts/assertions were not relaxed.

Failure-only screenshots and geometry probes established actual layout movement:
the task body was3631px tall before remount and2988px afterwards; saved scrollTop
remained2112px. The button moved from y427.55px/height58px to above the viewport
(final y−40.84px/height44px). Disclosure open states remained unchanged. Screenshot:
`.tmp-ui/goal-20261023/browser-failures/TestPrimaryRequestOpensEditablePlanBeforeExecution-1792746520940075500.png`.
Its review button is outside the visible pane; this is not evidence of a successful
click or a failed model operation.

The test changed viewport dimensions and scrolled immediately after DOM width
checks. Those checks can pass while the browser is still applying the emulated
layout. Remount then preserves a scroll value from the transient layout. Existing
working resize/native-input tests already wait for two animation frames. That
same synchronization now follows both viewport changes and the explicit remount.
No arbitrary sleep, larger timeout, skipped assertion or product layout change
was introduced.

Temporary detailed probes were removed. The reachability assertion retains
button/hit-target/viewport/scroll diagnostics. Optional
`HERMETRIX_E2E_FAILURE_DIR` captures only after a failed observation, because a
pre-assertion screenshot can itself settle compositor state and mask the failure.
Capture filenames are sanitized and bounded; failure of capture does not turn a
failed assertion into success.

## Observable acceptance

The browser follows workspace selection → primary request → editable brief →
saved criteria/experience preview → pending plan → desktop/mobile review.
Opening a brief creates neither a task nor an agent session prematurely.

After mobile remount, the original bounds/hit-target check must pass. Native CDP
pointer input then opens the actual approval dialog. Canceling it leaves the
stored plan `pending`, proving that inspection/cancellation did not approve work.
This exercises the real router, storage, browser layout and event handler.

Five consecutive unmasked focused runs pass after synchronization:

```text
go test ./internal/web -run '^TestPrimaryRequestOpensEditablePlanBeforeExecution$' -count=5 -v
PASS 4.88s / 4.91s / 4.74s / 5.08s / 4.84s
ok hermetrix-harness/internal/web 29.716s
```

A separate screenshot qualification with required Chrome passes4.72s
(package7.296s). Inspected1440×900 desktop and390×844 mobile images under
`.tmp-ui/goal-20261023/browser-qualified/`: `plan-brief-desktop.png`,
`plan-brief-mobile.png`, `plan-review-mobile.png`. The mobile confirmation button
and send-back action fit inside the visible pane and native confirmation works.
The brief screenshots also expose sticky-section navigation over the requested
top scroll anchor; that reading/layout observation is not repaired by this
test-synchronization change and needs separate user-workflow qualification.

## Scope and remaining gates

The isolated prior Team DAG2-second deadline passed without modification earlier;
it is being observed in the full repository run, not relaxed to hide load.
The uncached serial whole-repository command now passes:

```text
HERMETRIX_REQUIRE_BROWSER_E2E=1 go test -p 1 ./... -count=1
ok hermetrix-harness/internal/agent       55.256s
ok hermetrix-harness/internal/product     76.966s
ok hermetrix-harness/internal/store        4.234s
ok hermetrix-harness/internal/taskcoord  165.650s
ok hermetrix-harness/internal/taskengine  10.619s
ok hermetrix-harness/internal/web        103.551s
ok hermetrix-harness/internal/worker       3.056s
ok hermetrix-harness/scripts             51.722s
```

All other packages also pass or contain no tests; the process exit code is0.
Full log: `.tmp-ui/goal-20261023/full-go-suite.txt`. This includes actual long
Node verification and the unchanged Team DAG deadline. Final vet/gofmt/diff checks
pass. The normal package-parallel command also passes with required Chrome:

```text
go test ./... -count=1
ok hermetrix-harness/internal/agent       117.983s
ok hermetrix-harness/internal/product     112.564s
ok hermetrix-harness/internal/taskcoord   210.453s
ok hermetrix-harness/internal/taskengine   41.875s
ok hermetrix-harness/internal/web         145.550s
ok hermetrix-harness/internal/worker        5.272s
ok hermetrix-harness/scripts              54.009s
```

Exit code0; all other packages pass or have no tests. Full log:
`.tmp-ui/goal-20261023/parallel-go-suite.txt`. No deadline/assertion changed in
the previously intermittent Team DAG check. This qualifies these two actual
runs, not every possible machine/load. The separate full CI race command failed:
`internal/agent` reached the cumulative package timeout (615.962s), with the
wall-time-budget test blocked in HTTP test-server cleanup. No `DATA RACE` report
was observed. Its handler did not consume the request body and waited up to ten
seconds for cancellation; the isolated unchanged race test reproduced blocked
cleanup and took14.83s. The fixture now consumes the body and has a test-owned
release before server cleanup. Three isolated race runs pass5.82/5.90/5.87s
(package21.831s), preserving the original turn-time and lease assertions. This
does not yet establish a whole-repository race pass.
The subsequent whole race run also failed on cumulative agent migration time and
Chrome profile cleanup after actual workflow input assertions passed, and exposed
a real concurrent Goal Stop bug. That bug has a forced-order reproduction and
source fix with five repeated focused race passes; see
[current snapshot/Stop qualification](2026-10-23-goal-snapshot-fence.md).
Doc-truth's previously surfaced ignored-SQL errors remain a separate gate.

No activation action was attempted. The identity-checked pilot replacement was
previously rejected by automatic approval review (`blocked by policy`, no detailed
reason); source tests do not resolve that boundary. Real selected models, EA
history/criteria and authenticated remote host acceptance remain unproven.
