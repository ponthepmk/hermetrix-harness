# Hermetrix Harness — Architecture and Safety Contracts

เอกสารนี้อธิบาย implementation ปัจจุบันใน `Hermetrix-harness` และเส้นแบ่งที่ชัดเจนระหว่าง “ทำแล้ว” กับ “สถาปัตยกรรมระยะถัดไป” เป้าหมายหลักคือสร้างฐาน harness แบบ local-first ที่เรียนรู้ Skill ได้โดยไม่ drift และบีบ context ได้โดยไม่เสียสิ่งจำเป็นอย่างเงียบ ๆ

Hermetrix รุ่นนี้เป็น clean-room implementation ใหม่ทั้งหมด งานวิจัยและ requirement มาจากเอกสารใน [`../../Hermetrix-research`](../../Hermetrix-research/README.md) แต่ไม่ได้คัดลอก source, asset หรือ branding ของ Aetox เนื่องจาก Aetox รุ่นที่ตรวจเป็น proprietary source-available และไม่อนุญาตการแก้ไข/สร้าง derivative product ตาม [`../../Aetox/LICENSE`](../../Aetox/LICENSE)

## สถานะของผลิตภัณฑ์

สิ่งที่ทำแล้ว:

- Skill Control Center แบบ local web UI
- SQLite persistence, immutable Skill versions และ content-addressed blobs
- create/edit/improve/promote/reject/archive/restore lifecycle
- background learning review แบบ persisted, idempotent และ proposal-only
- provenance, activation/usage receipt และ curator report-only
- context compiler สำหรับ 32k/64k/128k/256k/1M envelopes
- deterministic compaction, causal-pair integrity, tool-output spill และ adaptive token estimate
- local runtime context probe สำหรับ Ollama, LM Studio, vLLM และ llama.cpp
- provider registry ที่เก็บเฉพาะ secret environment reference และรองรับ OpenAI-compatible streaming
- append-only agent session/events, frozen context snapshots และ immutable `StepBinding`
- immutable `SessionContract` ที่ freeze provider/model revision, context profile, policy/capability revision, Skill catalog, `CacheEpoch` และ `TaskBudget` ตอนเปิด session
- persisted per-session turn lease ที่ acquire lease และ append user event ใน transaction เดียว พร้อม recovery ของ orphaned turn ตอน restart
- `TaskBudget` แทน hard-coded step limit: model steps, tool calls, wall time และ cumulative tokens
- learning trigger outbox ที่เขียน trigger ใน transaction เดียวกับ turn commit แล้ว drain เป็น review job แบบ idempotent
- multi-step model/tool loop พร้อม bounded reads และ approval-gated atomic text write ที่มี path boundary, optimistic hash, deadline และ normalized receipt
- deferred capability catalog ที่ prompt เห็นเพียง `tool_search`, `tool_describe`, `tool_call` โดยไม่โตตามจำนวน remote tools
- MCP Streamable HTTP registry/client ที่รองรับ current stateless `2026-07-28` และ legacy handshake `2025-11-25`
- MCP Control Center สำหรับ connection, atomic discovery, bounded search และ on-demand schema inspection
- deterministic Skill metadata selection, lazy version-body injection และ activation receipt จาก runtime จริง
- Skill replay fixtures, baseline/candidate diff, no-regression promotion gate และ exact-revision capability review
- runtime outcome attribution และ policy-validated learning triggers
- verified compactor + independent deterministic verifier + extractive fallback
- bilingual context fidelity lab พร้อม full/compiled evidence blobs และ task/patch/retention metrics
- behavioral model qualification แยก context tier ออกจาก tool grade และไม่ silent downgrade
- Projects, project-bound sessions, direct background commands, Office, Artifacts, Settings, explicit Memory และ Usage
- checksum-verified backup/import preview และ restore เป็น candidate เท่านั้น
- curator stale/duplicate findings, consolidation/replay plan, idle/AC schedules และ recoverable CAS quarantine GC

