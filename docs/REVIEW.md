# Hermetrix Harness — Multi-pass Review Log

เอกสารนี้บันทึก review ที่ทำระหว่างสร้าง foundation เพื่อให้การตัดสินใจตรวจย้อนกลับได้ ไม่ใช่ assertion ว่าระบบ production-ready แล้ว

## Review 1 — License and product boundary

ผลตรวจ:

- Aetox รุ่นที่อยู่ใน workspace เป็น proprietary source-available ห้าม modify, rebrand หรือ reuse source ใน competing product
- requirement ด้าน UX/function นำมาใช้เป็น research input ได้ แต่ implementation ต้องสร้างใหม่

การแก้:

- สร้าง `Hermetrix-harness` เป็น Go/HTML/CSS/JS ใหม่
- ไม่ copy source, component, asset, logo หรือ branding
- แยก product parity ที่ยังไม่ทำออกจาก foundation อย่างชัดเจน

## Review 2 — Skill authority and provenance

ความเสี่ยงที่พบ:

- agent-created Skill อาจไหลเข้า active store โดยไม่มี approval
- improvement อาจใช้ metadata เปลี่ยน scope/owner/origin ของ Skill เดิม
- promotion ที่ base เก่าอาจทับ version ใหม่
- restore อาจ re-enable Skill โดยไม่ผ่าน review

การแก้:

- background reviewer เขียน candidate ได้เท่านั้น
- transaction promotion ตรวจ state/checks/revision/base version
- metadata authority ของ existing Skill immutable ใน content improvement
- `agent_candidate` เปลี่ยนเป็น `agent_promoted` หลัง explicit promotion เท่านั้น
- archive restore สร้าง proposal จาก exact archived blob และกัน restore ซ้ำ

หลักฐานทดสอบ:

- proposal ไม่ปรากฏใน active list
- stale-base promotion fail
- metadata-changing promotion fail
- restored bytes ตรง archive และ active state ไม่เปลี่ยนก่อน promotion

## Review 3 — Package and mutation safety

ความเสี่ยงที่พบ:

- unsafe relative paths, duplicate files, oversized/binary packages
- edit race ระหว่าง UI/worker
- DB update สำเร็จบางส่วนแต่ event/version ไม่ครบ

การแก้:

- canonical path/size checks และ binary quarantine
- content-addressed blob + checksum verification
- optimistic candidate revision
- promotion/archive/restore ใช้ SQLite transaction
- immutable versions และ append-only events

Remaining gap:

- executable scripts ยังไม่มี sandbox/replay runner
- orphan blob GC ยังไม่มี
- ไม่มี cryptographic actor identity ใน local single-user build

## Review 4 — Background learning lifecycle

ความเสี่ยงที่พบ:

- retry สร้าง candidate ซ้ำ
- restart ทิ้ง review ค้าง
- background LLM แย่ง compute กับ foreground
- reviewer ได้ transcript ใหญ่/secret มากเกินไป

การแก้:

- persisted idempotency key และ unique `source_review_id`
- recover `running` → `queued` ตอนเริ่มโปรแกรม
- foreground preempts cooperative background; waiting foreground มี priority
- reviewer รับ bounded structured digest
- failure/cancel ถูก persist และ candidate ยังผ่าน normal checks

Remaining gap:

- scheduler ยังไม่มี battery/thermal/VRAM telemetry
- reviewer ปัจจุบัน deterministic acknowledgement ไม่ใช่ semantic learner
- trigger producer จาก agent turn/session จริงยังไม่เชื่อม

## Review 5 — Context selection correctness

ความเสี่ยงที่พบ:

- direct tool schemas กิน window ก่อน history
- pinned user goal ถูก drop โดย priority selector
- tool call กับ result ถูก dedup/compact คนละฝั่ง
- compactor แต่งข้อเท็จจริงหรือให้ผลไม่ deterministic
- ลืม reserve output/next tool burst

การแก้:

- exact slice profiles รวมเป็น 32,768/65,536/131,072/262,144/1,048,576 พอดี
- hard fail เมื่อ direct tools หรือ pinned slice overflow
- tool fragments ใช้ ID+pair ID ใน dedup; pair เป็น selection/compaction unit
- pin หนึ่ง fragment แล้ว propagate ทั้ง pair
- structured extractive checkpoint พร้อม source IDs
- reserve output, uncertainty และ worst-case tool burst ก่อน compile สำเร็จ
- integrity report วัด pinned retention และ causal pairs selected/compacted/omitted

