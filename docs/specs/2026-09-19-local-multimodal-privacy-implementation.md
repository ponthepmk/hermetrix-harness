# Local Multimodal, Planner/Worker and Privacy — Implementation Specification

Date: 2026-09-19
Status: Implementation handoff; features and hardware qualification not yet implemented/verified.
Baseline: Git 08fcac4, schema v36. Recheck actual HEAD before implementation.

This document supersedes the proposed Downloads overlay for implementation decisions. The original hardware observations remain user-reported measurements, not reproduced results. This document does not authorize downloading large models, publishing data or invoking paid remote inference merely to run tests.

Related contracts:

- [Master project specification](2026-09-19-hermetrix-master-project-specification.md)
- [Correctness and performance remediation](2026-09-18-correctness-performance-remediation.md)
- [Implementation handoff](../handoffs/2026-09-19-sol-implementation.md)

## 1. Scope and decision precedence

Implement in the existing Go monolith, SQLite and CAS. Preserve existing API behavior through explicit compatibility paths. Use the existing task engine and worker structures, not a second planner engine. This overlay governs new ownership, sharing, presets and media behavior; remediation governs existing correctness boundaries. Existing behavior is not proof that a required invariant already holds.

All requirements here are TARGET requirements. Prefix OVL avoids collisions with existing SEC/PRV/MED/INF identifiers. SHALL/MUST are acceptance requirements. Defaults below are implementation decisions and do not require another design approval. Escalate only a concrete incompatibility or unavailable external evidence.

Deliver incrementally: first safe local text execution, then images, audio/video and portability. Do not report the whole overlay complete while later gates are pending.

## 2. Fixed decisions

| Topic | Decision |
|---|---|
| Local runtime | llama-server behind the provider adapter; no llama-specific branching in agent orchestration |
| Hardware | one local GPU execution slot initially; identify the actual host/device |
| Model | proposed Qwen3.8-27B UD-IQ3_XXS; verify installed model identity, template and projector before enabling |
| Context | 98,304 is requested runtime capacity, not a verified quality claim or default prompt size |
| Presets | immutable revisions, selected from a frozen role-to-preset binding |
| Review | retain distinct model/endpoint post-review gate; same-model planner is not an independent reviewer |
| Privacy | owner and visibility before new media; all existing user content backfills private |
| Export | explicit selected revisions; allowlisted field projection and checked dependency graph |
| Remote fallback | never automatic, including transcription, embeddings and review |
| Credentials | excluded from project sharing and workspace portability; full recovery handles vault separately |
| UI/config | existing settings/API architecture; YAML below is a design example, not a new YAML loader requirement |
| Migration numbers | allocate against actual next schema; planned groups v41–v46 assume remediation v37–v40 lands first |

## 3. Prerequisites and baseline gates

OVL-001. Before enabling new execution paths, close remediation SEC/AGT/PRV/DUR/FS boundaries used by those paths: request provenance, complete provider responses, frozen contracts, live lease authority and serialized file mutation. Require Windows child-environment fixes before qualifying Windows coding workflows.

OVL-002. Every provider dispatch, including planner, worker, learning, qualification, nested MCP sampling and team child, SHALL go through the common inference scheduler. Adapter-only protocol tests may call adapters directly; production services may not bypass admission.

OVL-003. Record initial HEAD, dirty paths, Go executable/version/architecture and targeted failing/passing tests. Existing failures remain visible and must not be converted to skips. Use an available amd64 toolchain for race evidence; record actual native OS coverage.

## 4. Principal ownership and visibility

OVL-004. Add an installation-local persistent principal ID. An unauthenticated loopback request uses that principal; authenticated requests resolve to it through the configured principal mapping. This is ownership attribution, not multi-user authentication. Owner IDs come from trusted request context, never caller-supplied JSON.

OVL-005. Sessions belong to a principal and may reference a Project. Add owner binding to sessions, tasks, artifacts, memories and Skills. Keep Skill origin/owner provenance semantics; add principal ownership separately instead of reinterpreting its existing owner field. Linked events/parts inherit session ownership. Public list/download/mutation paths validate ownership; unknown owners fail closed.

OVL-006. Standardize new fields:

```text
visibility    = private | project_shared
export_policy = deny | explicit_selection
revision      = monotonically increasing integer for share metadata
```

