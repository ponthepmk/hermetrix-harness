# Discord remote control audit against Hermes

Date: 2026-10-02. Scope: inspect the running Hermetrix pilot, reproduce protocol
and agent behavior, repair a demonstrated defect, and compare the implemented
surface with current primary Hermes documentation.

## Runtime verdict

The pilot at `http://127.0.0.1:18801/` has Discord **disabled**, no stored bot
token, and empty application/server/channel/user/project/provider bindings.
This was confirmed by the status API and the actual settings screen. No message
was sent to a real Discord server, and real-account Gateway Ready is unverified.
The user was asked to configure their existing bot in the app, not disclose its
token in chat. The preserved Easiiot data and Task acceptance remain unchanged.

Hermetrix implements a narrower remote command interface, not Hermes feature
parity. Its six commands are `new`, `ask`, `status`, `approve`, `deny`, `cancel`
under `/hermetrix`. They use the same agent session and exact tool authority as
the local app. Only an allowlisted server/channel/user and a configured local
model may control the configured project. Replies are ephemeral and suppress
mentions. Configuration/token changes revoke remote bindings; session history
is retained.

## Reproduced and repaired

`/hermetrix cancel` stopped the agent job, but the bridge reused the cancelled
job context to edit the original deferred response. Consequently zero terminal
PATCH replies reached the Discord fixture: the original message could remain
"thinking" although the job had ended. The original cancellation test checked
job cleanup, not user-visible completion.

The bridge now separates bounded response delivery from the agent job context,
using the application lifetime and a 10-second delivery bound. Cancellation and
deadline results explicitly say that already approved effects may have completed
and direct the caller to the local receipt. It does not replay inference, undo
effects or deliver an approval preview as a successful cancelled result.
Interactions retain `cancelled` / `timed_out` receipts for duplicate suppression.
An application shutdown can still prevent delivery; network failure remains
`reply_failed` and requires inspecting the local session, never action replay.

Before-fix reproduction: `.tmp-ui/discord-cancel-before.txt` failed with
"original deferred ask received 0 terminal replies after cancel". After-fix
cancellation/deadline tests verify one terminal response and no repeated work.

## Verification

- Discord bridge and real AgentAdapter suite passed. It covers explicit scope,
  ownership, provider/profile checks, separate sessions, exact approvals/denials,
  replay persistence, failed acknowledgement, oversized previews, status during
  a running request, cancellation, rate handling, heartbeat/resume and stop.
  `.tmp-ui/discord-bridge-audit.txt`.
- Added a complete Gateway -> real agent -> exact tool approval/denial -> REST
  reply integration. A model fixture proposes a real workspace write; the file
  is absent before approval, contains exactly reviewed content after approval,
  and is absent after denial. Duplicate Gateway events produce no extra
  inference. Both replies are delivered and acknowledgements are ephemeral.
  The Gateway/Discord REST and provider are local fixtures; the agent, store,
  session admission, filesystem and approval authority are real.
  `.tmp-ui/discord-gateway-agent-audit.txt`.
- Required-Chrome web tests and all bridge tests passed together:
  `.tmp-ui/discord-final-checks.txt`. Focused HTTP/setup/browser results:
  `.tmp-ui/discord-web-audit.txt`; UI settings Node tests **13/13**:
  `.tmp-ui/discord-ui-audit.txt`. Vet and application build passed.
- Rebuilt and relaunched the 18801 pilot with the cancellation fix. Discord
  remains disabled until its real configuration is supplied.
- Race detection passed for the whole bridge suite:
  `.tmp-ui/discord-race-audit.txt` (109.716 s). The installed GCC initially
  could not compile even a minimal C probe; prioritizing its UCRT runtime path
  in the test process made that probe and the race build pass. This was an
  ephemeral test environment change, not a machine-wide PATH edit or install.

## Comparison and remaining scope

| Behavior | Hermetrix today | Hermes reference |
|---|---|---|
| Agent and per-user session | Same local agent core; bound server/channel/user | Gateway routes messages through per-chat/per-user sessions |
| Entry | Six explicit slash commands in allowlisted guild channels | DMs, mentions, free-response channels and threads |
| Tool decisions | Exact preview + ID/hash, approve or deny once | Interactive approvals; also session/permanent approval modes |
| Output | Private ephemeral text; long output redirects to local app | Rich replies, text/voice/files and interactive controls |
| Long work | Five-minute bridge job; Plans remains the durable workflow UI | Wider messaging commands including goals, queues and loops |

Primary references inspected on the audit date:
[Hermes Discord guide](https://hermes-agent.nousresearch.com/docs/user-guide/messaging/discord),
[gateway internals](https://hermes-agent.nousresearch.com/docs/developer-guide/gateway-internals),
[messaging command reference](https://hermes-agent.nousresearch.com/docs/reference/slash-commands).
The Hermes side is documentation comparison, not an executed Hermes installation.

Priority next checks: connect a real configured bot and confirm Ready plus an
actual `/hermetrix ask`/`status` round trip; inspect delivery failures without
replaying effects. For broader parity, separately scope durable Task control,
tool-progress output and interactive decisions, then DM/mention/thread and
attachment/voice support. Those expand the current product surface and were not
implemented by this audit.
