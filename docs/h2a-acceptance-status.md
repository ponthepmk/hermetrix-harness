# H2A real Pi acceptance — 2026-09-23

`H2A_IMPLEMENTATION_STATUS: READY_FOR_REVIEW`

The previously missing one-time credential was resolved by rotating Pi's existing `hermetrix-bonsai` read credential through the P2 credential lifecycle. The new token travelled only in process memory over an authenticated SSH channel and was saved in the local Hermetrix credential vaults. Neither the token nor its digest appears in this record, source control, CLI output, or the acceptance receipt.

## Observed Pi baseline

- Production checkout HEAD `d68bdd3921d4555f7218a280ee62c9b5e6b78de0`; Git working tree clean. No Pi application source, schema, or deployment changed.
- Applied migrations `[1,2,3,4,5,6,8,9]`; `autonomous_coding_enabled=0`.
- Board and MCP services responded on ports 8901 and 8902.
- Node `3/windows-pc-main` and Agent `2/hermetrix-bonsai` (`managed`) were active and bound to one another.
- Pi credential 2 is revoked by rotation. Credential 3 is active, scoped exactly to `["read"]`, and records `rotated_from=2`. Its recorded expiry is `2026-10-23 11:51:44` in Pi's database time format.
- Pi audit contains a `ROTATED` event for credential 3. No production write tool was invoked.

## Real H2A receipt

The Windows adapter used an authenticated SSH tunnel terminating at `http://127.0.0.1:18902/mcp`. Its fixed allowlist invoked only `whoami`, `get_node`, `get_agent`, and `list_capabilities`. A second connection repeated `whoami` instead of trusting cached identity.

```json
{"pi_commit":"d68bdd3921d4555f7218a280ee62c9b5e6b78de0","pi_schema":9,"real_pi_contacted":true,"node":{"id":3,"node_key":"windows-pc-main","status":"active","agent_count":1},"agent":{"id":2,"agent_key":"hermetrix-bonsai","agent_type":"managed","node_key":"windows-pc-main","status":"active","node_status":"active"},"effective_scopes":["read"],"credential_id":3,"capabilities_count":4,"capabilities_informational":true,"retry_count":0,"transport_mode":"loopback HTTP","endpoint_class":"loopback","fresh_whoami_count":2,"candidate_executed":false,"task_runs_delta":0,"attempts_delta":0,"effects_delta":0,"run_updates_sent":0,"forbidden_invocation_counters":0,"workspace_before_after_digest_identical":true,"credential_exposed":false,"distributed_write_authority":false}
```

The probe scanned the workspace and dedicated H2A data root for the credential and digest. It found no leak.

## User-facing MCP verification

Hermetrix's installed application has one Pi MCP connection, `Pi Agent Platform (read-only)`, with the token saved in its protected vault. Discovery negotiated protocol `2025-11-25` and indexed 21 tools; the connection displayed `ready` in the MCP screen. In a new session under the release fixture project, the local model searched for and described `whoami`, requested that exact tool, received one-call approval, invoked it successfully, and reported credential 3, agent `hermetrix-bonsai`, node `windows-pc-main`, and scope `["read"]`. This verifies the app's model-to-MCP-to-Pi read path as well as the isolated H2A probe.

Because this Pi profile does not trust remote risk annotations, the UI labels an unannotated `whoami` call as an unknown effect and requires exact one-call approval. That is an intentional fail-closed behavior, though the wording is less clear than the Pi tool's actual read-only behavior. The token cannot authorize Pi writes.

## Regression and remaining operational limit

- Pi P2 identity: 64 checks passed. Bootstrap, Write Gate 30 checks, migrations, P1 identity, registry seed, reconciliation, and deployment controller 176 checks passed on Pi.
- Local H2A/agent platform tests and `go vet` passed.
- The previously documented Windows race-test limitation remains `RACE_TEST: ENVIRONMENT_BLOCKED`; no race-test success is claimed.
- The current SSH tunnel is live for this session. Automatic reconnection after a PC restart requires a separately scoped tunnel key and launcher wiring; until that is installed, the operator must reopen the tunnel before using Pi MCP. This does not alter the completed H2A read-only acceptance result.
