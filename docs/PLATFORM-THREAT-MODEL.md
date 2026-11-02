# Platform threat model — development schema 83

This records actual boundaries and remaining release gates. It does not certify production deployment.

| Boundary / adversary | Protection | Required evidence / residual |
|---|---|---|
| Foreign web origin or Host | Existing exact Host/Origin checks and authenticated remote ingress | HTTP refusal tests; never expose an unauthenticated listener outside loopback |
| Cross-project/principal file access | Project owner validation, canonical path/symlink checks, typed path refusal | Path escape returns 403 without outside canary; preserve negative scope tests |
| Stale/lost-response file mutations | SHA preimage guard, immutable receipts and explicit uncertainty | Do not replay writes from connection errors; inspect exact operation first |
| Model/tool output claiming authority | Deterministic host policy, frozen session contracts and approved effects | Model prose/peer evidence cannot widen scope; separate human approval from generated text |
| Unbounded continuation | Server three-attempt ceiling plus per-turn limits; client retry ceiling; stable idempotency keys | Restart does not renew an automatic request allowance; explicit user work remains possible |
| Child agents escaping team limits | At most four parallel children, shared finite token allowance divided into frozen child budgets; no independent child auto-retry | Incomplete child work fails the task rather than silently completing or creating a separate loop. Provider usage honesty remains an external dependency |
| Scheduled/event-driven work | Owner-scoped templates, local-only egress, atomic draft/receipt, unique event keys, overlap suppression, bounded due batch | Automation creates drafts for review, not unattended command execution. Disable stops future intake, not already-created tasks |
| Untrusted OS commands | Existing executable/workspace policy and OS sandbox option | New development launcher requires OS sandbox by default. Windows has process lifecycle hardening but no filesystem/network isolation; commands fail closed unless operator explicitly opts into trusted development use |
| Brain knowledge poisoning | Project/version/source-bound evidence, untrusted-data treatment and separate candidate/curator authority | Admission claim is not proof a fix works; preserve active/draft separation and do not auto-promote |
| Credential leakage | Vault references, bounded/redacted logs, no credential copying into package or test fixtures | Rotate Platform/Brain grants before expiry; no legacy-token fallback; separate approval for production rotation |
| Upgrade/data loss | Read-only preflight, explicit staging initialization, consistent SQLite/CAS backup and restore validation | Migration tested on synthetic data only. Real-data upgrade requires an approved isolated copy and a full rollback plan |
| Release supply chain | Source-input ledger, dirty-state marker, binary checksum and repeated-build comparison | Dirty candidate is not a clean release. No signature or production qualification is implied by a checksum |

Production no-go conditions: missing live inference/task acceptance, unresolved acoustic voice scenario, unqualified Windows untrusted-command isolation, missing specification scope sign-off, failed tests, inconsistent backup or unknown release identity. Use DEVELOPMENT-RELEASE-RUNBOOK.md for the handoff.