`project_shared` requires a valid Project ID. It means eligible for explicit project sharing, not public access or automatic export. Sessions and execution histories remain private in project-share packages. A principal's UI can list its own private and shared records.

OVL-007. Existing records backfill to the local principal, private, deny. New session-created artifacts, transcripts, summaries, embeddings and learned candidates default private. Setting Project ID never changes visibility. Shared promotion requires an explicit operator decision, expected revision and audit receipt. Agent/model tool arguments cannot grant this decision.

OVL-008. Derived outputs retain source lineage and inherit the most restrictive source policy. Declassification creates a reviewed, sanitized derivative and records source hashes, actor, reason and policy revision. Merely marking a raw private artifact exportable is insufficient to authorize its private dependencies.

OVL-009. Embedding/retrieval caches filter by owner and scope before ranking. Shared CAS storage does not confer read permission: authorize the Artifact reference, never accept an arbitrary blob hash as access authority. GC counts all authorized and private references across principals.

OVL-010. Local-only is an explicit egress policy persisted with the session/task. It applies to inference, embeddings, STT, image analysis and reviewer calls. A remote destination needs explicit user authorization and a new compatible binding before bytes leave the host; no availability fallback may override it.

## 5. Project sharing contract

OVL-011. Project share includes only explicitly selected source files, sanitized configuration, requirement revisions and shared Task/Skill/Memory/Artifact projections. It excludes sessions/events, provider usage, raw model prompts/reasoning, private checkpoints/tool logs, credentials and machine-local runtime state.

OVL-012. Export is a graph traversal from selected roots using a fixed allowlist of fields and references. Do not serialize DB rows or arbitrary metadata maps. A forbidden required dependency blocks export with a path to the offending reference; an optional dependency may be omitted only if the package schema permits omission and preview reports it. Never pull private sources into the package to satisfy a reference.

OVL-013. Source files use an explicit manifest. Exclude secret/config credential files, VCS internals, logs, runtime data roots, symlinks, device files and out-of-root paths by default. Show relative paths, bytes and hashes in preview. Content scanning is supporting evidence, not a guarantee of secret absence. Metadata such as EXIF/GPS, local absolute paths and tool command environment requires sanitization or exclusion.

OVL-014. Export preview produces an immutable manifest and digest containing selected revisions/content hashes, exclusions, policy revision, actor and expiry (30 minutes). Apply rechecks revisions/visibility and materializes exactly that manifest. A change yields 409 stale_preview; no silent regeneration. Once bytes have been downloaded, changing visibility cannot revoke prior copies; UI states that limit.

OVL-015. Package format v1 is ZIP with manifest.json plus content-addressed blobs. Manifest declares package_kind, format_version, source application/schema, export ID, object projections, dependency edges, sizes and SHA-256 hashes. It contains no active authority. Checksums detect corruption, not publisher authenticity.

OVL-016. Import is upload/stage → validate → preview → apply. Reject absolute paths, traversal, symlinks, duplicate/case-colliding entries, excessive compression and unknown mandatory fields. Limits: compressed 512 MiB, expanded 2 GiB, 10,000 entries, per blob 256 MiB; reject before exceeding limits. Validate checksum/size during streaming.

OVL-017. Import defaults to a new codeless Project and new object IDs with an ID map. Skills become candidates; tasks become draft copies with no valid completion evidence; no lease, approval, pending effect or process is restored as live. Imported material defaults private. Source files remain staged until explicit destination selection and conflict preview; never overwrite automatically. DB apply is transactional; unreferenced staged CAS is cleaned by bounded GC.

OVL-018. Project sharing, same-user workspace migration, full disaster recovery and existing Skill portability export are four distinct package kinds. Reject use of one kind in another importer. The existing backup endpoint remains compatible but its label must describe Skill portability.

## 6. Runtime identity and capability qualification

OVL-019. Runtime fingerprint records llama.cpp build/commit, resolved GGUF checksum(s), projector checksum if present, chat-template hash, tokenizer revision, model ID, KV settings, effective context capacity, GPU/device mapping and runtime configuration digest. If local evidence cannot supply a value, mark it unknown and restrict claims; never fabricate a checksum from a model name.

