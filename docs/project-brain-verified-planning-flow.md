# Pi planning project binding: live verification receipt

Date: 2026-09-24 (Asia/Bangkok)

Environment: production `agent-knowledge.go2gether-tech.online/mcp`, Codex's
scoped `work.read`/`work.write` credential, Kanban MCP source `e22cc27177422b8b37ae92a216be4217cb644eee`.
The Bearer credential is deliberately omitted from this receipt.

## Problem observed

`list_platform_projects` returned the Platform Project `hermetrix-harness`
with `id=3` and `access=write`. Calling `add_task` with `project_id=3` still
returned `{"error":"project write access denied","http_status":403}`.
The existing board `list_projects` response had only board projects `id=1`
(`platform_project_id=1`) and `id=2` (`platform_project_id=2`). There was no
board project bound to Platform Project 3. These are distinct ID namespaces.

## Correction and observed result

Calling `add_project(name="Hermetrix Harness", platform_project_id=3)` through
the same scoped MCP credential returned `{"id":4}`. Calling
`add_task(title="[integration-test] Hermetrix planning write", project_id=4,
status="todo")` returned `{"id":15}`. Calling `delete_task(task_id=15)`
returned `{"ok":true}`. The temporary task was removed; board project 4
remains for real Hermetrix planning work.

The MCP `tools/list` request through the public Cloudflare hostname returned
HTTP 200 and 22 tools, including `list_platform_projects`. An earlier request
to the same hostname returned HTTP 421 while FastMCP allowed only localhost;
the `e22cc27` host allowlist change corrected this without opening the service
listener beyond loopback.

## Reusable procedure

For work in `hermetrix-harness`, discover the Platform Project with
`list_platform_projects`, then find its board project with `list_projects`
using `platform_project_id`. Pass the **board project `id`** to task tools.
If no board project exists, create one with `add_project` bound to the granted
Platform Project before adding a task. Never assume the two IDs coincide.

This receipt documents observed integration behavior, not general permission
to create projects for an ungranted Platform Project. The board API enforces
the caller's scope and project grant on each write.