ขอบเขตที่ยังไม่ควรตีความว่า production-complete:

- crash-resume กลาง model sampling, live interjection และ generalized idempotency นอกเหนือจาก contracts ที่ระบุ
- direct background process มี no-shell/path/deadline/output/cancel hardening แต่ยังไม่ใช่ OS-level sandbox
- MCP stdio/resources/prompts/OAuth/subscriptions/MRTR และ plugin/dynamic catalog adapters
- semantic local-LLM compactor; ปัจจุบัน verifier/fallback พร้อมและ fidelity lab ใช้ deterministic task assertions
- runtime-specific RAM/VRAM/OOM telemetry และ exact tokenizer adapters; direct-tool accounting นับ payload จริงแล้วแต่ยังใช้ heuristic estimator ไม่ใช่ tokenizer ของ model
- Skill body ยัง inject เข้า prompt ล่วงหน้าโดยเลือกจาก goal ของ turn แรก ยังไม่มี `skill_search`/`skill_view` แบบ tool-based progressive disclosure (ADR-7)
- long-context recall probe ปลูก sentinel ห้าตำแหน่งแล้ว แต่ยังไม่เคยรันกับ local model จริงที่ 128k ขึ้นไป
- managed browser workbench, native desktop packaging/signing และ mobile UI

ทุก surface ที่แสดงใน navigation ปัจจุบันมี API/persistence จริง Projects, Office และ Artifacts ไม่ใช่ placeholder แล้ว แต่ label `Office` ยังกว้างกว่าความสามารถจริงซึ่งคือ background command jobs — มีแผนเปลี่ยนเป็น `Background Jobs`

รายการ finding ที่ยังเปิดอยู่พร้อมหลักฐานระดับ file/line อยู่ใน [AETOX-HERMES-TRACEABILITY-AUDIT.md](AETOX-HERMES-TRACEABILITY-AUDIT.md) หัวข้อ 4.2 และมาตรการอยู่ใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md)

## หลักการที่ห้ามละเมิด

1. **Agent สร้างได้แค่ candidate** — background reviewer และ curator ไม่มีสิทธิ์เขียน active Skill
2. **Promotion เป็น explicit decision** — ต้องมี actor, checks ผ่าน, revision ตรง และ base version ยังไม่เปลี่ยน
3. **Active version immutable** — การแก้ไขสร้าง blob และ version ใหม่เสมอ
4. **Archive ไม่ใช่ delete** — เก็บ exact version/blob/provenance และ restore กลับมาเป็น candidate ก่อน
5. **Origin ไม่ใช่ owner** — แหล่งกำเนิดความรู้กับผู้มีอำนาจจัดการเป็นคนละแกน
6. **Pinned intent ห้ามหายแบบเงียบ ๆ** — หาก verbatim goal/criteria ไม่พอ budget compiler ต้อง fail closed
7. **Tool call/result เป็น causal unit** — เลือก, compact หรือ omit ทั้งคู่ ห้ามแยก
8. **Reserve ก่อน sampling** — output, uncertainty และ worst-case next tool burst ต้องถูกกันพื้นที่ก่อน
9. **Training context ไม่ใช่ runtime allocation** — Certified 64k ต้องอ่านค่าจาก instance ที่โหลดจริง
10. **Analysis ไม่ใช่ mutation** — duplicate/overlap report ไม่มีสิทธิ์ merge/archive เอง

## ภาพรวม component