Browser diagnostic ล่าสุดใช้ input estimate 43k+ บน profile 32k แล้วรักษา essential retention 100%, spill tool output และอยู่ใน reserve โดยไม่ claim ว่า compression ratio เท่ากับ task fidelity

Remaining gap:

- exact tokenizer adapter ต่อ model
- semantic compactor + independent fidelity validator
- task-level differential eval เทียบ warm context กับ compact context
- compaction lineage/archive window และ summary-on-summary ceiling

## Review 6 — Runtime context truth

รอบแรก probe ใช้ metadata ที่ยังสับสนระหว่าง model maximum กับ runtime allocation และพบ URL suffix bug ของ LM Studio

การแก้รอบที่สองหลังตรวจ official runtime docs:

- Ollama ใช้ `/api/ps.models[].context_length`; `/api/show` แสดง configured/training แยกกัน
- LM Studio ใช้ native `/api/v1/models` และ loaded instance config
- vLLM ใช้ JSON `/server_info` runtime config
- llama.cpp ใช้ `/props` หรือ minimum `/slots`; `/v1/models` ใช้ training metadata เท่านั้น
- unverified result ถูกจัดเป็น `limited` เสมอ
- endpoint จำกัด loopback และ response/time bounds

Remaining gap:

- API key สำหรับ local runtime ที่เปิด auth
- runtime/version/reload-aware probe cache
- allocation smoke prompt และ OOM recovery
- full model qualification: tools/schema/cancel/recall/performance

## Review 7 — API and browser UX

พบและแก้:

- nil Go slices เคย serialize เป็น `null` → bootstrap collections บังคับเป็น arrays
- form handler ใช้ `event.currentTarget` หลัง `await` → เก็บ form reference ก่อน await
- native `prompt/confirm` ทำ browser automation ค้าง → เปลี่ยนเป็น in-app modal
- model form ล้น panel แคบ → ใช้ minmax responsive grid
- context sample เคยมี tool result ฝั่งเดียว → เพิ่ม tool-call คู่เดียวกัน
- inspector แสดง version/hash/provenance/usage และ reversible action

Browser flows ที่ exercise แล้ว:

- empty bootstrap
- create/promote Skill
- inspect provenance/usage
- propose improvement/edit candidate
- archive/restore proposal
- run curator report
- compile context diagnostics
- MCP save → discover → bounded search → on-demand schema/revision inspection

Remaining gap:

- accessibility audit เต็มรูปแบบและ mobile layout ต่ำกว่า 980px
- E2E suite ที่รันใน CI แทน manual browser pass
- localization และ desktop packaging

## Review 8 — Verification

Checks ที่ต้องผ่านก่อน handoff:

```text
go test ./...
go test -race ./...
go vet ./...
node --check internal/web/ui/app.js
browser smoke + visual inspection
```

## Open risks ordered by severity

1. Background process มี no-shell/path/deadline/cancel hardening แต่ยังไม่มี OS-level sandbox/network policy
2. MCP breadth ยังขาด stdio, OAuth, resources/prompts/subscriptions, MRTR และ connection edit/export UX
3. Model qualification ยังขาด exact tokenizer, RAM/VRAM/OOM telemetry และ live interjection
4. Product shell ยังไม่มี managed browser surface และ native desktop packaging/signing
5. local API ไม่มี auth แต่ CLI บังคับ loopback แล้ว
6. semantic local-LLM compactor ยังไม่ส่งมอบ; verifier/fallback และ deterministic fidelity evaluator พร้อมแล้ว
7. actor identity ยังเป็น local logical actor ไม่ใช่ cryptographic multi-user identity

## Review 9 — Provider, agent and bounded-tool vertical slice

สิ่งที่เพิ่ม:

- provider profile เก็บ endpoint/model/context evidence และชื่อ environment variable เท่านั้น ไม่เก็บ credential value
- remote endpoint ต้อง HTTPS; local HTTP จำกัด loopback
- session events, context snapshot และ StepBinding ถูก commit ก่อน sampling
- OpenAI-compatible SSE รองรับ content, reasoning phase, usage และ streamed tool calls
- core read tools freeze schema/revision/effect ต่อ step, reject unbound tools และกัน path/symlink escape
- active Skill ที่ selector เลือกถูก inject แบบ version-bound และเขียน exposure-only activation receipt

Integration test กับ external OpenAI-compatible Qwen endpoint ยืนยัน streaming สองกรณี: final response บน 128k envelope และ model-requested `workspace.list_files` ที่ได้ normalized receipt แล้ว sampling ต่อใน step ที่สอง การทดสอบนี้ยืนยัน protocol flow แต่ยังไม่รับรอง long-context fidelity 128k

