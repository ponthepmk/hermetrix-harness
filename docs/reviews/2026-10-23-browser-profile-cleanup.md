# Managed Chrome profile cleanup

Date: 2026-10-23. Broad goal remains active. This is a source resource-lifecycle
fix with actual Chrome and Windows file-handle qualification. Product UI JS/CSS
and installed runtime are unchanged.

## Earliest observed divergence

The preceding full race run had failed TempDir cleanup at
`CrashpadMetrics-active.pma` after its actual browser workflow assertions passed.
Earlier isolated workflow reruns passed but did not establish a cause or repair.

An ignored Go overlay probe now calls the real product `ensureBrowser` and
`closeBrowser`, snapshots owned descendant process handles, and inspects the
generated profile immediately after Close. The root and all four captured child
processes have exited, yet the profile remains. Three separate probes reproduced
this on their first iteration, in0.42-0.44s each. Instrumenting the exact ignored
RemoveAll error gives:

```text
PROFILE_FIRST_REMOVE_ERROR unlinkat .../chrome-profile-.../CrashpadMetrics-active.pma: Access is denied.
root_exited=true children=4 live_after_close=[] profile_remaining=true
remaining=CrashpadMetrics-active.pma directory=false
cleanup attempt=0 elapsed=524.6us error=<nil>
```

This establishes a transient filesystem cleanup failure after process exit at
the sampled boundary. It does not prove a surviving child process or require a
claim that Chrome descendants are controlled by a Windows Job Object.
Probes/results remain in `.tmp-ui/goal-20261023/browser-lifecycle-probe/`.

The known working sibling is Go's own `testing.removeAll`: Windows access-denied
and sharing-violation errors can require a short bounded retry during teardown.
The permanent original-source regression also failed immediately: the profile
still existed after Close returned (round0,0.42s).

## Fix and scope

All seven profile cleanup branches (startup failures/cancellation, duplicate
startup and normal shutdown) now use one scoped cleanup function. It accepts
only an absolute generated direct child of the configured data root, rejects
wrong names/outside/nested/root paths and symlink/non-directory profiles, and
rechecks parent/profile identities before every recursive removal. A pending
retry cannot inherit a replacement directory's authority.

Only Windows access-denied/sharing-violation errors retry, with a two-second
retry window and bounded backoff. Other errors return immediately. Persistent
failure logs a warning and retains the path for inspection rather than silently
discarding the error. No API state, operation replay policy, browser input
assertion or test deadline was relaxed. Other platforms use no filesystem retry.

## Observable acceptance

- Actual Chrome must exit and its generated profile must be absent before Close
  returns. The original regression fails before the fix, then three repeated
  normal runs pass (nine real startup/shutdown cycles, package5.617s).
- A real Windows handle denies file deletion; the ordinary initial RemoveAll
  demonstrably fails. Releasing that handle30ms later allows the production
  cleanup function to finish and remove the profile.
- Root itself, unrelated user folders, outside-root and nested folders are
  refused. Owner bytes remain intact before and after a valid owned-profile
  cleanup. Already removed profiles succeed without a new mutation.

Three repeated focused race runs pass21.236s (real Chrome, real Windows file lock
and scope checks): `browser-profile-final-race.txt`. A final scope test including
post-cleanup owner-byte assertions also passes. Source vet passes.

Windows build succeeds. Initial cross-build inherited Windows CGO/compiler
settings and failed on missing Linux C headers. Explicit `CGO_ENABLED=0` Linux
and Darwin builds/product vet pass using this pure-Go stack; those binaries were
not executed on target operating systems.

Prepared-only server `hermetrix-goal-with-browser-cleanup.exe`:51,279,360 bytes,
SHA256 `49A7D5202F15391B4B1ADE6C16D00738CD5E6C3B631AAAAC65ED1F5C4C1E2BBA`.
It was not executed/installed. The final whole-repository race command is a
separate qualification observation in `fixture-profile-final-ci-race.jsonl`.

## Remaining acceptance

The responsive model-choice preservation issue remains unproven/unrepaired;
successful reruns are not evidence of its cause. Doc-truth's existing ignored-SQL
gate remains open. Live selected-model quality, actual EA compile/backtest/history
criteria, authenticated remote host and installed pilot acceptance remain open.
Prior activation rejection (`blocked by policy`) is unchanged and was not retried.
