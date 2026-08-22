# Hermetrix Harness — Aetox/Hermes Traceability Audit

วันที่ตรวจครั้งแรก: 2026-08-22  
วันที่ตรวจซ้ำกับ source จริง: 2026-08-22 (verification pass)  
ขอบเขต: source, tests, product surface และเอกสารใน workspace เดียวกัน

> **สถานะเอกสาร:** audit รอบแรกเขียนก่อนที่ implementation จะตามแก้เสร็จ ทำให้ระบุ P0 ค้างสี่ข้อทั้งที่โค้ดปิดไปแล้วสามข้อครึ่ง ฉบับนี้ผ่าน verification pass กับ runtime แล้ว หัวข้อ 4 แยกเป็น “ปิดแล้ว” และ “ยังเปิดอยู่” ชัดเจน
>
> แผนที่ใช้เดินงานคือ [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md) ซึ่งเป็น forward source of truth

| Project | Snapshot ที่ตรวจ | License boundary |
|---|---|---|
| Aetox | `7d9ca19f29845a2a3266aa77202785aa96233a8e` | Aetox EULA v1.0 สำหรับ v1.3+; อ่าน/ตรวจ/เรียนรู้ได้ แต่ห้าม reuse source หรือทำ derivative |
| Hermes Agent | `2584b7c4eca82ada05f16eba08936d157b483329` | MIT |
| Hermetrix Harness | working tree ปัจจุบัน; **ยังไม่มี Git metadata ใน directory นี้** จึงอ้าง commit ไม่ได้ (finding O-1) | original clean-room implementation |

เอกสารนี้เป็น architecture audit ไม่ใช่คำรับรองความเท่าเทียมทาง feature และไม่ถือชื่อ package, README หรือ UI label เป็นหลักฐานว่าระบบทำงานครบ หลักฐานเรียงน้ำหนักจาก runtime path → integration test → unit test → documentation → UI copy

## Executive verdict

**สรุปสั้น:** Hermetrix นำแกนความคิดที่สำคัญมาใช้ได้ถูกทาง โดยเฉพาะ authority boundary, deferred capability, reversible Skill lifecycle, provenance และ typed context compiler หลัง verification pass พบว่า P0 ทั้งสี่ข้อของรอบแรกถูกปิดในโค้ดแล้ว เหลือช่องว่างเชิงสถาปัตยกรรมหนึ่งข้อและงาน correctness ที่วัดได้อีกชุดหนึ่ง

สถานะที่แม่นยำคือ **safe architectural vertical slice ที่ implementation ของ kernel correctness ครบแล้ว แต่หลักฐานยังไม่ครบ** — จากหก finding ที่เคยประกาศปิด มีหนึ่งข้อที่หลักฐานถึงเกรด A:

- แกน clean-room และ license boundary: **Correct**
- Skill lifecycle แบบปลอดภัยกว่า Hermes: **Adapted, stronger authority**
- token-efficient narrow tool waist: **Correct**; แต่ token accounting ยังต่ำกว่าจริง (O-3)
- context profile 32k/64k/128k/256k/1M: **Correct**; qualification ครบทุก tier แล้ว แต่หลักฐาน recall ยังเป็น flag เดียว (O-6)
- prompt-cache/session stability: **Correct**; SessionContract + TurnLease + CacheEpoch มีจริงและมี test
- Skill retrieval ตอน runtime: **Contradiction** — ต่างจากทั้ง Aetox และ Hermes ในทางที่เสียประโยชน์ (O-2)
- Aetox product UX/workbench: **Mostly missing** (โดยตั้งใจ ตาม ADR-8)
- Hermes background learning/curator/MCP breadth: **Partial**
- local-model harness ที่พร้อมใช้ต่อเนื่อง: **Not yet production-ready**

