# Hermetrix Harness — Delivery Status and Forward Gates

Roadmap นี้วางลำดับจาก correctness ไป product breadth เพื่อหลีกเลี่ยงการมี UI ครบแต่ kernel ตรวจสอบไม่ได้

> เอกสารนี้เป็น phase ledger ของ vertical slices ที่ส่งมอบถึง 2026-08-21 ผล architecture audit วันที่ 2026-08-22 พบ correctness gaps ที่ต้องปิดก่อนขยาย product ดู [AETOX-HERMES-TRACEABILITY-AUDIT.md](AETOX-HERMES-TRACEABILITY-AUDIT.md) และใช้ [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md) เป็น forward source of truth สถานะ `complete` ด้านล่างหมายถึงขอบเขต deterministic/vertical slice ที่ระบุ ไม่ใช่ Aetox/Hermes feature parity หรือ production qualification

## Phase 0 — Foundation (เสร็จแล้ว)

- Skill candidates/versions/checks/promotion/archive/restore
- background review queue + foreground priority
- curator report-only + usage/provenance
- 32k/64k/128k/256k/1M context compiler + diagnostics
- allocated-context runtime probe
- local Skill Control Center

Exit gate: unit/integration/race/static/browser checks ผ่าน และเอกสารไม่อ้าง feature ที่ยังไม่ทำว่าใช้งานแล้ว

## Phase 1 — Single-agent kernel (vertical slice complete)

สิ่งที่ทำแล้ว: append-only session/turn/tool event log, provider-flexible OpenAI-compatible adapter, streaming loop, context snapshot และ exact `StepBinding` ทุก sampling step

```text
history version
context snapshot ID
model/runtime config revision
capability catalog revision
policy revision
exact StepBinding
```

Tool pipeline:

```text
parse → binding lookup → schema validate → effect classify
→ policy/approval → effect lock → sandbox/deadline/idempotency
→ normalized receipt/artifact → event commit
```

สถานะปัจจุบัน pipeline นี้ครบสำหรับ workspace tools รุ่นแรกและ deferred MCP Streamable HTTP Workspace write กับ effectful/unknown MCP call ต้องเป็น single-call step, persist approval ก่อน mutation, ผูก grant กับ exact call/revision/argument hash และใช้ no-auto-retry contract หาก process หยุดขณะถือ effect lock ระบบจะสร้าง `uncertain` receipt ตอน restart Background command แยกจาก model tools มี process-group cancellation จริง แต่ OS sandbox และ crash-resume กลาง sampling ยังคงเป็น forward hardening

Exit gate:

- crash/restart resume ไม่ fabricate tool success
- cancellation ถึง child process
- write actions ผ่าน approval policy — ทำแล้วสำหรับ single-file UTF-8 write; network/process ยังเหลือ
- tool ที่ไม่อยู่ใน binding ถูก reject
- unknown side effect ห้าม auto-retry

## Phase 2 — Capability graph, MCP and Skills runtime (vertical slice ทำแล้ว, breadth ยังเหลือ)

- catalog แบบ source-aware เริ่มด้วย MCP และรองรับ atomic snapshot ต่อ server; plugin/dynamic source adapters ยังเหลือ
- readiness/credential/risk/approval state ทำแล้วสำหรับ MCP; dependency graph ทั่วไปยังเหลือ
- direct primitives มี 6 ตัว: workspace 3 + deferred `tool_search/describe/call` 3
- exact schema/handler revision binding ทำแล้วสำหรับ core และ MCP target revision; call ที่ revision drift ถูก reject
- Skill metadata selection, lazy body loading — ทำแล้วแบบ deterministic selector รุ่นแรก; resource loading ยังขาด
- activation receipt และ turn outcome เขียนจาก runtime จริงแบบ exposure-only; causal effectiveness attribution ยังขาด
- MCP connection secrets แยกจาก model-visible metadataและเก็บเฉพาะ environment reference
- MCP `2026-07-28` stateless Streamable HTTP + JSON/SSE/pagination/headers และ fallback `2025-11-25` initialize/session ทำแล้ว
- JSON Schema 2020-12 ถูก compile ตอน discovery; arguments ถูก validate ก่อน network call และ `structuredContent` ถูก validate กับ `outputSchema` ก่อนเข้า agent โดยไม่โหลด external `$ref`
- annotation trust เป็น explicit per-server; default untrusted ทำให้ call ต้อง approval
- MCP Control Center ทำ save/discover/search/on-demand describe โดย bootstrap ไม่คืน catalog ทั้งก้อน