OVL-020. Qualification records observed modalities and supported per-request controls: tools, structured output, streaming, reasoning mode/budget, output cap, cancellation, token counting and image limits. Unknown/unsupported requested controls return 422 unsupported_capability before dispatch. Never silently discard them. CLI flags are not proof that equivalent per-request fields work.

OVL-021. Provider adapter translates normalized preset fields into the exact tested runtime wire format. Store both requested preset revision and effective parameter digest. A template/model/runtime change invalidates qualification and blocks the next dispatch. Changing global server settings while a frozen request is queued is forbidden.

OVL-022. The original suggested configuration is a starting point only: context 98,304, Q8_0 K/V cache, flash attention and one slot. Text and vision qualify separately. Vision requires matching supported projector and actual image inference evidence; removing --no-mmproj alone is not qualification.

OVL-023. Runtime capacity reduction, such as 98,304 to 81,920, creates a new fingerprint and requires a new binding. Existing sessions do not silently shrink. No immediate new 80k profile is required: use an existing qualified envelope that fits until a separately tested profile is introduced.

OVL-024. Qualification evidence includes prompt size, media dimensions/count, prefill time, time to first token, decode speed, peak VRAM/system RAM, truncation/finish reason, tool validity and outcome checks. Use repeated short/medium/near-capacity workloads and record hardware/runtime revisions. User-reported 14.7 GB and 20–40 tokens/sec are baseline observations, not release gates.

## 7. Presets and exact budget semantics

OVL-025. Preset revisions are immutable records. Include ID, role, context target/max, reasoning mode, reasoning token cap, answer reserve, total generation cap, temperature, other supported sampling fields, schema version and canonical content digest. Do not permit arbitrary pass-through runtime parameters.

OVL-026. ContextTarget and ContextMax denote total input tokens, including text, media, tools and rendered template overhead. The compiler SHALL satisfy:

```text
input_tokens <= min(preset.context_max,
                    qualified_context_limit - generation_cap - safety_reserve)
generation_cap >= reasoning_cap + answer_reserve
```

Generation cap includes reasoning and visible output. ContextTarget is a soft input goal; exceeding it is permitted within ContextMax when required evidence needs space. Never fill toward the target merely to use spare context. Existing compiler slice accounting remains authoritative; replace its output reservation with the effective generation reservation once, without double-counting.

OVL-027. Initial preset defaults:

| Preset | Input target | Input max | Reasoning cap | Answer reserve | Generation cap | Temperature |
|---|---:|---:|---:|---:|---:|---:|
| planner | 40,000 | 60,000 | 3,072 | 2,048 | 5,120 | 0.2 |
| worker | 16,000 | 30,000 | 0 | 1,500 | 1,500 | 0.1 |
| worker-debug | 20,000 | 30,000 | 1,024 | 1,500 | 2,524 | 0.1 |

These are bounded defaults, not guarantees that a large patch fits. Provider output policy must permit the selected generation cap; a profile capped at 4,096 rejects the planner default until explicitly reconfigured. Pinned constraints exceeding the effective input budget fail closed. Truncated patches/plans are not accepted as complete; split work into bounded proposals through the planner.

OVL-028. Session Contract freezes allowed role→preset revisions, runtime fingerprint, resource binding and transition-policy revision. StepBinding, task attempt and request ledger store the selected revision. A role switch may select only a prebound preset. Changes to the allowlist require a new session or explicit new task revision/run, never in-place mutation.

OVL-029. Counting is against rendered request semantics, including tools and template. Cache fragment counts by content hash, tokenizer revision, special-token options and preprocessing revision; fragment sums are estimates until full-request accounting. Media counts use qualified runtime measurements or conservative bounds and carry a quality flag. /tokenize text counts alone are not proof of total multimodal cost.

OVL-030. Task budget includes classification, planner, worker, review, nested MCP, media analysis and retries that are explicitly safe. Use remediation usage ledger for reservation/reconciliation. Human approval wait is excluded from active time; provider/tool/queue time is included without double-counting overlapping child wall time at parent level.

## 8. GPU scheduling

OVL-031. Resource identity is host ID + stable device UUID or validated local device binding. cuda:0 is an installation-local alias only. The same physical device maps to one scheduler resource across provider aliases, vision, STT and embedding services.