ก่อนขยาย capability หรือ product surface ต้องปิดตามลำดับ: version control (O-1), documentation truth (O-7), verification gaps (V-1 ถึง V-6), exact token accounting (O-3), แล้วจึงแก้ Skill retrieval (O-2)

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
| Session capability | desk fixed for session | capability/session scoped | `SessionContract` freeze provider/model/profile/policy/capability/skill catalog + `TaskBudget`; `StepBinding` freeze ต่อ sampling step | **Correct** — desk/surface ceiling ยังรอ product shell |
| Prompt caching | prompt bootstrap-only; learning next session | byte-stable conversation | Skill เลือกครั้งเดียวต่อ session แล้ว freeze ใน contract; `compileTurn` อ่านเฉพาะ contract | **Correct** — มี `TestSessionUsesFrozenSkillVersionAfterLaterPromotion` |
| Direct tool budget | desk-narrowed 40 tools, enforced ceiling | narrow waist/toolsets/plugins/MCP | 6 direct tools; catalog defer เป็น search/describe/call | **Correct**, แต่ token accounting ยังไม่ครบ provider serialization |
| Skill disclosure | `skills_list` → `skill_view` เป็น **tool** | `skills_list` / `skill_view` เป็น **tool** | inject body สูงสุด 3 Skills เข้า prompt โดยเลือกจาก goal ของ turn แรกเท่านั้น | **Contradiction** (O-2) — ต้นแบบทั้งสองใช้ tool-based disclosure; Hermetrix ใช้ prompt injection ทำให้ session ที่เปลี่ยนหัวข้อไม่ได้ Skill ที่ตรง ดู ADR-7 |
| Skill authority | memory proposal + human approval | agent CRUD + guards/approval | candidate-only, immutable versions, promote by human, protected fork | **Adapted; safer default** |
| Skill replay | product behavior/testing | Skill package guidance | deterministic lexical fixtures + baseline diff | **Partial** — lifecycle gate จริง แต่ไม่ใช่ agent/tool behavioral eval |
| Learning producer | memory proposal + repeated-failure producer | background review/skill manage | `learning_trigger_outbox` + `StageTrigger` ใน transaction เดียวกับ turn commit + `DrainPending` หลัง turn | **Correct**, แต่ยังไม่มี test ครอบ path นี้เลย (O-4) |
| Usage/provenance | review reason/decision history | rich sidecar usage/provenance | activations, outcome, tools, owner/origin/version lineage | **Correct for exposure provenance**; ไม่ใช่ causal effectiveness |
| Curator | review-driven memory | auto-stale/archive agent-created + backup | report-only stale/duplicate/consolidation; no mutation | **Adapted; safer**, automation/semantic review ยังขาด |
| Archive/restore | reviewable memory | restorable archive + rollback | immutable archives, restore-as-candidate, CAS quarantine | **Correct**, พบ edge-case ใน partial GC restore |
| Context compiler | compact bootstrap prompt | mature compressor/context analytics | typed fragments, budgets, spill, causal pairs, verified fallback | **Correct vertical slice**, semantic/eval/tokenizer ยังไม่ production-grade |
| Context profiles | provider/desk aware | detected window + compression | exact 32k/64k/128k/256k/1M envelopes; qualification tier ครบถึง `ultra-1m` | **Correct**, แต่หลักฐาน recall ยังเป็น bool เดียวทุก tier (O-6) |
| Provider qualification | provider UX | many providers/profiles | OpenAI-compatible adapter + behavioral suite + `resolveQualification` ที่บังคับ exact eligibility หรือ expiring override ที่มี actor/reason | **Correct** — มี `TestSessionRequiresExactQualificationOrReviewedOverride` |
| MCP | stdio + HTTP, per desk/agent | broad protocol lifecycle | Streamable HTTP tools only, modern+legacy, safe revision binding | **Correct narrow slice**, breadth missing |
| Multi-agent | chairs + task fan-out | delegate/subagents | ไม่มี parent-child agent runtime | **Missing** |
| Background work | task/automation/work rooms | background terminal/cron/reviewer | allowlisted command jobs + maintenance scheduler | **Partial** — learning jobs ไม่ถูก schedule/produce อัตโนมัติ |
| Native workbench | browser/terminal/files/Office | Electron panes/browser/projects | local web control center | **Missing as Aetox-main UX** |
| Security boundary | approvals/safety chokepoint | tool guards/permissions | exact revision/args grants, no auto-retry, loopback | **Strong partial** — no OS sandbox, remote auth, actor identity or keychain |
| Tests | large platform-specific suite | wrapper-enforced Python suite + E2E policy | 96 Go test functions ใน 17 packages, race/vet/build/static JS pass | **Good foundation**, ขาด outbox suite และ real UX/local-model/restart concurrency E2E |