```text
Browser UI
   │
   ▼
HTTP API ─────────────── Local model context prober
   │
   ├── Agent Service ─── SessionContract/TurnLease/CacheEpoch/TaskBudget
   │        │              append-only events/context snapshots/StepBinding
   │        │
   │        ├── Provider Registry → OpenAI-compatible streaming adapter
   │        ├── Frozen Skill catalog → selected versions → activation receipt
   │        ├── Bound core tools → policy/path/deadline → normalized receipt
   │        └── 3 deferred primitives → Capability Catalog → MCP client
   │
   ├── Skill Service ─── SQLite views/events ─── content-addressed blobs
   │        │
   │        ├── candidates/checks/promotion
   │        ├── immutable versions/usage
   │        └── archive/restore/relations
   │
   ├── Learning Service ── persisted queue ── Inference Gate
   │        │
   │        └── Reviewer → checked candidate only
   │
   ├── Curator Service ── deterministic report-only snapshots
   │
   └── Context Compiler
            ├── typed fragments and exact budget profiles
            ├── dedup + pair pin propagation
            ├── tool spill → artifact receipt
            ├── deterministic selection
            ├── structured extractive checkpoint
            └── integrity report
```

## Skill learning lifecycle

### State flow

```text
evidence/digest
    │
    ▼
background review ── no_change ──► completed
    │
    └── create/improve suggestion
             │
             ▼
        candidate workspace
             │
             ├── checks fail ──► quarantined ── edit/recheck ──┐
             │                                                 │
             └── checks pass ─► needs_review ◄─────────────────┘
                                      │
                         ┌────────────┴────────────┐
                         ▼                         ▼
                      reject                   promote
                                                    │
                                                    ▼
                                        immutable active version
                                                    │
                             usage/outcome receipts + curator reports
                                                    │
                               ┌────────────────────┴──────────────┐
                               ▼                                   ▼
                       improve candidate                        archive
                                                                   │
                                                                   ▼
                                                         restore candidate
```

### Evidence intake

`learning.Digest` รับข้อมูลแบบมีโครงสร้าง: goal/constraints, outcome, decisions, tool receipts, Skill activations, corrections, artifacts และ redactions ไม่ส่ง warm transcript ทั้งก้อนให้ reviewer เหตุผลคือ:

- ลด token และ prompt-cache churn
- จำกัดข้อมูลส่วนตัว/secret ที่ worker เห็น
- ทำ retry และ idempotency ได้
- ตรวจว่าข้อเสนออ้าง evidence ใดได้

Idempotency key ผูก `session + milestone + reviewer revision` งานเดิมจึงไม่สร้าง candidate ซ้ำเมื่อ retry หรือ restart งานที่ค้างสถานะ `running` จะถูก requeue ตอนเปิดโปรแกรม

### Candidate boundary

Candidate มี package blob, hash, author/trigger/reason/evidence, checks, revision, target Skill/base version และ optional source review ID การแก้ candidate ใช้ optimistic revision แล้วรัน checks ใหม่ การ promote ใช้ transaction เดียวเพื่อ:

1. ตรวจ state/revision/checks
2. ตรวจ base version และ metadata authority ของ Skill เดิม
3. สร้าง immutable version
4. ชี้ active Skill ไป version ใหม่
5. ปิด candidate และเขียน audit event

สำหรับ Skill ที่ agent เสนอใหม่ origin เริ่มเป็น `agent_candidate`; หลังมนุษย์ promote จึงเปลี่ยนเป็น `agent_promoted` เพื่อไม่ทำให้ proposal ดูเสมือน trusted active knowledge ก่อนเวลา

### Checks ปัจจุบัน

- canonical package/path/size validation
- `SKILL.md` และ frontmatter name/description
- canonical name matching
- binary-file quarantine
- estimated token footprint
- declared tool hints สำหรับ review

Improvement ต้องรัน deterministic replay ผูก candidate revision/hash เทียบ exact base version ทุกครั้ง Test fixture เดิมห้ามถูกทำให้อ่อนลง, regression block promotion และ tool declaration ที่เพิ่มต้องมี approval แยกซึ่งผูก exact revision รายงานนี้เป็น deterministic contract proof ไม่ใช่หลักฐานว่า LLM ทุก model จะทำงานตาม Skill ได้เท่ากัน จึงยังต้องดู runtime/task eval ประกอบ