## Review 10 — Approval-gated write and restart uncertainty

ความเสี่ยงที่พบ:

- การ expose write tool โดยไม่มี durable approval อาจ mutate workspace ก่อนผู้ใช้เห็น payload
- approval ที่กว้างเกินไปอาจถูก reuse กับ call หรือ content อื่น
- ไฟล์อาจเปลี่ยนหลัง preview แต่ก่อน execute
- process อาจหยุดหลัง claim effect lock ทำให้ไม่รู้ว่า mutation เกิดแล้วหรือยัง
- tool call ที่ค้างโดยไม่มี result ทำให้ causal history และ provider protocol เสีย

การแก้:

- write request ต้องเป็น tool call เดียวใน step และหยุด session ที่ `awaiting_approval`
- pending approval เก็บ exact call, handler revision, effect และ SHA-256 ของ arguments
- UI แสดง path, byte count และ bounded content preview ก่อน approve/deny แบบ one-shot
- existing file ต้องส่ง SHA-256 จาก read receipt; create ต้องส่ง `absent`
- execution เปลี่ยน state เป็น `executing` ก่อน mutation, ใช้ atomic rename และ commit normalized receipt ก่อน resume
- restart ไม่ retry `executing`; เปลี่ยนเป็น `uncertain` พร้อม synthetic tool receipt ที่สั่งให้ inspect workspace
- denial ก็สร้าง receipt และให้ model ดำเนิน turn ต่อโดยไม่มี mutation

หลักฐานทดสอบ:

- direct registry execution ไม่สามารถเขียนหากไม่มี matching grant
- grant ที่ argument hash ไม่ตรงถูก reject
- stale file hash ไม่ overwrite เนื้อหาใหม่
- agent pause ก่อน write, persist approval แล้ว resume step เดิมหลังอนุมัติ
- denial ไม่สร้างไฟล์และ model ได้ denial receipt
- interrupted effect recovery ไม่เขียนซ้ำและปิด causal pair ด้วย `uncertain`
- browser QA ด้วย local provider stub ยืนยัน `awaiting_approval`, bounded preview, ไฟล์ยังไม่มีอยู่ก่อนอนุมัติ, exact-write confirmation, executed receipt และ model continuation ใน turn เดิม

Remaining gap:

- approval preview ยังเป็น complete-content preview ไม่ใช่ unified diff
- write ยังไม่มี backup/undo artifact ระดับไฟล์
- generalized process/network sandbox, cancellation และ idempotency key ยังไม่ทำ

## Review 11 — Deferred capability graph and dual-era MCP

### Pass A — Prompt scaling and exact binding

ความเสี่ยง:

- การใส่ MCP schemas ทั้ง catalog ในทุก request ทำให้ token/prompt cache โตตามจำนวน tools
- model อาจ call schema ที่ยังไม่เห็น หรือใช้ handler revision เก่า
- server สองตัวมี tool ชื่อซ้ำกัน

การแก้:

- direct prompt คงที่ 6 tools และ remote catalog เข้าได้ผ่าน `tool_search/describe/call` เท่านั้น
- search คืน opaque capability ID แต่ไม่คืน schema/revision; describe จึงเปิด exact binding on demand
- call ต้องส่ง exact revision และตรวจซ้ำทั้ง in-memory catalog กับ persisted MCP snapshot
- capability ID ใช้ server ID + encoded remote name จึงไม่พึ่งชื่อ server ที่อาจซ้ำ
- atomic source-ref replacement ป้องกันเห็น paginated snapshot ครึ่งเดียว

หลักฐาน:

- deterministic 1,500-tool catalog test ให้ direct schemas ยังเท่าเดิมและ search payload bounded
- stale revision ถูก reject ก่อน executor และ drift หลัง approval ก็ fail closed
- full agent E2E ทำ search → describe → call → final response ด้วย direct tools 6 ตัวทุก model step

### Pass B — Protocol evolution and transport correctness

ความเสี่ยง:

- MCP spec ล่าสุด `2026-07-28` เปลี่ยนจาก session handshake เป็น stateless per-request metadata
- server ที่ใช้อยู่จริงจำนวนมากยังเป็น `2025-11-25`
- SSE, pagination, header mismatch, timeout และ cancellation ทำให้ lifecycle ค้างหรือ call ซ้ำได้

การแก้:

