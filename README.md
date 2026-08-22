# Hermetrix Harness

![Hermetrix Engine](assets/brand/hermetrix-engine-v3-512.png)

Hermetrix is a local-first, provider-flexible agent harness built around three hard problems:

1. a reviewable, reversible learning lifecycle for skills;
2. token-efficient context compilation across 32k, 64k, 128k, 256k and 1M envelopes while keeping declared, probed and qualified context evidence distinct.
3. an auditable agent loop that freezes model, context and capabilities before every sampling step.

The implementation is original. The product requirements were informed by the research in [`../Hermetrix-research`](../Hermetrix-research/README.md), but no Aetox source code, assets, or branding are copied because the inspected Aetox snapshot is proprietary source-available.

The name reflects the architecture: a **hermetic core** for bounded local authority plus a **matrix** of versioned skills, tools, context and model capabilities. Product-owned identity files and their usage contract live in [assets/brand](assets/brand/README.md).

## What works in this foundation

- SQLite-backed skills, immutable versions, candidates, events, activations and archives
- content-addressed package/blob storage
- proposal-only create/improve/restore flow
- lint/check gate and optimistic base-version check before promotion
- reversible archive/restore without hard deletion
- provenance separated into origin and owner
- activation receipts that distinguish selection, injection and outcome
- duplicate/overlap candidate analysis with version-bound evidence
- persisted background reviews that yield to foreground inference and create candidates only
- versioned curator runs in report-only mode
- typed context fragments, 32k/64k/128k/256k/1M profiles and reserve-aware compilation
- deterministic deduplication, tool-output spill, structured checkpoints and pluggable semantic compaction
- runtime-allocation probes for Ollama, LM Studio, vLLM and llama.cpp
- provider registry with secret-by-environment references and OpenAI-compatible streaming
- append-only agent sessions, context snapshots and immutable step bindings
- an immutable session contract that freezes the provider revision, model, context profile, policy and capability revisions, Skill catalog, cache epoch and task budget when the session opens
- a persisted per-session turn lease so two concurrent requests cannot both commit a user message, with orphaned turns recovered on restart
- a task budget of model steps, tool calls, wall time and cumulative tokens instead of a hard-coded step limit
- a learning trigger outbox written in the same transaction as the turn commit and drained into idempotent review jobs
- bounded workspace read tools plus approval-gated atomic text writes with optimistic SHA-256 checks
- persisted one-shot effect grants, denial receipts and restart recovery that marks interrupted effects `uncertain` instead of retrying
- deferred capability catalog with fixed-size `tool_search`, `tool_describe` and `tool_call` prompt primitives
- MCP Streamable HTTP client for current stateless `2026-07-28` plus automatic legacy `2025-11-25` handshake fallback
- MCP pagination, JSON/SSE responses, request cancellation, timeout/error taxonomy, current `x-mcp-header` support and no automatic tool-call retry
- MCP connection/tool snapshot persistence with environment-only secrets, exact tool revisions, conservative risk classification and credential redaction
- host-side JSON Schema 2020-12 validation for arguments and structured output, with external schema references disabled
- deterministic active-Skill selection plus runtime activation receipts
- deterministic Skill replay (`tests/*.json`), baseline/candidate diff, regression gate and exact-revision capability widening approval
- automatic turn-outcome attribution plus validated learning triggers for milestones, corrections, explicit learn and Skill failure
- verified compactor wrapper with provenance/causal validation and deterministic extractive fallback
- bilingual full-vs-compiled context fidelity corpus and measured retention/delta/hallucination reports
- real model qualification suite separating allocated context tier from tool capability grade and refusing silent downgrade
- bounded Projects workbench, project-bound sessions, direct no-shell background commands, cancel/process-group cleanup and immutable command artifacts
- explicit user memory, non-secret settings, event-derived usage tracking and checksum-verified backup/import-as-candidate flow
- curator stale/duplicate findings with exact-version consolidation/replay plans and no mutation authority
- idle/AC-aware background maintenance schedules plus exact-snapshot CAS GC using recoverable quarantine instead of deletion
- local web control center for Chat, Projects, Office, Artifacts, Skills, Providers, MCP, Context, Fidelity and Maintenance

## Run

```bash
go run ./cmd/hermetrix serve --data ./.hermetrix --listen 127.0.0.1:7331
```

Then open <http://127.0.0.1:7331>.

