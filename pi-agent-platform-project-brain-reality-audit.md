# Pi Agent Platform + Project Brain — Architecture Reality Audit

> Historical pre-rollout snapshot. The live services were changed after this
> read-only audit. See `docs/project-brain-rollout-review.md` for the deployed
> state and post-change acceptance evidence.

**Observed:** 2026-09-24, Asia/Bangkok. **Method:** read-only inspection of the running Raspberry Pi via Raspberry Pi Connect remote shell; live MCP `tools/list` and `tools/call` reads; read-only SQLite queries; source inspection at the exact deployed Git commits; inspection of the running Hermetrix application on the Windows client. No service, schema, credential, Markdown knowledge page, deployment, or Pi source was deliberately changed. **Caveat:** the read-only retrieval implementation refreshes its disposable SQLite search cache, and that cache is tracked by the vault Git repository. The final vault status showed three modified `.second-brain/search.sqlite3*` cache files; no Markdown page was modified. The cache status had also been non-clean during the audit, so the exact portion attributable to these probes cannot be isolated.

**Evidence baseline:** Pi `kanban` at `d68bdd3921d4555f7218a280ee62c9b5e6b78de0`, `second-brain-mcp` at `7c55ce1c9bb670e9bbbe5a9145ab4a5908f41643`; Windows Hermetrix checkout at `2ef6b7e0f4916cd25b7bb91c10ef4d3368d7768a`. Source paths cited below are paths on the Pi unless prefixed with `D:\`. The source repos on Pi were clean. The audit tested behavior and inspected code; it did not rely on architectural prose as proof.

## 1. Executive summary

The Pi **does run two useful, independent services**, but it does **not yet implement the full shared Agent Platform + Project Brain learning loop**. `agent-knowledge.go2gether-tech.online/mcp` is the Kanban/Agent Platform MCP wrapper on port 8902, whereas `secondbrain.go2gether-tech.online/mcp` is the Second Brain MCP server on port 8765. The former exposes planning/work and identity tools; it exposes **no knowledge retrieval or knowledge-candidate tools**. Hermetrix's live `agent-knowledge` profile points to Kanban and reports 21 discovered tools. The Second Brain server has 22 tools and can retrieve versioned, sourced pages, but is not yet connected to Codex or Hermetrix as their shared Project Brain path.

The key security finding is **query scoping is not authorization**. Second Brain accepts caller-supplied `project`, and its `read_page` and `list_pages` APIs do not bind a page to an authorized project. A live read using the configured legacy Bearer credential returned a draft belonging to `cop-svt-svtap-api` by path despite the caller having no project-specific authorization claim. The running Second Brain process has `SECOND_BRAIN_TOKEN` configured and no `SECOND_BRAIN_CLIENTS` map; that legacy token grants read, write, and curate in `second_brain/access.py`. It is unsuitable as a shared credential among separate company, client, and personal agents.

Retrieval is real but limited: seven global pages met its current `active` + source + medium/high confidence readiness rule, while most indexed pages were unknown or draft. It preserves SHA-256 page versions, source strings, line coordinates, and a byte budget. It uses FTS/trigrams and lexical passage scoring, with no embedding search, task-aware reranker, platform identity, verified-evidence gate, or exposed ranking scores. A live exact-query probe returned a less relevant watchdog page before the state-database recovery page. This is a useful substrate, not evidence that a Context Gateway selects the *best* knowledge for every agent/task.

**Assessment:** `PROJECT_BRAIN_STATUS: PARTIAL`. Shared read retrieval and draft capture exist; safe cross-tenant sharing and the verified Codex→Pi→Bonsai learning loop do not.

## 2. Actual topology

| Runtime element | Observed production state | Evidence |
|---|---|---|
| `kanban-board.service` | Running Flask UI/API on `0.0.0.0:8901`; `/home/pizp2e0/workspace/kanban/kanban.py`; SQLite `kanban.db` | `systemctl --user`, `ss`, process and schema queries |
| `kanban-mcp.service` | Running stateless streamable HTTP on `0.0.0.0:8902/mcp`; `kanban_mcp.py` forwards to port 8901 | unit and `kanban_mcp.py:1-100` |
| `second-brain-mcp.service` | Running stateless streamable HTTP on `0.0.0.0:8765/mcp`; `/home/pizp2e0/projects/second-brain-mcp/server.py` | unit, process environment and `ss` |
| `hermes-gateway.service` | Running Hermes agent gateway, separate from the two MCP servers | user unit and process list |
| `cloudflared.service` | Running system service; ingress `agent-knowledge.go2gether-tech.online`→8902, `secondbrain.go2gether-tech.online`→8765, `kanban.go2gether-tech.online`→8901 | `/etc/cloudflared/config.yml` and active service |
| `tailscaled.service`, `ssh.service` | Running; access infrastructure, not Platform Router | system service list |
| Timers | `second-brain-backup.timer` daily; `hermes-gateway-restart.timer` weekly | `systemctl --user list-timers --all` |
| Pi repositories | Kanban and Second Brain above; Hermes agent at `/home/pizp2e0/.hermes/hermes-agent` (`17b5df02f2`) | Git and unit paths |
| Second Brain authoritative store | Markdown/Git vault `/home/pizp2e0/Documents/Obsidian Vault/SecondBrain`; disposable `.second-brain/search.sqlite3` search cache | `server.py:70-100`, `second_brain/retrieval.py:108-195`; live filesystem/SQLite |
| Second Brain session store | Code targets `.second-brain-state/state.sqlite3`; file **absent at audit time** | `second_brain/sessions.py:25-45`; `test -e` |

Kanban's live schema migration is **v9**. It contains **0** `workspaces`, **0** `platform_projects`, **0** `platform_services`, **3** nodes, **3** agents, **4** agent credentials, **4** repositories, **7** worktrees, **2** Kanban projects and **5** tasks. `agent_capabilities` has 4 rows. The three registered agents are `codex-windows` (simple), `hermetrix-bonsai` (managed), and `operator-pi` (simple). These are registry identities, not evidence of a running Platform Router or managed dispatch. `task_repo_scopes` has zero rows at the audit snapshot.

Hermes' local `/home/pizp2e0/.hermes/config.yaml` has an `mcp_servers.second-brain` entry executing the Second Brain Python server over **stdio** on the Pi. That is a local trusted-client route, separate from the public tunnel route. There was no running Pi `context-gateway` or Platform Router unit/process. No cron entry related to this stack was found; the observed scheduling is through the two user timers.

## 3. MCP inventory and transport/authentication

The **live** Second Brain `tools/list` returned the following 22 names. `R` means read-scope; `W` write-scope; `C` curate-scope. On the running HTTP service all are effectively reachable with the configured legacy token because its compatibility fallback does not restrict scope. The code has a scoped-client path, but `SECOND_BRAIN_CLIENTS` was absent from the running process environment. Source for every row is `server.py` at the cited tool definition; all were advertised by live `tools/list`, but **only read tools were invoked** in this audit. “Working” for a write row means advertised/source-present, not a production write-test.

| Service | Actual tool | R/W | Auth | Purpose | Implementation | Currently working? |
|---|---|---|---|---|---|---|
| Second Brain | `search` | R | Brain Bearer `read` | Human-readable lexical matches, drafts opt-in | `server.py:502` | Live read tested |
| Second Brain | `get_context` | R | Brain Bearer `read` | Compact curated evidence pack | `server.py:525` | Live read tested |
| Second Brain | `get_context_multi` | R | Brain Bearer `read` | Multiple related queries under one byte budget | `server.py:541` | Advertised; source inspected |
| Second Brain | `save_session` | W | Brain Bearer `write` | Actor-owned working-state snapshot | `server.py:554` | Advertised; no live session DB yet |
| Second Brain | `resume_session` | R | Brain Bearer `read` | Restore actor-owned snapshot | `server.py:568` | Advertised; no live snapshot shown |
| Second Brain | `list_sessions` | R | Brain Bearer `read` | List actor-owned snapshots | `server.py:580` | Advertised; no live snapshot shown |
| Second Brain | `refresh_search_index` | W | Brain Bearer `write` | Refresh derived SQLite index | `server.py:587` | Advertised; not invoked |
| Second Brain | `read_passage` | R | Brain Bearer `read` | Digest/version-bound exact lines | `server.py:603` | Advertised; source inspected |
| Second Brain | `read_page` | R | Brain Bearer `read` | Whole Markdown page by vault path | `server.py:628` | **Live read tested; project bypass demonstrated** |
| Second Brain | `list_pages` | R | Brain Bearer `read` | Enumerate pages including raw | `server.py:641` | Advertised; source inspected |
| Second Brain | `index` | R | Brain Bearer `read` | Wiki index | `server.py:663` | Advertised; source inspected |
| Second Brain | `get_schema` | R | Brain Bearer `read` | Vault conventions/schema document | `server.py:671` | Advertised; source inspected |
| Second Brain | `history` | R | Brain Bearer `read` | Vault Git history | `server.py:679` | Advertised; source inspected |
| Second Brain | `capture` | W | Brain Bearer `write` | Free-form draft observation | `server.py:695` | Advertised; no write-test |
| Second Brain | `record_lesson` | W | Brain Bearer `write` | Structured draft lesson | `server.py:743` | Advertised; no write-test |
| Second Brain | `record_use` | W | Brain Bearer `write` | Version-bound reuse feedback draft | `server.py:765` | Advertised; no write-test |
| Second Brain | `ingest` | C | Brain Bearer `curate` | Directly create active curated page | `server.py:792` | Advertised; no write-test |
| Second Brain | `promote` | C | Brain Bearer `curate` | Move raw draft into curated category | `server.py:853` | Advertised; no write-test |
| Second Brain | `update_page` | C | Brain Bearer `curate` | Update page body | `server.py:928` | Advertised; no write-test |
| Second Brain | `set_frontmatter` | C | Brain Bearer `curate` | Change page metadata/status/project | `server.py:988` | Advertised; no write-test |
| Second Brain | `rebuild_index` | W | Brain Bearer `write` | Rebuild wiki index | `server.py:1098` | Advertised; no write-test |
| Second Brain | `append_log` | W | Brain Bearer `write` | Append audit log | `server.py:1177` | Advertised; no write-test |

The Kanban MCP server advertises **21** tools through `agent-knowledge.go2gether-tech.online/mcp`. Nine read tools are `list_projects`, `list_tasks`, `get_brief`, `get_roadmap`, `get_calendar`, `whoami`, `list_capabilities`, `get_node`, `get_agent`; `list_subtasks` is also read. Eleven write tools are `add_task`, `update_task`, `complete_task`, `delete_task`, `add_project`, `update_project`, `delete_project`, `update_task_ai`, `add_subtask`, `complete_subtask`, `delete_subtask`. All are implemented in `kanban_mcp.py:101-301` as wrappers to `kanban.py` REST endpoints. Generic list/brief reads are open; identity reads forward a scoped agent Bearer token; writes forward the caller's token to the board, which checks scopes. Trusted local stdio can use the legacy Kanban write key. The source's top-level description saying all reads are open should not be mistaken for proof that identity reads work anonymously. Live Hermetrix GET `/api/mcp/servers` reported this endpoint `ready`, credential stored, 21 tools, last discovered `2026-09-23T16:26:46Z`. No `knowledge.*`, `get_context`, `capture`, or `promote` is present in this service.

The following Kanban rows were independently confirmed by live `tools/list` on port 8902. `Open` means the underlying general read endpoint is anonymous; `Platform read` means a scoped Platform Bearer credential is required; `Platform write` means a scoped credential is forwarded and checked by Kanban, with a separate trusted-stdio legacy path. `Advertised` does **not** claim a mutating tool was invoked during this audit.

| Service | Actual tool | R/W | Auth | Purpose | Implementation | Currently working? |
|---|---|---|---|---|---|---|
| Kanban MCP | `list_projects` | R | Open | List board projects | `kanban_mcp.py:102` | Advertised; UI/API live |
| Kanban MCP | `list_tasks` | R | Open | List board tasks | `kanban_mcp.py:108` | Advertised; UI/API live |
| Kanban MCP | `get_brief` | R | Open | Daily board brief | `kanban_mcp.py:115` | Advertised; UI/API live |
| Kanban MCP | `get_roadmap` | R | Open | Project/task timeline | `kanban_mcp.py:121` | Advertised; UI/API live |
| Kanban MCP | `get_calendar` | R | Open | Due-date calendar | `kanban_mcp.py:127` | Advertised; UI/API live |
| Kanban MCP | `whoami` | R | Platform credential | Caller identity/scopes | `kanban_mcp.py:135` | Advertised; source inspected |
| Kanban MCP | `list_capabilities` | R | Platform read | Caller capability metadata | `kanban_mcp.py:144` | Advertised; source inspected |
| Kanban MCP | `get_node` | R | Platform read | Node registry lookup | `kanban_mcp.py:152` | Advertised; source inspected |
| Kanban MCP | `get_agent` | R | Platform read | Agent registry lookup | `kanban_mcp.py:159` | Advertised; source inspected |
| Kanban MCP | `add_task` | W | Platform write | Create task | `kanban_mcp.py:169` | Advertised; not invoked |
| Kanban MCP | `update_task` | W | Platform write | Edit task | `kanban_mcp.py:179` | Advertised; not invoked |
| Kanban MCP | `complete_task` | W | Platform write | Complete task | `kanban_mcp.py:199` | Advertised; not invoked |
| Kanban MCP | `delete_task` | W | Platform write | Delete task | `kanban_mcp.py:208` | Advertised; not invoked |
| Kanban MCP | `add_project` | W | Platform write | Create board project | `kanban_mcp.py:217` | Advertised; not invoked |
| Kanban MCP | `update_project` | W | Platform write | Edit board project | `kanban_mcp.py:227` | Advertised; not invoked |
| Kanban MCP | `delete_project` | W | Platform write | Delete board project | `kanban_mcp.py:245` | Advertised; not invoked |
| Kanban MCP | `update_task_ai` | W | Platform write | Edit task AI notes/state | `kanban_mcp.py:256` | Advertised; not invoked |
| Kanban MCP | `list_subtasks` | R | Open | List task subtasks | `kanban_mcp.py:271` | Advertised; UI/API live |
| Kanban MCP | `add_subtask` | W | Platform write | Create subtask | `kanban_mcp.py:277` | Advertised; not invoked |
| Kanban MCP | `complete_subtask` | W | Platform write | Mark subtask done/undone | `kanban_mcp.py:286` | Advertised; not invoked |
| Kanban MCP | `delete_subtask` | W | Platform write | Delete subtask | `kanban_mcp.py:295` | Advertised; not invoked |

`gateway_server.py` defines another MCP app named `context-gateway` with `ctx_analyze_file`, `ctx_run_profile`, `ctx_search`, `ctx_read`, `ctx_stats`, `ctx_route` (`gateway_server.py:1-90`). It is designed to run **on an agent's own machine via stdio** as a local log/output filter; no Pi unit or live `tools/list` for it was found. It is not the live Project Brain context selector and not a remote Pi MCP server.

## 4. Second Brain reality and storage

Second Brain is both **structured Markdown knowledge** and a **raw draft inbox**; it is not merely a conversation dump. Curated pages use frontmatter `title`, `created`, `updated`, `type`, `tags`, `sources`, `confidence`, `author`, `status`, `project`, plus Markdown body. Git commits are the write audit/version history; retrieval returns a SHA-256 content version and line references. The separate SQLite FTS/ngram database is a disposable search cache, not the source of truth (`server.py:310-322`, `second_brain/retrieval.py:108-195`).

The live vault had **52 Markdown files** and **49 indexed retrieval files** at the snapshot: 7 global `active/ready`, 5 global `draft`, 34 global `unknown`, and 3 `cop-svt-svtap-api` `draft`. The earlier query taken while indexing showed 2 project drafts; the later stable query showed 3, illustrating that the derived index can change during refresh without an authoritative knowledge write. Seven ready pages are global; none is project-ready. One page's `sources` field may point to raw conversation material, but the curated page itself is structured. We did not inspect private body content beyond what was necessary to assess retrieval.

| Intended knowledge kind | Support today | Grounded interpretation |
|---|---|---|
| ARCHITECTURE_DECISION | PARTIAL | Can be written as a page, but no decision ID/status/supersession model or typed query. |
| CONSTRAINT | PARTIAL | Can appear in a page/body; no typed constraint field or enforcement. |
| PROCEDURE | IMPLEMENTED | Curated pages and `record_lesson`'s Solution/Applicability/Verification sections support procedures. |
| FAILURE_PATTERN | PARTIAL | Existing recovery pages and draft lessons can describe failures; no typed failure signature. |
| KNOWN_FIX | PARTIAL | Solution sections are retrievable; no verified fix object bound to test results. |
| ARCHITECTURE_FACT | PARTIAL | Curated facts exist as prose; freshness/validity is not mechanically checked. |
| PROJECT_CONVENTION | PARTIAL | `project` frontmatter exists, but no scope authorization or typed convention lifecycle. |
| VERIFICATION_EVIDENCE | PARTIAL | Source strings and Verification prose exist; no immutable test/effect evidence object validation. |
| LESSON | IMPLEMENTED | `record_lesson` creates a structured **draft** without transcript dumping. |
| HISTORICAL_KNOWLEDGE | IMPLEMENTED | Raw pages, unknown pages, Git history and direct page reads persist older material. |

“Implemented” in this table means representable and present in code, **not** safe to treat as automatically verified.

## 5. Knowledge schema/types

The real ontology is five curated folders (`entities`, `concepts`, `comparisons`, `queries`, `reference`) plus `raw`, and a frontmatter `type` such as entity/concept/comparison/query/summary/note. It does **not** implement the requested architecture-decision, constraint, failure/fix or verification-evidence types as first-class entities. Pages are versioned by content SHA-256 at retrieval time and by Git history. There is no stable Project Brain knowledge ID beyond path + content digest, no immutable evidence reference schema linked to Pi Work State, and no separate validation record. `SCHEMA.md` is a vault convention document exposed via `get_schema`, not an enforced platform-wide typed graph.

## 6. Knowledge lifecycle

The implemented lifecycle is `capture`/`record_lesson` → raw `draft` → manual `promote` with a `curate` credential → active curated page → `get_context`; `set_frontmatter` can mark `draft`, `deprecated`, or `revoked` (`server.py:695-925,988-1095`). Retrieval excludes `deprecated`, `revoked`, and `rejected`; `get_context` further requires active, non-draft, at least one source, and medium/high confidence (`second_brain/retrieval.py:154-263`). `record_use` creates version-bound feedback as another draft and does not itself promote or alter confidence.

The source validator checks source *form* and, for a raw vault path or Git ref, existence; an HTTPS URL is accepted as a source string. `record_lesson` requires human-readable Verification text and source input, but there is no machine check that a test passed, that the evidence belongs to the same repo/revision, that the solution works, or that a curator is a distinct human. `promote` may carry `low` confidence and still set status `active` (it then fails the ready-only retrieval filter). `ingest` can create active knowledge directly with `curate` scope. The `author` argument is overwritten with the authenticated actor for writes; provenance strings and Git commits are kept, but source truth is not independently established. No conflict detector, globally unique knowledge key, explicit `supersedes` link, or automatic stale invalidation was found. Multiple conflicting pages can coexist; the lexical ranker may return either. The actual state names are `active`, `draft`, `deprecated`, `revoked` (plus legacy/unknown in indexed data and `rejected` filtered by retrieval code); there is no first-class `verified` state.

## 7. Context Gateway reality

The **actual live Project Brain retrieval entry point is Second Brain `get_context`/`get_context_multi`**, not a separately deployed Context Gateway. Its path is: MCP request → `server.py` → `second_brain/retrieval.py` → vault/SQLite FTS and trigrams → lexical passage scoring/filtering → JSON evidence pack → caller. `gateway_server.py` is a local output/log filter with a misleadingly similar name and is not installed as a Pi retrieval router.

| Required behavior | Status | Evidence / limit |
|---|---|---|
| Project scoping | PARTIAL | Exact `project` string plus global pages, supplied by caller; not an ACL. |
| Workspace/repository/task/session/agent scoping | MISSING for knowledge | No such selectors in `get_context`; session snapshots are a separate actor-owned store. |
| Semantic retrieval | MISSING | No embeddings/vector search in Pi Second Brain retrieval. |
| Keyword retrieval | IMPLEMENTED | SQLite FTS5 + trigram candidates and lexical passages. |
| Hybrid retrieval/reranking | PARTIAL | FTS + trigrams and internal lexical score; no semantic ranker/task relevance. |
| Deduplication | PARTIAL | Identical normalized passage text is deduplicated; at most 2 passages/page; overlapping facts remain. |
| Freshness/version filtering | PARTIAL | File signature check and content digest; no age/commit/applicability decay. |
| Lifecycle filtering | IMPLEMENTED for `get_context` | Only active/sourced/medium-high ready pages; `search(include_drafts=true)` intentionally exposes drafts. |
| Token/context budgeting | PARTIAL | Exact UTF-8 byte budget 1024–24000, not model token budget or cross-source context assembly. |
| Provenance preservation | PARTIAL | `sources`, path, line range, digest, status and confidence returned; quality of source not verified. |

The internal passage score is calculated but **not emitted** in `get_context` items, so consumers cannot inspect confidence of ranking. The `trust` field reminds clients to treat source material as data, not instructions; it is advisory, not a factual-verification result.

## 8. HOT / WARM / COLD ownership

**HOT** current goal, task state, errors and recent actions properly remain with each agent/Hermetrix durable task engine, CompactState, StepPacket and local context compiler. Pi need not own Hermetrix CompactState. Second Brain's session snapshot API could hold a small actor-owned continuity summary, but its state DB was absent at audit time; no evidence of a live shared session-context router was found.

**WARM** curated architecture, procedures, constraints and fixes are the right job for Second Brain. It stores some of these and retrieves seven ready global pages; no platform-aware assembly into a task-specific ContextPack is live. **COLD** raw drafts, unknown pages, historical notes and Git history exist in the vault and can be read via explicit tools. The ready-only retrieval path excludes most cold/unverified material, but broad read tools can still expose it. The ownership boundary should remain: Pi stores reusable knowledge and exposes scoped retrieval; Hermetrix compiles local context and decides actions.

## 9. Multi-project isolation

The Platform schema contains Workspace, Platform Project and Platform Service, but all three tables are **empty in production**. Second Brain pages have only a free-text `project` field; no foreign key binds them to those Platform objects or to repository/worktree/task/session/agent IDs. `get_context(project="")` returned only global ready knowledge; a named `project` includes that project's material plus global material. This is useful relevance filtering, **not tenant isolation**.

Kanban's general `list_projects`/`list_tasks` reads also expose board-wide data without a workspace-scoped identity check (`kanban_mcp.py:101-114`, `kanban.py:640-700`). The production board currently contains only two projects and five tasks, but the code boundary would not segregate future company/client/personal work on that board either. Platform agent identity endpoints require credentials; that does not retroactively scope the open board reads.

Live isolation probe: `search(query="employeeType position", include_drafts=true, project="")` returned no match. The same authenticated caller setting `project="cop-svt-svtap-api"` received paths of that project's raw drafts. A subsequent `read_page` on `raw/2026-09-15-smart-visit-api-position-filter-by-employeetype-and-session-propagation.md` succeeded (1,368 bytes; frontmatter `project: cop-svt-svtap-api`, `status: draft`) without any project-bound claim. We did not output or alter the draft body. Thus a credential holder can select another project by name and can read its page by path. Company/client/personal separation is **not strong enough** today. Global knowledge also has no policy for which workspaces may consume it.

## 10. Multi-agent sharing

The Second Brain MCP API is model-agnostic: its tools take strings/project names and return evidence, without requiring Hermetrix, Bonsai, a particular prompt format or model. Hermes already configures `second-brain` as a local stdio MCP child. Codex, Claude Code and OpenCode could consume the same protocol with separate credentials and proper scope, but the current Windows Codex/Hermetrix setup is pointed at **Kanban**. Live Hermetrix `agent-knowledge` has one ready profile at `https://agent-knowledge.go2gether-tech.online/mcp`, 21 tools. No live Second Brain profile for Hermetrix was observed. The Pi Platform identity registry has no shared authorization bridge to Second Brain; its `codex-windows`/`hermetrix-bonsai` credentials authenticate Kanban, not the knowledge server. This is transport interoperability, not yet a unified Agent Platform knowledge contract.