## 4. Findings in Hermetrix

### 4.1 Implementation ครบแล้ว — แต่หลักฐานยังไม่ครบ

รอบตรวจแรกใช้ *ชื่อ test* เป็นหลักฐาน รอบที่สองอ่าน **เนื้อ assertion** จริง ผลคือหลายข้ออ่อนกว่าที่ประกาศไว้

เกรดหลักฐาน: **A** = assertion ตรวจ behavior ตรงและจะ fail ถ้าถอย behavior ออก · **B** = ตรวจบางส่วน · **C** = มี test แต่ไม่ครอบ mechanism หลัก · **D** = ไม่มี test

**finding ถือว่าปิดได้เมื่อ implementation ครบและหลักฐานเป็นเกรด A เท่านั้น**

| Finding รอบแรก | Implementation | เกรด | ปิดจริง? | ช่องว่าง |
|---|---|---|---|---|
| **P0-1** turn lease | ครบ — `acquireTurn` CAS `internal/agent/service.go:324`; recovery `:1066`; schema `internal/store/store.go:759` | **C** | ยัง | test เป็น deterministic sequencing ไม่ใช่ race — block request แรกใน HTTP handler แล้วยิง turn ที่สองแบบ synchronous ความปลอดภัยมาจาก SQL CAS + single-connection SQLite ไม่ใช่จาก test → **V-1** |
| **P0-2** SessionContract | ครบ — `buildSessionContract` `:144`; `initializeSessionSkills` `:363`; `compileTurn` `:1148` | **A** | **ปิดแล้ว** | test promote version ใหม่กลาง session แล้ว assert ว่า system prompt ยังมี `FROZEN_VERSION_ONE` ไม่มี `NEW_VERSION_TWO` และ binding ไม่ drift |
| **P0-3** learning outbox | ครบ — `StageTrigger` `internal/learning/service.go:55`; `DrainPending` `:86`; hook `internal/agent/service.go:1431`/`:1480`/`:309` | **D** | ยัง | ไม่มี test ใดแตะ outbox → **O-4 / V-2** |
| **P0-4** qualification gate | ครบ — `contextTier` `internal/qualification/service.go:341`; `resolveQualification` `internal/agent/service.go:130` | **B** | ยัง | assert แค่ reject-without-qualification และ freeze run ID **ไม่ assert override path เลย** ทั้งที่ชื่อ test บอกว่า `OrReviewedOverride`; ไม่ทดสอบ expiry; ไม่ทดสอบ gating ของ 128k/256k/1M; ไม่ทดสอบ revision staleness → **V-4** |
| **P1-4** GC restore | ครบ — `RestoreGC` `internal/curator/maintenance.go:188`; compensating rollback `:174` | **B** | ยัง | test ครอบ stale guard, quarantine และ convergence ของ `partial_quarantine` แต่สร้าง state นั้นด้วย `UPDATE` ตรง ๆ จึง**ไม่เคยรัน compensating rollback path จริง** → **V-5** |
| **P1-6** TaskBudget | **ครบกว่าที่เอกสารเคยอ้าง** — enforce ครบสี่มิติ (model steps `:504`, tool calls `:508`, cumulative tokens `:494`, wall time `:294`) และมี loop detector หยุด identical call ครั้งที่สาม `:514` | **D** | ยัง | grep หา `TaskBudget`/`MaxModelSteps`/`MaxToolCalls`/`MaxWallTime`/`MaxCumulative` ใน test ทั้งโครงการได้ศูนย์ผลลัพธ์ → **V-3** |

**สรุป:** จากหก finding มี **หนึ่งข้อ (P0-2) ที่หลักฐานถึงเกรด A**

implementation ไม่ได้ผิด — อ่านโค้ดแล้วทั้งหกข้อทำถูกตามสัญญา และ P1-6 ทำมากกว่าที่เอกสารเคยอ้าง สิ่งที่ขาดคือ **หลักฐานว่าจะไม่ถอยกลับ** งานที่ตามมาคือ V-1 ถึง V-6 ซึ่งเป็นงานเขียน test ทั้งหมด ประมาณ 3.5 วัน-คน

