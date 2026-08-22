# Hermetrix Harness — Aetox/Hermes Traceability Audit

วันที่ตรวจ: 2026-08-22  
ขอบเขต: source, tests, product surface และเอกสารใน workspace เดียวกัน

| Project | Snapshot ที่ตรวจ | License boundary |
|---|---|---|
| Aetox | `7d9ca19f29845a2a3266aa77202785aa96233a8e` | Aetox EULA v1.0 สำหรับ v1.3+; อ่าน/ตรวจ/เรียนรู้ได้ แต่ห้าม reuse source หรือทำ derivative |
| Hermes Agent | `2584b7c4eca82ada05f16eba08936d157b483329` | MIT |
| Hermetrix Harness | working tree ปัจจุบัน; ยังไม่มี Git metadata ใน directory นี้ | original clean-room implementation |

เอกสารนี้เป็น architecture audit ไม่ใช่คำรับรองความเท่าเทียมทาง feature และไม่ถือชื่อ package, README หรือ UI label เป็นหลักฐานว่าระบบทำงานครบ หลักฐานเรียงน้ำหนักจาก runtime path → integration test → unit test → documentation → UI copy

## Executive verdict

**สรุปสั้น:** Hermetrix นำแกนความคิดที่สำคัญมาใช้ได้ถูกทาง โดยเฉพาะ authority boundary, deferred capability, reversible Skill lifecycle, provenance และ typed context compiler แต่ยังไม่ถูกต้องพอที่จะเรียกว่า “Aetox เป็น Main และรวมระบบดีของ Hermes ครบแล้ว”

สถานะที่แม่นยำคือ **safe architectural vertical slice**:

- แกน clean-room และ license boundary: **Correct**
- Skill lifecycle แบบปลอดภัยกว่า Hermes: **Adapted, stronger authority**
- token-efficient narrow tool waist: **Correct and promising**
- context profile 32k/64k/128k/256k/1M: **Implemented as envelopes, not qualified at every tier**
- prompt-cache/session stability: **Partial; มีข้อขัดกับ invariant ของ Aetox/Hermes**
- Aetox product UX/workbench: **Mostly missing**
- Hermes background learning/curator/MCP breadth: **Partial**
- local-model harness ที่พร้อมใช้ต่อเนื่อง: **Not yet production-ready**

ก่อนขยาย native UI หรือเพิ่ม tool จำนวนมาก ต้องปิด P0 สี่เรื่อง: per-session turn lease, immutable session contract, qualification gate เกิน 64k และ runtime-to-learning producer

## วิธีอ่านสถานะ

| Status | ความหมาย |
|---|---|
| **Correct** | runtime path ตรงกับ invariant และมี test รองรับ |
| **Adapted** | ไม่ได้ทำเหมือน reference แต่แก้ trade-off อย่างมีเหตุผล |
| **Partial** | มี data model/API หรือ vertical slice แต่เส้นทางใช้งานจริงยังไม่ครบ |
| **Missing** | ยังไม่มี implementation ที่เทียบกันได้ |
| **Contradiction** | UI/docs/behavior ขัดกับ invariant หรือความจริงของ runtime |

## 1. Aetox: สิ่งที่เป็น product contract จริง

Aetox เป็น native Windows desktop app บน Wails/Svelte มี product model ที่ชัดกว่าความเป็น “chat UI”:

- สองประตู `Assistant` และ `Code` ใช้ binary/data/settings เดียวกัน แต่แยก session list และ workspace semantics
- session มี desk คงที่ (`assistant`, `coding`, `specialized`) ซึ่งเป็น tool ceiling ตลอดอายุ session
- workbench จริงประกอบด้วย Files, terminal, managed browser, artifacts และ Office deliverables
- มี agent chairs `doc`, `sheet`, `github`, `automation`, `research` และ fixed internal subagents; `task` กระจายงานพร้อมกันได้สูงสุดสี่งาน
- learning เข้าคิว review; memory tool เสนอแต่เขียนเองไม่ได้; repeated-failure summarizer เป็น deterministic producer
- approved learning มีผลใน session ถัดไป เพื่อไม่ทำลาย prompt prefix cache
- tool registry ส่ง 40 tools ใน fresh install และมี test บังคับ schema ceiling; desk ลดชุดที่ session ต้องแบก
- `skills_list` ส่ง metadata สั้นและ `skill_view` โหลด body ตามต้องการ ทำให้จำนวน Skills ไม่ทำให้ tool schema โต
- MCP วางตาม desk/agent และมี allowlist; implementation รองรับ stdio และ Streamable HTTP