## 11. Codex contribution loop

The server has a viable **draft submission primitive**: `record_lesson(title, problem, solution, verification, applicability, limitations, sources, project)` creates a structured draft, without storing a whole conversation (`server.py:743-763`). `capture` accepts free-form draft observations, and `record_use` records version-bound reuse feedback. However, the live Codex MCP endpoint is Kanban and does not expose these tools; Codex has no observed Second Brain-scoped client credential. A Codex user can only follow this loop after connecting directly to Second Brain with a separate credential. Even then, promotion is a curator operation based on source metadata and manual assessment; tests/evidence are not cryptographically or transactionally bound to the candidate. There is no automatic candidate validation, conflict/supersession decision, or safe promotion workflow proven by production read-only evidence. Therefore the **end-to-end Codex→verified candidate→Pi validation→promoted Project Brain loop is not live**.

## 12. Hermetrix/Bonsai reuse loop

Hermetrix has a durable Task Engine, CompactState, CandidateAction/DecisionEngine, StepPacket and Context Compiler in its Windows checkout. The DecisionEngine includes `retrieve_memory` as a read candidate (`internal/taskengine/decision.go:263-291`), but that choice is **not proof of a call to Pi Second Brain**. Hermetrix's live MCP profile is Kanban, not Second Brain. Its own `context_search` retrieves local session event history (`internal/agent/contextsearch.go`) and its compiler budgets/deduplicates local fragments (`internal/context/compiler.go`); neither is a duplicate Pi Project Brain. H1/H2 read-only Platform integration code and a durable outbox exist locally (`internal/agentplatform/observe.go`), but that is assignment observation/RunUpdate plumbing, not a deployed knowledge retrieval/compile adapter. The smallest required Hermetrix side addition is a **read-only, scoped Second Brain MCP retrieval adapter** that maps versioned citations into Context Compiler fragments/StepPacket evidence, with task/repository/project binding and fail-closed trust labels. Keep DecisionEngine, CompactState, leases, effect handling and local task authority unchanged. Pi must supply the authorization/scope side; do not copy the vault into Hermetrix.

