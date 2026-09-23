# H2A acceptance status — 2026-09-23

`H2A_IMPLEMENTATION_STATUS: BLOCKED`

The read-only H2A adapter is committed on `codex/h2a-real-pi-readonly` at `e991ffb1bb975d6a3650730d94bc11863080acf7`, based directly on H1 `ceb72fe0e28309db3c0ed472a8463310b9cf1e77`. The working tree is clean. Nothing was pushed, merged, or deployed.

## Independently observed Pi baseline

- Production checkout HEAD: `d68bdd3921d4555f7218a280ee62c9b5e6b78de0`; Git working tree clean.
- Applied migrations: `[1,2,3,4,5,6,8,9]`.
- `autonomous_coding_enabled=0`.
- MCP user service active and listening on port 8902; board listening on 8901.
- Node `3/windows-pc-main` and Agent `2/hermetrix-bonsai` (`managed`) are active and bound to one another.
- Credential record 2 is active, with stored scope `["read"]` and an expiry after this review date. Only non-secret metadata was read.
- Through a temporary authenticated SSH tunnel, unauthenticated MCP `tools/list` negotiation succeeded using protocol `2025-11-25`; the four required read tool names and input keys were present. No Pi tool was invoked in this discovery step.

## Local verification

- H2A identity, mismatch, scope, retry, transport, redaction, and fixed-allowlist tests passed, including a legacy Streamable HTTP end-to-end fixture.
- H1, Task Engine, store/migration v48→v49, CAS/blob, MCP, and task coordination package tests passed; `go vet` passed.
- The H1 fixture acceptance demo passed with zero task runs, attempts, effects, and forbidden invocations, and identical before/after workspace digest.
- `go test ./...` has one unrelated browser fixture failure, `TestManagedBrowserInteractsWithProjectPageAndCapturesEvidence`: Chrome exits before DevTools starts. The same test fails on unchanged H1 baseline `ceb72fe0` in this Windows environment.
- `go test -race` fails because ThreadSanitizer cannot allocate shadow memory on this Windows host. `RACE_TEST: ENVIRONMENT_BLOCKED`.

## Remaining real acceptance gate

The plaintext scoped H2A credential was not supplied and is not present in the dedicated local Hermetrix vault. Pi stores only its digest, so it cannot be recovered from the Pi credential record. No authenticated `whoami`, `get_node`, `get_agent`, or `list_capabilities` call was made, and no success receipt was produced. Real negative tests involving administrative identity changes were not executed.

The operator must store the read-only token locally using the hidden terminal prompt described in [h2a-real-pi-readonly-probe.md](h2a-real-pi-readonly-probe.md), or issue a replacement through the approved Pi administrative mechanism if the one-time plaintext was lost. Then run the authenticated probe through a secure loopback tunnel and verify the before/after receipt and Pi audit. The temporary SSH key used for this review was removed from Pi and deleted locally; the tunnel was stopped.
