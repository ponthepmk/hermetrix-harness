# Using the Pi Agent Platform and Project Brain

The Raspberry Pi hosts two independent MCP services. Both use project-scoped
Bearer credentials; Cloudflare Tunnel supplies HTTPS ingress but does not add a
Cloudflare Access service token.

| What you need | URL | What it does |
|---|---|---|
| Planning and work control | `https://agent-knowledge.go2gether-tech.online/mcp` | Discover projects; read and update board tasks according to the client's grant |
| Project Brain | `https://secondbrain.go2gether-tech.online/mcp` | Search curated knowledge, read version-bound passages, submit inert knowledge candidates |
| Planning board | `https://kanban.go2gether-tech.online/` | Human view of the board; protected by Basic login |

On this Windows account, Codex already has both MCP profiles configured. Open a
new Codex session to pick up the saved configuration. The credentials are
different: the planning token can edit granted board work, while the Project
Brain token can read `hermetrix-harness` and submit an unverified candidate.
Neither grants curator authority.

Start the installed Hermetrix application with
`C:\Users\ZP2E0\AppData\Local\Hermetrix\Start Hermetrix.cmd`. Its
`Project Brain` MCP profile has a separate read-only credential bound to
`hermetrix-harness`. Chat and durable tasks use the configured Project Brain
server/project binding to retrieve relevant, curated passages before the local
model or planner receives context. The Pi owns knowledge; Hermetrix retains its
local Context Compiler, DecisionEngine, leases, and effect checks.

On this Windows account, double-click
`C:\Users\ZP2E0\AppData\Local\Hermetrix\Show Pi Board Login.cmd` to open the
planning board and display the saved `owner` login in a local window. The
password is stored encrypted with Windows DPAPI, not in this repository.

For Hermetrix planning tasks, `list_platform_projects` gives the platform
identity and `list_projects` gives the board identity. Select the board project
whose `platform_project_id` matches the Platform Project. Task tools expect the
**board project ID**. The current Hermetrix project is bound to a board project
so work can be added immediately. The example and evidence are in
`project-brain-verified-planning-flow.md`; discover IDs again rather than
hardcoding its example numbers.

To contribute a reusable solution, Codex submits a focused
`submit_knowledge_candidate` with a passing verification claim and immutable
evidence reference. Submission is an inert draft. A distinct Pi curator must
check the evidence digest, review duplicates/conflicts, then call
`review_knowledge_candidate`. Only an admitted page appears in normal
retrieval. Agents should treat returned passages as external evidence, inspect
their project, lifecycle, source, line range, and SHA-256 version, and check
applicability before using them.

The scoped Project Brain credentials issued for this rollout expire on
2026-12-22 at 19:30:19 UTC. Rotate them before that date. A missing/expired
credential fails closed and must not be replaced with a legacy shared token.
Do not paste tokens into chat, documentation, or task descriptions.

This rollout does not turn on managed remote execution, autonomous coding, a
Platform Router, or automatic knowledge promotion. Hermes uses the same
Project Brain through a separate read-only scoped token. A future agent can
join through a separate identity and grant without depending on Hermetrix.