## 13. Retrieval-quality tests on the real Pi

Tests used the live localhost Second Brain MCP HTTP endpoint and its existing server credential; all calls were reads. `get_context` used `project=""`, default 6,000-byte budget, limit 5 unless stated. Latency is one observed request, not a benchmark. IDs are relative vault paths. Each returned item carried a SHA-256 page `version`, line coordinates and one `sources` entry; returned item `score` was absent. Context size below is encoded JSON bytes as measured by the client (the server also reports `used_bytes`). Private excerpts are intentionally omitted.

| Probe/query | Tool/scope | Returned IDs in order (duplicates indicate different passages) | State/provenance | Bytes | Latency | Assessment |
|---|---|---|---|---:|---:|---|
| Exact: `state db corruption recovery` | `get_context`, global | `concepts/system-watchdog-setup.md`; `queries/hermes-sessions-catalog.md`; `reference/hermes-state-db-summary.md` ×2; `entities/state-db-corruption-recovery.md` | all active, sourced, versioned; no scores | 5,139 | 35.0 ms | Relevant page came fifth: poor top precision. |
| Architecture: `Hermes state DB` | `get_context`, global | `reference/hermes-state-db-summary.md` ×2; `entities/state-db-corruption-recovery.md` ×2; `queries/hermes-sessions-catalog.md` | all active, sourced | 5,139 | 18.9 ms | Relevant, but repeated pages and no score. |
| Failure/fix: `corruption recovery` | `get_context`, global | `reference/hermes-state-db-summary.md`; `concepts/system-watchdog-setup.md`; `queries/hermes-sessions-catalog.md`; `reference/hermes-state-db-summary.md`; `entities/state-db-corruption-recovery.md` | all active, sourced | 5,139 | 20.0 ms | Recovery page again lower ranked than nearby notes. |
| Procedure: `system watchdog setup` | `get_context`, global | `entities/state-db-corruption-recovery.md`; `reference/hermes-state-db-summary.md`; `concepts/system-watchdog-setup.md` ×2 | all active, sourced | 3,726 | 18.8 ms | Procedure found, but not ranked first. |
| Ambiguous: `state` | `get_context`, global | watchdog ×2; corruption-recovery ×2; sessions-catalog | all active, sourced | 4,570 | 17.7 ms | Expected broad/noisy match. |
| Unrelated: `quantum banana protocol` | `get_context`, global | none | `no_match` | 179 | 17.9 ms | Correctly abstained. |
| Cross-project default: `employeeType position` | `get_context`, global | none | `no_match` | 179 | 17.3 ms | Global ready-only query did not return project draft. |
| Cross-project selected: `employeeType position` | `search(include_drafts=true)`, `cop-svt-svtap-api` | project draft paths, including `raw/2026-09-15-smart-visit-api-position-filter-by-employeetype-and-session-propagation.md` | `[DRAFT]` text response, path/version but no JSON score | not measured | 229.0 ms | Caller-selected project gave draft discovery. |
| Stale/deprecated | code path plus live index check | no live deprecated/revoked page in indexed snapshot | retrieval excludes these states in code | — | — | **Not empirically testable without altering knowledge**; do not claim a live stale-case pass. |

