# Bonsai bilingual voice and workbench UX — implementation status (2026-09-27)

This compares `C:/Users/ZP2E0/Downloads/hermetrix_bonsai_bilingual_voice_stt_spec.md` with the current dirty checkout and an isolated Preview. It is an implementation record, not a claim that every acceptance gate passed.

## User-visible changes

| Area | Current implementation | Verification |
| --- | --- | --- |
| Chat | Opens without a Project. A personal session has no Workspace tools. Voice uses the selected session provider and automatically submits a final transcript once. | Agent tests, browser tests, real Bonsai text turn and local voice browser tests. |
| Workspace | Requires explicit Project selection for file/tool work. Flexible panes have no fixed four-pane limit; opening without a Project shows the picker. | Browser layout tests and prior 16-pane Preview inspection. |
| Plans | Opens with an overview; a Task opens a detail view with Goal, review and plan steps. Chat handoff opens an editable Goal before planning. | Browser Task and Plans tests. |
| Voice capture | AudioWorklet sends local 16 kHz mono PCM over authenticated same-origin WebSocket to Windows VAD and resident whisper.cpp. Partial hypotheses are display-only. A final transcript enters the existing session turn API with an idempotency key. Batch transcription remains available. | Live WebSocket probe and opt-in Chrome E2E. |
| Voice reply | Local Windows TTS plays the streamed answer when enabled; speech start interrupts playback, and the user can stop output. | Chrome E2E observed TTS playback state. Subjective speaker playback and repeated barge-in remain unverified. |

## Runtime checked

### Concurrent GPU check (2026-09-27, later user requirement)

The user explicitly requested that Bonsai and Whisper run on the GPU at the same time. This supersedes the earlier conservative plan to avoid overlapping inference until measured. Both servers were loaded on the single RTX 5070 Ti 16 GB: Bonsai 27B PQ2_0 with 16,384 context and one slot on loopback port 8088, and Whisper Large v3 Turbo Q5_0 CUDA on loopback port 8082. During a 27.9-second Bonsai generation (2048 output tokens), 12 Whisper transcriptions of the same 3-second fixture ran inside the Bonsai generation window. All 12 returned the expected words in 0.31–0.36 seconds each after warmup. GPU utilization reached 97%; sampled VRAM peaked at 11,576 MiB overall, and the 7.2-second overlap window retained at least 4,537 MiB free. Bonsai completed and both services remained healthy. This proves concurrent GPU execution for the tested short speech and generation workload. Long context, multiple LLM slots, Comfy video generation and physical microphone use were not included. The Bonsai test server is bound to loopback and its CORS origin is limited to the Hermetrix Preview.

The tested servers and Preview were running again after the benchmark. Whisper was started as a hidden background process. Automatic approval review rejected launching Bonsai as a detached background process (`blocked by policy`), so Bonsai and the Preview were run in attached test sessions; their lifetime after this task is not guaranteed. Recheck the three health endpoints before relying on the Preview in a later session.

- Isolated Preview: `http://127.0.0.1:18734/`; original 18731 and 18732 Previews were left untouched. The isolated data includes a Bonsai OpenAI-compatible provider at `http://127.0.0.1:8088/v1`.
- User-owned Bonsai `llama-server` on port 8088 was healthy with `Ternary-Bonsai-2-27B-PQ2_0.gguf`. A personal Hermetrix Chat turn returned “ระบบเสียงพร้อมทดสอบ” in about 1.55 seconds. It was not restarted or reconfigured.
- whisper.cpp `large-v3-turbo-q5_0` CUDA server is resident on loopback port 8082. The installed model is about 574 MB on disk and used about 573 MB GPU model buffer. A 3-second English clip took about 479 ms for warm server inference; CPU small model took about 4.6 seconds for the same clip. These are single samples, not latency percentiles.
- With 100 ms frames, a WebSocket probe of the English clip reached speech start at 403 ms, first partial at 1540 ms, and final at 3253 ms from capture start. After changing browser frames to 40 ms, one probe reached speech start at 485 ms, first partial at 1763 ms, and final at 3265 ms. Smaller frames did not improve this single observed partial latency; the proposed 1-second p95 target is not established.
- Thai synthetic speech transcribed correctly through direct Whisper inference; one Chrome live run changed “เสียง” to “สิน”. A continuous Thai + English synthetic clip transcribed both languages in one WebSocket final; the opt-in Chrome flow with a fixture provider also passed when the fake microphone began before the phrase. With the real Bonsai provider, Chrome's looping fake microphone sometimes began mid-clip and produced only the English portion. Real human microphone speech and mixed-language browser + Bonsai acceptance are still open.
- A Chrome flow with an English synthetic phrase and real Bonsai completed partial/final transcription, automatic personal Chat submission, answer and TTS state. A Thai synthetic Chrome + Bonsai flow completed with a Thai transcript and answer. The mixed prompt sometimes exhausted a 512-token Bonsai output budget on reasoning; test configuration was increased to 2048. Provider behavior under sustained mixed speech remains unverified.
- Parallel full Go runs intermittently failed Chrome UI assertions for preset persistence, research entry, first Chat reply and model-dialog draft preservation. Project opening now gates shell interaction until hydration completes, and a sequential `go test -p 1 ./... -count=1 -timeout 240s` passed. The 44 Node UI tests and syntax checks passed. Repeat normal parallel runs before declaring the flake closed.

## Gaps against the attached specification

1. The browser now sends 40 ms frames as proposed. First-partial latency and its distribution still need profiling with natural Thai/English speech before setting latency claims.
2. The WebSocket uses one active voice slot and now issues a voice session ID and one voice turn ID per finalized utterance. That ID is used for the text turn's idempotency key. Distinct Audio/Transcript Artifacts with source hash, model version and retention controls are still absent. Raw capture is currently ephemeral; only a submitted final text turn is durable.
3. A loopback resident server can fall back to CPU CLI on failure. The short-workload Bonsai/Whisper concurrent GPU test above passed, but a formal VRAM reservation, long-context and Comfy peak measurements, visible queue status and CPU fallback benchmarks are not complete. The full unquantized Turbo model was loaded for a cold comparison, then stopped because it left little free VRAM beside Bonsai and ComfyUI; concurrency with that larger model was not claimed.
4. Voice playback can be interrupted. In-flight Bonsai generation is allowed to finish before the next queued utterance because cancellation previously quarantined the local inference resource. This differs from full generation cancellation and needs a safe provider-level recovery design.
5. A second real LLM provider, microphone permission-denied path, no-device path, natural microphone, and repeated interruption/recovery have not passed end-to-end acceptance. Social publishing remains per-destination approval gated and was not exercised by this voice change.
6. Project/open hydration previously raced early Chrome interactions; the shell now stays inert until loading finishes. Verify this remains stable under parallel test load and slower machines.

## Acceptance sequence still needed

1. Record user-approved natural Thai, English and code-switching samples locally; compare Q5 and unquantized Turbo on the same clips for exact words, language retention and first partial/final latency distributions.
2. Add provenance/retention UI and durable transcript metadata, then verify replay and refresh cannot submit the same utterance twice.
3. Extend the concurrent GPU measurement to long-context generation and Comfy work; add a resource queue or CPU fallback threshold based on measured peak VRAM.
4. Run two real provider types and repeat voice interruption, restart and no-device/permission recovery. Verify user-visible outcome in a real browser and on physical mic/speakers.