OVL-032. Execution capacity defaults to one. Separately track resident model/projector/KV allocations and transient headroom. Serial requests are not sufficient when several resident models exhaust VRAM. Do not auto-load/unload or alter model placement inside a frozen dispatch. Report resource_not_ready/OOM and require an explicit reconfiguration path.

OVL-033. Foreground and approval continuation have priority with bounded aging for background requests. Bound queue size (64 pending per local resource); excess returns 429 resource_queue_full. Waiting may be cancelled and must release its budget reservation if dispatch never occurred.

OVL-034. Release inference slots before waiting for tools or human input. Nested MCP sampling acquires its own slot with immutable owner context. Prove the cap=1 parent-tool-sampling path does not deadlock.

OVL-035. A cancelled request retains physical slot occupancy until the runtime acknowledges idle or the owned runtime is confirmed stopped. If state cannot be determined, quarantine the resource; do not start another request on a possibly busy backend. Connection loss does not authorize replay.

## 9. Planner/worker workflow and review

OVL-036. Use existing worker.PlannedStep/taskengine.StepSpec fields: key, title, instructions, requirement_ids, dependencies, checks, effect_scope. Persist plan artifact with exact requirement revision, plan revision, packet hash, allowed file scope, provider/runtime/preset binding and budget. Do not replace this structure with string-only steps.

OVL-037. First classifier is deterministic and revisioned. Existing valid active plans go directly to the runnable worker step. A simple task may receive a deterministic one-step plan only if scope and acceptance checks are explicit and bounded; uncertain complexity goes to planner. Record the decision and evidence, not hidden reasoning.

OVL-038. Worker produces proposals and uses existing review/apply/verification gates. Read/edit/run in the diagram does not authorize direct unreviewed workspace mutation. Human approval remains required by existing policy; classifier/preset cannot widen effect_scope.

OVL-039. Two consecutive confirmed execution/test failures for the same step trigger planner escalation with normalized error signature, diff, test evidence and prior attempted changes. Transport/configuration errors block for resolution; uncertain effects reconcile first and never count as a reason to replay. Maximum two planner escalations per step and existing task-wide budgets; exceeding either pauses with evidence.

OVL-040. Planner must revise the causal hypothesis or produce a bounded new plan. A plan revision creates appropriate new attempts; previously approved effects are not transferable. Forward concise rationale, constraints and evidence, never hidden chain-of-thought, to workers.

OVL-041. Preserve current independent post-review checks: different provider profile and different model or endpoint. Planner/worker may share a model, but a preset/name alias is not a reviewer. If no allowed reviewer exists, pause in awaiting_post_review and show reviewer_required. Human-only closure is out of scope in this release; never silently mark verified. A local-only task cannot automatically select a remote reviewer.

## 10. Multipart events and provider payloads

OVL-042. Add ordered agent_event_parts with id, event_id FK, ordinal, kind, optional text_content, optional artifact_id FK, bounded metadata_json and created_at. Enforce UNIQUE(event_id, ordinal), allowed kinds and appropriate exactly-one content/reference constraints. No base64 media is stored in agent_events/parts.

OVL-043. Preserve legacy text requests and events. An event with persisted parts reads from parts; an old event without parts reads from content. Do not concatenate both and duplicate text. New text-only writes may keep the existing representation; multipart events use parts as canonical content. Persist event/parts atomically; blobs precede the transaction.

OVL-044. Add optional parts to TurnInput; exactly one of non-empty content or non-empty parts is accepted. Validate owner, Project access, artifact completion, media type and count before committing the turn lease. Provider Message gains a typed content union with adapter-specific materialization. Unsupported modality returns 422 before sending a request.

OVL-045. First implementation materializes owned local Artifact bytes as image payload only for qualified vision dispatch. No arbitrary external image URL fetching. Do not inject audio/video bytes into a text adapter; use transcript/frame evidence. Content materialization is bounded and transient; request receipts store hashes and refs instead of copying base64.

## 11. Media processing and limits

OVL-046. Store raw uploads as immutable private artifacts after MIME sniffing and bounded validation. Defaults:

| Input | Upload ceiling | Decoding/processing limits |
|---|---:|---|
| Image | 8 MiB | JPEG/PNG/WebP, 24 MP, 5 images/vision request, reject animation initially |
| Audio | 128 MiB | approved WAV/MP3/FLAC/M4A, 30 minutes, <=2 channels, normalize to STT-supported PCM |
| Video | 256 MiB | MP4/WebM, 10 minutes, <=4K frame, <=32 sampled frames, sequential decode |