**ข้อสังเกตเรื่องกระบวนการ:** พบชื่อ test สองตัวที่สัญญามากกว่าที่ assertion ตรวจ (`TestConcurrentTurnsCommitOnlyOneUserEvent`, `TestSessionRequiresExactQualificationOrReviewedOverride`) ซึ่งเป็นต้นเหตุที่ audit รอบแรกให้เครดิตเกินจริง ดู V-6

### 4.2 ยังเปิดอยู่

รายละเอียดเต็มและมาตรการอยู่ใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md) หัวข้อ *Findings ที่ยังเปิดอยู่จริง* และ *Risk register*

| ID | เรื่อง | severity | หลักฐาน |
|---|---|---|---|
| **O-1** | ไม่มี `.git` ใน `Hermetrix-harness/` ทั้งที่มี source 19,693 บรรทัด | critical | ไม่มี history/rollback/bisect และ audit อ้าง commit ของ Hermetrix ไม่ได้ ต่างจาก Aetox/Hermes ที่อ้าง SHA ได้ |
| **O-2** | Skill retrieval เป็น prompt injection ที่เลือกครั้งเดียวจาก goal แรก แทน tool-based progressive disclosure แบบต้นแบบทั้งสอง | high | `internal/agent/service.go:387`; เทียบ `../../Aetox/README.md:298-331` และ `../../hermes-agent/tools/skills_tool.py` |
| **O-3** | tool-schema token accounting ต่ำกว่าจริง — `ContextSpecs()` ทิ้ง description และ function wrapper | high | `internal/tools/registry.go:158` เทียบกับ `ProviderDefinitions()` ที่ `:150` ซึ่งส่ง description ออกไปจริง |
| **O-4** | outbox path ไม่มี test | medium | ไม่มีคำว่า outbox ปรากฏใน `internal/learning/service_test.go` หรือ test ใดในโครงการ |
| **O-5** | `(*Service).selectSkills` เป็น dead code ที่ยังอ่าน live `CurrentVersionID` | medium | `internal/agent/service.go:1243`, `:1260`; ไม่มีผู้เรียก และ `go vet` ไม่จับ |
| **O-6** | `LongContextRecall` เป็น bool เดียวสำหรับทุก tier ตั้งแต่ 32k ถึง 1M | medium | `internal/qualification/service.go:342`; ขัดกับ ADR-5 ที่ระบุว่า 1M ต้องผ่าน chunk-position recall และ long-run stability |
| **O-7** | documentation drift | medium | audit/plan/roadmap ระบุ P0 ค้างทั้งที่ปิดแล้ว; test count เขียน 89 ค่าจริง 96; UI ยังใช้ label `Office` — `internal/web/ui/index.html:21`, `:62` |

### 4.3 Findings เดิมที่ยังคงสถานะ

- **P1-2 deterministic replay ยังไม่วัด agent behavior** — replay ตรวจ required/forbidden terms และ tool hints เหมาะเป็น fast lint gate แต่ตอบไม่ได้ว่า Skill candidate ทำให้ model แก้ task ได้ดีขึ้นจริง ต้องคง deterministic gate ไว้แล้วเพิ่ม sandboxed behavioral runner เป็นชั้นถัดไป ไม่ใช่แทนที่
- **P1-3 attribution เป็น correlation ไม่ใช่ causation** — activation ที่เกิดใน successful turn ไม่พิสูจน์ว่า Skill เป็นสาเหตุ ต้องคง label `exposure_only` และเพิ่ม controlled baseline/candidate eval ก่อนใช้ข้อมูลนี้ promote หรือ consolidate อัตโนมัติ
- **P1-5 provenance actor ยังเป็น claim** — control server เป็น loopback single-user และยังไม่มี authenticated principal จึงไม่ควรตีความว่าเป็น cryptographic identity provenance จนกว่าจะมี local principal, OS keychain และ signed audit chain

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

คำว่า “Office” ใน UI ปัจจุบันควรเปลี่ยนเป็น “Background Jobs” จนกว่าจะมี deliverable workspace จริง เพื่อไม่ให้ product claim สูงกว่าความสามารถ — **ยังไม่ได้แก้** ณ รอบ verification pass (`internal/web/ui/index.html:21`, `:62`) จัดเป็นส่วนหนึ่งของ O-7