- modern mode ส่ง body `_meta`, `MCP-Protocol-Version`, `Mcp-Method`, `Mcp-Name` และรองรับ JSON/request-scoped SSE
- auto mode modern-first แล้ว fallback เฉพาะ recognized legacy initialization failure
- legacy mode ทำ initialize/initialized, bind optional session ID และปิด session หลัง operation
- pagination มี cursor-loop, 100-page และ 10,000-tool limits
- current `x-mcp-header` validation/encoding รองรับ nested primitive path และ Unicode sentinel base64
- HTTP context เป็น cancellation boundary; error taxonomy แยก timeout/cancel/transport/protocol/remote/not-ready/revision/policy
- `tools/call` ไม่มี automatic retry ทั้งสอง protocol eras

หลักฐาน:

- real HTTP integration ครอบ JSON page + SSE page/call, modern headers/body metadata และ Unicode custom header
- legacy fixture ยืนยัน modern probe หนึ่งครั้ง, initialize/session lifecycle และ remote tool call หนึ่งครั้ง
- timeout/cancel tests ยืนยัน typed taxonomy และ request ไปถึง HTTP server จริง

### Pass C — Authority, secret and untrusted-data review

ความเสี่ยง:

- MCP annotations เป็น hints ที่ server อาจประกาศเท็จ
- remote description/schema/output อาจเป็น prompt injection
- server อาจสะท้อน bearer tokenกลับมาใน error/result จน persist ลง event/SQLite
- failed refresh อาจทิ้ง snapshot เก่าที่ยัง callable

การแก้:

- default `trust_annotations=false`; ทุก capability เป็น unknown + approval-required จนผู้ใช้ trust server
- trusted read-only call ตรงได้ แต่ mutation/destructive ยังคงใช้ persisted one-shot approval
- policy revision v3 ระบุ catalog/schema/output เป็น untrusted data ไม่ใช่ instructions
- query/title/description/schema/response/result มี size bounds และ malformed tool ถูก reject แยกรายตัว
- input/output schemas ถูก compile เป็น draft 2020-12 ตอน discovery; invalid arguments fail ก่อน network และ invalid `structuredContent` ไม่เข้า agent
- external schema reference ถูกปิด จึงไม่มี schema-time network fetch/SSRF และ revision ไม่พึ่ง remote document ที่เปลี่ยนภายหลัง
- secret value มาจาก environment เท่านั้น, HTTP redirect ถูกปิด และ response/error ถูก redact ก่อน model/event/persistence
- discovery failure เปลี่ยน persisted status เป็น error และ reload snapshot เดิมเป็น stale

หลักฐาน:

- effectful deferred agent test ยืนยัน pause, exact approval metadata, zero calls ก่อน approval และหนึ่ง call หลัง approval
- reflected credential test ยืนยัน raw secret ไม่อยู่ใน normalized result
- invalid `x-mcp-header` และ external-`$ref` tools ถูก reject โดย valid tool ใน page เดียวกันยังเข้า snapshot
- browser QA ต่อ localhost fixture จริง ยืนยัน save/discover/search/describe, exact revision/schema และ console ไม่มี warning/error

### Remaining gaps หลัง review

- stdio/child process lifecycle และ OS sandbox
- OAuth/authorization discovery, credential-rotation invalidation และ private-network scope policy
- resources, prompts, subscriptions/listen และ MRTR `input_required`
- persistent invocation/idempotency key สำหรับ remote APIs ที่รองรับ (ปัจจุบัน safety contract คือ no automatic retry)
- automated browser CI; manual visual pass ปัจจุบันตรวจ desktop viewport เท่านั้น

## Review 12 — Schema-bound MCP execution

### Pass A — Discovery-time contract validation

ความเสี่ยงคือ server อาจส่ง schema ที่ parse เป็น JSON ได้แต่ไม่ใช่ JSON Schema ที่ใช้งานได้ หรือใช้ external `$ref` เพื่อให้ harness fetch resource ภายนอกระหว่าง discovery ซึ่งเปิดทั้ง SSRF และ contract drift นอก revision snapshot

การแก้คือ compile input/output schema ด้วย draft 2020-12 ก่อน commit snapshot ปิด external loader ทั้งหมด และ reject เฉพาะ tool ที่ผิดโดยไม่ทิ้ง valid tools ใน discovery page เดียวกัน

### Pass B — Pre-call and post-call validation

arguments ถูก decode โดยรักษาชนิดตัวเลขและ validate กับ exact input schema หลัง persisted revision check แต่ก่อนเปิด HTTP request ผลลัพธ์ที่มี declared `outputSchema` ต้องมี `structuredContent` ที่ผ่าน schema หลัง credential redaction และก่อนสร้าง normalized receipt ให้ agent