Caps are validated from decoded/probed content, not extensions alone. New media streaming upload limits are separate from generic 16 MiB Artifact creation and the generic JSON body limit.

OVL-047. Media job limits: one decode job per host initially, 1 GiB temporary-file budget/job, 2 GiB process memory ceiling where enforceable, image 120 s, audio 1,800 s, video 1,800 s wall deadline. Check available space before accepting and during processing. Exceeding an enforceable cap cancels with resource_limit; if a mandatory bound cannot be enforced on a platform, disable that processor there and report unsupported rather than claim containment.

OVL-048. Decoder/FFmpeg/Whisper processes use structured executable/arguments, minimal environment, owned temp directory, process-tree cancellation and no network protocols. Add a dedicated media processor allowlist; do not widen general agent command access to arbitrary FFmpeg. Reject playlists/external references and path escape. An installed executable and supported build are prerequisites, not an implicit software download.

OVL-049. Persist media_jobs states queued/running/completed/failed/cancelled/interrupted, owner, source refs/hashes, processor/model revisions, settings digest, operation ID, progress, attempt count, timestamps and result refs. Cancellation is idempotent. Derived artifact creation and successful job result linkage commit atomically after CAS writes.

OVL-050. Startup marks interrupted local processing explicitly. Deterministic local extraction/transcription may resume only after confirming the old owned process is gone and reusing the same idempotency identity. Model inference with uncertain dispatch follows provider reconciliation and is never automatically replayed. Partial output is not a completed summary.

OVL-051. Derived cache key includes owner/scope, source hash(es), processor/runtime/model revision, prompt/schema revision, crop/frame interval and preprocessing options. No cross-owner cache bypass. Default retention: raw/derived artifacts stay until explicit deletion; temp/staged failed jobs are cleaned within 24 hours with startup scavenging. Deletion of a source marks dependent evidence unavailable and invalidates caches; do not erase audit IDs silently.

OVL-052. Vision summary is reusable evidence, not a lossless replacement. Store source/crop/frame anchors, extracted text and limitations. Provide inspect-source and explicit reanalysis/crop actions when a new question needs missing detail. Cache reuse must not silently answer a different question from an insufficient summary.

OVL-053. STT outputs source hash, model revision, language, segment start/end in milliseconds and text. Confidence is nullable and tagged with the processor's actual score semantics. Record no-speech/partial/failure separately; do not invent calibrated confidence. Test Thai/English fixtures and silence.

OVL-054. Video extraction records source timebase, original presentation timestamps, rotation, selected frame hashes and audio offset. Sampling combines interval and bounded scene-change candidates; deduplicate under the 32-frame limit. Timeline links every assertion to frame/segment evidence and declares unobserved intervals. Never imply full-motion coverage from sparse frames.

OVL-055. OCR, transcripts, media metadata and summaries remain untrusted evidence. Their text cannot authorize tool calls, change scope or promote sharing. Redact metadata in export projections; raw-original sharing requires explicit preview of what metadata remains.

## 12. API additions

OVL-056. Use existing authenticated JSON/error conventions plus remediation typed errors. Lists are paginated, mutations bind actor from context. Proposed operations:

| Method/path | Contract |
|---|---|
| GET/POST /api/inference-presets | list / create immutable revision |
| POST /api/providers/{id}/runtime-qualification | enqueue qualified local probe; return job ID |
| POST /api/media/uploads | streaming media multipart upload; return private Artifact |
| POST /api/media/jobs | source refs + processor kind + bounded options + idempotency key; return 202 |
| GET /api/media/jobs/{id} | state, progress and safe result refs |
| POST /api/media/jobs/{id}/cancel | idempotent cancel |
| PATCH /api/artifacts/{id}/sharing | expected revision, visibility, export policy, reason |
| PATCH /api/tasks/{id}/sharing | same sharing contract |
| PATCH /api/memories/{id}/sharing | same sharing contract |
| PATCH /api/skills/{id}/sharing | same sharing contract; preserve Skill provenance |
| POST /api/projects/{id}/share/previews | selected IDs/revisions and source paths; stage validated export manifest |
| POST /api/projects/{id}/share/exports | preview ID + digest + idempotency key; return 202 export job |
| GET /api/share/exports/{id} | status and safe download link |
| GET /api/share/exports/{id}/content | authorized completed package download |
| POST /api/projects/share/import-previews | bounded streamed package; staged validation |
| POST /api/projects/share/imports | preview ID/digest + explicit destination policy |