The first query includes pages from unrelated neighboring topics, so the current lexical ranking is insufficient for confidently selecting *the most relevant* knowledge without client review. Text-level duplicate suppression works, but repeated paths remain because different passages are allowed. The test set is deliberately small and cannot establish corpus-wide precision. `search` returns human-readable text, unlike `get_context`'s structured JSON, which complicates a stable shared agent contract.

## 14. Knowledge Compiler reality check

**Existing:** `record_lesson` structured draft capture; `record_use` version-bound feedback; source checks; Git commit/audit; manual promotion; readiness filtering. **Partial:** provenance, validation text, dedup of retrieval passages, lifecycle flags and versioning. **Missing:** a verified KnowledgeCandidate object tied to immutable tests/diffs/effects/repository revision; a deterministic evidence validator; conflict/supersession detection; an approval decision record; compiler that converts a successful Codex/Hermetrix result into a compact reusable candidate; automatic safe promotion. An LLM-written summary and a filled `Verification` section are **claims**, not verified evidence. No hidden chain-of-thought storage is needed or proposed. Pi/Second Brain must retain promotion authority; a Harness may submit an inert candidate only.

## 15. Knowledge quality and trust

The live trust rule for `get_context` is `status=active`, not raw/draft, nonempty source list, `confidence ∈ {medium,high}`. This is useful filtering, but it equates curator-set metadata with verification. A curator credential can `ingest` an active page directly using a plausible URL as source; no external evidence validation was found. The running legacy token is accepted for any scope (`second_brain/access.py:8-30`), so the practical HTTP trust boundary is possession of one shared secret. `search(include_drafts=true)` clearly marks drafts, while `read_page` returns raw content without imposing readiness or project checks. Thus a local LLM can receive stale/unverified content if its client chooses the broad tools, and a ready-only result can still be factually wrong. A client should not treat `confidence: high` as equivalent to a passing test.