ข้อจำกัดที่ต้องไม่สืบทอดแบบไม่พิจารณา:

- source ปัจจุบันเป็น proprietary source-available; ห้าม copy/reuse source, UI assets หรือทำ derivative จาก current implementation
- current product เป็น Windows-first และ test suite ที่ตรวจบน macOS มี failure ที่ project เองยอมรับว่าเป็น platform gap รวมทั้ง frontend embed ที่ยังไม่มี `frontend/dist`
- direct tool schema ใช้เกือบเต็ม budget ประมาณ 9.8k/10.1k tokens จึงต้องรักษา desk/toolset discipline อย่างเคร่งครัด
- documentation บางไฟล์ยังมี tool counts เก่ากว่าตัว registry ปัจจุบัน จึงควรใช้ generated contracts แทน hand-maintained claims

Hermetrix ทำถูกต้องแล้วที่ใช้ Aetox เป็น **behavioral research input** และประกาศ clean-room boundary ไม่ใช่ฐาน source สำหรับ port

## 2. Hermes Agent: สิ่งที่เป็น architecture contract จริง

Hermes มีฐานกว้างกว่าในด้าน harness และ multi-surface:

- system prompt ต้อง byte-stable ตลอด conversation; ห้าม reload memory/skills หรือเปลี่ยน toolset กลาง session ยกเว้น context compression
- capability เป็น property ของ session ไม่ใช่ environment ของ backend process
- core เป็น narrow waist; ลำดับการเพิ่ม capability คือ extend existing code → CLI+Skill → gated tool → plugin → MCP → core tool เป็นทางสุดท้าย
- Skill เป็น procedural memory ที่ agent สร้างและแก้ได้ผ่าน `skill_manage` พร้อม guards/read-before-write และ optional approval gate
- `skills_list`/`skill_view` เป็น progressive disclosure; usage/provenance เก็บ view/use/patch/created/installed/archive/restore
- background review ใช้ digest, yield ให้ foreground และผูก usage กลับ parent
- curator ดูแลเฉพาะ `created_by: agent`, ไม่ลบถาวร, archive/restore ได้, pinned ไม่ถูก auto-transition และมี backup/rollback
- MCP client มี stdio/HTTP, OAuth, resources, prompts, sampling, elicitation, reconnect และ lifecycle ที่กว้าง
- มี CLI/TUI/Electron/messaging, subagents, background terminal, cron และ profiles

ข้อจำกัดที่ Hermetrix ควรแก้ ไม่ควรคัดลอก:

- autonomy ของ Skill edit/auto-archive สูงกว่า authority model ที่ผู้ใช้ขอ; guard ที่ optional ย่อมมี configuration risk
- tool/product breadth สูงมาก และมีโมดูลหลักขนาดหลายพันบรรทัด ทำให้ change blast radius และ integration burden สูง
- narrow-waist เป็นหลักการที่ดี แต่จำนวน tool surface รวมยังใหญ่ จึงต้องพึ่ง toolsets/session gating อย่างถูกต้องเสมอ
- multi-surface Python/Electron เพิ่ม packaging, lifecycle และ state-authority complexity

## 3. Traceability matrix: นำมาใช้ถูกต้องหรือไม่

