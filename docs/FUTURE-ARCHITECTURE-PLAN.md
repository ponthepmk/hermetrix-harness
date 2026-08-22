# Hermetrix Harness — Future Architecture and Delivery Plan

วันที่วางแผน: 2026-08-22  
ฐานการตัดสินใจ: [AETOX-HERMES-TRACEABILITY-AUDIT.md](AETOX-HERMES-TRACEABILITY-AUDIT.md)

แผนนี้เริ่มจากแก้ correctness ก่อน product breadth ทุก phase ต้องมี behavior contract, migration, negative test, E2E ที่เส้นทางจริง และ documentation truth gate ก่อนถือว่าเสร็จ

## North star

Hermetrix คือ local-first AI workbench ที่:

- ให้ Aetox-style friendly Assistant/Code UX โดยเขียนใหม่แบบ clean-room
- ใช้ Hermes-style extensible harness โดยคง core tool waist ให้เล็ก
- รองรับ OpenAI-compatible และ provider families หลายแบบ โดย local LLM เป็น first-class
- support target ขั้นต่ำ 64k และเลือก 128k/256k/1M ได้เมื่อ runtime/model qualification ผ่านจริง
- เรียนรู้เป็น Skill ได้ แต่ durable behavior ทุกชิ้นมี provenance, eval, human authority, archive/restore และ rollback
- ลด token โดยไม่แลกกับ goal/decision/tool-causality fidelity
- ไม่อ้าง tool success, context capacity หรือ Skill effectiveness หากไม่มี receipt/evidence

## Non-goals

- ไม่ port/reuse current Aetox source, UI code, assets หรือ branding
- ไม่เพิ่ม core model tool เพียงเพราะทำได้; capability edge ต้องมาก่อน
- ไม่ใช้ long context เป็นข้ออ้างให้ส่ง raw history/catalog ทั้งหมด
- ไม่ให้ curator/reviewer/Skill ขยาย authority หรือ promote ตัวเอง
- ไม่รองรับ remote control ก่อนมี authentication, origin binding และ secret storage ที่เหมาะสม
- ไม่เรียก lexical replay ว่า model behavioral evaluation

## Target architecture

```text
Native shell / Web / CLI / future gateways
                  │ typed session API + event stream
                  ▼
┌──────────────── Session Authority ─────────────────┐
│ SessionContract · TurnLease · CacheEpoch · Policy │
│ cancellation · approval · recovery · identities   │
└───────────────┬──────────────────────┬─────────────┘
                │                      │
        Agent Runtime             Work Scheduler
        context compiler          foreground/background
        model/tool loop           device/runtime queues
                │                      │
┌───────────────▼──────────────────────▼─────────────┐
│ Capability Plane                                  │
│ 6 core primitives · toolsets · plugins · MCP      │
│ exact schema/revision · risk · grants · receipts  │
└───────────────┬──────────────────────┬─────────────┘
                │                      │
        Skill Learning Plane        Provider Plane
        candidate/eval/curator      adapters/qualification
                │                      │
┌───────────────▼──────────────────────▼─────────────┐
│ SQLite event/state projections · CAS artifacts    │
│ audit chain · backups · migrations · quarantine   │
└───────────────────────────────────────────────────┘
```

หลักสำคัญคือ UI ไม่เป็นเจ้าของ truth; renderer cache เพื่อความเร็วได้ แต่ session/event/approval state ต้องมาจาก backend authority เดียว

## Architectural decisions ที่ต้องล็อกก่อนเพิ่ม breadth

### ADR-1 — Immutable SessionContract

ตอนเปิด session ให้ freeze:

```text
provider_id + provider_revision + model
context_profile + qualification_revision/override
surface + desk/toolset ceiling
policy_revision + prompt_revision + cache_epoch
project roots + identity/memory revisions
skill catalog revision + selected skill version IDs
MCP/plugin placement revisions
```

การแก้ settings/Skill/memory/MCP default มีผล session ถัดไป การ apply-now ต้องสร้าง cache epoch ใหม่และ event ที่ user เห็นได้ ห้ามเปลี่ยนเงียบ

### ADR-2 — Persisted TurnLease

หนึ่ง session มี active turn ได้หนึ่ง turn การ acquire lease, transition state และ append user event เป็น transaction เดียว Global model/device gate ใช้จัดทรัพยากร แต่ห้ามใช้แทน per-session lease

### ADR-3 — Authority ladder สำหรับ Skill

```text
observation
→ structured evidence digest
→ candidate
→ deterministic checks/replay
→ sandboxed behavioral eval
→ human review/capability approval
→ immutable active version (next-session default)
→ usage/outcome observation
→ improve/consolidate/archive proposal
```

