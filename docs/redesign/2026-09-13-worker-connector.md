# External worker proposal connector

Status: bounded proposal connector with a typed task contract, observable
provider phase, compact exact-text edits, and classified failures. It is one
worker boundary for the redesign; the durable planner/checkpoint/verification
engine described in the blueprint is still a separate milestone.

## Usage

Build with `go build ./cmd/hermetrix-worker`. Provide a JSON task on stdin:

```json
{"id":"fix-add","kind":"fix","brief":"Fix the sum function","acceptance_criteria":["Add(2,3) returns 5"],"constraints":["Keep the public signature"],"recommended_checks":["go test ./..."],"max_output_tokens":4096,"files":{"sum.go":"package sum\nfunc Add(a,b int) int { return a-b }\n"}}
```

Run using an existing environment credential (never pass the value as a flag):

```sh
./hermetrix-worker --base-url https://gateway.9arm.co/v1 \
  --model qwen3.8-27b-fp8 --key-env HERMETRIX_WORKER_API_KEY < task.json
```

Alternatively `--data .hermetrix --credential-ref provider:PROFILE_ID` reads the
existing vault without changing it. It does not discover or select credentials
automatically. No global Codex configuration is changed.

Only caller-supplied file contents are sent to the remote gateway. Curate that
bundle: do not include customer secrets, environment files, credentials, or
unrelated source. Exact matching of this provider credential is an additional
guard, not a general secret scanner.

Standard output is one structured result with `status: proposed_unverified`, a
stable input/proposal identity, materialized replacement contents, original
SHA-256 hashes, change strategy, assumptions, risks, suggested checks, and
provider-reported token usage. A proposal may use a full replacement or exact
text edits; every edit preimage must occur exactly once. The result always
contains the fully materialized content for the independent reviewer.

Standard error receives JSON progress events by default. Long provider calls
emit a `requesting_provider` heartbeat every 15 seconds; use `--progress=false`
for a quiet machine invocation. Worker failures use safe codes such as
`provider_timeout`, `provider_cancelled`, `provider_rejected`,
`provider_failed`, `response_truncated`, and `invalid_proposal`. HTTP 400/401
class failures are non-retryable; 408, 429, and 5xx are classified retryable for
a future coordinator to decide. The connector itself never retries a request.

Verify every preimage hash against current source before applying. This CLI does
not apply a proposal. Reject stale proposals and review all output as untrusted
code. Suggested checks are requests for the reviewer, not evidence that a test
ran. Coordinators can call `worker.ValidateResult` after persistence or transport
to bind the proposal identity back to the exact task and file preimages before
creating approval-bound write intents.

## Boundaries

- One request, one `submit_changes` call, at most 32 supplied files / 128 KiB task.
- Caller-selectable 1–32,768 output tokens, 256 KiB proposal argument limit,
  and a 30-second to 15-minute deadline (defaults remain 32,768 and 4 minutes).
- HTTPS only, redirects rejected; no automatic request retry.
- No shell, filesystem discovery, workspace writes, MCP, or tool-execution loop.
- Exact text edits or full replacements; no new files outside the supplied bundle.
- Provider error bodies are suppressed; credentials are not included in results.
- These byte limits are not a verified 96k model-context qualification.

## Verification on 2026-09-13

A live request to the configured gateway/model used only the synthetic task in
`internal/worker/testdata/synthetic-task.json`. Native `submit_changes` returned
`func Add(a, b int) int { return a + b }` to replace subtraction. Provider-reported
usage: 474 prompt + 105 completion = 579 tokens. The existing vault supplied the
credential in memory; its value was not written into this connector or report.

Independent regression tests failed for three cases on the original subtraction.
The reviewed replacement is retained in `internal/worker/testdata/verified` for
repeatable testing. This is a small live transport/proposal check, not evidence
of repository-scale coding quality or parity with Astra.

Verification passed: provider regression suite, worker validation suite, worker
race test, and all four synthetic Add cases after the reviewed change. Windows
amd64 CLI cross-build also passed; it was not executed on Windows. An independent
read-only reviewer identified missing/null content and trailing JSON acceptance;
both are now rejected with regression tests.

```sh
go test ./internal/worker ./cmd/hermetrix-worker
go test ./internal/worker/testdata/verified
```

## Verification on 2026-09-16

Deterministic tests cover the task contract, exact-text edit materialization,
ambiguous/stale edit rejection, progress ordering and heartbeat shutdown,
timeout/cancel/HTTP retry classification, provider-detail redaction, one-request
semantics, malformed proposals, input/output ceilings, and credential-value
rejection. Provider, worker, and CLI packages pass under the race detector.
These checks use deterministic fixtures and do not depend on gateway behavior.

After those checks, the connector was built from the current source and called
the configured gateway/model with only `testdata/synthetic-task.json`; the
credential was read from the existing vault in memory. The provider completed
the final-source check in about 22.4 seconds and proposed the exact edit
`a - b` → `a + b`, with the 15-second heartbeat, all four progress phases, and
a stable proposal identity covering code and review metadata. Provider-reported
usage was 697 prompt + 399 completion = 1,096 tokens. The proposal remained unverified until
the separate four-case regression package passed both normal and race-enabled
Go tests. No repository source file was applied by the live connector.

Next stages: durable task/plan revisions and checkpoints; isolated workspaces
and preimage-checked application through the existing approval/effect receipt
path; bounded read/test loops with cancellation and evidence capture;
repository-level quality evaluations; optional MCP exposure. Windows runtime
testing remains separate from cross-compilation. Production/security execution
is not enabled.
