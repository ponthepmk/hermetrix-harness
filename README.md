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
- learning digests that distinguish measured command success (`verified_by`) from an outcome merely claimed in conversation
- bounded workspace read tools plus approval-gated atomic text writes with optimistic SHA-256 checks
- persisted one-shot effect grants, denial receipts and restart recovery that marks interrupted effects `uncertain` instead of retrying
- deferred capability catalog with fixed-size `tool_search`, `tool_describe` and `tool_call` prompt primitives
- a 13-tool direct waist: four bounded file tools, four Skill/context tools, three deferred-catalog tools, `workspace.run` and `browser`
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
- clean-room cockpit shell with a project picker, three independently resizable and collapsible zones, four views (Chat, Work, Code, Knowledge) and per-project per-view layout memory
- optimistic project file editor with bounded diff and immutable write receipt
- persisted real PTY sessions on macOS/Linux, bounded output tail, resize/input/interrupt/close and honest interrupted recovery
- managed Chrome/Chromium tabs through DevTools (not an iframe), bounded untrusted DOM snapshots, numbered click/type references, screenshot artifacts and explicit private/local URL opt-in
- native DOCX/XLSX/PPTX package generation with Unicode, deterministic package contents and provenance; PDF generation fails closed when the built-in Basic-Latin font cannot represent the text
- reusable/editable Agent Team rosters with one explicit lead, UI-authored validated DAGs, up to four scheduled children, independent child Session Contracts, frozen run/member instruction snapshots, durable exact-effect approval pause/resume without prompt replay, parent cancellation propagation, labelled untrusted peer evidence and aggregated token/provenance tracking

## Run

```bash
go run ./cmd/hermetrix serve --data ./.hermetrix --listen 127.0.0.1:7331
```

Then open <http://127.0.0.1:7331>. `serve` is a local web server and the
terminal stays attached to it until `Ctrl+C`. Add `--open` to launch the
control center in your default browser once the listener is up.

Add `--desktop` instead to open it as its own application window. Hermetrix
launches the first installed Chromium-family browser it finds — Chrome,
Chromium, Edge or Brave on macOS, Chrome or Edge on Windows, and whatever is on
`PATH` elsewhere — with `--app`, its own `--user-data-dir` under the data
directory, and no browser chrome. The window never touches your everyday
browser profile, and closing it leaves the server running so an in-flight turn
or background job is not cancelled; reopen it at the URL, or stop the server
with `Ctrl+C`. With no such browser installed, `--desktop` logs why and falls
back to `--open`.

The cockpit is sized for both a laptop and a desktop display. Density follows
the window and can be pinned with the toolbar toggle or `⌘K → Toggle density`;
`⌘K` (`Ctrl+K`) opens the command palette for every page, workbench room and
action.

Inside a session, **Skills & tools** (or `@` in the composer) opens a
session-aware capability picker. Skill insertion is bound to the exact Skill
ID and version frozen in that Session Contract; a Skill promoted later is
refused until a new session is opened. Direct tools retain their approval
policy, while an MCP choice inserts the auditable
`tool_search → tool_describe → tool_call` path. The Review workbench shows the
current model, context envelope, project, Skill selection, direct-tool count,
pending approvals and contract revisions beside the conversation.

Use `--workspace PATH` to register the initial Project. Every session freezes its
selected Project, and bounded file, command and browser-file operations resolve
against that Project's root rather than a process-global directory. Reads stay
inside the root. `workspace.write_file` can replace one UTF-8 file or create one
file in an existing directory, but every exact write pauses for approval in Chat
and uses `expected_sha256` to reject stale changes.

The same root is registered as the initial Project. The Project workbench may start only an allowlisted executable (`go`, `git`, `node`, `npm`, `python3`, `rg`, `ls`) directly—never through a shell. Jobs have a bounded working directory, minimal non-secret environment, 1–120 second deadline, 2 MiB output ceiling, process-group cancellation and an immutable terminal-log artifact. This is process hardening, not an OS security sandbox; run untrusted code only inside a separate OS/container sandbox.

### Projects and views

A **project** is the root everything else hangs off — sessions, files,
background jobs, artifacts, team runs. A project does not need a code folder:
creating one from the picker with an empty root path is a valid, permanent
state, not a step waiting to be finished. The picker is the first screen for
exactly this reason, and the project chip in the header returns to it at any
time.