agent/reviewer/curator สร้างได้สูงสุด candidate/proposal เท่านั้น ผู้ใช้เป็น promotion authority; admin policy ในอนาคตอาจ auto-promote เฉพาะ low-risk cohort แต่ต้อง opt-in, signed rule และ rollback window

### ADR-4 — Narrow capability waist

คง direct primitives ชุดเล็ก Tool ใหม่ผ่านลำดับ:

1. extend action ของ existing bounded primitive หาก semantics/risk เดียวกัน
2. executable/CLI + Skill
3. session-gated toolset
4. plugin provider
5. MCP server
6. core tool เฉพาะ universal, latency-critical และทำผ่านชั้นอื่นไม่ได้

ทุก source implement capability contract เดียว: readiness, dependencies, credential refs, schema revision, effect/risk, approval, execute/cancel, normalized receipt

### ADR-5 — Certified context, not declared context

product mode ปกติเริ่ม 64k Certified; 32k เป็น compatibility mode Context tier ต้อง bind ถึง provider/model/runtime/config revision exact ไม่ใช่ชื่อ model อย่างเดียว

| Profile | เปิดใช้ปกติเมื่อ | ถ้าหลักฐานไม่ครบ |
|---|---|---|
| 32k | user เลือก compatibility | badge degraded |
| 64k | allocation + recall + grade A/B ผ่าน | block หรือ explicit override |
| 128k | exact 128k allocation/remote evidence + recall ผ่าน | visible but unavailable |
| 256k | exact 256k suite ผ่าน | visible but unavailable |
| 1M | chunk-position recall, prefill/resource limits และ long-run stability ผ่าน | visible but unavailable |

remote gateway ที่เปิดเผย allocation ไม่ได้ใช้ signed/admin evidence หรือ expiring override พร้อม risk text; declaration อย่างเดียวไม่ใช่ certification

### ADR-6 — Clean-room product implementation

Aetox ใช้ระบุ behavior/acceptance criteria เท่านั้น Design files, component structure, layout, visual system และ source ของ Hermetrix ต้องสร้างใหม่ เก็บ decision log, attribution และ third-party notices ทุก dependency

## Phase 7.1 — Correctness closure

เป้าหมาย: ปิด contradictions ก่อนสร้าง native shell

งาน:

1. เพิ่ม persisted TurnLease + unique active-turn constraint + crash recovery
2. เพิ่ม immutable SessionContract/CacheEpoch และ freeze Skills/toolsets/policy ต่อ session
3. ต่อ post-commit learning producer จาก runtime events; แก้ UI claim จนกว่าจะต่อครบ
4. ขยาย qualification tiers 128k/256k/1M และ enforce latest exact eligibility ตอน create/resume session
5. นับ direct-tool budget จาก exact provider serialization
6. แก้ partial GC restore CAS/state transition และเพิ่ม compensating recovery
7. เปลี่ยน `maxAgentSteps=4` เป็น bounded TaskBudget policy พร้อม loop detector
8. ทำ documentation truth pass: label `Office → Background Jobs`, phase states และ feature flags จาก runtime

Exit gates:

- concurrent-turn E2E 100 รอบไม่เกิด double user commit/role violation
- Skill promote/archive ระหว่าง active sessionไม่เปลี่ยน prompt fingerprint; next sessionเห็น revision ใหม่
- successful milestone/correction/explicit-learn/Skill failure สร้าง idempotent review job จาก committed event จริง
- 128k/256k/1M session ถูก block เมื่อไม่มี exact qualification; override ถูก persist/audit/expire
- GC fault-injection ทุกจุด recover ได้และ DB/CAS ไม่รายงาน state เท็จ
- generated capability/profile status ใน docs/UI ตรง runtime registry

## Phase 8 — Skill Learning OS 2.0

เป้าหมาย: ทำ lifecycle ที่วิเคราะห์ย้อนหลังและปรับปรุงตัวเองได้โดยไม่เสีย user authority

งานหลัก:

- semantic reviewer ผ่าน local-model queue ใช้ structured digest ที่ไม่มี raw secrets
- candidate generation templates สำหรับ create/improve/split/merge/deprecate
- full YAML parser + schema version, platforms, prerequisites, resources, scripts, expected MCP/plugin dependencies
- resource CAS manifest และ immutable package revision
- sandboxed behavioral runner: baseline/candidate, fixed seeds เมื่อ providerรองรับ, tool simulator/real temp workspace, cost/time cap
- eval cohorts แยก task class/language/model tier; deterministic replay เป็น fast gate ชั้นแรก
- provenance DAG: evidence events → reviewer → candidate → checks/evals → decision → active version → activations
- duplicate/overlap workflow: retrieve → deterministic evidence → semantic judge → merge plan → replay absorbed cases → human approve
- skill health dashboard: activation, success correlation, confidence, regressions, cost delta, last review, stale reasons
- curator policy simulator ก่อนเปิด auto archive; undo snapshot และ restore-as-candidate คงเดิม