| Concern | Aetox pattern | Hermes pattern | Hermetrix ปัจจุบัน | Verdict |
|---|---|---|---|---|
| License/source boundary | current source ห้าม reuse | MIT reuse ได้ | original Go implementation; docs ระบุ clean-room | **Correct** |
| Product information architecture | Assistant/Code, rooms, workbench | Desktop/TUI/gateways | มี tabs Chat/Projects/Office/Artifacts/Skills/Providers/MCP/Context | **Partial** — map ชื่อได้ แต่ยังไม่ใช่ UX/function parity |
| Session capability | desk fixed for session | capability/session scoped | StepBinding freeze ต่อ sampling step | **Partial** — ยังไม่มี immutable SessionContract ที่ freeze desk/toolset/skill set |
| Prompt caching | prompt bootstrap-only; learning next session | byte-stable conversation | compile prompt และเลือก active Skill ใหม่ทุก model step | **Contradiction** |
| Direct tool budget | desk-narrowed 40 tools, enforced ceiling | narrow waist/toolsets/plugins/MCP | 6 direct tools; catalog defer เป็น search/describe/call | **Correct**, แต่ token accounting ยังไม่ครบ provider serialization |
| Skill disclosure | list metadata → view body | list/view progressive disclosure | selector โหลด body สูงสุด 3 Skills | **Correct concept**, selector/reload boundary ต้องแก้ |
| Skill authority | memory proposal + human approval | agent CRUD + guards/approval | candidate-only, immutable versions, promote by human, protected fork | **Adapted; safer default** |
| Skill replay | product behavior/testing | Skill package guidance | deterministic lexical fixtures + baseline diff | **Partial** — lifecycle gate จริง แต่ไม่ใช่ agent/tool behavioral eval |
| Learning producer | memory proposal + repeated-failure producer | background review/skill manage | Enqueue API และ policy validator มี แต่ agent runtime ไม่ enqueue | **Contradiction** กับ UI claim |
| Usage/provenance | review reason/decision history | rich sidecar usage/provenance | activations, outcome, tools, owner/origin/version lineage | **Correct for exposure provenance**; ไม่ใช่ causal effectiveness |
| Curator | review-driven memory | auto-stale/archive agent-created + backup | report-only stale/duplicate/consolidation; no mutation | **Adapted; safer**, automation/semantic review ยังขาด |
| Archive/restore | reviewable memory | restorable archive + rollback | immutable archives, restore-as-candidate, CAS quarantine | **Correct**, พบ edge-case ใน partial GC restore |
| Context compiler | compact bootstrap prompt | mature compressor/context analytics | typed fragments, budgets, spill, causal pairs, verified fallback | **Correct vertical slice**, semantic/eval/tokenizer ยังไม่ production-grade |
| Context profiles | provider/desk aware | detected window + compression | exact 32k/64k/128k/256k/1M envelopes | **Partial** — qualification capacity hard-stop ที่ 64k |
| Provider qualification | provider UX | many providers/profiles | OpenAI-compatible adapter + behavioral suite | **Partial** — session creation เชื่อ declared window และไม่ได้ enforce latest eligible qualification |
| MCP | stdio + HTTP, per desk/agent | broad protocol lifecycle | Streamable HTTP tools only, modern+legacy, safe revision binding | **Correct narrow slice**, breadth missing |
| Multi-agent | chairs + task fan-out | delegate/subagents | ไม่มี parent-child agent runtime | **Missing** |
| Background work | task/automation/work rooms | background terminal/cron/reviewer | allowlisted command jobs + maintenance scheduler | **Partial** — learning jobs ไม่ถูก schedule/produce อัตโนมัติ |
| Native workbench | browser/terminal/files/Office | Electron panes/browser/projects | local web control center | **Missing as Aetox-main UX** |
| Security boundary | approvals/safety chokepoint | tool guards/permissions | exact revision/args grants, no auto-retry, loopback | **Strong partial** — no OS sandbox, remote auth, actor identity or keychain |
| Tests | large platform-specific suite | wrapper-enforced Python suite + E2E policy | 89 Go tests, race/vet/build/static JS pass | **Good foundation**, real UX/local-model/restart concurrency E2E gaps |

## 4. Critical findings in Hermetrix

### P0-1 — ไม่มี per-session turn lease

`RunTurn` ตรวจว่า session เป็น `active` แล้ว append user event ก่อนเข้า global inference gate สอง request ของ session เดียวกันจึงผ่าน state check และเขียน user message ได้พร้อมกัน เมื่อ request แรกได้ gate มันอาจเห็น user message ของ request ที่สองใน history ทำให้ strict role alternation และ turn isolation เสีย

หลักฐาน: `internal/agent/service.go:140-183`

สิ่งที่ต้องมี:

- persisted lease/CAS transition `active → running(turn_id)` ก่อน append user event
- unique active-turn constraint ต่อ session
- acquire/commit เป็น transaction เดียว
- recovery เปลี่ยน orphaned running turn เป็น interrupted โดยไม่ fabricate result
- concurrency integration test ที่ยิงสอง requests พร้อมกันและยืนยันหนึ่ง request ถูก reject/queue ก่อน event commit

### P0-2 — prompt และ Skill set ไม่คงที่ตลอด session/turn

ทุก model step เรียก `compileTurn` ใหม่ จากนั้นเลือก active Skills ตาม current goal และอ่าน `CurrentVersionID` ในขณะนั้น การ promote/archive Skill ระหว่าง tool steps จึงเปลี่ยน prompt ของ turn เดิมได้ แม้ StepBinding จะ freeze context ของแต่ละ sampling stepก็ตาม

หลักฐาน: `internal/agent/service.go:204-249`, `789-817`, `875-932`