### Usage and provenance

Activation receipt แยก:

- selected เพราะอะไร/จาก selector ใด
- metadata ถูก expose หรือไม่
- body ถูก inject จริงหรือไม่
- tool calls ใดเกี่ยวข้อง
- outcome และแหล่งข้อมูลของ outcome
- attribution kind/score

Aggregate ใน UI จึงไม่ถือว่า “มองเห็น metadata” เท่ากับ “Skill ถูกใช้งานสำเร็จ” Version ID ต้องเป็นของ Skill ID เดียวกันก่อนบันทึก receipt

### Curator

Curator snapshot รายชื่อ Skill และ exact active version ก่อนวิเคราะห์ แล้วใช้ deterministic retrieval หา duplicate/overlap ผลลัพธ์ผูก version, analyzer revision, score และ shared evidence ทุก run ถูกเก็บใน `curator_runs`

Curator เป็น `report_only` เสมอ Findings แยก stale/overlap/possible duplicate/duplicate พร้อม exact versions, evidence, absorbed lineage และ replay plan ไม่มี API สำหรับ auto-merge, auto-archive หรือ rewrite Schedule เรียกได้เฉพาะเมื่อ policy idle/AC ผ่าน และ GC ย้าย exact dry-run snapshot ไป recoverable quarantine แทน delete

## Context compiler

### Typed fragments

Context ไม่เป็น string ก้อนเดียว แต่ทุก fragment มี:

- ID, kind, scope และ version
- provenance/trust
- priority/pinned/cache class
- causal `pair_id`
- created time และ metadata

ชนิดสำคัญ ได้แก่ identity, policy, user goal, acceptance criteria, project instruction, selected Skill, conversation, tool call/result, decision, open task, artifact receipt และ checkpoint

### Budget profiles

| Slice | Compact 32k | Certified 64k | Extended 128k | Extended 256k | Ultra 1M |
|---|---:|---:|---:|---:|---:|
| Output reserve | 4,096 | 8,192 | 16,384 | 32,768 | 65,536 |
| Estimator uncertainty | 2,048 | 4,096 | 8,192 | 16,384 | 32,768 |
| System/policy | 2,048 | 3,072 | 4,096 | 6,144 | 8,192 |
| Direct tool schemas | 3,584 | 4,096 | 4,096 | 6,144 | 8,192 |
| Selected Skill + project | 3,072 | 6,144 | 12,288 | 24,576 | 65,536 |
| Pinned intent | 2,048 | 3,072 | 6,144 | 12,288 | 32,768 |
| Active history + next-tool burst | 15,872 | 36,864 | 79,872 | 163,840 | 835,584 |
| **Total** | **32,768** | **65,536** | **131,072** | **262,144** | **1,048,576** |

64k คือ minimum ของเป้าหมาย Certified Agent Mode; 32k เก็บไว้เป็น Compact compatibility/stress profile ส่วน 128k, 256k และ 1M เป็น selectable extended envelopes สำหรับ runtime ที่ probe allocation ได้จริง การมี window ใหญ่ไม่ทำให้ direct tool schemas โตตามสัดส่วน—เพดานยังอยู่ที่ 4k–8k เพื่อรักษา progressive capability exposure และ prompt-cache efficiency

### Compile pipeline

```text
validate exact profile total
→ count direct-tool schemas and fail if over ceiling
→ record original token estimate
→ exact dedup (tool IDs remain distinct)
→ propagate pin across causal pairs
→ spill large tool output to blob + checksum/preview receipt
→ assign typed slices
→ select deterministic causal units by priority/recency
→ fail if pinned units do not fit
→ reserve worst-case next tool burst
→ compact omitted active units into extractive checkpoint
→ canonical ordering
→ calculate free space and integrity report
→ fail if any invariant or total window is violated
```