Exit gates:

- ไม่มี path ที่ agent/reviewer/curator promote หรือ widen capability เอง
- candidate ทุกชิ้นย้อนถึง committed event range และ reviewer/model revision ได้
- merge candidate ต้อง replay union ของ test sets และไม่ทำ lineage หาย
- behavioral eval แยก `not_run`, `inconclusive`, `passed`, `failed`; ห้ามถือ missing eval เป็น pass
- background reviewer yield/preempt ได้และ restart แล้ว resume แบบ idempotent

## Phase 9 — Context and token efficiency 2.0

เป้าหมาย: context เล็กที่สุดที่ยังให้คุณภาพเท่า full context อย่างวัดได้

งานหลัก:

- tokenizer adapters ต่อ provider/model และ usage calibration; heuristic เป็น fallback พร้อม error band
- count exact serialized messages/tools/provider wrappers
- stable-prefix planner แยก identity/policy/session contract จาก rolling suffix
- conversation checkpoints แบบ structured: goal, constraints, decisions, open tasks, files, tool causal receipts, unresolved uncertainty, evidence refs
- compaction lineage ห้าม summary-of-summary เกิน policy depth; rehydrate จาก CAS/event log ได้
- semantic compactor local model + independent deterministic/source verifier; optional second-model judge ไม่ self-grade
- artifact pointers/preview retrieval แทน raw tool output
- real task A/B fidelity lab: full/warm vs compiled/compacted ทำ task/patch/tool flow จริงใน temp workspace
- benchmark Thai/English/code/document/research/long-running sessions ที่ 64k/128k/256k/1M
- context inspector แสดง token by category, pinned reasons, compression lineage, cache hit/write และ free reserve

Budget policy ที่ 64k:

- direct tool schema hard ceiling 4k จาก exact serialization
- system/identity/policy ≤3k โดย default
- output reserve 8k, uncertainty 4k และ worst-case tool burst กันก่อน sampling
- Skill bodyโหลดเฉพาะ selected exact versions; metadata index bounded
- พื้นที่ที่เหลือให้ project/active historyตาม compiler policy ไม่ใช่ static prompt growth

Exit gates:

- essential goal/constraint/decision retention 100% บน gold corpus
- tool call/result causal split = 0
- false-success delta = 0 และ hallucination ไม่เกิน full-context baseline tolerance
- task/patch success delta ผ่าน threshold แยกตาม task class
- predicted token error อยู่ใน calibrated band และไม่เกิด provider overflow/silent truncation
- prompt prefix fingerprint คงที่ภายใน cache epoch

## Phase 10 — Capability, plugin and MCP breadth

เป้าหมาย: เพิ่มความสามารถโดยไม่ทำ core/schema/authority โตตาม ecosystem

งานหลัก:

- plugin SDK/manifest/versioning/signature/permissions/dependency resolver
- core adapters สำหรับ file patch/git/shell/browser/Office ผ่าน session-gated toolsets ไม่โหลดทุก session
- OS sandbox profiles ต่อ platform; network/filesystem/process ceilings
- MCP stdio child lifecycle, stderr bounds, process cancellation และ sandbox
- MCP resources, prompts เป็น human-invoked palette, notifications/subscriptions
- OAuth/authorization discovery, keychain secret refs, rotation invalidation
- elicitation/input-required flow ผ่าน trusted UI; sampling requests มี governance/budget/recursion guard
- per-session/per-desk/per-agent MCP placement และ allow/deny list
- capability search ranking/evidence พร้อม catalog scale benchmark 10k entries

Exit gates:

- plugin/MCP ไม่สามารถเพิ่ม authority เกิน SessionContract + user policy
- untrusted metadata/result ไม่ถูกตีความเป็น instruction
- catalog 10k entriesไม่เพิ่ม bootstrap prompt ตามจำนวน entries
- stdio/HTTP cancellation, timeout, restart, OAuth expiry และ no-retry effect tests ผ่าน
- signed plugin update rollback ได้; unsigned/local plugin มี trust badge ชัด

## Phase 11 — Native Aetox-style product shell, clean-room