The shell itself is three zones — rail, main and side — each dragged from its
own handle rather than fixed at a width someone guessed once. Either the rail
or the side pane can be collapsed independently, and every one of the four
views (**Chat**, **Work**, **Code**, **Knowledge**) remembers its own zone
widths and collapsed state per project, restored the next time that project
opens onto that view.

Chat is the one view built today; Work and Knowledge are still specs and say
so on screen rather than opening onto something blank. Code is where the main
area splits instead of staying one surface: up to four panes, each showing
files, a real terminal, the managed browser or background job output,
reassignable and independently maximisable from its own header. A project
remembers exactly which panes were open and which one was maximised, the same
way it remembers zone widths — the file tree that will eventually sit in
Code's own rail is still a spec too.

### Connecting a model

The quickest path is the **Models** screen: name the endpoint, give it a model
ID, paste the API key, press *Connect model*. The key takes effect immediately —
there is nothing to export in your shell and nothing to restart. A local runtime
that needs no key can leave the field empty.

The key is written to `secrets.json` in the data directory with owner-only
permissions (`0600`). It never enters SQLite, a backup export, a log line or any
API response; the API reports only whether a credential is present. MCP bearer
tokens work the same way on the **Tool Center** screen.

An environment variable is still supported and is checked whenever no key has
been saved — use it when a process manager or secret manager injects the value.
To seed a provider that way at startup, pass only the variable name:

```bash
go run ./cmd/hermetrix serve \
  --data ./.hermetrix \
  --provider-name "My Gateway" \
  --provider-base-url "https://gateway.example/v1" \
  --provider-model "my-model" \
  --provider-api-key-env HERMETRIX_PROVIDER_API_KEY \
  --provider-context 131072
```

A saved key takes precedence over the variable. Remote providers require HTTPS; the Hermetrix control server refuses non-loopback listeners while authentication is not implemented.

MCP connections are managed from the Tool Center screen, and a server can run either way:

- **A program on this machine (stdio).** How almost every published MCP server ships. Give the command that starts it, for example `npx -y @modelcontextprotocol/server-everything`. The launcher must be one of `npx`, `node`, `bun`, `deno`, `uv`, `uvx`, `python`, `python3`, `docker`, `go`; it is executed directly rather than through a shell, and it sees only `PATH`, `HOME`, locale and its own token, not the rest of this process's environment.
- **A URL (Streamable HTTP).** Paste the bearer token, or put it in an environment variable and name it under Advanced.

Discovery indexes all three catalogs a server publishes: its **tools**, its
**resources** (data it can hand over) and its **prompt templates**. A server
that implements only tools answers "method not found" for the other two, which
is reported as none rather than as a failure. All three reach the model through
the same deferred catalog, so a large server costs no prompt tokens until the
model searches for something in it.

A stdio server is launched once and kept, not relaunched per call, so a local
server does not pay its startup on every request. An idle server is stopped
after five minutes, changing a server's settings starts a fresh process rather
than reusing the old one, and timeout/cancellation kills and discards the whole
session so it cannot wedge later calls. Catalog-list requests may reconnect once
because they are read-only; `tools/call`, resource reads and prompt rendering
are never replayed after an uncertain connection failure. The next distinct
request starts a fresh process. Discovery is explicit and atomically replaces one server's catalog snapshot. By default Hermetrix treats all MCP annotations as untrusted, so remote calls require approval; enable annotation trust only for a server whose behavior you control or have audited.

The Models screen exposes a behavioral qualification suite. Remote gateway metadata can test tools, cancellation, recall and latency, but it cannot certify local allocation. `Certified 64k` requires a verified loaded-runtime probe; missing or failed evidence produces an explicit decision report rather than changing the selected profile silently.

### The composer

**Enter** sends. **Shift+Enter** adds a line, and Cmd/Ctrl+Enter still sends.
**Escape** clears a draft you have not sent. **Up** on an empty box brings back
your last message so you can fix it rather than retype it. **@** opens the
Skill and tool picker. The box grows with what you write and scrolls past 40%
of the window.

A turn re-renders the conversation on every streamed token, so the composer
keeps your draft, your caret and your focus across those renders: you can keep
typing your next message while the current answer is still arriving.