## 16. Observability

Observable today: systemd service health; Kanban credential/write-gate audit records; vault Git history/log; current page/index counts; session DB schema if used; and an individual `get_context` response's status, item count and byte budget. `ctx_stats` belongs to the **optional local output gateway**, not Pi Project Brain. No persistent Pi-level counters/latency histograms for knowledge retrieval, no candidate→promotion funnel, no cited-knowledge-to-task-outcome join, and no knowledge reuse or escalation metric were found in the inspected service code. The requested `knowledge_reuse_rate`, `escalation_rate`, and task success rate cannot currently be measured end-to-end. Do not infer them from raw tool call counts.

## 17. Legacy and duplication

There are overlapping names and stores, but not necessarily redundant implementations: (1) `agent-knowledge` is actually Kanban MCP; `secondbrain` is the knowledge service; (2) `gateway_server.py` is a local output filter, while live `get_context` is Pi knowledge retrieval; (3) Kanban `projects` and `platform_projects` are different concepts, and the new Platform registry is empty; (4) Hermes has its own local memory/session workflow, while Second Brain is the shared vault; (5) Hermetrix local `context_search` and Context Compiler serve hot local context, while Second Brain is warm/cold reusable knowledge. Consolidate **names, credential/scoping contracts, and provenance mapping**, not storage by deleting local working memory. Do not remove legacy code based on this audit alone.