เป้าหมาย: ให้ Aetox เป็น Main ในระดับ function/UX โดยไม่ copy implementation

แนวทางเทคนิคที่แนะนำ:

- Go core คงเป็น backend authority
- cross-platform native shell ใช้ Wails หรือ Tauri หลังทำ prototype benchmark; renderer Svelte/TypeScript เหมาะกับ footprint แต่การเลือกต้องอิง PTY/browser/accessibility/signing spike ไม่ใช่ความคุ้นเคย
- typed generated RPC/event contracts; renderer ไม่มี direct DB/filesystem mutation
- UI state รองรับ local backend, remote authenticated backend และ restart reconnect ตั้งแต่ contract แรก

Surfaces:

1. **Assistant / Code doors** — session templates/desk fixed, shared settings/Skills แต่แยก navigation และ project semantics
2. **Chat** — streaming, tool receipts, approval/uncertain states, context/cache inspector, queued apply-next-session changes
3. **Projects** — roots, project instructions/provenance, recent sessions, task/artifact status
4. **Workbench** — Files/editor/diff/diagnostics, PTY terminal, managed browser, artifacts/previews
5. **Deliverables** — documents, spreadsheets, slides พร้อม preview/export/checksum/source receipts
6. **Agent team** — task graph/status/budget/result handoff
7. **Automation** — durable schedules/runs/approvals/recovery
8. **Skill Control Center** — catalog, candidate diff, replay/eval, provenance graph, usage analytics, duplicate/merge, archive/restore
9. **Settings** — providers/models, profiles, tools/plugins/MCP, memory/identity, usage, backups, security

Exit gates:

- keyboard/accessibility/i18n Thai-English และ responsive window tests
- real PTY/browser/file diff/approval flows ผ่าน packaged-app E2E บน macOS/Windows/Linux target matrix
- restart/reconnect ไม่ทำ state ซ้ำหรือสูญ approval/result
- UI labels/features generate จาก backend capability state; unavailable feature ไม่แสดงเป็น completed
- signed installers, migration/backup/rollback และ crash reportingแบบ opt-in พร้อม

## Phase 12 — Multi-agent and durable background runtime

เป้าหมาย: งานยาวทำต่อได้และขยายทีม agent โดยไม่ปน context/authority

งานหลัก:

- parent-child task DAG; child มี SessionContract, context, Skill/memory/tool ceiling แยก
- role templates เช่น explore/plan/general/doc/sheet/research/code review แต่ติดตั้งเพิ่มผ่าน manifest ได้
- handoff ด้วย artifact/evidence refs ไม่ copy raw context ทั้งก้อน
- per-task model/provider selection และ resource budget
- scheduler keyed ตาม runtime/device เพื่อ serialize local model ที่ใช้ VRAM เดียวกันและ parallelize remote/independent work
- foreground priority, background preemption, resumable checkpoints, notifications
- durable cron/workflow run with approval boundaries และ restart recovery
- parent usage/provenance attribution รวม child token/tool/artifact receipts

Exit gates:

- child ไม่มีทางเห็น memory/project/tool ที่ไม่ได้ grant
- cancellation propagate parent→children→process/network
- local model memory pressure ไม่ทำ OOM cascade; scheduler remediation ชัด
- restart แล้ว task graph resume/interrupt อย่างซื่อสัตย์ ไม่ duplicate side effects
- parent summaryอ้าง artifact/evidence ที่ childผลิตได้ครบ

## Phase 13 — Provider and local-runtime ecosystem

เป้าหมาย: provider-flexible จริง ไม่ผูกกับ OpenAI-compatible semantics เพียงแบบเดียว

งานหลัก:

- Provider interface แยก messages/tools/streaming/usage/cache/context evidence/error normalization
- adapters: OpenAI-compatible, native Anthropic, Gemini และ local runtimes Ollama/LM Studio/vLLM/llama.cpp ตาม demand
- provider profiles + auth strategy + keychain; secrets ไม่เข้า event/model/UI response
- model capability registry ที่ runtime probe override static metadata ได้อย่างมี provenance
- fallback/router เป็น user policy ที่เห็นได้; ห้าม silent model/context/tool downgrade
- load/TTFT/prefill/generation/RAM/VRAM/OOM telemetry และ warm lifecycle
- qualification cache invalidation เมื่อ model hash/runtime/config/GPU allocation เปลี่ยน

Exit gates:

- contract suite เดียวผ่านทุก adapter ที่ประกาศ supported
- provider-specific tool/result serialization ไม่ทำ message alternationหรือschemaผิด
- cost/cache/usage receipts มี confidence/source ชัด
- qualification result stale ทันทีเมื่อ binding revision เปลี่ยน
- 64k default workflow ผ่านบน local reference hardware matrix ที่กำหนด

