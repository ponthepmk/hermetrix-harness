# Next session — 2026-09-24

State: Pi Kanban and Second Brain run with project-scoped MCP credentials,
Project Brain candidate admission, and explicit Platform Project grants. Codex,
Hermes, and installed Hermetrix have separate credentials. The Codex→Pi→Bonsai
loop was observed on the live `hermetrix-harness` project: a curated lesson with
Git/evidence digests reached a version-bound Hermetrix context snapshot and
Bonsai answered a natural planning question. See
`project-brain-rollout-review.md` for exact SHAs, receipts and safety limits.

Source: `main` and `origin/main` contain Hermetrix `fe27153`, Kanban
`e22cc27`, and Second Brain `454bc00`. The installed Hermetrix binary was
built from a clean detached worktree at `fe27153`, and the Pi runs the two Pi
source SHAs. Concurrent unrelated work under
`internal/product`, `internal/skills` and `internal/web` is uncommitted in the
original Windows worktree; preserve it and use the clean release worktree for
this rollout.

Next: keep the scoped Project Brain loop operational, rotate credentials before
expiry, and review the separate uncommitted Windows work before integrating
it. Future product work is automatic candidate extraction, measured knowledge
reuse, deliberate shared/global scope, and a separate Platform Router if
needed. No managed write authority or unattended production coding was added.

Blockers: none for scoped Project Brain use. Full target architecture remains
partial. Four Pi Second Brain client credentials expire on 2026-12-22
19:30:19 UTC and need rotation before that date. `go test -race` remains an
unverified environment limitation.