## 18. Reality matrix

| Component | Target responsibility | Current implementation | Status | Owner | MCP/API | Gap / next action |
|---|---|---|---|---|---|---|
| Agent Platform | Shared control plane | Kanban UI/API and MCP, schema v9 | PARTIAL | Pi | `agent-knowledge`/REST | Registry empty for Workspace/Project/Service; no dispatch. |
| Second Brain | Knowledge store | Git/Markdown vault and SQLite search cache | LIVE | Pi | `secondbrain` MCP | Small ready corpus and weak tenant boundary. |
| Project Brain | Verified reusable cross-agent knowledge | Some curated pages, drafts and provenance | PARTIAL | Pi | `get_context`, `record_lesson`, `promote` | No verified candidate/quality contract. |
| Context Gateway | Task-specific selection | `get_context` substrate; local output gateway is different | PARTIAL | Pi/client | `get_context`, `get_context_multi` | Platform/task-aware filtering and ranking. |
| Knowledge lifecycle | Candidate→verified→deprecated | Draft→manual active/deprecated/revoked | PARTIAL | Pi | capture/lesson/promote/frontmatter | No first-class verified/evidence/supersession. |
| Knowledge Compiler | Successful work→validated candidate | Structured draft helper only | MISSING | Pi with client evidence | `record_lesson` primitive | Evidence-bound candidate/validator. |
| Knowledge retrieval | Relevant sourced context | FTS/trigrams, lexical ranking, digest citations | LIVE | Pi | search/context/read | No semantic/task-aware rerank or score output. |
| Knowledge promotion | Authoritative admission | `promote`/`ingest` with curate credential | PARTIAL | Pi | MCP writes | No hard test/evidence gate; legacy token too broad. |
| Workspace/Project scoping | Tenant and relevance isolation | v8 tables empty; free-text knowledge `project` | PARTIAL | Pi | Kanban REST; Second Brain project arg | Bind identity to allowed scopes; close path reads. |
| Node/Agent/Auth | Per-agent identity | 3 nodes, 3 agents, 4 scoped Platform credentials | LIVE | Pi | `whoami`, `get_node`, `get_agent` | Second Brain auth separate; live legacy token. |
| Work State | Plans/tasks and audit | 2 Kanban projects, 5 tasks, Write Gate | LIVE | Pi | Kanban MCP/REST | No knowledge/evidence link or managed run state. |
| Platform Router | Choose agent/node/work location | No service/code proven in production | DESIGNED_ONLY | Pi future | none | Do not conflate with context retrieval. |
| Hermetrix DecisionEngine | Choose local next action | Local Task Engine and `retrieve_memory` candidate | LIVE | Hermetrix | local | Do not move to Pi; wire retrieval adapter later. |
| Hermetrix Context Compiler | Assemble bounded local context | Local compiler and StepPacket | LIVE | Hermetrix | local | Map Pi citations into fragments/evidence. |
| Codex contribution path | Verified solution→draft | Draft API exists, Codex knowledge connection not configured | PARTIAL | Codex/Pi | `record_lesson` on separate MCP | Scoped credential and evidence binding. |
| Hermetrix reuse path | Bonsai consumes Project Brain | Kanban MCP profile only; local memory distinct | PARTIAL | Hermetrix/Pi | no live brain profile | Read-only brain adapter plus scoped auth. |
| Cross-agent learning | Codex→Pi→Bonsai | Transport primitives, no demonstrated closed loop | MISSING | Pi + clients | none end-to-end | Safe candidate/curation/retrieval linkage. |
| Observability | Reuse/escalation/success metrics | Logs, Git, auth audit, response sizes only | PARTIAL | Pi + clients | history/log | Join retrieval/candidate/outcome without secrets. |