นี่ขัดทั้ง Aetox next-session learning rule และ Hermes byte-stable system-prompt rule วิธีที่ควรใช้คือสร้าง immutable `SessionContract` ตอนเปิด session และบันทึก `skill_catalog_revision`, selected Skill version IDs, toolset/desk, policy/prompt revision, provider/model revision และ context profile การเปลี่ยน durable state default เป็น `pending_for_next_session`; หากมี “apply now” ต้องเริ่ม cache epoch ใหม่อย่าง explicit และบันทึกเหตุผล

### P0-3 — UI อ้าง automatic learning producer แต่ runtime ไม่ได้เรียก

ใน production code พบ `learning.Enqueue` จาก HTTP handler เท่านั้น ไม่พบ producer จาก agent loop UI กลับบอกว่า agent runtime จะ enqueue successful milestones, repeated corrections, explicit learn และ Skill failures จึงเป็น claim drift เส้นทาง learning ปัจจุบันคือ manual/API → queued reviewer ไม่ใช่ runtime evidence → reviewer

หลักฐาน: `internal/web/server.go:672-676`, `internal/web/ui/app.js:194-199`

ต้องเพิ่ม event consumer ที่ idempotent หลัง turn commit, correction detector และ explicit-learn event โดย job ต้องผูก exact event range/digest hash และไม่ enqueue จาก output ที่ยังไม่ committed

### P0-4 — context 128k/256k/1M เลือกได้โดยไม่ผ่าน qualification gate

session creation ตรวจเพียง `profile.Total <= provider.ContextWindow` ซึ่งเป็นค่าที่ผู้ใช้/metadata ประกาศ ขณะที่ qualification แปลง tier เป็น capacity ได้สูงสุด 65,536 tokens ดังนั้น 128k/256k/1M ไม่มีทาง `eligible` จาก suite ปัจจุบัน แต่ UI/session ยังเลือกได้ตาม declared window โดยไม่ตรวจ latest successful qualification หรือ persisted override

หลักฐาน: `internal/agent/service.go:43-75`, `internal/qualification/service.go:95-101`, `348-356`, `internal/web/ui/app.js:310`

นโยบายที่แนะนำ:

- product minimum/default = **64k Certified**
- 32k คงไว้เป็น compatibility/degraded mode ที่แสดง badge ชัด ไม่ใช่ default
- 128k/256k/1M แสดงได้ แต่เปิด session ได้เมื่อมี qualification exact tier + model/provider/runtime revision ตรงกัน
- remote endpoint ที่พิสูจน์ loaded allocation ไม่ได้ต้องใช้ explicit, persisted, expiring override—not a silent declaration

### P1-1 — tool-schema token accounting ต่ำกว่าของจริง

`ContextSpecs` ส่ง name + parameter schema + revision/effect ให้ estimator แต่ไม่รวม description, function wrapper และ provider-specific JSON serialization ทั้งก้อน Budget จึงอาจผ่านใน compiler แต่ request จริงเกิน ceiling

หลักฐาน: `internal/tools/registry.go:148-164`

ต้องนับจาก exact serialized request adapter ของ provider และ calibrate กับ provider usage response เมื่อมี

### P1-2 — deterministic replay ยังไม่วัด agent behavior

Skill replay ปัจจุบันตรวจ required/forbidden terms และ tool hints เหมาะเป็น fast lint gate แต่ยังตอบไม่ได้ว่า Skill candidate ทำให้ local modelแก้ task จริงดีขึ้นหรือแย่ลง ต้องคง deterministic gate ไว้ แล้วเพิ่ม sandboxed behavioral runner เป็น gate ชั้นถัดไป—not replace it

### P1-3 — attribution เป็น correlation ไม่ใช่ causation

Hermetrix บันทึก selection, body injection, outcome และ tool calls ได้ดี แต่ activation ที่เกิดใน successful turn ไม่ได้พิสูจน์ว่า Skill เป็นสาเหตุ ควรคง label `exposure_only` และเพิ่ม controlled baseline/candidate eval ก่อนใช้ข้อมูลนี้ promote หรือ consolidate อัตโนมัติ

### P1-4 — GC partial restore state update ไม่ครอบคลุม

`RestoreGC` รับทั้ง `quarantined` และ `partial_quarantine` แต่ SQL update ยอมเปลี่ยน state เฉพาะ `state='quarantined'` และไม่ได้ตรวจ rows affected จึงอาจ return object ว่า restored ทั้งที่ DB ยังเป็น partial นอกจากนี้ถ้าย้าย blobs ครบแล้วแต่ DB update ล้มเหลว ยังไม่มี compensating recovery record