### When a tool server asks you something

MCP is two-way. A server can stop in the middle of a tool call to ask Hermetrix
to run a model for it (**sampling**) or to ask you a question (**elicitation**).
Both spend something of yours, so neither is answered by default: a server has
to have **Trust this server's risk annotations** turned on in the Tool Center
first. Anything else gets a plain refusal that names that setting, which
conformant servers handle.

For a trusted server:

- **Sampling** runs on the session's own provider, capped at 1024 tokens and
  four requests per tool call. The server's text goes in as user content under
  a line saying which server it came from, so a server cannot promote its own
  words into instructions Hermetrix must follow.
- **Elicitation** puts the question in the conversation with the server named,
  and the fields come from the schema the server asked for. The call waits three
  minutes for you. Declining tells the server you said no; letting it expire
  tells it the question was cancelled, which is a different fact.

This works on stdio servers. A Streamable HTTP server cannot receive either
today, and is told so rather than left waiting.

### Skills the agent writes

Hermetrix writes Skills as it works. `skill_manage` lets the agent record a
procedure that just worked, or improve one it loaded with `skill_view` in the
same session; improving requires the exact version it read, so it cannot
overwrite text it has not seen.

The shipped authority policy promotes what the agent writes, rather than
holding every one for approval. What keeps that reviewable is the record: each
automatic promotion is an authority action, the Skill appears in Skill Studio
marked **promoted by agent** with the date and the policy revision, and the row
carries an **Undo this promotion** button. Automatic *archiving* stays off, and
nothing is ever hard-deleted. To hold every promotion for review instead, set
Skill authority to Manual on the Skill Studio screen.

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

- no real local model has been run against `no_skill_requested_rate` yet, so whether small models actually reach for `skill_search` is measurable but unmeasured;
- token estimation still has no exact per-model tokenizer, so budget numbers carry a calibrated error band rather than an exact one;
- long-context recall now probes five positions across the envelope, but only against fixtures; no real local model has been qualified at 128k or above;
- there is no OS-level sandbox, authenticated principal, signed native desktop package or Windows ConPTY implementation; the current cockpit is a loopback web app and Windows builds report PTY unavailability explicitly;
- managed browser automation requires an installed Chrome/Chromium and its host/DNS guard is not yet a full egress proxy against DNS rebinding;
- native PDF output currently supports printable Basic Latin only; use DOCX/PPTX for Thai and other Unicode scripts until a redistributable embedded-font pipeline is selected.

Each gap has an ID, evidence down to file and line, and a mitigation phase. See [docs/AETOX-HERMES-TRACEABILITY-AUDIT.md](docs/AETOX-HERMES-TRACEABILITY-AUDIT.md) section 4.2 and the risk register in [docs/FUTURE-ARCHITECTURE-PLAN.md](docs/FUTURE-ARCHITECTURE-PLAN.md).

## Documentation map

| Document | Role |
|---|---|
| [docs/HANDOVER.md](docs/HANDOVER.md) | **start here on a new machine** — what is done, what is blocked, what is missing, and how to run it |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | current implementation and safety contracts |
| [docs/FUTURE-ARCHITECTURE-PLAN.md](docs/FUTURE-ARCHITECTURE-PLAN.md) | **forward source of truth** — ADRs, open findings, phases, risk register |
| [docs/AETOX-HERMES-TRACEABILITY-AUDIT.md](docs/AETOX-HERMES-TRACEABILITY-AUDIT.md) | source-to-Hermetrix comparison against the Aetox and Hermes snapshots |
| [docs/DECISIONS.md](docs/DECISIONS.md) | ADR ledger — status and implementation evidence per decision |
| [docs/ROADMAP.md](docs/ROADMAP.md) | historical phase ledger through 2026-08-21 |
| [docs/REVIEW.md](docs/REVIEW.md) | multi-pass review log kept during the build |
| [docs/PHASE-COMPLETION.md](docs/PHASE-COMPLETION.md) | Phase 0–7 completion report |
| [`../Hermetrix-research`](../Hermetrix-research/README.md) | research and requirement source; not updated to track this implementation |

When any document disagrees with the runtime, the runtime is the truth and the document is the bug.