Existing /api/artifacts/upload remains compatible with image uploads; it may delegate to shared media validation. Do not reinterpret its body shape silently. Add polling status endpoints for staged imports if processing is asynchronous; publish their final contract in the implementing PR before UI integration.

OVL-057. Idempotency uniqueness is scoped to principal + operation + client key, with a separately stored payload hash. Same key/different payload yields 409. Persisted same-request retries return the existing job/result without redoing external effects. Preview/status GET never starts an export or mutates source data.

## 13. Workspace migration and full recovery

OVL-058. Same-user workspace migration is a separate later milestone. Package includes selected private canonical content under an encrypted authenticated envelope. Implement with a maintained, pinned encryption library and established format (age v1 passphrase mode); never invent crypto or persist the passphrase. Unsupported/missing vetted dependency blocks this milestone rather than allowing plaintext private export.

OVL-059. Workspace import maps source principal explicitly to destination local principal, stages Project-root remapping and defaults local runtime configurations disabled pending qualification. Imported active turns/runs become interrupted/paused, approvals cannot execute, and unresolved effects remain unresolved. Exclude credentials, browser cookies/profiles, process handles and device-specific caches. Source version and format compatibility are checked before writes.

OVL-060. Full backup follows remediation recovery requirements for a consistent SQLite/CAS snapshot and separate protected vault capture. DPAPI ciphertext cannot be assumed portable across accounts/machines. Same-machine restore and cross-machine credential reenrollment are documented/tested separately. Project share never accepts backup packages.

## 14. Schema implementation plan

OVL-061. Proposed migration groups after remediation v37–v40:

| Group | Proposed version | Tables/fields and constraints |
|---|---:|---|
| Ownership | 41 | local_principals; owner FKs; visibility/export-policy checks; sharing revision; share audit records |
| Inference | 42 | immutable inference_presets; runtime qualification evidence; session role-binding snapshots; attempt/step/request preset fingerprints |
| Multipart | 43 | agent_event_parts; derivation edges with source/result FKs; indexes by event/ordinal and source |
| Media | 44 | media_jobs, idempotency uniqueness and result bindings; owner/state/recovery indexes |
| Sharing | 45 | share_previews/export jobs/import staging; manifest hashes, expiry, actor, idempotency records |
| Workspace portability | 46 | migration package/job metadata and source→destination ID/root maps |

Choose actual numeric versions from HEAD; never skip unimplemented versions or reserve them by setting user_version. Add nullable ownership staging fields, backfill local principal, validate and rebuild for final NOT NULL/FK constraints where needed. Preserve indexes/triggers and check foreign keys on populated aged fixtures. Missing owner/unknown visibility fails closed. Blob deduplication never merges ownership rows.

OVL-062. Snapshot preset fields into immutable contracts as well as FK revision IDs so audit survives retirement. Do not mutate historical contract JSON to imply it used a new preset; mark legacy execution explicitly and require a new binding for role switching.

## 15. UI acceptance

OVL-063. Composer exposes only qualified input kinds and shows source upload, processing, ready, failed and cancelled states independently. A failed upload/processor cannot be sent as completed evidence. Preserve text drafts and show source/derived citations.

OVL-064. Review panel shows effective preset, runtime revision, input/generation budgets, estimated/actual usage and egress policy. Task UI shows escalation reason, remaining budget and reviewer_required when blocked.

OVL-065. Share dialog presents included paths/objects, omitted private dependencies, remaining metadata and preview expiry. Use exclusion icons/text, not checked boxes that appear to include chat history. Workspace migration and full backup are separately named destinations.

## 16. Implementation work packages