Compactor ทุกตัวถูกครอบด้วย verifier ที่บังคับ source marker และ causal pair integrity หากผล semantic ใดตรวจไม่ผ่านจะ fallback ไป structured extractive checkpoint แบบ deterministic ทุกบรรทัดมี source kind/ID การตัดข้อความใช้ head/tail และจัดทั้ง causal pair เป็น unit เดียว Fidelity lab เก็บทั้ง full/compiled evidence ใน CAS และวัด Thai/English benchmark แยกต่างหาก

### ความหมายของ “บีบแล้วประสิทธิภาพเท่ากัน”

ห้ามรับประกัน semantic equivalence ด้วย compression ratio เพียงตัวเดียว Definition of fidelity ที่ใช้ในรุ่นนี้คือ:

- goal และ acceptance criteria ที่ pinned อยู่ครบ 100% แบบ verbatim
- tool call/result pair ไม่ถูกแยก
- selected Skill/project/policy ไม่ข้าม slice authority
- checkpoint ทุก claim trace กลับ source ID ได้
- output/uncertainty/tool-burst reserve ไม่ถูกยืม
- deterministic input ให้ selected IDs/checkpoint เดิม

สิ่งที่ยังต้องวัดเพิ่มก่อนเรียก “เทียบเท่าเชิงงาน” คือ task-success delta, long-context retrieval, decision/file/open-task retention และ tool recovery หลัง compact ซึ่งอยู่ใน roadmap

### Token estimation

Fallback estimator ให้ non-ASCII/Thai อย่างน้อยหนึ่ง token ต่อ rune และเพิ่มค่าเผื่อ punctuation/code จากนั้น calibrate multiplier ด้วย provider-reported predicted/actual usage แบบไม่ย้อนแก้ historical report

ลำดับที่ต้องพัฒนาต่อคือ exact model tokenizer → runtime usage → calibrated estimator → conservative heuristic ปัจจุบันมีสามขั้นหลัง ยกเว้น exact tokenizer adapter

## Local model runtime truth

Mode ปัจจุบันคำนวณจาก **verified allocated context** เท่านั้น:

```text
>= 65,536  → certified-context
>= 32,768  → compact-context
<  32,768  → limited
unverified → limited
```

แหล่งข้อมูล:

| Runtime | Runtime allocation ที่เชื่อ | Metadata ที่แสดงแต่ไม่ใช้รับรอง |
|---|---|---|
| Ollama | loaded model `context_length` จาก `/api/ps` | Modelfile `num_ctx`, model `context_length` |
| LM Studio | `loaded_instances[].config.context_length` | `max_context_length` |
| vLLM | `vllm_config...max_model_len` จาก `/server_info?config_format=json` | model card/HF context |
| llama.cpp | `default_generation_settings.n_ctx` จาก `/props`, fallback เป็นค่าต่ำสุดของ `/slots` | `/v1/models.meta.n_ctx_train` |

Probe จำกัด endpoint ที่ loopback (`localhost`, `127.0.0.1`, `::1`) และจำกัด response 4 MiB/timeout 4 วินาที ลด SSRF และ unbounded response risk

คำว่า `certified-context` ยังรับรองเฉพาะ allocation ไม่ใช่ Certified Agent Mode เต็มรูปแบบ การรับรองเต็มต้องเพิ่ม tool calling, JSON schema, cancellation, recall, Thai+English, latency และ memory-pressure tests

## Foreground/background scheduling

`InferenceGate` serialize การใช้ local runtime ค่าเริ่มต้นเพื่อไม่ให้ foreground 64k และ reviewer แย่ง KV/compute พร้อมกัน Background review cooperative-cancel เมื่อ foreground มา และงานถูก requeue ไม่ทิ้ง candidate ครึ่ง transaction Waiting foreground มี priority เหนือ background job ใหม่