หมายเหตุลำดับความสำคัญ: ตาราง gap นี้เป็น *product parity* ที่ FUTURE-ARCHITECTURE-PLAN จัดให้เป็น optional track ตาม ADR-8 ไม่ใช่งานที่ต้องปิดก่อน kernel gates

## 7. Test evidence

### Hermetrix

รันใน snapshot นี้และผ่าน:

```text
go build ./...               passed
go vet ./...                 passed
go test ./...                96 test functions, 17 packages, 0 failed
go test -race ./...          passed
node --check internal/web/ui/app.js   passed
```

สิ่งที่ test ปัจจุบัน **ยืนยันแล้ว** (เพิ่มจากรอบแรก):

- concurrent turns ต่อ session — `TestConcurrentTurnsCommitOnlyOneUserEvent`
- frozen Skill version ข้ามการ promote ระหว่าง session — `TestSessionUsesFrozenSkillVersionAfterLaterPromotion`
- qualification gate ต่อ profile และ reviewed override — `TestSessionRequiresExactQualificationOrReviewedOverride`
- interrupted effect กลายเป็น `uncertain` โดยไม่ retry — `TestInterruptedWriteEffectRecoversAsUncertainWithoutRetry`

สิ่งที่ยัง **ไม่ยืนยัน**:

- outbox path ทั้งเส้น: turn commit → staged trigger → drain → idempotent job (O-4)
- prompt-cache epoch stability วัดด้วย prompt fingerprint ตรง ๆ
- certified 128k/256k/1M recall/allocation ที่มีหลักฐานแยกต่อ tier (O-6)
- direct-tool budget ที่นับจาก exact provider serialization (O-3)
- crash/restart ระหว่าง model streaming และ DB/CAS split-brain
- native UI/browser/PTY flows
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

ดังนั้นคำตอบหลัง verification pass คือ **“โครงหลักถูกทิศ, P0 ของรอบแรกปิดครบแล้ว, เหลือ contradiction เชิงสถาปัตยกรรมหนึ่งจุดคือ Skill retrieval และงาน correctness ที่วัดได้อีกชุดหนึ่ง”**

ความเสี่ยงอันดับหนึ่งของโครงการ ณ ตอนนี้ไม่ใช่เรื่องสถาปัตยกรรม แต่คือ **ไม่มี version control** (O-1) ซึ่งทำให้ผลงานทั้งหมดหายได้ในเหตุการณ์เดียว

แผนแก้และ phase ถัดไปอยู่ใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md)

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

ปรับเป็นตำแหน่งจริงหลัง verification pass:

- `internal/agent/service.go:130` — `resolveQualification` และ expiring override
- `internal/agent/service.go:144-176` — `buildSessionContract`, `TaskBudget`, skill catalog freeze
- `internal/agent/service.go:324-360` — `acquireTurn` CAS lease
- `internal/agent/service.go:363-386` — `initializeSessionSkills` one-shot selection
- `internal/agent/service.go:387` — `selectSkillBindings` lexical scorer (O-2)
- `internal/agent/service.go:1066-1120` — orphaned turn recovery
- `internal/agent/service.go:1148-1162` — `compileTurn` อ่านเฉพาะ frozen contract
- `internal/agent/service.go:1243-1300` — `selectSkills` dead code (O-5)
- `internal/agent/service.go:1431`, `:1480` — `StageTrigger` ใน turn-commit transaction
- `internal/learning/service.go:55-86` — outbox stage/drain (O-4)
- `internal/qualification/service.go:341-378` — `contextTier` / `contextCapacity` และ recall flag เดียว (O-6)
- `internal/tools/registry.go:150-165` — `ProviderDefinitions` เทียบ `ContextSpecs` (O-3)
- `internal/curator/maintenance.go:160-215` — GC quarantine/partial restore transitions
- `internal/store/store.go:759-786` — migration ของ lease/contract/outbox
- `internal/web/ui/index.html:21`, `:62` — label `Office` ที่ยังไม่เปลี่ยน (O-7)
- `internal/context/` — profiles, compiler, compactor, spill, estimator และ verified fallback
- `internal/skills/` — candidates, versions, replay, provenance, activations, archive/restore และ curator evidence
- `internal/mcp/` — Streamable HTTP, schema/revision/risk binding และ redaction