หลักฐาน: `internal/curator/maintenance.go:172-193`

### P1-5 — provenance actor ยังเป็น claim

control server เป็น loopback single-user และยังไม่มี authenticated principal API ที่รับ actor/origin จึงไม่ควรถูกตีความว่าเป็น cryptographic/user identity provenance จนกว่าจะมี local principal/session identity, OS keychain และ signed/exportable audit chain

### P1-6 — agent loop จำกัดสี่ steps แบบ hard-coded

`maxAgentSteps = 4` เหมาะกับ vertical slice แต่สั้นเกิน coding/research/Office workflows ไม่ควรเพิ่มเป็นตัวเลขใหญ่เฉย ๆ ต้องเปลี่ยนเป็น bounded budget policy: model calls, tool calls, wall time, token spend, effect count และ user-configured task class พร้อม loop detector

หลักฐาน: `internal/agent/service.go:22-26`, `204-214`

## 5. สิ่งที่ Hermetrix ออกแบบได้ดีกว่า reference

ควรรักษาส่วนต่อไปนี้ไว้เป็น core identity ของ Hermetrix:

1. **Candidate-first learning.** Agent/reviewer ไม่มีสิทธิ์ promote durable behavior เอง เป็น default ที่ปลอดภัยกว่า Hermes autonomous CRUD
2. **Exact-revision capability widening.** Skill content กับ authority แยกกัน และ approval ผูก revision/argument hash
3. **Three deferred capability primitives.** Catalog 1,500 tools ไม่ทำให้ prompt schema โตตาม catalog เป็น narrow waist ที่เข้มกว่า direct registry ขนาดใหญ่
4. **Typed context fragments + causal pairs.** compaction ไม่ควรแยก tool call/result และ pinned overflow fail closed
5. **Uncertain-not-success recovery.** interrupted side effect ไม่ถูก retry หรืออ้างว่าสำเร็จ
6. **Report-only curator.** เหมาะกับช่วงที่ attribution/eval ยังไม่พอ; auto-archive ควรเป็น policy opt-in หลังมี undo snapshot เท่านั้น
7. **Restore-as-candidate.** archive/import ไม่สามารถลัด review gate กลับมา active

## 6. ช่องว่างของ Aetox-main UX

tab names ปัจจุบันไม่เท่ากับ function parity:

| Surface เป้าหมาย | Hermetrix ปัจจุบัน | Definition of done ที่ต้องการ |
|---|---|---|
| Native shell | loopback web app | signed cross-platform desktop, typed bridge, backend เป็น state authority |
| Assistant/Code doors | ไม่มี fixed surface contract | session template/desk แยก navigation, tool ceiling, project semantics และ prompt revision |
| Files | bounded list/read/write APIs | tree, editor, diff, diagnostics, optimistic saves, artifact handoff |
| Terminal | background allowlisted jobs | interactive PTY, foreground/background, cancel, durable receipt, sandbox profile |
| Browser | ไม่มี | managed tabs, observe/action receipts, download/artifact policy, session capability gate |
| Office | label ใช้กับ command jobs | real document/spreadsheet/slides deliverables, preview, export, provenance |
| Agent team | ไม่มี | parent-child task graph, budgets, isolated context/memory/tool ceilings, result artifact handoff |
| Automation | maintenance schedules เท่านั้น | durable user workflows, restart recovery, approval/effect policy, observable runs |
| Skill Control Center | มี vertical slice | diff/replay/eval/duplicate merge/provenance graph/usage cohorts/rollback UX ครบ |

คำว่า “Office” ใน UI ปัจจุบันควรเปลี่ยนเป็น “Background Jobs” จนกว่าจะมี deliverable workspace จริง เพื่อไม่ให้ product claim สูงกว่าความสามารถ

## 7. Test evidence

### Hermetrix

รันใน snapshot นี้และผ่าน:

```text
go test ./...                89 tests passed, 0 failed
go test -race ./...          passed
go vet ./...                 passed
go build ./...               passed
node --check internal/web/ui/app.js   passed
```

ผลนี้ยืนยัน current unit/integration/race/static contracts แต่ยังไม่ยืนยัน:

- concurrent turns ต่อ session
- prompt-cache epoch stability ข้ามการ promote/archive Skill
- runtime auto-enqueue learning
- certified 128k/256k/1M recall/allocation
- crash/restart ระหว่าง model streaming และ DB/CAS split-brain
- native UI/browser/PTY/Office flows
- real local-model end-to-end matrix

