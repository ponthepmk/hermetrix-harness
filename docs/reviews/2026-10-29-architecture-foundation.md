# Architecture and context foundation — 2026-10-29

Status: foundation implemented; the complete owner request, including real-time
voice interruption and simpler operation, remains in progress. See the
[architecture contract](../architecture/2026-10-29-evolution.md).

## Applied changes

- Partitioned the coordinator into planning, selection, proposal, apply,
  verification, review, reconciliation and rollback. The initial mechanical
  split preserved every declaration body byte. Task tests passed before/after.
- Shared the existing lexical/Thai/semantic-bonus Skill ranking and project-scope
  policy in `internal/skills/selection.go`. Agent public JSON is preserved through
  a type alias; Chat and Task reuse the same policies.
- Added typed `worker.SkillRef` inputs for planner/proposer: immutable identity,
  exact body hash, three whole bodies maximum, 5,000 bytes per body and 6,000 bytes
  aggregate. Validation rejects invalid UTF-8, duplicates and mismatched hashes.
  Canonical/wire snapshots retain independent refs; nil preserves legacy JSON.
- Wired a read-only Task Skill source into composition. Only applicable active,
  enabled owner-visible Skills qualify. Version identity is rechecked; local
  read errors stop dispatch; the provider credential detector checks refs.
- Planner fits the source manifest before optional Skills. Proposer fits
  requirements/source first, then whole Skills, then Project Brain references.
  Frozen hashes and immutable proposal inputs bind the actual refs used.
- Plan/proposal metadata records IDs/versions/hashes; planner records selected
  versus included counts. These are context receipts, not invented success or
  learning-improvement metrics. No migration/dependency was needed.

## Verification

This section records the earlier foundation checkpoint. Later direct Codex
implementation, schema 82, current tests and remaining acceptance are in the
[interaction/learning checkpoint](2026-10-29-interaction-and-learning.md).

The original Task, Skills, Worker, Project Brain, Context and Inference suites
passed. The complete Task suite passed after partition (188.404 seconds).
Agent Skill/semantic/scope checks passed after extraction. The Worker suite and
new contract regression tests passed. Targeted planner/proposal/curation checks
passed after wiring. `go vet ./...`, `go build ./...` and all 159 existing Node UI
tests passed. The complete Go run finished with 1,516 passing test events,
20 conditional skips and zero failures across 36 passing packages (eight commands
have no tests). Exact source hashes are in the companion validation file. The
historical Windows desktop observation failure was not observed in this run;
that does not establish its intermittent cause was fixed.

At this earlier checkpoint the documentation gate failed on pre-existing issues: the old
`empty-answer-fails` text anchor is absent at baseline HEAD, and the negative
check finds ignored database Exec errors in unchanged source files. The moved
Thai tokenizer anchor was updated to `internal/skills/selection.go`. These
remaining findings are not suppressed or treated as passed.

Four independent operational checks passed against applied product code:

1. Scope/inactive isolation, exact hashes and version mismatch rejection.
2. Individual/aggregate whole-body budgets.
3. Overflow omits optional refs while preserving source, requirements and input.
4. A real isolated SQLite candidate/promotion/registry reaches actual planner and
   proposer request construction. Captured requests, planning hash, proposal
   metadata and persisted canonical input agree. Closing/reopening SQLite and
   reading the proposal preserves exact Skill refs without replaying inference.
   Provider calls use the standing native fixture adapter, not live Bonsai inference.

The first operational proposal fixture had a correct addition source, incompatible
with the standing adapter's exact subtraction edit. Using the exact native
standing-test preimage fixed the fixture. No product guard/assertion was weakened.

## Provenance and pending work

Production Go semantics and standing Go tests were generated through Hermetrix
Worker with saved local Bonsai64k. Codex designed/reviewed, wrote operational
drivers/DATA/prose, and mechanically copied qualified native output. Raw evidence
and apply ancestry remain in `.tmp-ui/architecture-20261029/`. Rejected output was
never salvaged. Accepted outputs with compile/fixture defects received fresh
native repairs before application. CRLF disk preimages and normalized model
snapshots are distinguished in guarded apply receipts.

The earlier native voice-controller candidates remain unapplied: independent checks reproduce
early/duplicate interruption and pending-stop cleanup defects. A Goal-view
candidate also remains unapplied. Protocol acceptance alone does not qualify
browser syntax/interaction. They were superseded by separately tested direct
Codex modules in the later checkpoint.

The owner subsequently authorized direct Codex edits with Bonsai review for this
restructuring. The old native-only constraint no longer applies to this goal;
historical native provenance and rejected candidates remain distinct.

Remaining outcomes: exact generation stop and queue recovery; natural repeated
Thai/English voice interruption at the current 64k allocation; clear Thai Goal
and knowledge/context UI at 390/768/1440px; evidence-bound learning with held-out
benefit; broader MCP/browser effect reconciliation. Prior 16k GPU measurements,
more stored Skills, passing infrastructure or same-model review do not prove
these outcomes.

No installed app, serving route, model default, historical marketplace goal,
private credential or external curated knowledge was changed by this milestone.