## 19. Critical gaps

1. **Tenant isolation and least privilege:** the live Second Brain legacy token has all scopes; `project` is caller-controlled and `read_page` bypasses query filtering. Kanban general reads are board-wide/open. Resolve both before sharing client/company/personal material or giving multiple agents the same credential.
2. **Endpoint confusion:** `agent-knowledge` is Kanban only. Codex/Hermetrix cannot retrieve Second Brain through that live profile, and Platform credentials do not authorize Second Brain.
3. **Evidence trust:** active/high-confidence metadata is not a verification proof; direct `ingest` and manual `promote` do not require immutable test/effect evidence. No conflict/supersession admission policy exists.
4. **Context selection quality:** only seven global ready pages; lexical results can rank nearby but less relevant pages first. No task/repo-aware scope, semantic retrieval or exposed score.
5. **Unpopulated Platform hierarchy:** Workspace/Platform Project/Platform Service are schema-live but zero-row in production, so Project Brain cannot be joined to them today.
6. **No measured learning loop:** no cross-agent candidate/evidence/outcome linkage and no reuse/escalation rate.

## 20. Recommended implementation order (analysis only)

1. **Close the Second Brain authorization boundary first.** Use existing per-client hashed scoped-token support instead of the full-power legacy token for agents; bind allowed Workspace/Project/Repository to the authenticated actor; enforce it on `get_context`, `search`, `read_page`, `read_passage`, `list_pages`, session and write tools. Preserve an explicit safe global-knowledge policy. Validate with real negative cross-project tests before onboarding company/client/personal work. This reuses Second Brain rather than replacing it.
2. **Make the two MCP services explicit to users/agents.** Keep Kanban as work/control plane and Second Brain as Project Brain; configure separate Codex and Hermetrix read credentials for the latter. Avoid implying the Kanban `agent-knowledge` domain contains knowledge tools. Reuse Pi's existing identity registry and Second Brain client scopes via a narrow mapping/adapter, without making Pi dependent on Hermetrix.
3. **Populate and reconcile Platform scope identities** for existing repositories/worktrees and Second Brain project tags, with explicit shared/global policy. Do not infer workspace from an absolute client path or fabricate ownership from names.
4. **Add the smallest read-only Context Gateway adapter**: caller identity + project/repo/task context → scoped `get_context` → versioned citations/trust labels → Codex/Hermetrix context. The Hermetrix side maps this into its existing Context Compiler/StepPacket; DecisionEngine stays local. Test actual retrieval relevance and leakage. Do not start a Platform Router here.
5. **Complete the verified contribution loop** using `record_lesson`/drafts as the input surface: immutable evidence references to test results/diff/repo revision, deterministic validation, curator approval, conflict/supersession decision, then promotion. Clients submit inert candidates; Pi/Second Brain owns admission. This is the smallest *new capability* needed to turn Codex results into safely reusable knowledge after the authorization foundation is fixed.
6. **Measure outcomes** by recording retrieval/candidate IDs, citation versions, selection decisions, task verification and escalation outcomes. Compare reuse and escalation rates only after the above loop works. Improve ranking based on the live failure cases; do not replace the store just to add a reranker.

