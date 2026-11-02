# Hermetrix architecture evolution

Date: 2026-10-29. Status: implementation in progress; this is an acceptance
contract, not a claim that every capability has passed live qualification.

Applied foundation and current evidence: [implementation review](../reviews/2026-10-29-architecture-foundation.md).
Current interaction/learning evidence and remaining gates:
[checkpoint](../reviews/2026-10-29-interaction-and-learning.md).

## Product and scope

The owner wants an understandable local workbench whose code is easy to read
and change, makes effective use of Skills, learning, MCP, Project Brain and
bounded context, and supports spoken conversation with interruption.
Keep the existing local64k model, project boundaries and durable history.
The marketplace is a separate project and is not completed by this refactor.

Everyday workflow: choose a project, type or speak a request, see what is
happening, inspect changes/checks and accept the result. Preserve Chat, Plans
and Workspace. Put connection setup and advanced tuning in Settings; keep
protocol fields, hashes and diagnostics behind disclosures. A stopped model,
an idle model, a queued request and a running task must be distinguishable.

## Architecture decision

Retain a modular monolith and the existing Go module. The executable composes
services; HTTP handlers and CLI are adapters. Domain-specific packages retain
their state machines. Use interfaces at actual external boundaries, not one
interface per function. Avoid a parallel framework, generic event bus, second
task engine or replacement database.

| Boundary | Responsibility | Current implementation |
|---|---|---|
| Composition | Configuration, wiring, startup, recovery and shutdown | `cmd/hermetrix` |
| Agent | Frozen conversation contract, tool loop, messages and stop controls | `internal/agent` |
| Tasks | Requirement/plan revisions, leases, attempts, effect/completion state | `internal/taskengine` |
| Task application | Plan/select/propose/apply/verify/review/reconcile | `internal/taskcoord` |
| Proposal boundary | Typed immutable model inputs and validated proposals | `internal/worker` |
| Model execution | Provider adapters, admission, usage, cancellation and idle proof | `internal/providers`, `internal/inference` |
| Context | Budgeting, exact pinned authority, references, history and compaction | `internal/context` plus bounded task curation |
| Learning | Versioned Skills, replay/evaluation, review outbox and promotion | `internal/skills`, `internal/learning`, `internal/curator` |
| Integrations | Deferred tool discovery/schema validation and knowledge retrieval | `internal/mcp`, `internal/capabilities`, `internal/projectbrain` |
| Voice | Capture/VAD, ASR, transcript submission, generation stop, TTS/playback | Web voice adapters and independently testable browser controller |
| Persistence | SQLite transactions, immutable CAS artifacts and separate vault | `internal/store`, `internal/blob`, `internal/secrets` |
| Presentation | Thai task language, progressive disclosure and responsive controls | `internal/web/ui` |

Dependency direction: UI/CLI -> application services -> domain policy and
external ports. Provider/tool integrations never obtain task authority from
retrieved text. A context reference cannot add writable paths or approvals.

## Maintainability

Split large files by complete workflows, preserving package/API boundaries and
declaration bodies. First split the task coordinator into planning, selection,
proposal, apply, verification, review, reconciliation and rollback files.
Record source/declaration fingerprints for a mechanical split and run the
existing suite. Group new behavior with its tests; keep operational experiments
and raw evidence in ignored directories. Do not move code merely to satisfy a
directory diagram. Extract a package when there is a cohesive independently
testable responsibility, not an arbitrary line-count threshold.

Frontend modules should own one feature/controller and expose a small explicit
interface. Start with interruption control and Goal controls. Keep existing
native HTML controls and presentation tokens; preserve drafts and project state.

## Skills, knowledge and context

Skills and Project Brain are distinct inputs. Skills are immutable local
procedures; Project Brain supplies externally curated, version-checked evidence.
MCP supplies tools, resources and prompts. Discovery does not itself authorize
tool execution. Read and candidate-submission credentials remain separate.

Task Skills use typed references carrying Skill ID, immutable version ID,
content hash and exact body. Select only active enabled owner-visible Skills
with an applicable scope. Selection must be bounded and deterministic. Include
only whole references that fit after authoritative requirements and source.
Persist the references in the frozen input/proposal metadata. Do not invent a
file in the project to smuggle a Skill into source context. No automatic
activation of a candidate and no hidden capability widening.

Context priority: original requirements and constraints; current source and
checks; exact observed failure evidence; relevant Skills; external references;
optional older history. Reject authority overflow. Optional references can be
omitted with an inspectable selection receipt; never silently truncate a rule
into a different instruction. Every provider request remains hash/revision bound.