หลักฐาน:

- real HTTP test ส่ง arguments ที่ขาด required field และยืนยัน remote call count ยังเป็นศูนย์
- real HTTP test ให้ server คืน `structuredContent` ผิดชนิดและยืนยันได้ typed protocol error หลัง remote call เดียว
- valid JSON + SSE path ผ่านทั้ง input และ output validation พร้อม Unicode `x-mcp-header`
- external `$ref`, invalid header annotation, missing/invalid structured output และ additional property ถูกทดสอบเป็น fail-closed

### Pass C — Regression and migration

รัน full suite, race detector, vet, build, JavaScript/Python syntax checks และ critical packages ซ้ำ 10 รอบ Schema migration จาก v1 ถูกยืนยันว่าขึ้น v6 พร้อม `mcp_servers`/`mcp_tools` โดยข้อมูลลับยังคงเป็น environment reference เท่านั้น

## Review 13 — Skill replay, attribution and trigger policy

พบและแก้:

- replay evidence เคยมีเพียง field แต่ไม่มี runner → เพิ่ม package fixtures, exact base/candidate binding และ persisted cases/diff
- candidate revision ใหม่อาจ reuse replay เก่า → gate ตรวจ revision/hash ทุก promotion
- proposal อาจลบ/ทำ test เดิมให้อ่อนลง → weakened fixture เป็น failure
- tool declaration อาจกว้างขึ้นโดย content review อย่างเดียว → เพิ่ม exact-revision capability approval
- imported/bundled Skill อาจถูก improve ตรง → บังคับ fork
- outcome receipt เคยค้าง exposure-only → runtime ปิด turn activations แบบ one-shot success/failure

## Review 14 — Independent context verification

พบและแก้:

- compactor interface อาจคืน summary ที่แต่ง claim ใหม่ → verified wrapper ตรวจ source marker ทุก statement
- semantic result อาจแยก causal pair → verifierบังคับ all-or-none
- validator failure ต้องไม่ทำ turn พังโดยไม่มีทางลด context → fallback ไป deterministic extractive checkpoint พร้อม provenance `verified-fallback`
- compression ratio ไม่พอเป็น correctness proof → เพิ่ม bilingual full/compiled evaluator และ task/patch/hallucination metrics

## Review 15 — Model qualification and no silent downgrade

พบและแก้:

- declared/training context เคยเสี่ยงถูกใช้แทน loaded allocation → context tier ใช้ verified runtime allocation เท่านั้น
- tool success แบบเดียวไม่พอ → เพิ่ม Thai/schema, sequential, malformed recovery, deferred, cancellation และ preemption
- profile ไม่ผ่านอาจถูกลดเอง → persist `eligible`/`requires_decision` พร้อม remediation
- migration schema 11 ระหว่าง development มีสอง shape → V12 ตรวจรายคอลัมน์และมี regression test ทั้งฐานที่มี/ไม่มี field

## Review 16 — Product shell and recoverable data movement

พบและแก้:

- project path/symlink อาจ escape root → canonicalize และตรวจ `filepath.Rel`
- background terminal ผ่าน shell เพิ่ม injection surface → direct executable + argument array + allowlist
- cancel process เดี่ยวอาจทิ้ง child → Unix process group + kill on cancel/timeout
- minimal environment รุ่นแรกทำ `go test` ไม่ได้เพราะ Go cache ไม่ถูก export → derive เฉพาะ GOPATH/GOMODCACHE/GOCACHE โดยไม่ส่ง parent secrets
- backup restore อาจ overwrite active Skill → checksum preview และ candidate-only apply
- setting/memory อาจเก็บ secret/implicit inference → secret-like field rejection และ active memory ต้อง `source=user`

## Review 17 — Curator, scheduling and GC

พบและแก้:

- relation score อย่างเดียวไม่มี review action → persisted finding พร้อม exact versions/lineage/replay plan
- stale analyzer อาจ archive เอง → report-only proposal และไม่มี mutation endpoint
- scheduled maintenance อาจรันตอนผู้ใช้ทำงาน/ใช้แบตเตอรี่ → conservative idle/AC detector
- mark-and-sweep ที่ delete ทันทีย้อนกลับไม่ได้ → dry-run snapshot, stale guard, run-specific quarantine และ checksum restore
- `gc_runs.candidates_json` อาจทำให้ object ที่กำลังตรวจถูก mark reachable เอง → exclude GC report table จาก reference scan
