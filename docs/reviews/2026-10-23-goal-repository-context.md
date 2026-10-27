# Ranked repository context and bounded planner snapshots

Date: 2026-10-23. Broad goal remains active. This source/performance slice does
not qualify an installed release, actual model quality, EA or remote machine.

## Evidence and causal change

An actual temporary repository with 2048 unrelated sources followed by
`z_trading/risk_manager.mq5` reproduces the old 2000-file lexical cutoff. Even when
explicitly writable, that target appeared at planner rank 511 of 512 because the
priority list was finally sorted alphabetically. Planner byte fitting could then
drop the owner's target. The planner also read whole source files to hash them
before fitting the serialized input and queried the project again for every file.

Project Brain was inspected as a working retrieval sibling. It supplies curated
remote knowledge, not current local source, so substituting it would not repair
this local source-authority gap. User memories likewise do not index local files.

The new path ranks a fresh metadata walk using requirement/step/check terms and
nearby writable directories. It handles Unicode/CamelCase path words, keeps a
bounded candidate heap, and avoids stat calls for candidates that cannot enter
the retained set. Exact writable paths are independently inspected first and
always precede discovery, including absent leaves in existing directories. Source,
secret, symlink, owner/root and 32-file authorization rules remain unchanged.
The compatibility writable-manifest method delegates to this same path.

Planner and selector fit serialized entries in linear work before source hashing.
Their body ceilings are 48/120 KiB, further reduced to the selected preset input
budget with framing reserve. The existing provider preflight still checks the
actual rendered request. If all writable pins cannot fit, dispatch is refused.
Planner hashes only fitted files under one canonical-root identity and an 8 MiB
total read budget. Reads are bounded even if a file grows after metadata discovery;
one overflow sentinel byte is permitted. Unknown hashes remain absent. Prompts
explicitly identify ranked/truncated context and forbid claiming omitted files
do not exist. Planner/selector artifacts retain discovery and read-budget receipts.

Why fresh metadata rather than another source index/cache is recorded in
[ADR-003](../decisions/ADR-003-task-source-discovery.md).

## Measured fixture

Windows amd64, Intel i7-12700F. 4096 unrelated 32 KiB Go files plus two late MQL
sources. Both workflows retain at most 512 paths. Three measured operations per
benchmark; setup/source creation is excluded. The baseline preserves the actual
before-change prefix/writable-sort/per-file-read algorithm in test code and excludes
planner JSON fitting. The ranked workflow performs fresh ranking plus bounded
batch hashing; it does not call a model.

```text
go test ./internal/product -run '^TestTaskManifest' -bench 'BenchmarkPlannerManifest(Baseline|Ranked)$' -benchtime=3x -count=1 -v
BenchmarkPlannerManifestBaseline-20  3  2319329267 ns/op  16744464 snapshot-bytes/op  61932901 B/op  110572 allocs/op
BenchmarkPlannerManifestRanked-20    3   905822033 ns/op   8355872 snapshot-bytes/op  32653968 B/op   83224 allocs/op
```

This fixture took 2.319s versus 0.906s (about 61% lower elapsed time), with 50.1% fewer source bytes read and
47.3% fewer allocated bytes per operation. These are small-sample mechanism
measurements, not end-to-end model latency, peak resident memory or a universal
repository-size guarantee. Ranked discovery issued 513 file metadata reads for
4,098 source-like paths; traversal still visits the tree.

## Actual coding acceptance

An integration fixture with 2200 long unrelated source paths and a 32768-token
writer exercises the real provider preflight and actual Node coding/check/review
cycle. The writable `sum.js` is first in both contexts; its unchanged acceptance
test remains visible. Planner input is 48116 bytes/240 files, selector input 87952
bytes/713 candidates. Both truthfully report truncation; the planner artifact
retains its discovery receipt. Actual code passes and the task completes.
Model transport is simulated; filesystem, SQLite, provider framing checks and Node
commands are real. The test passed in 2.08s (focused package 6.513s).

Other tests prove deterministic ranking, late paths, exact pins, missing files
without placeholders, fresh additions/removals, secret/generated/symlink filters,
Windows case aliases, canceled scans, changed-root rejection and a source growing
from its old metadata size to 3 MiB while a 1024-byte read budget remains enforced.
Small-context tests refuse to silently remove authorized paths. Existing native
61440-context planning tests pass; their expected order now follows task relevance.

## Final qualification

```text
go test -p 1 ./internal/product ./internal/taskcoord ./internal/worker ./cmd/hermetrix-goal -count=1
ok hermetrix-harness/internal/product        78.700s
ok hermetrix-harness/internal/taskcoord     165.294s
ok hermetrix-harness/internal/worker          3.341s
ok hermetrix-harness/cmd/hermetrix-goal        3.388s
go test ./internal/web -run TestGoal -count=1
ok hermetrix-harness/internal/web             9.016s
```

The full coordinator suite includes the actual over-120-second process test;
ordinary goal creation, correction, rollback, review and async recovery still pass.
Focused race qualification for manifest filters/absence, late-source coding,
provider fitting and immutable native-context planning passes: product26.248s,
taskcoord22.763s. Worker had no matching tests in that focused race command; its
full ordinary suite passed above. Vet/build/gofmt/diff and file-map checks pass.

Latest prepared-only server:
`.tmp-ui/goal-20261023/hermetrix-goal-with-repository-context.exe`, 51,234,304 bytes,
SHA256 `ABE9FD714AC734EAA7988F35833B492EA9B78CD3D0D0C7C3720C54FE1E7B0D8C`.
Source remains schema78. The existing prepared async CLI can read this source API;
neither this server binary nor an alternative runtime was activated.

## Limits and remaining acceptance

The metadata walk stops after 100000 callbacks and reports truncation; exact pins
are retained independently. `filepath.WalkDir` reads a directory's entries before
visiting them, so huge flat-directory enumeration memory remains unqualified.
This is lexical path ranking, not a content/dependency index. Fresh traversal
remains O(N); the bounded heap reduces retained candidates and metadata work.
Hash limits can omit digests, never fabricate proof. Large multi-step source
creation/deletion/directory changes still need their own contracts.

Prior activation denial remains in force. The pilot at 18816 predates these changes;
no stop, replacement or alternative runtime activation was attempted. Overall
completion still needs selected live model/EA/history/remote acceptance and scale
targets. Existing whole-suite browser timing and older ignored-SQL failures remain
qualified in the earlier reviews; this is not a whole-repository green claim.
