# Hermetrix Harness — Phase 0–7 Completion Report

เอกสารนี้เป็นรายงานปิดงาน Phase 0–7 วันที่ 22 สิงหาคม 2026 คำว่า “complete” หมายถึง behavior contract ใน vertical slice นี้มี code, persistence, API, UI และ test จริง ไม่ได้หมายความว่า breadth ทุกอย่างของ desktop agent เชิงพาณิชย์เสร็จแล้ว

> **historical report** — ไม่ใช่ source of truth ปัจจุบัน งาน correctness หลัง Phase 7 และ finding ที่ยังเปิดอยู่ดูที่ [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md); สถานะ ADR ดูที่ [DECISIONS.md](DECISIONS.md)

## Executive status

| Phase | Delivered gate | Evidence |
|---|---|---|
| 0 Foundation | Skill authority, immutable versions, context profiles, local-first store | store/skills/context tests |
| 1 Agent kernel | append-only events, frozen step binding, approval-gated write, uncertain restart receipt | agent/tools/web E2E |
| 2 Capability graph | fixed direct prompt, deferred search/describe/call, dual-era MCP | 1,500-tool + real HTTP MCP E2E |
| 3 Skill learning | deterministic replay, attribution, trigger policy, capability widening gate | regression/weakened-test/stale-revision tests + browser flow |
| 4 Context fidelity | verified compactor fallback and bilingual full-vs-compiled evaluator | forced compaction + hallucinating compactor tests |
| 5 Model qualification | context tier × tool grade, behavioral suite, explicit decision UX | real HTTP fixture and external gateway run |
| 6 Product shell | Projects, project sessions, background jobs, Artifacts, settings, memory, usage, backup/import | direct process/cancel/backup E2E + browser flow |
| 7 Maintenance | stale/duplicate findings, schedules, recoverable CAS GC | policy/stale snapshot/quarantine/restore tests |

## Phase 3 — Skill learning lifecycle

```text
runtime evidence
   → validated trigger
   → persisted background review
   → candidate only
   → lint/security
   → exact baseline/candidate replay
   → explicit capability review when tools widen
   → human promotion
   → immutable active version
   → activation/outcome receipts
   → curator findings
   → improvement/archive/restore candidate
```

Replay reads deterministic `tests/*.json` fixtures from the Skill package. Every run binds candidate ID, revision, hash, base version and runner revision. It rejects a candidate when a formerly passing baseline fails, a base fixture is weakened, or evidence belongs to an older candidate revision. If an existing Skill has no explicit tests, an implicit manifest contract protects its name, description and declared tools.

The runtime closes unknown activation receipts once a turn succeeds or fails. Attribution distinguishes model-visible metadata from injected body and records relevant tool calls. Learning accepts only four trigger families with their evidence preconditions; retry/idempotency cannot create duplicate durable proposals.

## Phase 4 — Context fidelity and compaction

Profiles remain exactly selectable at 32,768, 65,536, 131,072, 262,144 and 1,048,576 tokens. 64k is the minimum target for Certified Agent Mode; 32k is a compact compatibility/stress envelope.

The compiler reserves output, estimator uncertainty and worst-case next tool burst before history. Pinned goal/criteria fail closed on overflow and tool call/result fragments move as one causal unit. A verifier wraps the compactor and accepts a checkpoint only when all factual statements carry known source markers and causal pairs are preserved. Failure routes to the deterministic extractive compactor, marked `verified-fallback`.

The fidelity lab compares full/warm and compiled context for Thai and English cases. It stores both evidence arms in content-addressed storage and reports exact essential retention, decision/open-task/file recall, causal splits, task/patch delta, hallucination, false success, token saving, compile time, heap delta, silent truncation and fallback use.

## Phase 5 — Runtime qualification

Qualification intentionally separates two axes:

