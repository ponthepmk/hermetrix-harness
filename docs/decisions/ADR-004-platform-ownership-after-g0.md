# ADR-004: Preserve existing execution, work registry and knowledge ownership

Date: 2026-11-01 · Status: Proposed

Context: Live read-only G0 finds a working Pi board/MCP/Brain stack, a distinct Hermes agent, Windows Harness development and installed-state drift (schema 50 versus source 82), and a fixture-only managed-platform adapter. Existing systems already own different authoritative state.

Decision proposed: Keep execution authority, Context Compiler, task/effect leases and local workspace guards in Harness. Keep Pi Platform as identity/work registry and Project Brain as curated knowledge owner. Introduce optional bounded/versioned adapters and a separately qualified GPU worker rather than replacing these owners. Defer Godot. Do not introduce a new gateway until measured integration needs justify it.

Alternatives: A new gateway increases operational/authentication surface before its need is demonstrated. Consolidating all stores risks migration, rollback and authority ambiguity. Neither is justified by current evidence.

Consequences: Legacy services remain independently operable and rollbackable. Explicit project-ID mapping, release provenance, staged schema migration, worker isolation and integration contract tests become required dependencies. This ADR does not approve production deployment or certify fixture adapters as production-ready.

Evidence: ../AUDIT-REPORT.md and ../G0-IMPLEMENTATION-PLAN.md. Missing specifications 00–22 remain a scope-signoff limitation.
