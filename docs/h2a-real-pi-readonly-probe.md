# H2A real Pi read-only identity probe

This adapter is built on Hermetrix H1 commit `ceb72fe0e28309db3c0ed472a8463310b9cf1e77`. Its Pi source contract was inspected at `d68bdd3921d4555f7218a280ee62c9b5e6b78de0`. It creates no Platform task, session, run, effect, or RunUpdate.

## Pi P2 wire mapping

The Pi FastMCP server at `/mcp` exposes `whoami`, `get_node(node_key)`, `get_agent(agent_key)`, and `list_capabilities`. It wraps each board JSON response in one MCP text content item. The board's `whoami` response carries the credential-bound `agent_key`, `node_key`, `credential_id`, `auth_method`, and effective `scopes`, but no numeric Node/Agent IDs. The numeric IDs and active status are cross-checked with `get_node` and `get_agent`. `list_capabilities` is recorded only as a count and never grants authority.

The fixed expected identity is Node `3/windows-pc-main`, Agent `2/hermetrix-bonsai` (`managed`), and exactly one effective scope: `read`. The probe records the positive credential ID returned by Pi. It does not pin that ID because Pi rotation issues a new ID while preserving the agent binding. The probe rejects extra scopes, inactive records, mismatched binding, or malformed responses before it can produce a success receipt.

## Secure local operation

Use an existing trusted HTTPS endpoint or an authenticated SSH tunnel terminating at Windows loopback. The probe rejects remote plaintext HTTP. A temporary example tunnel is:

```text
ssh -N -L 127.0.0.1:18902:127.0.0.1:8902 pizp2e0@192.168.1.123
```

Store the operator's scoped read credential through the hidden terminal prompt. Do not put the token on the command line, in an environment variable, or in this document:

```text
go run ./cmd/hermetrix-h2 credential-store --data <dedicated-local-data-directory>
```

The command initializes the local Hermetrix schema and writes the credential only to the existing protected Hermetrix vault under `mcp:pi-h2a-readonly`. Then run:

```text
go run ./cmd/hermetrix-h2 probe --read-only --data <dedicated-local-data-directory> --endpoint http://127.0.0.1:18902/mcp --workspace <workspace-directory>
```

The probe takes read-only local DB and workspace snapshots, performs the four fixed Pi read calls, reconnects and performs a second fresh `whoami`, verifies zero local run/attempt/effect/outbox deltas, and scans local output for the credential and its SHA-256 digest before printing a secret-free JSON receipt. The application-level Pi commit and schema fields in that receipt are pinned contract metadata; the operator must independently verify the deployed Pi checkout and migration set before accepting it as real-Pi evidence.

## Test gates

- `go test ./internal/agentplatform/... ./internal/taskengine/... ./internal/taskcoord/... ./internal/store/... ./internal/blob/... ./internal/mcp/... ./cmd/hermetrix-h2`
- `go vet ./internal/agentplatform/... ./internal/taskengine/... ./internal/store/... ./internal/mcp/... ./cmd/hermetrix-h2`
- `go run ./cmd/hermetrix-h1 demo --fixture-only`
- Set `H2A_LIVE_MCP_ENDPOINT` to a secure Pi endpoint and run `go test ./internal/agentplatform/piidentity -run '^TestLivePiToolDiscovery$' -v` to validate the live read tool names and input keys without sending a credential.

The broader `go test ./...` currently has one browser fixture failure (`TestManagedBrowserInteractsWithProjectPageAndCapturesEvidence`: Chrome exits before DevTools is ready), reproduced on unchanged H1 commit `ceb72fe0` as well. Windows ThreadSanitizer cannot allocate its shadow memory, so race status is `ENVIRONMENT_BLOCKED` rather than pass.