| WP | Deliverable | Primary files/packages | Depends on | Exit gate |
|---|---|---|---|---|
| O0 | baseline + required remediation | web, agent, providers, taskengine, product, store, inference | none | relevant pre-fix regressions reversed; no bypass |
| O1 | ownership/private defaults/egress | store, web/auth, agent, skills, product, taskengine | O0 | old/new records private; cross-owner refs refused |
| O2 | runtime fingerprint + scheduler | providers, qualification, localmodel, inference | O0/O1 | cap=1, cancel quarantine, nested sampling no deadlock |
| O3 | presets + effective budgets | agent, context, providers, worker, taskcoord | O2 | generation accounting and immutable role switches |
| O4 | bounded planner/worker orchestration | taskengine, taskcoord, worker, web/tasks, UI | O3 | explicit plan coverage, escalation cap, reviewer block |
| O5 | image multipart vertical slice | providers, agent, product/media, store, web/UI | O1/O3 | source→vision→summary→cited answer on real runtime |
| O6 | audio/video durable processing | product/media, agentruntime, store, UI | O5 | native decode/STT/cancel/recovery and evidence anchors |
| O7 | project share/import | product/sharing, store, web/UI | O1/O5 | transitive private exclusion, preview race, malicious archive |
| O8 | encrypted workspace migration/recovery | product/portability, store, secrets, UI | O7 | restore drill + no authority replay |

Create focused packages only where boundaries justify them; these names are proposed ownership boundaries, not a directory rewrite mandate. Avoid frontend framework changes. Keep one schema owner per PR. Every package updates handoff with evidence and remaining work.

## 17. Mandatory acceptance matrix

| Test | Requirement coverage | Expected outcome |
|---|---|---|
| A01 old populated DB migration | 004–009,061 | every record has correct principal/private default; zero FK errors |
| A02 indirect private dependency | 007–014 | shared task/Skill cannot export private prompt/artifact via metadata or derived refs |
| A03 source and share TOCTOU | 013–014 | changed hash/visibility after preview yields 409; no partial downloadable package |
| A04 adversarial import | 015–018 | traversal, zip bomb, case collision, missing blobs and wrong kind rejected |
| A05 runtime drift | 019–024 | model/projector/template/context change blocks frozen dispatch |
| A06 preset wire semantics | 025–030 | requested limits match effective request; unsupported fields reject; no budget double-count |
| A07 GPU contention/cancel | 031–035 | one active inference; cancelled-but-busy backend quarantined; child sampling completes |
| A08 planner/worker | 036–041 | criteria mapped, no direct unauthorized write, max two escalations; same-model review refused |
| A09 multipart compatibility | 042–045 | legacy text unchanged, mixed parts round-trip, private foreign artifact denied |
| A10 media bounds | 046–055 | forged MIME/oversize decode/network reference refused; temp cleanup and cancelled tree |
| A11 cache correctness | 051–054 | question/crop/model revision change invalidates cache; citations retain source timestamp |
| A12 local-only egress | 010,041,045 | remote inference/embed/STT/reviewer receives zero requests without explicit permission |
| A13 workspace authority | 058–060 | encrypted package restores content; no live approvals/effects/credentials imported |
| A14 native end-to-end | 024,063–065 | installed runtime handles text/tools/image and supported STT/video, UI shows truthful state |

Use fake adapters for deterministic faults and real SQLite/filesystem for persistence boundaries. Hardware/model/native qualification is a separate evidence gate, not satisfied by mocks or cross-compilation. Test short/medium/near-capacity text and bounded image workloads with timestamps/runtime fingerprint. Report skipped/not-run gates as pending.

## 18. Release and evidence requirements

OVL-066. Each work package report includes requirement IDs, changed paths, command/toolchain, exit codes, observed user workflow, before/after evidence where applicable, known limitations and next dependency. Run focused tests first, then relevant integration/build/vet/UI checks; run release race/native/browser gates on supported hosts.

OVL-067. First release slice is O0–O5: corrected execution, privacy foundation, local scheduling, immutable presets, planner/worker and image evidence. O6–O8 remain explicitly pending until implemented and verified. A fully qualified overlay requires A01–A14 and remediation acceptance; unavailable hardware or reviewer is a named blocker, not a reason to weaken the gate.

## 19. Reference runtime documentation

- llama.cpp server controls and endpoints: https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
- Proposed model distribution: https://huggingface.co/unsloth/Qwen3.8-27B-GGUF

Pin the exact installed revisions in implementation evidence. These mutable links describe discovery sources, not immutable production dependencies.
