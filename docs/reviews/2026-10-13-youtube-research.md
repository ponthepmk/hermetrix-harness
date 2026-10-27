# YouTube audio, vision and research — 2026-10-13

The owner explicitly selected actual audio transcription and visual analysis,
including adding appropriate local models. The module is in
`scripts/video-research/README.md`. It provides an owned foreground CLI for the
existing Hermetrix workspace.run lifecycle and an optional local HTTP/MCP UI.
There are no YouTube account actions, cookies or login automation.

## Models and evidence

Qwen3-VL-4B-Instruct Q4_K_M plus its Q8_0 visual projector were downloaded from
the official Qwen repository and verified against pinned revision/hash/size.
The model runs on loopback8089, PID53620, with limited GPU layers and CPU
projector/KV to coexist with original Bonsai8088 PID32164. Whisper
large-v3-turbo runs CPU/int8, two threads, beam1, single worker. The manifest
is `D:/llm-server/Hermetrix-research/models/manifest.json`.
Dependencies are isolated under `.tmp-ui/youtube-research-20261013/python-packages`.
PyAV16.0.1 is pinned: PyAV19 actually failed faster-whisper1.2.1 decoding.
FFmpeg and the bundled Node/Python runtimes are used. No original model stop.

The public clip `https://www.youtube.com/watch?v=jNQXAC9IVRw` was actually
downloaded, audio decoded/transcribed, and two real frames sent to Qwen.
Actual frames show a man, elephant, fence, hay and rocks. Raw ASR misheard
some important words; manual captions were separately read and compared,
retained with the raw ASR, and used to clarify the synthesis. Similarity is
an observed comparison metric, not proof of perfect transcription. This is a
short English-clip verification, not comprehensive Thai/long-video qualification.

## Causal fixes and completion authority

- yt-dlp's combined best format did not exist for the actual clip. Separate
  video/audio download and FFmpeg merging resolved it.
- Free Bing RSS returned unrelated results. Domain/term relevance filters
  reject them; Wikipedia fallback is labeled metadata. Explicit authoritative
  URLs are supported and remain readable when search fails. No search result
  alone is accepted as corroborated information.
- Bonsai produced malformed or nonsensical Thai despite stopped generation.
  Qwen now performs both frame reading and Thai synthesis. Valid JSON alone
  does not establish user-visible correctness; failed quality receipts remain.
- Generated speech quotes/timestamps drifted. The model now selects actual
  evidence IDs; code binds timestamps and sources. Adjacent split caption
  entries are grouped into a real contiguous range, with original indices.
- Local b10743 accepted simple nested OpenAI-style schemas, but ignored the
  entire complex grammar when it contained a Unicode exclusion pattern.
  Actual adversarial probes reproduce the difference. Wire schema omits that
  pattern while preserving structural/size constraints; the full local
  validator still checks it. Wrong types, unknown IDs, excessive counts,
  empty/overlong output and truncated generation fail visibly.
- Filming dates are not established by this pipeline. A publication/upload
  date cannot silently become a filming date. Output instructions and an
  additional conservative Thai date guard enforce this boundary; it is not a
  general automated fact-check guarantee.
- A native MKL allocation failure occurred during a four-thread/beam3 run.
  Two-thread/beam1 verification retains the failure evidence. General resource
  contention still needs broader load qualification.

Source tests, actual model results and failed reports are in
`.tmp-ui/youtube-research-20261013/`. Acceptance and Hermetrix/browser receipts
are recorded below after their observed terminal results; no game acceptance
or complete Codex parity is implied by this module.

## Runtime boundary

Original18810 PID22292 and isolated18812 PID91896 remain unchanged binaries.
The research project/session/provider were added only to scratch18812:
project_b6b6a266e7989b651950008fd7300c84,
session_60a215017617dad8045156e80cca09ee,
provider_05cbf31ef07e9e745f4e9bf0559e82e5.
They have ordinary compatibility, with no qualification override or automatic
file permission grant. The CLI uses normal Job Object ownership and terminal
receipts; no detached/unref child or artificial successful job is used.

The initial HTTP service18814 PID75232 is **stale and not qualified**. Restart
was rejected before execution with generic `blocked by policy`; no detailed
policy class was returned. It was not stopped/replaced through another route.
No MCP registration to the stale service occurred. The launcher refuses its
old code revision. Human closure of that exact service is needed before the
prepared same-port launcher can activate new HTTP/MCP code. Finite source
verification and ephemeral browser QA are tests, not replacement deployment.

New runtime probing also found Bonsai's per-request reasoning-budget fields
were not enforced. `chat_template_kwargs.enable_thinking:false` was honored.
Earlier core wire/admission tests do not prove a hard runtime cap. This new
finding qualifies the prior workflow/reasoning review; a core adapter follow-up
remains separate from this module. No core deployment is claimed here.

## Final observed source acceptance

Latest actual report: `scripts/video-research/.research-jobs/4cd3d17658be473481efda0cbdb2dde7/report.md`.
The finite source integration `TestLiveHermetrixBonsaiVideoResearchOwnedWorkflow`
passed143.54s. Actual Bonsai read README, started exactly one normal owned Node
command, followed its exact job ID to completed/exit0, read the real report and
returned the report in Thai. The actual clip went through Whisper audio4segments,
Qwen2sampled frames and1read YouTube Blog page. No deterministic/model-stub
response was used. Temporary test DB only: this is not live18812 deployment.
Raw receipt: `.tmp-ui/youtube-research-20261013/hermetrix-bonsai-source-workflow.json`;
log: `hermetrix-bonsai-source-workflow-final-cue.txt`. The actual command is
job_24f9e0864995714af4ac01657d46c0f2, with1222output characters after throttling.