ในอนาคต gate ต้องอยู่ระดับ `runtime instance + device`, รองรับ telemetry, battery/quiet hours และ concurrency ที่ runtime ยืนยันว่าปลอดภัย ไม่ควรเปิด parallelism จากจำนวน CPU core อย่างเดียว

## Persistence

SQLite เปิด WAL, foreign keys, busy timeout และจำกัด connection เดียวเพื่อให้ local mutation ordering เข้าใจง่าย ตารางหลัก:

- `skills`, `skill_versions`, `skill_candidates`
- `skill_events`, `skill_activations`
- `skill_archives`, `skill_relations`
- `learning_reviews`, `curator_runs`
- `provider_profiles`, `agent_sessions` (พร้อม `active_turn_id`, `lease_acquired_at`, `contract_json`, `contract_revision`, `cache_epoch`), `agent_events`
- `learning_trigger_outbox` สำหรับ runtime evidence → review job แบบ transactional
- `context_snapshots`, `step_bindings`
- `tool_approvals` สำหรับ one-shot grants, effect-lock state และ receipt linkage
- `skill_replay_runs`, `skill_replay_cases`, `candidate_capability_reviews`
- `context_eval_cases`, `context_eval_runs`, `model_qualification_runs`
- `projects`, `artifacts`, `background_jobs`, `settings`, `memories`, `backup_runs`
- `curator_findings`, `maintenance_schedules`, `gc_runs`

Package และ tool artifacts เก็บเป็น SHA-256 blobs แบบ atomic write/read integrity check Database เก็บ hash/ref ไม่เก็บ binary ขนาดใหญ่

CAS blobs ที่เขียนสำเร็จแต่ transaction DB ล้มเหลวอาจกลายเป็น orphan ระบบจึงมี reference scan + dry-run snapshot + stale guard และย้ายไป recoverable quarantine ก่อนเสมอ ไม่มี GC path ที่ hard-delete

## HTTP surface

กลุ่ม API ปัจจุบัน:

- `/api/skills`, `/api/candidates`, `/api/archives`
- `/api/candidates/{id}/replays`, `/api/candidates/{id}/capability-review`
- `/api/reviews` และ `/api/reviews/run-next`
- `/api/relations`, `/api/curator/run`, `/api/curator/findings`
- `/api/activations`
- `/api/context/profiles`, `/api/context/compile`, `/api/context/observe`
- `/api/local-model/probe`
- `/api/providers`, `/api/providers/{id}/test`
- `/api/qualifications`, `/api/fidelity/cases`, `/api/fidelity/runs`
- `/api/mcp/servers`, `/api/mcp/servers/{id}/discover`
- `/api/capabilities`, `/api/capabilities/{id}`
- `/api/sessions`, `/api/sessions/{id}`, `/api/sessions/{id}/turns`
- `/api/projects`, `/api/jobs`, `/api/artifacts`
- `/api/settings`, `/api/memories`, `/api/usage`
- `/api/backups`, `/api/imports`
- `/api/maintenance/schedules`, `/api/maintenance/gc`

JSON decoder จำกัด body 10 MiB และ reject unknown fields Import backup แยก endpoint ที่จำกัด 256 MiB พร้อม envelope/blob checksums UI ใช้ CSP/no-frame/no-sniff/no-referrer headers Default listener เป็น loopback และ CLI ปฏิเสธ non-loopback listener เพราะ single-user build ยังไม่มี identity/authentication

## Tool runtime และ deferred capability graph

Agent loop expose direct tools เพียง 6 ตัว: `workspace.list_files`, `workspace.read_file`, `workspace.write_file`, `tool_search`, `tool_describe` และ `tool_call` ทุก sampling step freeze exact direct name/schema/revision/effect/approval requirement ลง `StepBinding` ก่อน model call Tool ที่ไม่อยู่ใน binding ถูก reject, argument ถูก decode แบบ strict, symlink/path escape ถูกกัน และ file read จำกัด 1 MiB พร้อม SHA-256 ใน receipt