### Aetox reference

`go test ./...` บน macOS ไม่ผ่านจาก Windows/path/sandbox expectations และ missing frontend embed; หลาย package อื่นผ่าน ผลนี้ใช้เป็น evidence ว่า reference snapshot ไม่ใช่ cross-platform oracle ไม่ใช่ข้อสรุปว่า architecture ทั้งหมดเสีย

### Hermes reference

targeted tests ไม่ได้ถูกรัน เพราะ repository policy บังคับ wrapper ที่ต้องใช้ project venv ซึ่ง snapshot นี้ไม่มี pytest ใน `.venv`/`venv` การข้าม wrapper จะขัด project instructions จึงบันทึกเป็น **not executed**, ไม่ใช่ failed หรือ passed

## 8. Final assessment

Hermetrix ไม่ควรย้อนกลับไป copy architecture ทั้งก้อนจาก project ใด project หนึ่ง สูตรที่เหมาะคือ:

- ใช้ **Aetox เป็น behavioral product contract** สำหรับ Assistant/Code, desk, workbench และ human-review learning
- ใช้ **Hermes เป็น harness contract** สำหรับ prompt-cache invariant, session-scoped capability, background review, progressive Skills, plugins/MCP และ multi-surface operational maturity
- ใช้ **Hermetrix authority model** เป็นตัวตัดสินเมื่อ reference ขัดกัน: candidate-first, exact revision, reversible mutation, typed context และ measured qualification

ดังนั้นคำตอบคือ “โครงหลักถูกทิศ แต่ยังนำมาใช้ไม่ครบและมี P0 contradictions สี่จุด” แผนแก้และ phase ถัดไปอยู่ใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md)

## Appendix — Source evidence map

ตำแหน่งเหล่านี้เป็นจุดเริ่มต้นสำหรับ reproduce audit บน snapshots ที่ระบุด้านบน; line number อาจเลื่อนหาก source เปลี่ยน

### Aetox

- `../../Aetox/LICENSE:1-99` — source-available terms และข้อห้าม reuse/derivative
- `../../Aetox/README.md:181-250` — Assistant/Code, agents, learning approval, next-session effect และ fixed desks
- `../../Aetox/README.md:298-331` — generated tool inventory, schema budget และ list/view progressive disclosure
- `../../Aetox/internal/prompt/README.md:5-24` — prompt assembly, bootstrap-only reload และ local-model token discipline
- `../../Aetox/internal/turn/README.md` — model-first turn loop, safety chokepoint และ pending slow-tool behavior
- `../../Aetox/internal/skill/README.md` — packed tools, action-level safety และ placement rules

### Hermes Agent

- `../../hermes-agent/AGENTS.md:16-27` — prompt-cache invariant และ narrow waist
- `../../hermes-agent/AGENTS.md:71-91` — footprint ladder, behavior contracts, strict alternation และ byte-stable prompt
- `../../hermes-agent/AGENTS.md:213-250` — session-scoped surface capability
- `../../hermes-agent/AGENTS.md:1090-1114` — curator usage/provenance/archive/restore invariants
- `../../hermes-agent/AGENTS.md:1204-1217` — deferred cache invalidation policy
- `../../hermes-agent/agent/background_review.py`, `agent/curator.py`, `agent/curator_backup.py` — background lifecycle implementation
- `../../hermes-agent/tools/skill_usage.py`, `skill_manager_tool.py`, `skills_tool.py` — usage, Skill mutation guards และ progressive disclosure
- `../../hermes-agent/tools/mcp_tool.py` — broad MCP transport/protocol lifecycle

### Hermetrix

- `internal/agent/service.go:43-75`, `140-249`, `789-932` — session admission, turn concurrency, per-step compile และ Skill selection
- `internal/qualification/service.go:95-101`, `348-356` — eligibility และ 64k capacity ceiling
- `internal/tools/registry.go:148-164` — provider definitions เทียบ context token specs
- `internal/web/server.go:672-676`, `internal/web/ui/app.js:194-199` — manual enqueue path เทียบ automatic-runtime UI claim
- `internal/curator/maintenance.go:145-193` — GC quarantine/partial restore transitions
- `internal/context/` — profiles, compiler, compactor, spill, estimator และ verified fallback
- `internal/skills/` — candidates, versions, replay, provenance, activations, archive/restore และ curator evidence
- `internal/mcp/` — Streamable HTTP, schema/revision/risk binding และ redaction