## Phase 14 — Security, operations and ecosystem release

งานหลัก:

- local principal, remote authentication, TLS/origin/CSRF policy และ per-device sessions
- OS keychain/secret broker; env refs ยังเป็น compatibility mode
- tamper-evident audit export และ signed backup manifest
- DB/CAS transactional recovery journal, migrations, integrity scan, repair preview
- plugin/Skill supply-chain provenance, signatures, quarantine และ policy bundles
- telemetry/usage export local-first; outbound opt-in เท่านั้น
- release channels, reproducible builds, SBOM, dependency scanning, signed artifacts
- performance/error budgets และ upgrade/rollback drills

Exit gates:

- threat model/penetration review ครอบคลุม local/remote/plugin/MCP/browser/PTY
- disaster-recovery drill restore sessions/Skills/artifacts/provenance ได้และไม่ auto-activate imported behavior
- release matrixผ่าน clean machine installs และ upgrades จากสอง versions ก่อนหน้า
- no-secret-in-log/event/model automated scanning ผ่าน

## Dependency order

```text
7.1 correctness
 ├─→ 8 Skill Learning OS ─┐
 ├─→ 9 Context 2.0       ├─→ 11 Native Product Shell
 └─→ 10 Capability/MCP ──┘          │
                                     ├─→ 12 Multi-agent
                                     └─→ 13 Provider ecosystem
                                              │
                                              └─→ 14 Release/security
```

Phase 8–10 ทำบาง workstreamขนานกันได้หลัง 7.1 แต่ acceptance gates ของแต่ละ phase ห้ามข้าม Native shell เริ่ม contract/design system prototype ได้ก่อน ทว่าไม่ควรประกาศ product parity จน kernel gates ผ่าน

## Test strategy

ทุก feature ต้องมีสี่ชั้นตามความเสี่ยง:

1. **Invariant/unit:** state machine, schema, token budget, permission intersection
2. **Integration:** SQLite/CAS/network/process/temp workspace โดยใช้ implementation จริง
3. **Behavior contract:** ความสัมพันธ์ข้าม component เช่น session revision ↔ prompt fingerprint ↔ tool binding
4. **Packaged E2E/fault injection:** UI→backend→provider/tool→receipt, cancel/restart/network loss/DB error/partial CAS move

Required continuous suites:

- race/concurrency and fuzz for parsers/state transitions
- local-model qualification matrix 64k/128k tiersที่มี hardware
- provider contract fixtures + selected live canary โดย secret ผ่าน CI vault
- prompt/cache regression fingerprint suite
- context fidelity benchmark with threshold dashboard
- Skill baseline/candidate eval corpus
- MCP/plugin hostile server/package fixtures
- packaged desktop accessibility/PTY/browser/install/upgrade E2E

การทดสอบ external endpoint ต้องแยก `protocol pass` ออกจาก `context certified`; final text หนึ่งครั้งบน 128k envelope ไม่ใช่หลักฐานว่า long-context recall 128k ผ่าน

## Delivery policy and milestone truth

แต่ละ phaseใช้สถานะเท่านั้น:

- `designed`: contract/ADR reviewed
- `vertical_slice`: happy pathจริงหนึ่งเส้น + tests
- `qualified`: negative/fault/E2E/performance gatesผ่านใน declared scope
- `production`: packaging/migration/security/operationsผ่าน

ห้ามใช้คำว่า `complete` หากเป็นเพียง deterministic evaluator, API surface หรือ UI tab และทุก release note ต้อง generate capability inventory/test evidence จาก runtime registry เพื่อลด documentation drift

## Recommended next execution batch

ลำดับ implementation ถัดไปที่ให้ risk reduction สูงสุด:

1. TurnLease + concurrency test
2. SessionContract/CacheEpoch + prompt fingerprint regression test
3. exact qualification binding + tiers 128k/256k/1M
4. runtime learning producer + idempotent event consumer
5. exact provider request token accounting
6. GC partial recovery fix/fault tests
7. TaskBudget แทน hard-coded four steps
8. Skill semantic/behavioral evaluator design spike
9. native shell/PTY/browser technical spike หลัง P0 gates ผ่าน

หลัง batch นี้จึงควรเริ่ม product shell phase เต็มตัว เพราะ state authority, context truth และ learning lifecycle จะมั่นคงพอให้ UI เป็นหน้าต่างของระบบจริง ไม่ใช่หน้าจอที่ต้องแก้สถาปัตยกรรมซ้ำภายหลัง