Use `--workspace PATH` to choose the root exposed to bounded tools. Reads stay inside that root. `workspace.write_file` can replace one UTF-8 file or create one file in an existing directory, but every exact write pauses for approval in Chat and uses `expected_sha256` to reject stale changes.

The same root is registered as the initial Project. The Project workbench may start only an allowlisted executable (`go`, `git`, `node`, `npm`, `python3`, `rg`, `ls`) directly—never through a shell. Jobs have a bounded working directory, minimal non-secret environment, 1–120 second deadline, 2 MiB output ceiling, process-group cancellation and an immutable terminal-log artifact. This is process hardening, not an OS security sandbox; run untrusted code only inside a separate OS/container sandbox.

To seed an OpenAI-compatible provider without persisting its credential, place the credential in an environment variable and pass only its name:

```bash
go run ./cmd/hermetrix serve \
  --data ./.hermetrix \
  --provider-name "My Gateway" \
  --provider-base-url "https://gateway.example/v1" \
  --provider-model "my-model" \
  --provider-api-key-env HERMETRIX_PROVIDER_API_KEY \
  --provider-context 131072
```

Provider profiles may also be added from the Providers screen. The UI never accepts or returns the credential value. Remote providers require HTTPS; the Hermetrix control server refuses non-loopback listeners while authentication is not implemented.

MCP connections are managed from the MCP screen. Use a Streamable HTTP endpoint, store its bearer token in an environment variable, and enter only that variable name. Discovery is explicit and atomically replaces one server's catalog snapshot. By default Hermetrix treats all MCP annotations as untrusted, so remote calls require approval; enable annotation trust only for a server whose behavior you control or have audited.

The Providers screen exposes a behavioral qualification suite. Remote gateway metadata can test tools, cancellation, recall and latency, but it cannot certify local allocation. `Certified 64k` requires a verified loaded-runtime probe; missing or failed evidence produces an explicit decision report rather than changing the selected profile silently.

## Test

```bash
go test ./...
go test -race ./...
go vet ./...
node --check internal/web/ui/app.js
```

For deterministic manual/E2E MCP QA, run `python3 scripts/e2e/mcp_fixture.py` on loopback and connect the MCP screen to `http://127.0.0.1:18444/mcp`.

## Known gaps

This is a vertical slice, not a finished product. Kernel correctness is closed and every claim behind it is
mutation-tested — disabling a guard turns its test red. The gaps that matter most right now:

- Skill bodies now arrive through `skill_search` / `skill_view` as tool results, but the `no_skill_requested_rate` metric that would show whether small models actually reach for them does not exist yet;
- token estimation still has no exact per-model tokenizer, so budget numbers carry a calibrated error band rather than an exact one;
- long-context recall now probes five positions across the envelope, but only against fixtures; no real local model has been qualified at 128k or above;
- there is no OS-level sandbox, no authenticated principal and no managed browser or native shell.

Each gap has an ID, evidence down to file and line, and a mitigation phase. See [docs/AETOX-HERMES-TRACEABILITY-AUDIT.md](docs/AETOX-HERMES-TRACEABILITY-AUDIT.md) section 4.2 and the risk register in [docs/FUTURE-ARCHITECTURE-PLAN.md](docs/FUTURE-ARCHITECTURE-PLAN.md).

## Documentation map

| Document | Role |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | current implementation and safety contracts |
| [docs/FUTURE-ARCHITECTURE-PLAN.md](docs/FUTURE-ARCHITECTURE-PLAN.md) | **forward source of truth** — ADRs, open findings, phases, risk register |
| [docs/AETOX-HERMES-TRACEABILITY-AUDIT.md](docs/AETOX-HERMES-TRACEABILITY-AUDIT.md) | source-to-Hermetrix comparison against the Aetox and Hermes snapshots |
| [docs/DECISIONS.md](docs/DECISIONS.md) | ADR ledger — status and implementation evidence per decision |
| [docs/ROADMAP.md](docs/ROADMAP.md) | historical phase ledger through 2026-08-21 |
| [docs/REVIEW.md](docs/REVIEW.md) | multi-pass review log kept during the build |
| [docs/PHASE-COMPLETION.md](docs/PHASE-COMPLETION.md) | Phase 0–7 completion report |
| [`../Hermetrix-research`](../Hermetrix-research/README.md) | research and requirement source; not updated to track this implementation |

When any document disagrees with the runtime, the runtime is the truth and the document is the bug.
