# Hermetrix planning lab — first bounded self-improvement trial

This optional supervisor lets Hermetrix propose a reusable planning checklist
from one development observation, then measures it against the current behavior
on separate tasks. It does not edit the serving application, promote Skills,
accept Tasks, deploy code, or train model weights. This is the first experimental
slice, not an autonomous successor-building system.

## What runs

1. Run the full Go suite with mandatory Chrome browser checks and all frontend
   Node tests. Require real passing tests and named core anchors for uncertain
   effects, current user acceptance, learning scope, Skill scope and promotion.
2. Open a fresh personal chat for one synthetic development request. Reject any
   returned session with project or effectful-tool authority.
3. Ask Hermetrix for a short checklist using that request, answer and coverage
   gaps. Keep the checklist as an untrusted artifact, never an active Skill.
4. Run three other planning requests twice in both conditions. Alternate the
   condition order. Every answer uses a different session and turn; do not carry
   the other condition's conversation into it.
5. Score the answers outside the model using the same fixed representation,
   per-role allowed capabilities and explicit invariant requirements. The model
   receives the vocabulary and customer request, not the grader's required subset.
6. Run core/browser/frontend checks again and compare original implementation
   and test fingerprints. Keep the current version unless all guards pass.

The output is a structured coverage plan. It can identify omissions in declared
capabilities and inappropriate role assignments. It does **not** demonstrate that
an app was built, a browser flow worked, a transition was implemented correctly,
or that general reasoning improved. Adding browser/API/database business tasks is
the next capability layer; do not substitute this score for their acceptance.

## Candidate review gate

`review_candidate` requires complete paired observations, two repeats, the same
provider revision and Skill catalog, measured nonzero token usage, valid JSON,
coverage of every required capability, a strict coverage improvement, no loss of
any previously covered requirement, and at most 1.5 times baseline tokens.

An equal score retains the current version. A gain on one requirement cannot
cancel a regression on another. An error, uncertain response, changed core/test
bytes, skipped required core anchor or incomplete experiment retains the current
version. The lab recomputes scores from answers; it never trusts model-supplied
grades. `auto_promoted` is always false, including a successful review candidate.

The original core fingerprint includes all files under `cmd/` and `internal/`,
plus `go.mod` and `go.sum`, except this new `cmd/hermetrix-lab` and
`internal/selfimprove` feature. It is not a database backup or an OS security
sandbox. The supervisor executable and fixtures belong to the operator; an
adversarial candidate must not get filesystem access to them in a future
code-generating experiment.

## Model/session boundary

Use an explicitly selected existing provider; no credential is printed, exported
or added by the lab. Remote prompts require `--allow-remote`. Fresh personal
sessions use the normal Harness API and retain its global Skill catalog, normal
calibration, event history and learning policy. They are named `Lab: ...` and are
not project tasks. This is isolation of task history and effectful tools, not a
second server or a private copy of the serving database. Do not claim only the
literal synthetic prompt reaches a provider if existing global Skills are bound.

The default `compact-32k` profile may exhaust the answer reserve on a reasoning
model. Changing `--context-profile` applies to new lab chats only. A higher
profile uses normal qualification, or an explicitly recorded
`--review-context-override` for this bounded experiment. An override is never
described as measured context qualification. Existing provider configuration,
shared presets and other sessions are not edited by the lab; the normal observed
provider calibration can change through these new chat turns.

At most 14 observations are dispatched: development, proposal and 12 held-out
answers. The model phase has a 20-minute bound, each HTTP call a 180-second
timeout, and an observed token dispatch ceiling. Reserve 16,384 tokens before
another observation; stop after an error or uncertain response. This is not a
guarantee about provider billing or unknown usage on a failed stream. No automatic
retry, model restoration or replay occurs.

## Run locally

From the Harness source workspace, create the parent evidence directory first.
Every run requires a new output directory so prior receipts cannot be overwritten.

```powershell
go build -o .tmp-ui/hermetrix-lab.exe ./cmd/hermetrix-lab

# No model call: verify core/browser/frontend only.
./.tmp-ui/hermetrix-lab.exe --workspace . --out .tmp-ui/lab-core-new --core-only

# Synthetic planning trial using an explicitly chosen existing provider.
./.tmp-ui/hermetrix-lab.exe --workspace . --out .tmp-ui/lab-plan-new `
  --server http://127.0.0.1:18801 --provider PROVIDER_ID --allow-remote
```

For the observed Qwen reasoning exhaustion, the supervised trial used
`--context-profile extended-96k --review-context-override --token-ceiling 220000`.
The lab caps selectable profiles at 96k and token ceilings at 250,000. Use these
options only as a reviewed experiment; they do not change the live app's defaults.

`report.json` is updated atomically after milestones and answers. It includes the
fixed suite hash, generated checklist hash, session/turn IDs, raw answers,
per-requirement omissions, tokens, latency, core fingerprints and final verdict.
Go logs are structured JSON lines; frontend logs and command stderr are separate.
An inconclusive/error exit is nonzero. `keep_current` without execution errors is
a completed comparison, not evidence that the candidate was better.

## Next successor-building layer

Add isolated source/data copies, actual task execution and private business
acceptance tests before permitting generated code. Pin the evaluator outside
candidate authority, compare matched baseline/challenger trials, independently
review actual changes, and require a staged release plus rollback evidence.
Preserve existing user-acceptance gates and original data throughout.
