# Harness Git checkpoint

Date: 2026-10-27 · Branch: feature/goal · Starting commit: 4459036

The Harness implementation is separated into commits without adding generated
applications or new raw experiment captures. Local projects and evidence remain
on disk.

| Commit | Scope |
|---|---|
| `4ba10de` | Ignore new local evidence and generated projects; document the policy |
| `f8e1a2d` | Bounded worker edits, request budgets and evidence-bound review |
| `334bcb6` | Paired behavioral checks, immutable runs, scoped claims and migration 79 |
| `0af7c66` | Durable coding operations, named goal replay, planner output fitting and migration 80 |
| `3066c41` | Mixed-language Project Brain fallback and portable measurement summaries |

The following documentation commit records the implementation contracts,
validation and continuation plan. Use Git history for its exact identity.

## Verification of the separated commits

Each code commit was exported from the Git index into an ignored local snapshot
before committing. Checks used only files present in that staged version:

- Worker: full worker and provider package tests passed.
- Behavioral runs: all packages compiled; skills, store and agent tests passed.
- Coding operations: all packages compiled; task coordinator, task engine and
  provider tests passed.
- Project Brain: Project Brain and context package tests passed.

All 253 changed source and standing-test paths retained their pre-commit byte
hashes. The complete source had already passed 1,435 test events, zero failures,
20 test skips, vet and build after native apply. See the
[portable checkpoint](../validation/2026-10-27-harness-checkpoint.json) and
[Project Brain measurement](2026-10-27-project-brain-measurement-results.md).

## Repository contents

Harness source, standing tests, their required JSON fixtures, implementation
documents and compact validation summaries belong in Git. The JSON fixtures
under `internal/` are test inputs, not generated application workspaces.

New `docs/evidence/*` captures, generated example projects, temporary workspaces,
local databases and model runtime files stay local under the ignore rules.
Previously tracked historical evidence remains tracked. The original source
changes introduced no extra standalone project manifest: the tracked manifests
remain the Harness `go.mod` and the existing frontend vendor package manifest.

The installed application was not updated and no remote push was performed.