`workspace.write_file` เป็น effectful vertical slice รุ่นแรก:

```text
model requests one exact write
→ validate path/content/current SHA-256 without mutation
→ persist tool call + pending approval; session pauses
→ user sees path, byte count and bounded content preview
→ approve/deny one exact call
→ approval moves pending → executing before mutation
→ atomic write or denial receipt is committed
→ agent resumes the same turn with the real tool receipt
```

Grant ผูก tool call ID, name, handler revision, effect และ hash ของ arguments จึงนำไปใช้กับเนื้อหาหรือ call อื่นไม่ได้ Existing file ต้องใช้ SHA-256 จาก read receipt และ create ต้องประกาศ `expected_sha256=absent` หากไฟล์เปลี่ยนระหว่างรอ execution จะ fail closed หาก restart พบ state `executing` จะปิด causal pair ด้วย receipt `uncertain`, ปลด session และห้าม auto-retry

effect-lock/approval state เดียวกันใช้กับ deferred MCP call ได้ด้วย Unknown หรือ effectful MCP capability ต้องเป็น call เดียวใน step, persist exact capability revision และ argument hash ก่อนผู้ใช้เห็น preview แล้วจึง execute one-shot หลัง approval หาก process หยุดขณะ `executing` ระบบไม่ retry และสร้าง `uncertain` receipt แบบ generic ให้ตรวจระบบปลายทางก่อน

### Progressive exposure

Catalog snapshot อาจมี 1,000–10,000 tools แต่ prompt ไม่รับ schemas ทั้งหมด Flow คือ:

```text
tool_search(query)
  → bounded ID/name/description/source/effect/readiness; ไม่มี schema/revision
tool_describe(capability_id)
  → exact schema + revision + risk/approval state
tool_call(capability_id, revision, arguments)
  → current-revision check → readiness/policy/approval → exactly one remote call
  → normalized receipt with source/server/protocol/revision/no-retry metadata
```

การไม่คืน revision จาก search บังคับให้ model เปิด schema แบบ on-demand ก่อน call และ expected revision ทำให้ catalog drift ระหว่าง describe/call fail closed Direct prompt จึงคงที่ 6 schemas ซึ่งผ่าน 1,500-tool scale test

### MCP lifecycle

Connection profile เก็บ endpoint, protocol mode, timeout, annotation-trust decision และ **ชื่อ** environment variable เท่านั้น ไม่เก็บ credential value Discovery เป็น explicit operation และเขียน `mcp_tools` snapshot ใน transaction เดียว จากนั้น swap in-memory catalog atomically หาก refresh ล้มเหลว profile เป็น `error` และ snapshot เดิมเปลี่ยน readiness เป็น `stale` จึง call ต่อไม่ได้

Transport matrix ปัจจุบัน:

| Contract | `2026-07-28` | `2025-11-25` fallback |
|---|---|---|
| Lifecycle | stateless per-request `_meta` | `initialize` → `notifications/initialized` |
| Headers | protocol/method/name + `x-mcp-header` | negotiated protocol + optional session ID |
| Response | JSON หรือ request-scoped SSE | JSON หรือ SSE |
| Pagination | `nextCursor`, loop/page/tool limits | `nextCursor`, limits เดียวกัน |
| Cancellation | close HTTP request/response stream ผ่าน context | cancel request; session ไม่ถูก reuse |
| Retry | ไม่มี automatic `tools/call` retry | ไม่มี automatic `tools/call` retry |

Auto mode ทดลอง modern `tools/list` ก่อน และ fallback เป็น legacy เฉพาะ `400` ที่ไม่ใช่ recognized modern negotiation error/ระบุว่ายังไม่ initialize จึงไม่ใช้ effectful `tools/call` เป็น protocol probe Legacy call เปิด session ใหม่ต่อ operation แล้วปิดด้วย DELETE เมื่อทำได้