| Axis | Values | Required evidence |
|---|---|---|
| Context tier | `compact-32k`, `certified-64k`, `extended-128k`, `extended-256k`, `ultra-1m`, `limited` | loaded runtime allocation plus long-context recall |
| Capability grade | A, B, C | tool/schema/recovery/deferred/cancellation behavior |

The suite measures connectivity, provider usage calibration, a long-context sentinel, Thai instruction against an English JSON schema, native/sequential/malformed-recovery/deferred tools, cancellation, foreground preemption, TTFT, total latency and throughput. If a requested profile is not eligible the report sets `requires_decision`; it never changes profile/provider/tool mode silently.

External gateway evidence on 22 August 2026 for `qwen3.8-27b-fp8`:

- connectivity passed with exact response;
- native Thai/schema tool call, malformed recovery, deferred tool, cancellation and preemption passed;
- sequential same-response tool calls failed, therefore grade B;
- remote gateway exposed no loaded-runtime allocation and its sentinel run did not pass, therefore context tier remained `limited` and Certified 64k required an explicit decision;
- observed connectivity TTFT was approximately 1.8 seconds in that run.

This result does not contradict a provider declaration of 128k; it says the evidence available to this harness did not certify the loaded allocation/recall contract.

## Phase 6 — Product shell

The local web product now has working navigation and APIs for:

- Chat with provider, exact context envelope and optional Project binding;
- Projects with canonical root, bounded file browsing and symlink/path escape rejection;
- background command jobs (labelled `Office` in the current UI) with persisted direct commands, progress/state, cancel and restart interruption recovery;
- Artifacts backed by CAS checksums, including terminal logs;
- Providers/MCP/Skills/Context/Fidelity control centers;
- non-secret JSON settings, explicit user/project memory and event-derived usage;
- backup export/download, checksum preview and candidate-only Skill import.

Background commands never invoke a shell. Executables are allowlisted; arguments are passed as an array; working directories stay inside the Project; environment exposure is minimal; deadline is 1–120 seconds; output is capped at 2 MiB; cancellation kills the Unix process group. The Go cache paths are derived explicitly so `go test` works without exposing the full parent environment. This is not an OS sandbox, so untrusted executables still require a VM/container/sandbox outside Hermetrix.

Backups use a versioned JSON envelope, payload checksum and per-blob SHA-256 verification. Preview reports name/scope conflicts. Apply restores blobs but creates Skill candidates only—never active versions and never an overwrite.

## Phase 7 — Curator and maintenance

Curator snapshots exact active versions and persists findings independently from retrieval relations. Duplicate proposals contain both version IDs, absorbed lineage, review steps and a replay plan. Stale scoring uses age/last-use and observed failure ratio; pinned and protected Skills are excluded. Curator has no mutation API.

Maintenance schedules persist interval, enabled state and idle/AC requirements. On macOS the detector reads HID idle time and power source conservatively; probe failure means “busy/on battery,” so a gated task is skipped. Foreground interaction remains independent.

CAS GC is recoverable:

```text
scan DB references + active CAS
  → persist exact snapshot revision and candidate list
  → user apply
  → rescan and reject stale snapshot
  → move exact objects to run-specific quarantine
  → optional checksum-verified restore
```

No GC path hard-deletes user data.

## Honest forward boundary

The vertical slices are complete, but the following remain future breadth rather than hidden placeholders:

- native desktop packaging/signing and managed browser workbench;
- OS-level process sandbox, network policy and generalized effect idempotency;
- live interjection and crash-resume during an in-flight model sample;
- MCP stdio, OAuth, resources, prompts, subscriptions and MRTR;
- exact model tokenizer plus runtime RAM/VRAM/OOM telemetry;
- semantic local-LLM compactor beyond the verified/fallback interface;
- cryptographic multi-user actor identity and authenticated non-loopback control API.

Any future feature must preserve the same authority boundaries and pass the same replay/fidelity/qualification gates before it can be described as complete.
