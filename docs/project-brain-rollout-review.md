# Pi Agent Platform + Project Brain: rollout and reality review

Observed 2026-09-24 on the running Pi and installed Windows Hermetrix. This is
the post-change record; `../pi-agent-platform-project-brain-reality-audit.md`
preserves the read-only baseline before these changes.

## What is live

| Boundary | Running implementation | Source authority |
|---|---|---|
| Work and control plane | Kanban board/API on Pi port 8901; scoped MCP on loopback port 8902, public Tunnel hostname `agent-knowledge.go2gether-tech.online` | Kanban `e22cc27177422b8b37ae92a216be4217cb644eee`, schema v10 |
| Project Brain | Existing Second Brain Git/Markdown vault, scoped HTTP MCP on loopback port 8765, public Tunnel hostname `secondbrain.go2gether-tech.online` | Second Brain `454bc009ebe821da194ec7495cff05db1ad39731` |
| Hermetrix client | Native Windows app on `127.0.0.1:7331`; its Project Brain MCP profile is read-only and bound to `hermetrix-harness` | Hermetrix `fe27153e9161ceee2eb2eed6999e87388e755c27`, schema v49 |
| Hermes client | Pi gateway uses a separate read-only `hermes-pi` Second Brain credential and allows only six retrieval/read tools | Hermes gateway running; source was not changed |
| Codex client | Two enabled MCP profiles: `agent-knowledge` for granted planning writes and `project-brain` for scoped knowledge read/candidate submission | Windows user Codex configuration; credentials stored outside the repository |

All three Pi application listeners bind to `127.0.0.1`; Cloudflare Tunnel
handles HTTPS ingress. MCP requires Bearer authentication. The human board UI
requires its own Basic login. No Cloudflare Access service token is needed.
The Pi services and database were backed up before rollout; the backup directory
is `/home/pizp2e0/workspace/project-brain-rollout-backup-20260924/`.

## Evidence-first review and corrections

The original audit found a single legacy Second Brain credential with broad
power, caller-chosen project scoping, a cross-project draft read, no curated
candidate gate, no Hermetrix Project Brain adapter, and empty production
Workspace/Platform Project rows. The rollout added per-client hashed scopes and
project grants, closed legacy fallback, provisioned explicit platform identities,
connected Codex/Hermes/Hermetrix, and added inert candidates with separate
curator admission. Pi remains agent-agnostic: Hermetrix IDs and local absolute
paths are never required by either Pi MCP server.

Live use exposed three integration failures beyond unit tests:

1. Public Kanban MCP initially returned HTTP 421: FastMCP's default localhost
   Host check rejected the Cloudflare hostname despite the listener being
   loopback. The exact public hostname is now allowed; foreign Host/Origin
   requests remain denied.
2. Hermetrix's capability catalog copied an empty metadata map instead of the
   original map. It silently lost `annotations_trusted`, so the read-only
   adapter rejected `get_context` before a network call. The clone and a
   regression test were corrected.
3. The first Project Brain context search favored candidate provenance over
   the solution. The Pi now ranks Solution first and excludes provenance and
   evidence-receipt sections from ordinary topical queries, while explicit
   provenance/evidence queries can still retrieve them. A natural user question
   was also too strict for the lexical gateway; Hermetrix now retries one
   bounded, two-topic-word query only after a genuine no-match. Both passes
   retain the project scope and version-bound passage check.

## Observable acceptance

| Gate | Actual result |
|---|---|
| Public work MCP | HTTP 200, 22 tools; `list_platform_projects` returned three granted Platform Projects. Foreign Host and Origin were rejected in staging tests. |
| Planning write | A write-granted Codex call bound board project 4 to Platform Project 3. `add_task(project_id=3)` correctly returned 403 because task APIs need a **board** project ID; `add_task(project_id=4)` created test task 15; `delete_task(15)` returned `ok`. Five original tasks remain, with no test task. |
| Scope isolation | A Codex Second Brain credential read `hermetrix-harness` but was denied a foreign `cop-svt-svtap-api` project page and a personal global page. Hermes' read-only credential was denied foreign and omitted project requests. Hermetrix has a separate read-only Project Brain credential. |
| Candidate admission | Codex submitted candidate `hermetrix-harness:planning-platform-board-project-binding:2026-09-24` as an inert draft. The receipt at Hermetrix commit `6534dfd` hashes to `sha256:0ecc65fa3bda0c6988dc5f327fffb4c1fd3ab5cdf96ef2800d4a91c8d9d0926f`; the JCS candidate digest is `sha256:6fcef1fa877b12d72cb1e833eedd9fd91470c2293d9880322f4961a0221a28f2`. A distinct curator credential independently checked the digest, board binding, preserved task count, and curated-vault duplicates before admission at vault commit `f8ceee4`. |
| Knowledge retrieval | Public Codex and Hermes MCP calls retrieve the admitted, active `hermetrix-harness` page with SHA-256 version `7f18e3102f70215acca3ef3238014e4970fa4f66cea6d4721b146c099a43f35b`, Git source, medium confidence, and Solution before Problem. An ordinary `planning tasks` query does not return the evidence-receipt section; an explicit evidence query does. |
| Bonsai outcome | Native Bonsai Local session `session_ecf0f2b93fbef04d9053549d6c5cdd87` completed a natural question after the fixes. Its first committed context snapshot `ctx_725ee30670eee04b509910b9911ebf90` selected two `project-brain:` fragments from the admitted page, including `## Solution`; both carry `external_curated_not_verified` and the exact page version. The CAS blob SHA-256 matches the snapshot digest. Bonsai's answer explained the board/Platform ID distinction; this is an observed model turn, not only an MCP health check. |
| Data and runtime | Kanban SQLite `integrity_check=ok`; five original tasks. `kanban-board`, `kanban-mcp`, `second-brain-mcp`, and `hermes-gateway` active. The installed Hermetrix `/api/health` returned HTTP 200 with schema v49. |

Verification was run on a clean Hermetrix worktree at `fe27153`: `go test
./...` and `go vet ./...` passed. Second Brain staging ran the complete
`pytest` suite after the final retrieval change: **179 passed**. Kanban staging
ran the project-access, provisioning, identity, migration, write-gate,
reconcile, registry, deployment-controller, and host-security suites before
publication. Live positive and negative MCP calls above were repeated against
the real Pi. The Go race test is not claimed: its earlier environment/toolchain
limitation was not independently resolved.

## Ownership and remaining limits

The Pi owns project/work identity, scopes, Project Brain knowledge lifecycle,
and MCP transport. Hermetrix owns local paths, CompactState, Context Compiler,
DecisionEngine, leases, effect intents, and no-replay. This rollout does not
grant distributed write authority or enable autonomous coding.

The scoped Codex→Pi→Bonsai learning loop now works **with manual curator
admission**. There is no automatic Knowledge Compiler that extracts candidates
from every successful session, no Platform Router or managed HANDOFF, no
cross-project shared/global knowledge policy for these clients, and no measured
reuse/escalation rate. Context selection remains lexical and project-scoped;
the retrieval corrections above address demonstrated failures, not a claim of
semantic relevance for every future query. The Pi stores metadata and curated
text; immutable evidence can remain on its originating node.

Operational steps, URLs and the installed launch paths are in
`PROJECT_BRAIN_OPERATIONS.md`. No credential values are in source or this
review. The four scoped Second Brain clients expire on 2026-12-22 at
19:30:19 UTC and must be rotated before then.

PROJECT_BRAIN_SCOPED_USE: VERIFIED
FULL_TARGET_ARCHITECTURE: PARTIAL