No Pi source, service configuration, Markdown knowledge page, credential, promotion, schema migration, autonomous coding setting, or implementation was changed for this audit. The derived SQLite search cache was modified by the live service while read-only retrieval was exercised; its tracked-files status is disclosed above. The running Kanban and Second Brain services remained active and Kanban schema remained v9 at the final check.

## Direct answers

1. **Can Codex use the Pi Project Brain today?** The Pi Second Brain MCP is live and agent-agnostic, so Codex *could* connect separately, but its observed current endpoint is Kanban. **Not through the current Codex setup.**
2. **Can Hermetrix use the same Project Brain today?** Its generic MCP client could, but the sole observed live profile is Kanban; no Pi Second Brain retrieval-to-Context-Compiler adapter was demonstrated. **Not end-to-end today.**
3. **Can Bonsai retrieve knowledge learned from previous Codex sessions today?** **No demonstrated path.** Those solutions are not being admitted as verified shared knowledge and Hermetrix is not connected to Second Brain.
4. **Can Codex submit a verified reusable solution today?** `record_lesson` can store a structured **draft** if separately connected and authorized, but it does not verify evidence and current Codex MCP does not expose it. **Not as a verified solution.**
5. **Can that solution be promoted safely without manual conversation dumping?** Conversation dumping is unnecessary (`record_lesson` exists), and a curator can manually promote, but the verification and isolation gates are insufficient for safe automatic promotion. **Only manual, trust-dependent curation today.**
6. **Is project isolation strong enough for company/client/personal work?** **No.** The live cross-project draft read and caller-chosen `project` demonstrate this directly.
7. **Is the Context Gateway actually selecting the most relevant knowledge?** **No dedicated Pi gateway is live.** `get_context` selects relevant lexical passages, but live tests showed poor top ranking on known facts.
8. **What is the smallest missing piece required to complete the learning loop?** After closing the auth boundary, a **scoped, evidence-bound candidate→validation→promotion adapter** linking `record_lesson` drafts to immutable test/revision evidence, plus a read-only Hermetrix retrieval adapter. A new memory store or Platform Router is not required.
9. **Which existing Pi components should be reused rather than rebuilt?** Second Brain Markdown/Git vault, `get_context`/`get_context_multi`, `record_lesson`/`record_use`/`promote` primitives, Kanban Workspace/Project/Service and Node/Agent/Auth registry, existing MCP transports and audit logs.
10. **What should we build next?** First enforce per-agent, project-bound Second Brain access and test leakage. Then connect Codex/Hermetrix read-only retrieval and validate real task relevance. Only then add evidence-bound candidate validation/promotion and outcome metrics.

PROJECT_BRAIN_STATUS: PARTIAL