Learning loop: observed result -> evidence-bound candidate -> baseline/candidate
replay -> independent review -> explicit promotion -> held-out reuse measurement.
Measure correctness/regression, applicability, context cost and actual outcome.
Conversation claims, a passing build or more saved Skills alone do not prove
improvement. Same-model review is a separate pass, not diverse review. Automatic
model-weight training and unbounded self-modification are outside this design.

Completion prepares immutable local solution context outside the SQLite
transaction and stages a durable learning fact inside the completion transaction.
This also covers delayed user acceptance. Task and conversation outboxes retain
their own real source foreign keys and use the same recovery/drain policy.
Human acceptance is recorded separately from measured check receipts.

## Voice interaction

Local pipeline: microphone -> 40ms PCM frames -> WebSocket/VAD -> local ASR ->
final transcript -> ordinary durable text turn -> streamed answer -> local TTS.
Partials are editable hypotheses, never separate committed user turns.

Separate capture, transcript queue, turn control and playback lifecycles. Use a
generation/epoch fence so late events from interrupted turns cannot restart
playback. Barge-in immediately mutes playback, requests Stop for the exact owned
turn, and waits for durable stop plus provider-idle confirmation before a new
turn. Never equate aborting fetch with confirmed model/tool termination.
Repeated speech-start events must not duplicate stop requests. Preserve final
utterance identity through queueing and network uncertainty. Permission denied,
no microphone, ASR unavailable, stop requiring inspection and reconnect all need
human-readable recovery. Native Windows capture without echo cancellation must
not feed the assistant's speaker output back as a new user message.

Natural Thai/English microphone and repeated barge-in acceptance require the
owner's actual device. Synthetic clips and browser fixtures prove mechanisms,
not physical microphone/speaker quality. The old 16k concurrent GPU measurement
does not qualify the current 64k allocation.

## Acceptance ledger

| ID | Required observable outcome | Evidence required |
|---|---|---|
| A1 | Coordinator workflows can be changed independently without semantic drift | Declaration fingerprints and existing task suites |
| A2 | Task planner and worker receive the applicable exact Skill | Captured canonical/wire input, version/hash and real SQLite fixture |
| A3 | Disabled, foreign-project, oversized or stale/mismatched Skills do not enter a request | Negative tests; no provider dispatch on invalid contracts |
| A4 | Authority/source remains intact when optional knowledge exceeds context | Curation boundary tests and captured frozen input |
| A5 | MCP/Project Brain remains scoped and exact-version checked | Existing registry/schema/retrieval tests plus live scoped read where available |
| A6 | Learning produces a reviewable candidate with measured held-out benefit | Actual evidence-bound trial; remains open until performed |
| A7 | Speech start stops old output and targets only its owned generation | Deterministic controller tests and exact-stop browser fixture |
| A8 | Repeated interruption/late completion/failed stop does not replay a message | Queue and epoch regression tests |
| A9 | Owner can start/stop/inspect a Goal and understand a wait | Existing Goal API connected to Thai UI; real browser workflow |
| A10 | Primary controls remain usable at 390, 768 and 1440px | Rendered screenshots, keyboard and overflow checks |
| A11 | Natural voice works with the current local64k runtime | Physical mic/speaker trial, repeated interruption and measured latency |
| A12 | Restart preserves history and shows unresolved work accurately | Real SQLite close/reopen and recovery tests |

2026-10-29 checkpoint: A1–A4 and A7–A10 have technical/browser evidence;
A12 has actual SQLite recovery evidence. A5 has a live scoped Codex connector
read and exact passage, but native pilot authentication is pending. A6 produced
a real inert candidate. The first trial's combined incomplete/approval-bound
error is inconclusive. A fresh v2 trial completed all12observations and returned
keep_current (129→109/130); positive benefit remains unproved. See the
[learning fidelity checkpoint](../reviews/2026-10-29-learning-fidelity.md).
A11 passed synthetic live audio with repeated interruption against
the actual local model; physical acoustic acceptance remains pending.

Report each gate as pending, implemented, technically verified or outcome
verified. Preserve pre-existing failures separately. No whole-system completion
claim until required gates have actual evidence.

## External primary references

- [Go module organization](https://go.dev/doc/modules/layout): commands and
  application-private packages fit the existing module.
- [MCP architecture](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/docs/2026-07-28/learn/architecture.mdx): preserve the host/client/server and discovery boundaries.
- [AudioWorklet](https://developer.mozilla.org/en-US/docs/Web/API/AudioWorklet):
  browser audio processing is separate from network and application state.