MCP annotations เป็น hints ที่ไม่ trusted ตาม default หากผู้ใช้ไม่เปิด trust ให้ server นั้น tool ทุกตัวถูกจัดเป็น `unknown` และต้อง approval แม้ประกาศ `readOnlyHint=true` เมื่อ trust แล้ว read-only จึง call ได้ตรง ส่วน mutation/destructive ยังต้อง approval เสมอ Catalog description/schema/result ถูกระบุเป็น untrusted data และ system policy ห้ามปฏิบัติตามในฐานะ instructions

ขอบเขตเพิ่มเติม:

- remote response จำกัด 8 MiB, tool result เข้า agent จำกัด 2 MiB, schema 256 KiB ต่อก้อน, description 4 KiB และ search สูงสุด 20 รายการ
- response ที่สะท้อน bearer credential ถูก redact แบบ recursive ก่อนเขียน event/ส่ง model; error ที่ persist ก็ redact
- redirect ถูกปฏิเสธเพื่อไม่ส่ง Authorization ไป host อื่น; remote ต้อง HTTPS และ HTTP ใช้ได้เฉพาะ loopback
- current `x-mcp-header` ถูก validate เฉพาะ statically reachable primitive properties, reject duplicate/invalid header names และ encode non-ASCII ด้วย sentinel base64
- input/output JSON Schema ใช้ draft 2020-12 และ compile ตอน discovery; arguments ถูก validate ก่อนเปิด network call ส่วน declared `outputSchema` ตรวจ `structuredContent` หลัง secret redaction และก่อนเข้า agent
- external `$ref` ถูกปิดเพื่อไม่ให้ schema compilation fetch network resource หรือเปลี่ยน contract นอก snapshot revision
- tool call ทุกครั้งตรวจ persisted server/tool revision ซ้ำหลัง catalog lookup และไม่ retry อัตโนมัติ

ยังไม่มี network/process core tools หรือ OS-level sandbox จึงห้ามตีความว่าเป็น autonomous coding runtime ที่สมบูรณ์ Write รองรับเฉพาะ complete UTF-8 single-file replacement/create ใน directory ที่มีอยู่ MCP ยังขาด stdio, OAuth, resources/prompts/subscriptions และ MRTR

Skill declaration ไม่เพิ่ม permission สิทธิ์จริงเป็น intersection ของ session ceiling, user/admin policy, catalog risk state และ exact task grant Scripts ใน Skill ต้องผ่าน tool execution pipeline เดียวกัน ไม่มีทางเขียน MCP profileหรือเปลี่ยน annotation trust ผ่าน Skill content

MCP implementation อ้างอิง specification ทางการ:

- [2026-07-28 Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http)
- [2026-07-28 Tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools)
- [2026-07-28 Versioning and compatibility](https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning)
- [2025-11-25 Streamable HTTP compatibility target](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)
- [santhosh-tekuri/jsonschema v6](https://github.com/santhosh-tekuri/jsonschema) สำหรับ local draft 2020-12 validation

## มุมผู้ใช้และผู้พัฒนา

ผู้ใช้ต้องเห็น:

- อะไร active, proposal, quarantined หรือ archived
- ใคร/อะไรสร้าง proposal และอ้าง evidence ใด
- runtime จัดสรร context จริงเท่าไร แยกจาก configured/training max
- ทำไม Skill ถูกเลือก/inject และผลลัพธ์ถูกนับอย่างไร
- ทุก action ที่ reversible และ action ที่ต้อง approval

ผู้พัฒนาต้องรักษา:

- transaction/invariant tests ก่อนเพิ่ม UX shortcut
- revisioned analyzer/checker/prober/context profile
- pure/deterministic retrieval ก่อนใช้ LLM judge
- API/state names ที่ไม่ทำให้ suggestion ดูเหมือน autonomous truth
- migrations แบบ forward-only พร้อม backup/export ก่อน destructive schema change