Exit gate:

- direct schemas ต่ำกว่า profile ceiling — ผ่านด้วย 6 schemas
- catalog 1,000+ tools ไม่เพิ่ม prompt ตามจำนวนทั้งหมด — ผ่าน deterministic 1,500-tool test
- Skill ไม่สามารถเพิ่ม permission ให้ตัวเอง — architecture แยก Skill declaration ออกจาก catalog/policy และ MCP dynamic approval
- MCP timeout/cancel/no-retry/error taxonomy ผ่าน integration tests — ผ่านสำหรับ Streamable HTTP

Remaining ก่อนปิด Phase 2 breadth:

- stdio transport และ child-process sandbox/lifecycle
- MCP resources, prompts, subscriptions/listen และ MRTR `input_required`
- OAuth/authorization discovery และ secret rotation invalidation
- plugin/dynamic adapters และ generalized dependency graph
- edit/disable/delete connection UX พร้อม export/import policy

## Phase 3 — Skill replay and learning quality (vertical slice complete; runtime producer ยังเหลือ)

สถานะที่ส่งมอบ:

- `tests/*.json` package fixtures, baseline/candidate runner และ bounded line diff
- fixture เดิมห้ามอ่อนลง, exact candidate revision/hash binding และ stale replay block
- improvement promotion ต้อง no regression; capability widening ต้อง exact-revision approve
- imported/bundled Skill ต้อง fork และ protected Skill ไม่ถูก mutation
- agent runtime ปิด activation outcome แบบ one-shot พร้อม source/kind/confidence/tool calls
- trigger policy ตรวจ milestone outcome, correction count, explicit learn และ activated Skill failure
- Candidate inspector แสดง evidence, content, checks, replay cases/diff และ capability approval

- Skill package `tests/` และ deterministic replay fixtures
- baseline vs candidate comparison
- outcome attribution confidence
- trigger policy: successful milestone, repeated correction, explicit learn, Skill failure
- candidate diff/provenance/evidence UI
- review policy ตาม owner/origin/protected scope

Promotion gates แนะนำ:

| Change | Required evidence |
|---|---|
| new user Skill | lint + security + human content review |
| improve active Skill | above + replay against base + no regression |
| widen tools/scope | explicit capability review + approval |
| imported/bundled/protected | fork candidate; never mutate original |

Exit gate: learning eval แสดงว่า proposal precision ดีขึ้นโดยไม่เพิ่ม active Skill อัตโนมัติ และ stale-base/replay regressions ถูกกัน

ข้อจำกัดที่พบภายหลัง: trigger validator และ queue API มีจริง แต่ agent runtime ยังไม่ enqueue triggers เหล่านี้จาก committed events อัตโนมัติ จึงยังไม่ถือว่า runtime evidence → background review เป็น end-to-end flow

## Phase 4 — Context fidelity laboratory (complete for deterministic evaluator scope)

สถานะที่ส่งมอบ:

- corpus Thai/English เริ่มต้นและ API/UI สำหรับเพิ่ม case/run
- warm/full เทียบ compiled/compacted พร้อม evidence blobs ใน CAS
- essential/decision/open-task/file/causal/task/patch/hallucination/false-success/token/time/heap metrics
- verified compactor ตรวจ source marker/causal pair และ fallback แบบ extractive เมื่อไม่ผ่าน
- forced-compaction integration tests รวม hallucinating compactor regression

สร้าง corpus งานจริงหลายภาษาและเทียบสองแขน:

```text
warm/full context vs compiled/compacted context
```

Metrics:

- goal/criteria exact retention
- decision/open-task/file-state recall
- tool causal recovery
- task completion and patch correctness delta
- hallucination/false-success delta
- token saved, TTFT, throughput และ peak RAM/VRAM

Semantic compactor ต้องสร้าง structured output พร้อม evidence refs แล้วใช้ validator คนละ prompt/model pass หรือ deterministic verifier ห้าม self-grade อย่างเดียว มี rollback ไป extractive checkpoint เมื่อ validation ไม่ผ่าน

Exit gate แนะนำ:

- essential exact retention 100%
- causal-pair split 0
- task success delta อยู่ใน tolerance ที่กำหนดต่อ benchmark class
- overflow/silent truncation 0
- deterministic fallback พร้อมเสมอ

## Phase 5 — Local model qualification (behavioral suite complete; hardware telemetry remains)

สถานะที่ส่งมอบ:

- allocation probe, long-context sentinel, usage calibration และ connectivity
- Thai user/English JSON schema, native/sequential/recovery/deferred tool calls
- cancellation และ foreground-preemption check
- TTFT/latency/throughput, context tier และ capability grade A/B/C
- `eligible`/`requires_decision` report; ไม่มี silent downgrade

ยังเหลือเฉพาะ hardware/runtime breadth: exact tokenizer, RAM/VRAM, OOM recovery, live interjection และ runtime auth adapters

แยกสองแกน:

- context tier: 64k Certified / 32k Compact / Limited
- capability grade: native tools A / constrained envelope B / chat C

Qualification suite:

- actual allocation + long-context recall
- exact tokenizer/provider usage calibration
- single/sequential/error-recovery/deferred tool calls
- JSON schema levels และ malformed arguments
- stream cancellation/interjection
- Thai user + English schema
- load/TTFT/prefill/generation/RAM/VRAM/OOM recovery
- foreground latency ขณะ background queue มีงาน

ห้าม downgrade context/tools/provider แบบ silent UI ต้องอธิบาย remediation และขอการเลือกจากผู้ใช้

## Phase 6 — Product shell (local web vertical slice complete)

สถานะที่ส่งมอบ:

- Chat + project/session navigation และ provider/context diagnostics
- project registry/file browser ที่กัน path/symlink escape
- direct no-shell background terminal jobs, allowlist, minimal env, timeout/output bound, process-group cancel
- Background Job state/restart recovery และ immutable terminal-log artifacts; ยังไม่ใช่ Office document/spreadsheet/slides workspace
- Artifact registry/content checksum, non-secret settings, explicit memory และ event-derived usage
- checksum-verified backup/download/import preview และ restore Skill เป็น candidate เท่านั้น
- UI/API สำหรับทุก surface ด้านบน

Managed browser surface และ native desktop packaging/signing ยังเป็น distribution breadth ถัดไป ไม่ถูกจำลองเป็น feature พร้อมใช้

สร้าง UX surface แบบ original implementation:

- chat + project/session navigation
- model picker badges/diagnostics
- workbench สำหรับ files/terminal/browser/artifacts
- background office/status/cancel
- settings: tools, MCP, Skills, usage, identity, memory
- import/export/backup ทำแล้ว; native desktop packaging/signing เป็น distribution breadth ถัดไป

เริ่มจาก behavior contract ไม่คัดลอก Aetox source/UI asset และรักษา Skill Control Center เป็น authority เดียวของ durable learning

## Phase 7 — Curator and long-term maintenance (safe vertical slice complete)

สถานะที่ส่งมอบ:

- stale scoring จาก retained activation/outcome evidence; pinned/protected ไม่ถูกเสนอ stale
- duplicate/overlap หลัง deterministic retrieval
- persisted findings และ consolidation proposal ผูก exact versions/absorbed lineage/replay plan
- report/proposal only; ไม่มี auto mutation
- schedules พร้อม idle 5 นาที/AC policy detector แบบ conservative
- GC dry-run ผูก snapshot revision; stale apply ถูก reject
- apply ย้าย CAS objects ไป recoverable quarantine และ restore ตรวจ checksum ก่อนคืน
- archive/backup import conflict count และ candidate-only resolution

- stale scoring จาก explicit usage/outcome windows
- duplicate pair judge หลัง deterministic retrieval
- consolidation proposal ที่มี diff/absorbed lineage/replay plan
- scheduled report ที่ idle/battery policy อนุญาต
- archive retention/export/restore conflict UI
- CAS mark-and-sweep พร้อม dry-run/report ก่อน delete

Curator ยังคง proposal/report-only default การ auto-archive อนุญาตได้เฉพาะ policy ที่ผู้ใช้เปิดชัดเจนและต้องมี undo snapshot

## Performance principles for the future

- 64k เป็น correctness headroom ไม่ใช่พื้นที่ให้ prompt โตไม่จำกัด
- stable prefix แยกจาก volatile suffix เพื่อใช้ prompt cache
- exact/deterministic reduction ก่อน semantic LLM call
- foreground มี priority และ background ใช้ digest ขนาดเล็ก
- defer capability catalogs; bind เฉพาะ step
- artifact/blob แทน raw tool output ขนาดใหญ่
- benchmark ด้วย measured tokenizer/runtime usage ไม่ใช้ character ratio อย่างเดียว
- optimization ใดที่ลด token ต้องผ่าน fidelity gate เดียวกับ compactor