Additional causal fixes from the real controller workflow:

- An initial five-source expansion reached19465tokens against the16384runtime
  context and failedHTTP400. Sources are no longer duplicated in synthesis.
  Explicit source URLs take priority; automatic search is not added to an
  explicit-source request unless the client supplies a research query.
- Progress output previously caused many context-artifact recovery calls and
  exhausted the12model-step controller budget. CLI output is now sparse status
  plus terminal metadata/path; the actual report is read as its own artifact.
- A caption sound cue `(baaaaaaaaaaahhh!!)` was incorrectly attributed to the
  human speaker. Entire bracketed sound/stage captions are excluded from speech
  evidence while the raw captions remain in audit. Full cue text is not re-fed
  through caption-comparison metadata. The final report has no such attribution.
- One corrective draft tried Thai/fullwidth digits after an ASCII constraint.
  Correction now selects actual date-free passages, excludes the erroneous
  draft from new model input, validates Unicode numeric characters and still
  fails if the one bounded repair is invalid. The original draft remains saved.
- The compact overview projects evidence-bound points; the controller displays
  the verified report instead of adding another free paraphrase. Earlier
  Chinese contamination/speculative emotion is retained in failed-quality logs.

Actual latest-report Chrome QA passed at390/1440: both real JPEGs load, invalid
pointer submit shows recovery, valid pointer submit returns the actual cached
report, and no page errors occur. Log `browser-final-hermetrix-report.txt`.
The initial browser run failed because its test did not enable Page before
installing early error listeners; the harness was repaired and rechecked.
10Python tests pass; Python/Node syntax and PowerShell AST checks pass. These
checks cover this extension, not a repeated full-repository release qualification.

Live limitation remains: scratch18812 Qwen-controller attempts invented paths
and failed even after explicit guidance; existing Bonsai session creation in
that old binary refused the91% historical reasoning calibration. Source with
explicit verified thinking-off control completed the real workflow. Both live
core update and18814companion activation still require the execution boundary
to be resolved by the human. No production/full-app completion is claimed.

## Later natural-client and resource checkpoint — not complete deployment

A natural Thai client request initially exhausted24tools by scanning historical
cache files. The operator README was shortened and setup moved to SETUP.md.
The next run repeated `node` inside argv, creating two failed processes; a
generic source diagnostic now rejects this precise absent-file mistake before
dispatch, supplies corrected arguments, and preserves real scripts named node.
The tool schema explicitly separates executable and argv.

With that repair, the natural request started1real command, completed exit0,
and read the actual report `bb8da708ab0a40ffade062c0af50e3af/report.md`. It then
re-read files and hit12model steps before returning its answer. This is a
remaining user-visible failure, not a passed natural-client scenario. Source
now derives a narrowly correlated completion handoff from the same-turn
owned CLI command, terminal metadata and actual report read. Its bounded small
report is retained as untrusted tool data; total/pinned/active/cumulative caps
and authority remain unchanged. Unit tests reject premature, failed,
cross-turn, mismatched-job, false-verified and incomplete-read handoffs. Large
reports still spill normally. The last handoff repair still needs a real-model
natural-client retest.

That retest could not finish: original Bonsai PID32164 crashed. Its existing
`project-automation-20261013/bonsai-error.txt` ends with `bad allocation` and
GGML_ASSERT(batch.slot_batched || batch.size() ==0), at15:12:55local.8088has no
listener. The agent did not issue a Bonsai stop. The actual binary help reports
RAM cache8192MiBdefault. New Qwen private memory grew from approximately6.3GB
to13.6GB; shared resource pressure is a plausible contributor, not fully
isolated proof of the allocation cause. The bounded startup source disables
extra RAM-cache and idle-slot snapshots and refuses an unqualified existing
vision configuration.

A concrete verified-idle stop/restart proposal for the agent-created vision
PID53620 to apply that bound was itself rejected before any command executed,
again with generic blocked by policy and no detailed classification. PID53620
and stale Python PID75232 remain unchanged. No alternate stop, cache-erase,
replacement service or retry route was used. Human-resolved restoration of
Bonsai, bounded vision activation and stale companion/core activation are now
needed for final live/natural verification.

Latest prepared core: `.tmp-ui/hermetrix-video-research-ready-20261013.exe`,
50,909,696bytes, SHA256
CEC37DC56901DC9AA91CCFB30DE5EE0B53EDBB39ED8CE69259D938754918D064.
Not deployed. Complete Agent48.960s and Tools1.713s passed after handoff source;
the first combined run failed because its new Context test referenced a
nonexistent Report.FinalTokens field. The test was corrected to the actual
PredictedInput ledger; complete Context/Tools and vet then passed. Python10,
Node/Python syntax, PowerShell AST and actual latest-report browser checks
passed. Preserve all failed raw runs. No original18810/18812 upgrade, game
acceptance, Project Brain change, self-improvement resume, commit or PR.

## External implementation references

- [Qwen official GGUF repository](https://huggingface.co/Qwen/Qwen3-VL-4B-Instruct-GGUF)
- [faster-whisper implementation](https://github.com/SYSTRAN/faster-whisper)
- [llama.cpp server API](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)
- [Actual primary history source read](https://blog.youtube/news-and-events/celebrating-10-years-of-youtube/)
