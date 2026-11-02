# Learning measurements and incomplete-output protection

This extends the [interaction checkpoint](2026-10-29-interaction-and-learning.md).
The architecture goal remains active. A completed experiment is evidence about
the candidate; it does not mean that the candidate improved the system.

## Changes and acceptance

The first trial's combined `incomplete or approval-bound turn` error cannot
identify its precise cause retrospectively. Its saved evidence lacks the aborted
request's complete identity and finish reason. Preserve that original report.

New observations retain session/turn/provider/catalog and prompt identities,
session contract revision, actual context/output/reasoning settings, finish
reason, token usage and approval state. An explicit lab reasoning setting must
be retained by the new session before any turn is sent. Existing sessions and
runtime defaults are not reconfigured. The optional lab CLI exposes this setting.

`model-reviewer-v2` rejects a length-limited generation as unusable even when its
visible JSON looks complete. A real HTTP streaming fixture verifies both the
complete and length-limited paths. The length-limited response cannot create a
Skill candidate.

Planning-lab v2 hashes the exact prompt, unchanged role authorization policy and
case definitions. The prompt now discloses that role policy equally to baseline
and candidate; v1 exposed only the global action vocabulary. Scoring remains
strict. Comparisons refuse missing/mismatched experimental contracts and changes
to context, output budget or reasoning between conditions.

## Actual Bonsai observations

One fresh development diagnostic compared runtime-default reasoning with explicit
disabled reasoning. Both used the same prompt and 4,096 output-token cap. The
wire capture confirmed `reasoning_budget_tokens: 0` and
`chat_template_kwargs.enable_thinking: false` for the disabled condition.
Observed durations were 29.236s and 4.893s, with 2,105 and 319 completion tokens.
Both scored 0/31 under the then-undisclosed role policy. This single observation
does not establish a general speed gain, learning benefit or the old failure's
cause. The diagnostic predates the final v2 protocol hash/prompt change.

A fresh v2 trial froze its protocol before the first call and used new personal
sessions, explicit disabled reasoning for all planning observations, the existing
three benchmark cases and two repeats in alternating condition order. The real
Learning reviewer generated an inert `planning-coverage-checklist` candidate
from the development example only. Its candidate hash is
`f24ed66ef24702a0255f4a55b933ea22467f3c398d3c2d3932699ee64be93050`.

All 12 paired observations completed. The recomputed result was:

| Measure | Baseline | Candidate |
|---|---:|---:|
| Strict planning coverage | 129/130 | 109/130 |
| Measured total tokens | 8,166 | 9,221 |
| Active Skills created | 0 | 0 |

One candidate answer used Markdown fences instead of the required bare JSON,
so its whole 21-item case scored zero. The comparison records 21 regressed
requirements and one improved requirement; this is not evidence of 21 separate
semantic mistakes. The decision is **keep_current**, with no automatic promotion.
The candidate remained `needs_review`; no checklist or evaluator was tuned using
these answers. This small reused benchmark is not newly sealed independent
generalization evidence. Positive held-out benefit remains unproved.

## Verification and remaining gates

The [validation record](../validation/2026-10-29-learning-fidelity.json) binds the
current sources, actual logs, protocol and raw trial report. The current complete
Go suite passed 1,545 test events across 36 packages, with 23 conditional skips
and no failures. Vet and build passed. Existing voice/UI
evidence is retained at the previous checkpoint; those assets are unchanged.
The pilot executable was rebuilt and its no-start preflight passed. The pilot
has not been served. Its original separate schema-81 runtime remains untouched.

Bonsai review v4 approved the supplied current code/evidence with no findings.
Review v2 exceeded the fixed input ceiling before adapter/network dispatch; its
local intent marker had already been consumed. Review v3 returned an oversized
2,241-character rationale and was protocol-rejected, never salvaged as approval.
V4 used a fresh identity and a shorter requested rationale with the same source,
validator and preset limits. Same-model review remains another pass on Bonsai,
not diverse review.

The native Project Brain read credential, approval for the intended pilot startup
and physical microphone/speaker acceptance are still pending. The existing
questions remain open. No alternative serving route or credential copying is
used. A6's positive-benefit requirement remains open; do not keep rerunning or
adjusting the same benchmark merely to obtain a favorable result.
