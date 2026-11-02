# Interaction, context and learning checkpoint

Date: 2026-10-29. Status: implemented and technically verified; whole-goal
acceptance remains open. This follows the [foundation checkpoint](2026-10-29-architecture-foundation.md)
and [architecture contract](../architecture/2026-10-29-evolution.md).

The owner authorized direct Codex implementation with local Bonsai review.
Earlier native-only prototypes remain historical evidence; rejected proposals
were not applied. The resulting architecture stays a Go modular monolith with
SQLite/CAS, explicit domain state machines and a vanilla browser UI.

## Applied behavior

- A separate voice turn controller owns interruption, exact turn identity,
  duplicate suppression, stop confirmation and stale callback fencing. Muting
  playback is immediate; the next request waits for the persisted stop and
  released lease. A failed/uncertain stop exposes an inspect action.
- Final utterances retain their identity. Closing voice preserves unsent text
  beside existing chat drafts. Voice requests do not enter the generic automatic
  resend path. A visible voice action remains beside the chat composer.
- Plans exposes Thai Goal start, wait, resume, stop and explicit new-round
  controls. A stopped Goal is terminal. Lost replies retain the exact request;
  a different Goal cannot serve as proof for that request. Writable-file consent
  starts unchecked. Same-model writer/reviewer profiles are disclosed.
- Proposal context disclosures are derived from validated immutable canonical
  input. They show exact Skill versions/hashes and Project Brain citations,
  without retrieved bodies or an invented claim that inclusion improved results.
- Task completion now stages learning inside the same SQLite transaction,
  including completion after later user acceptance. An application observer
  prepares immutable local solution evidence before the transaction; its stage
  only writes through that transaction. No inference runs there.
- Schema 82 adds `task_learning_outbox` with a real Task foreign key. The
  conversation outbox keeps its agent-session foreign key. Both use the existing
  idempotent drain/recovery workflow. A source-body-free solution digest records
  bounded summary, changed paths and included Skill identities. Human acceptance
  is an owner decision, not a measured command result.
- Eighteen existing best-effort database writes now use the existing durability
  error observer. The obsolete empty-answer documentation anchor now describes
  the actual evidence-summary fallback. The documentation check passes.

## Evidence

Raw evidence is ignored under `.tmp-ui/architecture-20261029/`; compact source
and evidence hashes are in the companion validation JSON.

- Full Go suite at the schema-82 checkpoint: 1,525 passing test events,
  22 conditional skips, zero failures, 36 passing packages. The later small
  acceptance-reference classification change also passed its targeted suites.
- All 178 browser-module Node tests pass. Real Chrome covers Goal start → wait
  → resume → stop, exact voice interruption, duplicates, preserved drafts,
  keyboard return, plan-review scroll restoration and actual pointer/touch Send.
- Goal and voice captures at 390, 768 and 1440px were inspected. Primary stop
  controls remain visible and at least 44px high; document overflow is checked.
- Real SQLite tests cover immutable Skill registry → planner/proposer input,
  source/authority preservation, scope/budget/hash rejection and reopen without
  inference replay. Completion/outbox failure rolls back both; reopening after
  success retains the pending fact. Delayed user acceptance drains exactly once.
- Fresh migration from a simulated schema 81 preserves its Task and the old
  conversation-outbox definition; invalid Task references/subjects are rejected.

Local Bonsai reviewed the UI bundle and approved it with zero blocking findings.
The first completion review rejected an obsolete failed-fixture log, rather than
finding a code defect; the fresh review used the actual current passing log and
approved with zero blocking findings.
These calls use the same physical Bonsai model in separate passes, not diverse
model evidence. Saved review identities are never replayed.

## Actual local voice

The installed CPU Whisper was functional, but the old CUDA debug binary could
not load its debug runtime DLLs. An isolated upstream release archive was fetched
from [whisper.cpp b5454](https://github.com/ggml-org/whisper.cpp/releases/tag/b5454)
and its SHA-256 matched the published asset digest:
`afef0b881c500958921c3f5523b50e59ee2ec9b6f5cbd25b324c51ed308a957a`.
The installed binaries were left intact. The first CUDA inference exceeded the
client's deadline during cold initialization; after warmup the same bilingual
test passed. Cold-start readiness needs to account for that warmup.

With the actual Bonsai server still allocated at 65,536 context on port 8088,
the release Whisper CUDA server on port 8082 passed Thai/English transcription.
A real Chrome worklet → PCM/VAD → Whisper → Bonsai → Windows Speech test passed.
A second, longer synthetic microphone clip committed exactly three utterances,
confirmed two durable interrupted turns, and observed the final short answer.
The initial multi-sentence fixture legitimately produced six VAD utterances;
the preserved failed run was not a duplicate-message defect. A fresh three-part
continuous-sentence fixture passed in 35.14 seconds.

This proves the tested synthetic audio/control path. It does not qualify the
owner's acoustic microphone/speaker environment, echo cancellation under real
playback, every accent/noise condition, or a saturated 64k prompt. Native Windows
capture has no browser echo cancellation and explains its button-based fallback.

## Learning and Project Brain limits

A real local-model Learning reviewer produced a valid, reviewable Skill candidate
from a measured development planning example. The experiment reused the existing
frozen planning-lab cases/scorer, fresh sessions, alternating condition order and
an unchanged catalog. Six held-out observations completed before the eighth
planning request returned a combined incomplete/approval-bound error. The old
report cannot establish its precise cause. The trial stopped without retrying
that request or promoting the candidate. Its outcome is **inconclusive**; no
positive learning benefit, model-weight training or autonomous improvement is
claimed. The first trial's report lacks the aborted request's complete identity;
the runner now retains that failure observation for future fresh trials.
The [subsequent fidelity checkpoint](2026-10-29-learning-fidelity.md) records a
complete fairer v2 trial, its negative result and the current source evidence.
This document's hashes and binary receipt remain a historical checkpoint.

The Codex Project Brain connection returned an active curated MCP-sharing lesson,
and an exact-version passage read succeeded for project `hermetrix-harness`.
Its rule agrees with the existing boundary: selected current passing evidence,
explicit owner-selected sharing and a separate curator before active promotion.
Other broad queries returned no match; drafts were not substituted as knowledge.

The prepared Hermetrix pilot has its own disabled `project-brain-read` MCP profile.
It still needs a project-scoped read credential and discovery. Codex's managed
OAuth connection is not silently copied into the application. A user question
about that credential is pending; no secret should be pasted into chat.

## Reviewable pilot and remaining acceptance

A separate pilot database, two explicit local profiles and a current executable
are prepared under `.tmp-ui/architecture-20261029/`. Its learning policy is manual,
with zero active Skills, Tasks or Goals at preparation. Historical schema-81
runtime and marketplace Goals are not opened/migrated by this pilot.

The [pilot launcher](../../scripts/start-architecture-pilot.ps1) accepts its exact
config, supports a side-effect-free preflight, checks listener process/data/reviewer
identity, restores parent voice environment, and never switches ports or replaces
another process. Actual pilot serving is still unperformed: the earlier recorded
automatic approval rejection for port 18816 must not be bypassed with another
port/binary/route. The prepared startup is the concrete approval step.

Remaining user-visible gates: approve the intended pilot startup, enter the
separate read credential in MCP Settings, discover/retrieve through that native
connection, and try real microphone/speaker interruption. Held-out benefit stays
unproven until a complete fair trial supports it. Keep the goal active.
