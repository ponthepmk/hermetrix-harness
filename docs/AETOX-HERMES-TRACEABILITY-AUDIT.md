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

สถานะที่แม่นยำคือ **kernel correctness ปิดครบพร้อมหลักฐานเกรด A ทุกข้อ** finding ทั้งหกที่เคยประกาศปิดตอนนี้มี test ที่ mutation-verified แล้วจริง เหลือ contradiction เชิงสถาปัตยกรรมข้อเดียว:

- แกน clean-room และ license boundary: **Correct**
- Skill lifecycle แบบปลอดภัยกว่า Hermes: **Adapted, stronger authority**
- token-efficient narrow tool waist: **Correct**; แต่ token accounting ยังต่ำกว่าจริง (O-3)
- context profile 32k/64k/128k/256k/1M: **Correct**; qualification ครบทุก tier แล้ว แต่หลักฐาน recall ยังเป็น flag เดียว (O-6)
- prompt-cache/session stability: **Correct**; SessionContract + TurnLease + CacheEpoch มีจริงและมี test
- Skill retrieval ตอน runtime: **Contradiction** — ต่างจากทั้ง Aetox และ Hermes ในทางที่เสียประโยชน์ (O-2)
- Aetox product UX/workbench: **Mostly missing** (โดยตั้งใจ ตาม ADR-8)
- Hermes background learning/curator/MCP breadth: **Partial**
- local-model harness ที่พร้อมใช้ต่อเนื่อง: **Not yet production-ready**

finding ทุกข้อที่ audit ตั้งไว้ปิดหมดแล้ว: O-1 ถึง O-7 และ V-1 ถึง V-6 งานถัดไปเป็นเรื่องของแผน ไม่ใช่หนี้จาก audit นี้ — gate audit ของ Phase 8–14 (P-3), effort band ที่ calibrate จาก velocity จริง (P-4) และ metric `no_skill_requested_rate` (R-14)

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
| Skill disclosure | `skills_list` → `skill_view` เป็น **tool** | `skills_list` / `skill_view` เป็น **tool** | `skill_search` → `skill_view` เป็น tool บน catalog ที่ freeze ต่อ session; pre-selection จาก goal แรกยังคงไว้เป็น floor | **Correct** — ตรงรูปแบบต้นแบบทั้งสองแล้ว และเข้มกว่าตรงที่ version นอก session contract ถูกปฏิเสธ ไม่ fallback ไป latest |
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

### 4.1 finding ที่ปิดแล้ว

เกรดหลักฐาน: **A** = assertion ตรวจ behavior ตรงและจะ fail ถ้าถอย behavior ออก · **B** = ตรวจบางส่วน · **C** = มี test แต่ไม่ครอบ mechanism หลัก · **D** = ไม่มี test

**finding ปิดได้เมื่อ implementation ครบและหลักฐานเป็นเกรด A** ทุกเกรด A ด้านล่างผ่าน mutation test แล้ว — ปิด guard ทิ้งแล้ว test ที่อ้างต้องเป็นสีแดง

| Finding | เกรดรอบแรก | เกรดตอนนี้ | หลักฐาน |
|---|---|---|---|
| **P0-1** turn lease | C | **A** | `TestConcurrentTurnsNeverDoubleCommitUnderRace` ปล่อย 4 goroutine เข้า `RunTurn` พร้อมกัน 100 รอบโดยไม่ block เทียม; test เดิมถูกเปลี่ยนชื่อเป็น `TestSecondTurnIsRejectedWhileFirstHoldsLease` ตามขอบเขตจริง<br>mutation: ปิด rows-affected guard ใน `acquireTurn` ทำให้ pass 0.6s กลายเป็น timeout 60s |
| **P0-2** SessionContract | A | **A** | `TestSessionUsesFrozenSkillVersionAfterLaterPromotion` promote version ใหม่กลาง session แล้ว assert byte ใน system prompt |
| **P0-3** learning outbox | D | **A** | 6 เคสบนเส้นทาง turn จริง: stage+drain, ไม่มี evidence ไม่ stage, drain ซ้ำไม่เพิ่ม job, failed turn stage เฉพาะที่มี evidence และ digest ยังรายงาน `outcome=failure`, stage ซ้ำ milestone เดิมถูก ignore, drain 4 ตัวพร้อมกัน claim ได้ตัวละครั้ง<br>mutation: ตัด producer, ตัด drain, ตัด `INSERT OR IGNORE`, ตัด claim guard — แดงทุกตัว |
| **P0-4** qualification gate | B | **A** | 6 เคส: override ไม่มี actor/reason/blank/ยาวเกิน ถูก reject, override ถูก freeze พร้อม expiry ใน 24 ชม., override หมดอายุ block turn ถัดไป, qualification 64k ไม่เปิด 128k/256k/1M, qualification จาก provider revision อื่นไม่ eligible, compact-32k เปิดเป็น compatibility<br>mutation: pin profile lookup เป็น `certified-64k` ทำให้ 128k เปิดได้บน qualification 64k — แดง |
| **P1-4** GC restore | B | **A (มีข้อยกเว้นบันทึกไว้)** | `TestGCRestoresMovedBlobsWhenQuarantineFailsMidway` บล็อกปลายทางของ candidate สุดท้ายด้วย directory ทำให้ apply ล้มหลังย้าย blob ไปแล้วบางส่วน แล้ว assert ว่าคืนครบและ run กลับเป็น `planned`<br>mutation: ลบ restore loop — แดง<br>**ข้อยกเว้น:** rows-affected guard บน UPDATE สุดท้าย (`maintenance.go:163`) ยังไม่มี test เพราะ guard ก่อนหน้าดักไปก่อน บันทึกไว้ใน comment ของ test ไม่เคลมว่าครอบ |
| **P1-6** TaskBudget | D | **A** | 6 เคส: model step, tool call, cumulative token, wall time (พร้อม assert ว่า lease ถูกปลด), loop detector หยุดครั้งที่สาม, loop detector ไม่รวม signature ที่ต่างกัน<br>mutation: ปิดทั้งห้า guard — แดงทุกตัว |
| **O-3** tool token accounting | — | **A** | `ContextSpecs` นับ payload เดียวกับที่ `ProviderDefinitions` ส่ง วัดได้ว่าเดิมนับต่ำไป **79%** (1,766 vs 3,156 bytes); ตัวเลขจริงคือ 840 token จาก budget 3,584 ของ compact-32k |
| **O-5** dead code | — | **ปิด** | ลบ `selectSkills` และเพิ่ม `.golangci.yml` (`unused`, `staticcheck`) + GitHub Actions CI ที่รัน gofmt/build/vet/test/race/lint/`node --check` — repo ไม่เคยมี CI มาก่อน |
| **O-6** recall ต่อ tier | — | **A** | probe ปลูก sentinel ที่ 0/25/50/75/100% แล้วบังคับให้คืนครบทุกจุด; runtime ที่คืนเฉพาะหัวได้ tier `limited` และ `eligible=false`<br>mutation: ผ่อนจาก “ครบทุกจุด” เป็น “อย่างน้อยหนึ่งจุด” — แดง |
| **O-1** version control | — | **ปิด** | repo อยู่ที่ `github.com/ponthepmk/hermetrix-harness` แล้ว |
| **V-6** test naming | — | **ปิด** | กฎอยู่ใน [DECISIONS.md](DECISIONS.md) ข้อ 5; เปลี่ยนชื่อ test สองตัวที่สัญญาเกินขอบเขต |

### 4.2 ยังเปิดอยู่

| ID | เรื่อง | severity | สถานะ |
|---|---|---|---|
| **R-14** | metric `no_skill_requested_rate` มีแล้ว แต่ยังไม่เคยรันกับ local model จริง | low | วัดได้แล้วผ่าน `GET /api/skill-retrieval`; เหลือแค่เก็บข้อมูลจากการใช้งานจริง ไม่ใช่งาน implementation |
| **O-7** | documentation drift | medium | มี `scripts/doc-truth.sh` สองชั้นแล้ว (facts + claim registry) claim registry จับ anchor ที่หายได้จริงตั้งแต่รันครั้งแรก; ข้อความเชิงความหมายยังต้องให้คนไล่ |
| **P-3** | exit gate ของ Phase 8–14 หลายข้อยังวัดไม่ได้ | medium | ยังไม่ทำ gate audit |
| **P-4** | effort band ไม่มีฐานจาก velocity จริง | medium | มี band แล้วแต่เป็นการเดา; calibrate ได้หลังมี git history พอ |

### 4.2b Findings จากการขับใช้งานจริง (2026-08-23)

รอบนี้เอา Hermetrix ไปรันกับ gateway จริง (`qwen3.8-27b-fp8` บน vLLM) แล้วขับงานจริงสองสาย — review โค้ดภาษี และคำนวณภาษีหัก ณ ที่จ่าย ทุก finding ด้านล่างมาจาก runtime จริง ไม่ใช่การอ่านโค้ด

| ID | เรื่อง | severity | สถานะ |
|---|---|---|---|
| **O-8** | probe output budget เล็กเกินไปสำหรับ reasoning model | high | **แก้แล้ว** |
| **O-9** | runtime evidence ไม่มีทางกลายเป็น Skill candidate ได้เลย | **critical** | **แก้แล้ว** |
| **O-10** | system prompt ไม่เคยบอก model ว่ามี Skill catalog อยู่ | high | **แก้แล้ว** |
| **O-11** | output reserve ไม่รู้จัก reasoning token | high | **แก้แล้ว** |
| **O-16** | คำตอบว่างเปล่าถูกบันทึกว่า turn สำเร็จ | **critical** | **แก้แล้ว** |
| **O-17** | duplicate analyzer ไม่เคย retrieve อะไรเลย | high | **แก้แล้ว** |
| **O-12** | `/api/` path ที่ไม่ match route คืน HTML 200 | high | **แก้แล้ว** |
| **O-13** | tool-call arguments ที่พังถูก replay กลับไปหา provider ทำให้ทั้ง turn ตาย | high | **แก้แล้ว** |
| **O-14** | ค่าที่อยู่กลางไฟล์ใหญ่เข้าถึงไม่ได้ — ไม่มี search ไม่มี range read | high | **แก้แล้ว** |
| **O-15** | reviewer จ่าย model call ทุก turn เพื่อได้ `no_change` | medium | **แก้แล้ว** |

#### รอบขับใช้งานที่สอง (2026-08-24)

รอบนี้ไล่ surface ที่ไม่เคยถูกขับเลย — compaction, GC, backup/import, fidelity lab, skill replay, capability review

| ID | เรื่อง | severity | สถานะ |
|---|---|---|---|
| **O-18** | compile report บอกไม่ได้ว่า 68% ของ token หายไปไหน | high | **แก้แล้ว** |
| **O-19** | `/api/health` ตอบ schema ที่ hardcode ไว้ | medium | **แก้แล้ว** |
| **O-20** | query ภาษาไทยหา Skill ภาษาไทยไม่เจอ และ metric มองไม่เห็นทั้งภาษา | **critical** | **แก้แล้ว** |
| **O-21** | export กับ import ไม่ใช่ฟังก์ชันผกผัน และไม่มีอะไรบอก | high | **แก้แล้ว (ส่วนรายงาน)** |
| **O-22** | *(ถอน)* — ผมเรียก fidelity ผิด API เอง ไม่ใช่ข้อบกพร่อง | — | ถอน |
| **O-23** | fidelity corpus ที่ seed มากับระบบล้มไม่ได้ | high | **แก้แล้ว** |
| **O-24** | replay สีเขียวที่ทดสอบแค่ manifest ถูกรายงานเหมือนทดสอบ procedure | high | **แก้แล้ว (ส่วนรายงาน)** |
| **O-25** | หน้า error HTML ของ gateway กลายเป็นบทสนทนาและถูกอัดเข้า checkpoint | high | **แก้แล้ว** |
| **O-26** | token error band ประเมินไม่ได้ — คู่ค่าถูกทิ้ง calibration หายตอน restart ใช้ร่วมข้าม model | **critical** | **แก้แล้ว** |
| **O-27** | band วัดเทียบ *งบ* ไม่ใช่ *prompt* ทำให้ค่าผิดแปรตามขนาด context | high | **แก้แล้ว** |
| **O-28** | calibration ลู่เข้า **รากที่สอง** ของค่าจริง ทิ้ง error ถาวร −10.6% | high | **แก้แล้ว** |
| **O-29** | estimator คิด 1 token ต่ออักษรที่ไม่ใช่ ASCII — ผิดราวสองเท่า | **critical** | **แก้แล้ว** |

surface ที่ขับแล้วสะอาด ไม่มี finding: **GC** (dry-run → quarantine → restore, staleness guard, actor guard) · **capability review** (deny บล็อก approve ปล่อย บันทึก actor/revision/tool ครบ) · **settings/memories** (lifecycle ครบ, `source` ต้องเป็น `user` เท่านั้นตามเจตนา)

#### O-18 — compile report บอกไม่ได้ว่า token หายไปไหน *(แก้แล้ว)*

session จริงรายงาน:

```
original_tokens  : 34038
selected_tokens  : 10794
dropped_tokens   :     0
compacted_tokens :     0
unaccounted      : 23244   ← 68% ของ input
```

spill เอาไปหมด — 12 receipt × 8,075 bytes = 96,900 bytes ตรงกับช่องว่างพอดี แต่ report เก็บ spill เป็นรายการ receipt หน่วย **byte** ไม่มี term หน่วย token เลย สองในสามของ input จึงออกจากบัญชีเงียบๆ และ `compression_ratio` ยกความดีความชอบให้ selection

ราคาไม่ใช่แค่ความสวยงาม: **"compaction ไม่เคยทำงาน" ดูเหมือนบั๊กของ compactor** อยู่หลายรอบ เพราะไม่มีอะไรใน report แสดงได้ว่า spill ดูดประวัติไปก่อนที่ active slice จะเต็ม

`Report` เพิ่ม `deduplicated_tokens` / `spilled_tokens` / `unaccounted_tokens` และ `Compile` **fail แทนที่จะคืน report ที่ไม่สมดุล**

และยอดที่สมดุลไม่ได้แปลว่าซื่อสัตย์ — ถ้าการนับ drift กลางคัน ผลรวมยังปิดได้เพราะส่วนต่างไปตกที่ term ที่ไม่มีพยานอิสระ dedup กับ spill จึงต้องมีพยาน (จำนวน fragment และรายการ receipt) และ term ที่อ้างงานโดยไม่ทิ้งร่องรอยที่อื่นถูกปฏิเสธ

mutation 6 ตัว แดงทั้งหมด

#### O-19 — health ตอบ schema ที่เขียนตายไว้ *(แก้แล้ว)*

`GET /api/health` ตอบ `{"schema": 16}` จาก literal ใน handler ขณะที่ฐานข้อมูล migrate ไป 17 แล้ว health คือ endpoint เดียวที่ client ใช้ตัดสินว่า server ตรงกับที่คาดไหม ตัวเลขที่พิมพ์มือจึงแย่กว่าไม่มีตัวเลข — มัน**ผิดอย่างมั่นใจตลอดการ migrate ที่มันมีไว้ตรวจ**

ตอนนี้อ่าน `PRAGMA user_version` จริง พร้อม `expected_schema` จาก build

#### O-20 — retrieval ภาษาไทย *(critical, แก้แล้ว)*

โค้ดเบสมีฟังก์ชันชื่อ `termSet` **สองตัว** ตัวใน `internal/skills/analyzer.go` ทำ character trigram สำหรับสคริปต์ที่ไม่เว้นวรรค ตัวใน `internal/agent/service.go` แยกด้วย whitespace และไม่ทำ ภาษาไทยไม่เว้นวรรคระหว่างคำ ประโยคไทยทั้งประโยคจึงกลายเป็น term เดียวและ match ได้เฉพาะประโยคที่เหมือนกันทุกไบต์

วัดกับ Skill ที่ summary ตรงกับ query พอดี:

```
"ปัดเศษสตางค์"                        0 hits
"การปัดเศษเงิน"                       0 hits
"ปัดเศษเงินไทยเป็นสตางค์แบบครึ่งขึ้น"  1 hit   (คือ summary ทั้งสตริง)
```

**ตัวที่ตาบอดคือตัวที่ผู้ใช้เจอ** `selectSkillBindings` ป้อนทั้งสามอย่าง: preselection เข้า session contract · `skill_search` · และ **ตัวหารของ skill-retrieval metric** เทิร์นภาษาไทยจึงถูกนับว่า "ไม่มี Skill ที่เกี่ยวข้อง" `no_skill_requested_rate` เลยรายงานว่าไม่มีโอกาสพลาดเลยสำหรับภาษาที่ผลิตภัณฑ์นี้เขียนขึ้นมาเพื่อ — **metric ที่มองไม่เห็นหัวข้อของตัวเอง**

ทั้งสองเส้นทางใช้ `internal/textmatch` ร่วมกันแล้ว word กับ trigram ให้คะแนนแยกกันเพราะให้ข้อมูลไม่เท่ากัน: ASCII word เป็นหน่วยความหมาย match เดียวคือหลักฐาน · trigram เป็นเศษ มีความหมายเฉพาะเป็นสัดส่วน

สัดส่วนคิดเทียบฝั่งที่สั้นกว่า จาก goal ไทยยาวเรื่องปัดเศษสตางค์ Skill สั้นที่ตรงเป๊ะแชร์ 7 trigram ส่วน summary ยาวที่ไม่เกี่ยวแชร์ 17 เพราะยาว — **นับดิบจะจัดตัวที่ผิดขึ้นก่อน**

**ยังไม่รองรับ และไม่กลบ:** query ภาษาไทยกับ catalog ที่เขียนเป็นอังกฤษ lexical matching ข้ามภาษาไม่ได้

#### O-21 — export กับ import ไม่ใช่ฟังก์ชันผกผัน *(แก้ส่วนรายงาน)*

export serialise 42 ตาราง — ทุก session, event, provider profile, tool approval, memory, snapshot · import อ่านสองตาราง (`skills`, `skill_versions`) แล้วแปลงเป็น candidate

restore ของจริงลง instance เปล่า: ไฟล์ที่มี 210 event, 4 session, 21 blob ให้ผลเป็น **Skill candidate 3 ตัวและไม่มีอะไรอื่น** พร้อมรายงาน `state: imported`, `conflicts: 0` ไม่มีอะไรบอกว่าบทสนทนาอยู่ในไฟล์และถูกทิ้ง

**export ควรใส่น้อยลง หรือ import ควรคืนมากขึ้น เป็นคำถามเชิงออกแบบ ยังเปิดอยู่** ส่วนการรายงานไม่ใช่ — ผลลัพธ์ระบุทุกตารางที่ไม่ว่างซึ่ง import ไม่ได้อ่าน

candidate-only promotion เป็นเจตนาและไม่เปลี่ยน — ความรู้ที่ import เข้ามาต้องไม่ active โดยไม่ผ่าน review

#### O-23 — fidelity corpus ล้มไม่ได้ *(แก้แล้ว)*

สองเคสที่ seed มาเป็น fragment ละสามชิ้น รวมราว 30 token ใส่ลงทุก profile ได้ทั้งก้อน จึงไม่มีอะไรถูก drop/spill/compact เลย **ทุก metric = 1 โดยโครงสร้าง** `compression_ratio` = 1 พอดี `tokens_saved` = 0

corpus ที่ถามว่า compaction รักษาสิ่งสำคัญไหม โดยไม่เคยทำให้เกิด compaction คือการตอบคำถามที่ไม่เคยถูกถาม — รูปแบบเดียวกับ threshold ของ duplicate analyzer ที่ตั้งเหนือทุกอย่างที่ข้อมูลจริงไปถึง

เคสใหม่ `context-pressure` ฝัง fragment สี่ชิ้นที่ต้องรอดไว้ในเนื้อหาที่เกิน active budget ของทุก profile ถึง extended-128k รันกับ compact-32k: **255,824 token เข้า 13,905 ออก** โดย pinned goal คงคำต่อคำ decision ยังอยู่ causal pair ไม่ถูกแยก — **compiler ไม่เคยเป็นส่วนที่อ่อน การวัดต่างหาก**

ปริมาณอย่างเดียวไม่ใช่แรงกดดัน: filler ที่เหมือนกันจะถูก dedup ทิ้งก่อนถึงขั้น selection ทำให้ `original_tokens` ใหญ่แต่ compiler ไม่ถูกทดสอบ ทุก filler จึงพก index ของตัวเอง และมี test ยืนยันคุณสมบัตินี้ตรงๆ

#### O-24 — replay สีเขียวที่ไม่ได้ทดสอบ procedure *(แก้ส่วนรายงาน)*

candidate `improve` ที่กลับด้านทุกขั้นตอน — จาก "เก็บเป็นสตางค์ ปัดครึ่งขึ้น" เป็น "ใช้ทศนิยมบาท ปัดลงและทิ้งเศษ" — ถูก promote ขึ้น active พร้อม `replay_passed: true` และไม่มีคำเตือน

กลไกไม่ได้พัง gate ไม่ได้ถูกข้าม: `CreateCandidate` และ `UpdateCandidate` **รัน replay ให้เองสำหรับ `improve`** freshness check ใน `requireCurrentReplay` จึงเจอ run ที่ตรงเสมอ และเมื่อ Skill ไม่มี fixture runner จะ**สังเคราะห์**หนึ่งตัวที่ตรวจว่า manifest name/description/tool list ไม่เปลี่ยน

การตรวจนั้นคุ้มที่จะรัน แต่ไม่ใช่การตรวจเชิงพฤติกรรม และผลถูกรายงานเป็น `fixtures_total: 1`, `summary.passed: true`, `replay_passed: true` — แยกไม่ออกจาก test suite ที่ไม่พบ regression

**implicit-only replay ควรบล็อก promotion ไหม เป็นคำถามเชิงนโยบาย ยังเปิดอยู่** ส่วนความซื่อสัตย์ไม่ใช่: run รายงาน `author_fixtures` และ `implicit_only` และ candidate พก finding ระดับ warning บอกว่า run ยืนยันแค่ manifest ไม่ได้ทดสอบ procedure — อยู่ตรงที่คนกดอนุมัติมองเห็น

#### compaction ทำงานจริงแล้วเป็นครั้งแรก

ตลอดการขับใช้งานทั้งสองรอบ compaction **ไม่เคยทำงานเลย** — เพราะ spill ดูดประวัติไปก่อนที่ active slice จะเต็ม และ O-18 ทำให้มองไม่เห็นว่าเกิดอะไรขึ้น

ขับ 31 เทิร์นบน `compact-32k` ด้วยเนื้อหาสนทนาไทยที่ไม่ซ้ำกัน (spill แตะเฉพาะ tool result ไม่แตะบทสนทนา) จน active เกิน 13,824 token:

| turn | original | selected | dropped | compacted | dedup | unaccounted |
|---:|---:|---:|---:|---:|---:|---:|
| 25 | 17,601 | 14,566 | 0 | **0** | 3,035 | 0 |
| 26 | 18,889 | 14,410 | 2,124 | **678** | 3,033 | 0 |
| 27 | 20,427 | 15,010 | 3,648 | **1,285** | 3,054 | 0 |
| 31 | 26,140 | 14,925 | 9,311 | **1,207** | 3,111 | 0 |

**original โตขึ้น 48% แต่ selected นิ่งอยู่ที่ ~14,900** — คือพฤติกรรมที่ compiler ควรมี

สถานะสุดท้ายภายใต้แรงกดดัน 11 เทิร์นติด:

```
pinned_retained      : 1 / 1
essential_retention  : 1
causal_pairs         : 21 selected / 21 total, 0 omitted, 0 split
active slice         : 15,838 ใช้จาก 15,872 (99.8%) ไม่ล้น
ledger               : 27,488 = 3,105 dedup + 0 spill + 13,778 selected + 10,605 dropped
unaccounted          : 0
```

checkpoint ที่ได้เป็น extractive จริง มี source ID กำกับทุกบรรทัด provenance `hermetrix:structured-compactor-v1:verified`

และมันเปิดเผย **O-25** ทันที: หาง checkpoint มี HTML ของ Cloudflare error page อยู่

#### O-25 — หน้า error ของ gateway กลายเป็นบทสนทนา *(แก้แล้ว)*

gateway timeout คืนหน้า error 2 KiB adapter ใส่ body ทั้งก้อนลงใน error → agent เก็บเป็น event `turn_failed` → replay กลับไปหา model ในฐานะประวัติของ assistant → **ถูกอัดเข้า checkpoint ที่ถูกบีบแล้ว** พร้อม stylesheet fragment และ conditional comment

ผิดสามชั้น: กินงบ summary ที่ควรเก็บสิ่งที่ session ตัดสินใจ · อ่านย้อนกลับเหมือนเป็นสิ่งที่ assistant พูด · และเป็น HTML ของ**ตัวกลาง** ไม่ใช่ของ provider ข้อความใดๆ ในนั้นจึงถูกนำเสนอเป็น output ของ assistant

การ redact credential ทำถูกอยู่แล้วและไม่เปลี่ยน ตอนนี้ body ถูกจำกัดความยาว ยุบ whitespace และถอดเหลือข้อความอ่านได้เมื่อเป็นหน้าเว็บ — ตัดเนื้อใน `<script>`/`<style>` ก่อน เพราะข้อความนั้นอยู่*ระหว่าง*แท็กไม่ใช่*ใน*แท็ก และ stylesheet ไม่ใช่การวินิจฉัย · error แบบ structured สั้นๆ ของ provider ซึ่งเป็นกรณีที่มีประโยชน์ ผ่านไปครบถ้วน

#### O-26 — gate ของ Phase 9 ประเมินไม่ได้เลย *(critical, แก้แล้ว)*

exit gate ถามว่า predicted input อยู่ใน ±10% ของ usage ที่ provider รายงานที่ p95 ไหม **คำถามนี้ตอบไม่ได้เลย**

`Observe(predicted, actual)` ยุบคู่ค่าเข้าค่าเฉลี่ยในหน่วยความจำแล้วทิ้งคู่ค่า ส่วน usage ที่เขียนลงที่อื่นคือ **ผลรวมทั้งเทิร์น** เก็บคู่กับ snapshot ของ step **สุดท้าย** — คนละปริมาณ เทิร์นหนึ่งส่ง context ซ้ำหนึ่งครั้งต่อ step ผลรวมจึงเป็นผลบวกของหลายการส่ง

ผมจับคู่แบบนี้ตอนแรกแล้วได้ error +226% เกือบสรุปว่าเป็นข้อบกพร่องร้ายแรงของ compiler **ตรวจก่อนเขียนจึงพบว่าเป็นความผิดของการวัดเอง** จาก 90 snapshot มีแค่ **2 ตัว** ที่เทียบได้ซื่อสัตย์ และพลาดแบนด์คนละทิศทั้งคู่ (−26%, +28%)

สามข้อบกพร่องในเรื่องเดียว:

1. **คู่ค่าไม่ถูกเก็บ** — หลักฐานของ gate ถูกทิ้งทันทีที่ผลิต
2. **calibration หายทุก restart** — 18 เทิร์นสอนจนได้ 0.766 บูตใหม่กลับเป็น 1.0 คือกระโดด 30% ทุก prediction
3. **ตัวเลขเดียวใช้ร่วมทุก provider/model** — และ `Observe` เขียนทับ **ระหว่าง step ในเทิร์นเดียว** วัดได้ว่า step 2 ทำนาย 3,632 token ให้ context ที่มากกว่า step 1 ซึ่งได้ 3,677 — **งบที่เปลี่ยนระหว่างกำลังสร้าง context ไม่ใช่งบ**

แก้: `token_observations` เก็บหนึ่งแถวต่อ model step · `GET /api/token-accuracy` รายงาน median/p95/mean signed/within-band/overflow **แยกตาม model** · verdict งดต่ำกว่า 30 sample · overflow เดียวตก gate ไม่ว่าแบนด์แคบแค่ไหน · calibration ย้ายไปบน provider profile ข้าง `reasoning_ratio` พร้อม clamp [0.50, 3.00] · เทิร์นอ่านค่าครั้งเดียวตอนเริ่ม ทุก step วัดด้วยไม้บรรทัดเดียวกัน

#### O-27 — band วัดเทียบงบ ไม่ใช่ prompt *(แก้แล้ว)*

`predicted_input` รวม `worst_case_tool_burst` 2,048 token ที่กันไว้ให้ tool result **ที่ยังไม่เกิด** เทียบกับ prompt usage คือเอาเงินสำรองไปเทียบใบเสร็จ

18 request ต่อเนื่อง: error ไต่จาก **−51.7% ไป −27.9% แบบ monotonic** เพราะเงินสำรองคงที่ถูกเจือจางเมื่อ prompt โต อ่านเป็น gate จะดูเหมือน estimator ที่ดีขึ้นตามการใช้งาน

| | mean | stdev | range |
|---|---:|---:|---:|
| รวมเงินสำรอง | −36.5% | 7.3% | 23.7 pp |
| ไม่รวม | **−21.5%** | **2.0%** | **7.4 pp** |

รูปร่างสำคัญกว่าตัวเลข: **offset เกือบคงที่ spread 2 จุด คือ bias ที่ calibration ลบออกได้** ไม่ใช่ noise ที่ลบไม่ได้ และถ้าไม่แยก multiplier จะดูดเงินสำรองเข้าไปเป็นส่วนหนึ่งของไม้บรรทัด ทำให้เนื้อหาจริงถูก under-predict ราว 13% **แบบเงียบ**

#### O-28 — calibration ลู่เข้ารากที่สองของค่าจริง *(แก้แล้ว)*

multiplier เรียนจาก `actual/predicted` แต่ `predicted` **ถูก scale ด้วย multiplier ตัวนั้นไปแล้ว** การเฉลี่ย ratio จึงเป็น feedback loop และจุดตรึงของมันไม่ใช่ ratio แต่เป็น **รากที่สองของ ratio**

model ที่ค่าจริง 0.80 → multiplier นิ่งที่ **0.894** และเหลือ error ถาวร **−10.6%** ซึ่งอยู่*ในขอบ* ±10% พอดี — ตำแหน่งที่แย่ที่สุด: ใกล้พอจะดูเหมือน noise และไม่มีวันปิด

ข้อมูลสดคือสิ่งที่เปิดโปงมัน 28 observation ต่อเนื่องใต้ calibration ที่ทำงานอยู่ ค้างที่ −20% ถึง −27% ขณะที่ multiplier เดินจาก 1.0 ไป 0.765 **โดยไม่เกิดผลอะไรเลย** จำลองสองกฎ 200 step แล้วเห็นรูปร่างชัด:

```
mean of actual/predicted            -> 0.8944   error −10.6%
mean of applied*actual/predicted    -> 0.8000   error   0.0%
sqrt(0.80) = 0.8944
```

`ObserveTokenScale` รับ multiplier ที่ผลิต prediction นั้นแล้วหารกลับออกก่อนเฉลี่ย observation ที่ไม่รู้ applied scale ไม่มีข้อมูลเรื่อง ratio จึงไม่ถูกนับ

#### O-29 — 1 token ต่ออักษรไทย *(critical, แก้แล้ว)*

`heuristicTokens` คิด **1 token ต่ออักษรที่ไม่ใช่ ASCII** ทุกตัว **ไม่มี tokenizer ไหนทำแบบนั้น** วัดกับ gateway จริงบนข้อความไทย ค่าจริงคือ **0.51** — สมมติฐานผิดราวสองเท่า และบน context ที่เป็นไทย 90% มันดัน estimate สูงเกิน 30%

และ multiplier ตัวเดียวแก้ไม่ได้ เพราะการแก้ที่ถูกต้อง**ขึ้นกับสัดส่วนอักษรที่ไม่ใช่ ASCII ซึ่งเปลี่ยนทุกเทิร์น** เมื่อ session เต็มไปด้วยบทสนทนาไทย ratio จริงก็เลื่อน scalar จึงไล่ตามเป้าที่เคลื่อนที่และไม่ลงที่ไหนสักที่ — ตรงกับที่ calibration สดทำ คือเดินเข้าหาพื้น 0.50 ขณะที่ error ไม่ขยับ

refit 23 request สดโดยให้ script rate เป็นพารามิเตอร์อิสระตัวเดียว:

| rate | p95 \|error\| | อยู่ใน ±10% |
|---|---:|---:|
| 1.0 (สมมติเดิม) | 41.5% | **1 / 23** |
| 0.51 (เรียนรู้) | **2.8%** | **23 / 23** |

request ทั้ง 23 ถูกเก็บไว้ใน test แบบคำต่อคำ เพราะมันคือหลักฐานของการเปลี่ยนแปลงนี้

**rate เรียนรู้ต่อ provider ไม่ใช่เขียนตายไว้** — 0.51 มาจาก model เดียวภาษาเดียว และค่าคงที่ที่ fit จากข้อสังเกตครั้งเดียวคือวิธีที่ analyzer กลายเป็นสิ่งที่ retrieve อะไรไม่ได้ provider ที่ยังไม่ calibrate ใช้ค่าเดิม ซึ่งเป็นค่าที่ conservative ที่สุด: **กันเกินดีกว่าล้น**

#### บทเรียนจากการวัด: สามครั้งที่ผมวัดผิดติดกัน

กว่าจะ fit ถูก ผมวัดผิดสามรอบ และ**ทุกรอบข้อมูลดูเป็นระเบียบ**

1. จับคู่ usage รวมทั้งเทิร์นกับ snapshot ของ step เดียว → error +226% ดูเหมือนข้อบกพร่องร้ายแรงของ compiler
2. หารด้วยงบที่รวมเงินสำรอง 2,048 → error หดลงเรียบๆ ตามขนาด context ดูเหมือน estimator ที่ดีขึ้นตามการใช้งาน
3. ลืม tool schema ในโมเดล → offset คงที่ 1,200 token อ่านเป็น error 135% ที่ context เล็ก ลดเหลือ 2% ที่ context ใหญ่

ทุกรูปร่างดูเหมือน finding และทุกอันคือ**รูปร่างของตัวแปรที่ผมไม่ได้ใส่** — สัญญาณที่เรียบเกินไปคือสัญญาณของพจน์ที่หายไป ไม่ใช่ของกฎที่ค้นพบ

#### ยังไม่ wired: memory ไม่เคยเข้า context

`memories` เขียนได้ อ่านได้ archive ได้ แต่ผู้อ่านเพียงรายเดียวคือ list endpoint — ไม่มีเส้นทางใดพามันเข้า session context

**แผนระบุไว้ถูกแล้ว** (บรรทัด 270: memory revision รอ Phase 11) จึงไม่ใช่ finding ที่ซ่อนอยู่ แต่ระดับผลิตภัณฑ์ยังมีช่องว่าง: API รับและเก็บ memory วันนี้ และผู้ใช้ย่อมเข้าใจว่ามันมีผล ไม่มีอะไรบอกว่ายังไม่ถูกใช้

#### O-8 — probe budget กับ reasoning model *(แก้แล้ว)*

`long_context_recall` ล้มบน gateway จริงโดยคืน sentinel ได้ 2 จาก 5 ตำแหน่ง ดูเผิน ๆ เหมือน model recall ไม่ไหว

ความจริง: เรียกตรงด้วย prompt เดียวกัน `max_tokens=256` คืนครบ 5/5 ต่างกันที่ **streaming**

```
non-stream: reasoning 377 chars → finish=stop   → 5/5
stream:     reasoning 656 chars → finish=length → 2/5 ตัดกลาง token
```

reasoning ถูกนับเป็น completion token แต่ทุก probe จอง `MaxTokens` 128–256 ไว้เผื่อแค่คำตอบ suite จึงรายงาน capability failure ที่จริงเป็น output-budget failure — และรายงาน external gateway วันที่ 22 ส.ค. ที่บันทึกว่า “sentinel run did not pass” น่าจะเป็นสาเหตุเดียวกันโดยไม่เคยถูกวินิจฉัย

แก้เป็น `qualificationOutputBudget = 1024` ตัวเดียวใช้ทุก probe พร้อม test ที่ห้าม hardcode `MaxTokens` ตัวเลข หลังแก้ recall ผ่าน 5/5 กับ model จริง

#### O-9 — learning loop ต่อท่อครบ แต่ไม่มีสมอง *(critical)*

ขับงานจริงหนึ่ง turn แล้วตามรอยทั้งเส้น:

```
turn สำเร็จ (tool 2 ตัว)
  → outbox: successful_milestone = processed   ✓
  → review job: queued                          ✓
  → reviewer รัน                                ✓
  → decision: no_change
     "digest contains no bounded, reusable procedure"
  → candidates: 0
```

ท่อทุกท่อนทำงานถูก แต่ `StructuredReviewer` คืน candidate เฉพาะเมื่อ `digest.SuggestedSkill` ถูกเซ็ตมาแล้ว และ **ไม่มีที่ไหนใน runtime เซ็ตมันเลย** — `learningTriggerForTurn` ไม่เคยแตะ field นี้ มีแต่ HTTP enqueue path (`internal/learning/service.go:200`) ที่รับมาจาก caller ภายนอก

ดังนั้นเส้นทาง **runtime evidence → Skill candidate เป็นไปไม่ได้เชิงโครงสร้าง** ไม่ใช่ “reviewer ยังอ่อน” แต่เป็น “ไม่มีเส้นทาง” เอกสารเดิมเขียนว่า reviewer เป็น deterministic acknowledgement ซึ่งจริงแต่บอกไม่ครบ

ผลต่อแผน: Phase 8 ไม่ใช่การ *ปรับปรุง* learning loop แต่คือการ **สร้างส่วนที่ขาดไปตั้งแต่แรก** — และ spike วัดคุณค่า Skill ทำไม่ได้จนกว่าจะมีตัวผลิต Skill

#### O-10 — prompt ไม่เคยบอกว่ามี Skill *(high)*

system prompt ที่ compile จริงมีสอง fragment:

- identity: “You are Hermetrix, a friendly and precise intelligent tool…”
- policy: “Skills and durable knowledge are proposal-only…”

**ไม่มีประโยคไหนบอกว่า session นี้มี Skill catalog หรือควรเรียก `skill_search` เมื่องานตรงกับ procedure** ประโยคที่พูดถึง Skill พูดเรื่อง *อำนาจ* และคำว่า “proposal-only” อ่านแล้วชวนให้คิดว่า Skill ยังไม่ใช่ความรู้ที่ใช้ได้

หลักฐานจากการขับจริง: session ที่มี Skill `thai-withholding-tax` อยู่ใน catalog แล้วผู้ใช้เปลี่ยนหัวข้อมาถามภาษีหัก ณ ที่จ่ายโดยตรง — model **ไม่เรียก tool ใดเลย** และตอบด้วยทศนิยมบนหน่วยสตางค์ แล้วย้อนถามผู้ใช้ว่าจะปัดเศษแบบไหน ซึ่งเป็นข้อที่ Skill ระบุคำตอบไว้แล้ว

R-14 ข้อมูลจริงชุดแรก: `relevant=1 requested=0 rate=1.0` (`insufficient_evidence` เพราะ sample=1)

นี่ตรงกับ failure mode ข้อ 2 ที่ ADR-7 เขียนทำนายไว้เอง มาตรการที่ ADR ระบุคือขยาย floor แต่หลักฐานชี้ว่าต้องแก้ prompt ก่อน เพราะ floor ปัจจุบันก็ไม่ทำงาน (`preselected: []` ทั้งสอง turn)

#### O-9 — ผลหลังแก้ *(ขับจริงกับ gateway)*

ต้นตอลึกกว่าที่รายงานรอบแรก มีสามชั้น ไม่ใช่ชั้นเดียว:

1. `Decision` ไม่มี field ให้ reviewer ใส่ Skill ที่เสนอ
2. `RunNext` อ่าน `job.Digest.SuggestedSkill` — **ของ digest ไม่ใช่ของ reviewer** ต่อให้ reviewer ตัดสินใจ `create` ก็ไม่มีที่ให้วาง
3. `StructuredReviewer` อ่าน evidence ไม่เป็นอยู่แล้ว

แก้ทั้งสามชั้น: `Decision.SuggestedSkill`, runner ใช้ข้อเสนอของ reviewer ก่อนแล้ว fallback ไป digest (เส้นทาง API), และเพิ่ม `ModelReviewer` ที่อ่าน digest แล้วตัดสินใจเอง โดย parser **fail closed** — อะไรที่อ่านไม่ครบกลายเป็น `no_change` ทั้งหมด

ขับจริง: ทำงานหนึ่ง turn แล้วให้ผู้ใช้แก้สองครั้ง

```
review 1  successful_milestone (แค่อ่านไฟล์)
          → no_change "only shows reading files and a one-off VAT amount;
                       no reusable, non-specific steps"
review 2  repeated_correction (ผู้ใช้แก้ซ้ำ)
          → create → cand_b8450c8e... "vat-rounding-consistency-check"
             lint ✓ security ✓ state=needs_review
             active skills = 0
```

Skill ที่ model เขียน:

```markdown
1. Calculate net, VAT, and gross amounts.
2. Apply half-up rounding to the monetary values.
3. Check that net + VAT = gross after rounding.
4. If the check fails, redo the calculation and recheck.
```

จับสิ่งที่ผู้ใช้แก้ได้ตรง generalize โดยไม่มีค่าเฉพาะของรอบนั้น และ **แยกแยะถูกระหว่างงานที่มี procedure กับงานที่ไม่มี** ซึ่งเป็นเงื่อนไขที่ทำให้ learning loop มีค่ามากกว่ามีเสียงรบกวน

นี่คือครั้งแรกที่ runtime evidence กลายเป็น Skill candidate ได้ authority ladder ยังยืนครบ: origin `agent_candidate`, created_by `background_reviewer`, ต้องมีมนุษย์ promote

ผลต่อแผน: spike วัดคุณค่า Skill ที่ค้างอยู่ **ทำได้แล้ว** เพราะมีตัวผลิต Skill จาก evidence จริง

#### O-10 — ผลหลังแก้ *(before/after กับ model จริง)*

เพิ่ม fragment ที่ derive จาก frozen catalog อย่างเดียว (จึง byte-stable ตลอด session) บอกชื่อ Skill ที่มีและสั่งให้เรียก `skill_search` ก่อนตอบ พร้อมแก้ประโยค policy เดิมที่อ่านแล้วเหมือน Skill ใช้ไม่ได้

รัน prompt เดิมเป๊ะ model เดิม:

| | ก่อน | หลัง |
|---|---|---|
| tool calls | `[]` | `skill_search` → `skill_view` |
| คำตอบ | ทศนิยมบนหน่วยสตางค์ แล้วย้อนถามผู้ใช้ว่าจะปัดแบบไหน | จำนวนเต็ม ปัดครึ่งขึ้น WHT บน net ก่อน VAT และตรวจยอดกระทบตามข้อ 5 ของ Skill |
| `no_skill_requested_rate` | 1.00 | 0.00 |

model อ้าง Skill ตามชื่อและทำครบทั้งห้าข้อ นี่คือหลักฐานตรงว่า ADR-7 ทำงานได้ทั้งเส้น และ O-10 คือสิ่งที่ขวางอยู่ — ไม่ใช่ขนาดของ model

เป็นข้อมูลชุดแรกของคำถาม Phase 8 ด้วย: Skill ทำให้คำตอบดีขึ้นแบบชี้ได้ (n=1)

#### O-12 — API path ที่ไม่ match คืน HTML 200 *(แก้แล้ว)*

พบตอนเรียก `GET /api/activations` ซึ่งลงทะเบียนไว้เฉพาะ `POST` ผลที่ได้คือ SPA HTML พร้อม status 200

ตรวจต่อพบว่าเป็นทั้งระบบ:

```
GET  /api/does-not-exist   200 <!doctype html>
GET  /api/activations      200 <!doctype html>
POST /api/usage            200 <!doctype html>   ← route จริง แต่ method ผิด
GET  /api/skills/typo      404 {"error":"not found"}
```

catch-all `mux.Handle("/", spa(...))` กว้างกว่า pattern ที่ลงทะเบียนไว้ จึงกลืนทั้ง path ที่พิมพ์ผิดและ method ที่ผิด client แยกไม่ออกจาก success — เป็น false success ระดับ transport ซึ่งขัดกับหลักการของโครงการเองที่ว่าห้ามอ้างผลโดยไม่มี receipt

แก้ด้วย handler `/api/` ที่คืน JSON 404 พร้อมระบุ method+path

#### O-16 — session เงียบไปเฉย ๆ 7 turn ติด *(critical, แก้แล้ว)*

เจอตอนพยายามดัน session ให้ยาวพอจะบังคับ compaction ผลคือ compaction ไม่เคยทำงาน และเหตุผลไม่ใช่เรื่อง compaction เลย

```
assistant len=7886  trunc=True
assistant len=1397  trunc=True
assistant len=3543  trunc=True
assistant len=0     trunc=True   ← ตั้งแต่ turn 4
assistant len=0     trunc=True
... อีก 6 turn
```

`maxTokens` = `OutputReserve` = 4,096 ของ compact-32k พอ prompt โตขึ้น reasoning ก็ยาวขึ้นจน**กินหมด 4,096 ไม่เหลือให้คำตอบ** และเมื่อถึงจุดนั้นมันไม่กลับมาอีก เพราะ history โตขึ้นเรื่อย ๆ

harness บันทึกทุก turn ว่าสำเร็จ ผู้ใช้ไม่ได้อะไรกลับมา 7 turn ติด และเป็นเหตุผลที่ active history โตแค่ ~90 token ต่อ turn — เพราะมีแต่ข้อความของผู้ใช้ ฝั่ง assistant ไม่เคยเติมอะไรเลย

flag `output_truncated` จาก O-11 ทำงานถูกทุกครั้ง แต่ไม่มีใครอ่านมัน

แก้: คำตอบว่างที่ถูกตัด **ไม่ใช่ turn ที่สำเร็จ** — fail พร้อมบอกตัวเลข แทนที่จะ commit ความเงียบ คำตอบที่ถูกตัดแต่ยังมีเนื้อยังเก็บไว้ตามเดิม เพราะงานบางส่วนดีกว่าไม่มีเลย

#### O-11 — reserve ที่รู้จัก reasoning *(แก้แล้ว: ข + freeze)*

เลือกทางที่วัดจริงแทนการตั้งค่าคงที่ เพราะค่าคงที่ตัวเดียวจะผิดกับ model ทุกตัวคนละแบบ

```
providers.ObserveReasoning   วัดสัดส่วนจากทุก completion เก็บเป็น running mean ต่อ provider
SessionContract.ReasoningRatio   ตรึงค่าตอนเปิด session
SessionContract.AnswerBudget     = OutputReserve × (1 − ratio)
```

**ทำไมต้อง freeze:** ถ้าปล่อยให้ ratio ขยับระหว่าง session budget จะเปลี่ยน → history ที่เลือกได้เปลี่ยน → **prompt เปลี่ยนกลาง session** ซึ่งขัด ADR-1 ตรง ๆ calibrate ข้าม session ได้ ภายใน session ห้ามขยับ

สองชั้นป้องกัน:

1. **ตอนเปิด session** — ถ้า answer budget ต่ำกว่า 512 token ปฏิเสธพร้อมบอกว่าทำไมและให้ทำอะไรแทน
2. **ต่อ turn** — คำตอบว่างที่ถูกตัด fail ทันที (O-16)

ชั้นเดียวไม่พอ: ratio ที่วัดได้จริงของ `qwen3.8-27b-fp8` คือ **0.597** ซึ่งผ่านด่านแรกใน compact-32k (เหลือ 1,650) แต่แต่ละ turn ยังล้นได้ตามความยาว prompt

ยืนยันกับ model จริง:

```
compact-32k    turn ที่เคยเงียบ → error ที่บอกว่า reasoning กินไป 3,019 จาก 4,096
certified-64k  answer_budget 3,303 → คำตอบกลับมาเต็ม 3,560 และ 4,938 ตัวอักษร
```

คำแนะนำที่ error บอกไว้ ("ใช้ profile ที่ output reserve ใหญ่กว่า") ทดสอบแล้วว่าแก้ได้จริง

#### O-17 — curator ไม่เคยเจออะไรเลย *(แก้แล้ว)*

seed Skill สี่ตัว หนึ่งคู่เป็น paraphrase ของกันและกันโดยตั้งใจ curator รันแล้วได้ **0 findings**

วัดส่วนประกอบจริง:

| คู่ | text | trigger | tool | weighted |
|---|---:|---:|---:|---:|
| paraphrase | 0.343 | 0.400 | 0.000 | **0.306** |
| อีกห้าคู่ | ≤0.075 | ≤0.077 | 0.000 | **0.015–0.064** |
| threshold เดิม | | | | 0.48 |

สัญญาณแยกกันชัด **ห่างเกือบห้าเท่า** แต่ threshold ตั้งไว้เหนือทุกอย่างที่ข้อมูลจริงไปถึง จึงไม่เคย retrieve อะไรเลย และ semantic judge ที่ควรรับต่อก็ไม่เคยได้รับ candidate

สองสาเหตุ:

1. `jaccard` คืน 0 เมื่อทั้งสองฝั่งว่าง — คู่ที่ไม่ประกาศ tool เลยเสีย weight 0.15 ทั้งก้อน **หลักฐานที่ไม่มี ถูกนับเป็นหลักฐานว่าต่างกัน** และ Skill ที่ ModelReviewer ผลิตประกาศ `tools: []` ทุกตัว คือกลุ่มที่ curator มีไว้เฝ้าโดยตรง
2. term overlap ระหว่างสอง procedure ที่เขียนคนละสำนวนอยู่ราวหนึ่งในสาม ไม่ใกล้ threshold ที่ตั้งไว้สำหรับข้อความที่แทบเหมือนกันคำต่อคำ

แก้: กระจาย weight ของ tool กลับไปที่ text/trigger เมื่อทั้งคู่ไม่มี tool และตั้ง threshold ใหม่ที่ระดับซึ่ง retrieval ทำงานจริง

**threshold calibrate จากตัวอย่างที่วัดได้ตัวอย่างเดียว** เขียนกำกับไว้ในโค้ดตรง ๆ ว่าต้องทบทวนกับ corpus จริง (P8-A) ไม่ใช่ถือว่าจูนแล้ว

ผลหลังแก้ — คู่ paraphrase 0.360 `overlap` · คู่อื่นสูงสุด 0.076 · ห่าง 4.7 เท่า และ curator สดคืน finding พร้อมหลักฐานและ `severity: info` โดย `proposals_count` ยังเป็น 0 ตามที่ควร เพราะ overlap ยังไม่ถึงระดับเสนอรวม

#### O-13 — tool call ที่พังฆ่าทั้ง turn *(แก้แล้ว)*

เจอตอนสั่งงานเขียนไฟล์จริง — งานที่ arguments ยาวที่สุดโดยธรรมชาติ

```
1. model ส่ง workspace.write_file arguments 788 bytes ถูกตัดกลาง string
2. registry จับได้ถูก: status=failed "invalid arguments: unexpected EOF"   ✓
3. arguments พังถูกเก็บเป็น tool_call event
4. step ถัดไป renderMessages ส่ง bytes เดิมกลับไปเป็น history
5. gateway parse ไม่ได้ → HTTP 400 → turn ตายทั้ง turn
```

ขั้นที่ 2 คือพฤติกรรมที่ถูกต้อง ปัญหาคือขั้นที่ 4 — harness เปลี่ยนสถานการณ์ที่ **กู้ได้** (tool call เสียหนึ่งครั้ง ซึ่งมี receipt อธิบายให้ model แก้อยู่แล้ว) ให้กลายเป็น turn ที่ตาย

น่าสังเกตว่า qualification มี check ชื่อ `malformed_argument_recovery` ที่ **ผ่าน** — แต่เส้นทางกู้นั้นถูกตัดขาดก่อน เพราะ request เองกลายเป็น invalid ก่อน model จะได้เห็น error

แก้ด้วย `replayableArguments`: history ส่ง `{}` แทน bytes ที่ parse ไม่ได้ ส่วน receipt ยังบอก model ว่าพลาดอะไร รันซ้ำงานเดิมกับ model จริงแล้วผ่านทั้งเส้น: อ่านไฟล์ → เสนอเขียน → หยุดรออนุมัติ → อนุมัติ → เขียนจริง → turn จบปกติ และโค้ดที่ได้ทำตามกฎของ Skill (integer satang, ปัดครึ่งขึ้นด้วย `(2n+d)//(2d)`)

นี่เป็นการยืนยัน effectful write slice ทั้งเส้นกับ model จริงครั้งแรก

#### O-14 — ไฟล์ใหญ่อ่านได้แค่หัวกับท้าย *(แก้แล้ว)*

เจอจากการขับจริง วางกฎไว้กลางไฟล์ 1,400 บรรทัด แล้วถามหา

```
read_file → ไฟล์ 61,490 token → spill เหลือ 7,067
model เห็นแค่หัวกับท้าย
ตอบ: "ผมหา RULE 4242 ไม่เจอ ... ตอนกลางถูกตัด
      และผมไม่มีเครื่องมือ grep/ค้นหาในไฟล์"
```

model ทำถูกทุกอย่าง — รายงานตรงว่าหาไม่เจอ ระบุว่าเห็นแค่ส่วนไหน และบอกสาเหตุ สิ่งที่ขาดคือ **เครื่องมือ**

`workspace.read_file` คืนทั้งไฟล์อย่างเดียว ไม่มี range ส่วน spill เก็บของจริงลง CAS พร้อม checksum แต่ไม่มีทางอ่านกลับ ไฟล์ใหญ่จึงใช้งานไม่ได้เกินหัวกับท้าย ซึ่งสำหรับ harness ที่ตั้งใจทำงาน coding คือช่องว่างใหญ่

แก้ตาม footprint ladder สองชั้น:

1. **extend primitive เดิม** — `read_file` v2 รับ `offset_line`/`max_lines` และรายงาน `total_lines` กลับมา; **hash ยังเป็นของทั้งไฟล์เสมอ** เพื่อไม่ให้อ่านทีละหน้าไปตอบ `expected_sha256` ที่ใช้ป้องกัน write ได้
2. **primitive ใหม่** — `workspace.search_files` ใช้ RE2 (ไม่มี catastrophic backtracking) จำกัด match/ไฟล์ที่สแกน/ความยาวบรรทัด ข้ามไฟล์ binary และไฟล์เกิน 1 MiB

direct tools 8 → 9 = **1,446 จาก 3,584 token** ของ compact-32k

รันงานเดิมซ้ำกับ model จริง: `read_file` → เห็นว่าถูกตัด → `search_files` → เจอบรรทัด 701 → `read_file` window ยืนยัน แล้วตอบถูกพร้อมข้อความเต็ม

#### O-15 — reviewer จ่ายแพงเพื่อรู้ว่าไม่มีอะไร *(แก้แล้ว)*

ขับงานหลากหลาย 7 turn (review โค้ด, แก้บั๊ก, อ่านไฟล์ใหญ่, ค้นหา, เรียก MCP) ผลคือ:

```
trigger ที่ยิง:     successful_milestone × 7
model call:        7
candidate:         0
```

`successful_milestone` ต้องการแค่ `outcome == success && (มี tool สำเร็จ || มี Skill activation)` — การอ่านไฟล์สำเร็จก็เข้าเงื่อนไข จึงยิงเกือบทุก turn

reviewer แยกแยะได้ดี (เหตุผลทั้ง 7 ข้อจำเพาะกับงานจริง ไม่ใช่ข้อความสำเร็จรูป) แต่ **ต้นทุนไม่คุ้ม** บน local-first ที่ reviewer แย่ง device กับ foreground การจ่าย model call เพื่อฟังว่า "ไม่มีอะไร" คือต้นทุนที่กระทบผู้ใช้โดยตรง

และมันขัดหลักการที่โครงการเขียนไว้เองใน ROADMAP: *"exact/deterministic reduction ก่อน semantic LLM call"*

แก้ด้วย `worthAModelCall` — filter แบบ deterministic ที่รันก่อน ส่งต่อให้ model เมื่อมีอย่างน้อยหนึ่งอย่าง: user correction, Skill activation, explicit learn request, หรือ tool receipt ที่ไม่ใช่ read-only

**filter ข้ามได้เฉพาะสิ่งที่มั่นใจ** — receipt ที่ parse ไม่ได้ถือว่าไม่ใช่ read-only จึงถูกส่งต่อ ไม่ถูกทิ้งเงียบ

ผลข้างเคียงที่ดี: marker vocabulary ของ trigger เคยอยู่ใน `internal/agent` ที่เดียว พอ filter ต้องใช้ด้วยจึงย้ายไป `internal/learning/triggers.go` เป็นแหล่งเดียว — สอง list ที่ถามคำถามเดียวกันย่อม drift ไปคนละคำตอบ

#### O-11 — output reserve ไม่รู้จัก reasoning token *(แก้แล้ว)*

`Profile.OutputReserve` เป็นตัวเลขเดียว (4,096 ถึง 65,536) ที่สมมติโดยปริยายว่า completion token ทั้งหมดคือคำตอบ บน reasoning model ไม่จริง — และจากที่วัด reasoning ยาวไม่คงที่แม้ prompt เดิม (`grep -rn Reasoning internal/context/` ได้ศูนย์ผลลัพธ์)

turn ที่จองไว้ 8,192 แล้วโดน reasoning กิน 6,000 จะเหลือ 2,192 ให้คำตอบจริงโดย compiler ไม่รู้ตัว O-8 คือกรณีเดียวกันที่เกิดใน probe; O-11 คือกรณีเดียวกันที่ยังเปิดอยู่ใน agent loop

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

หมายเหตุลำดับความสำคัญ: ตาราง gap นี้เป็น *product parity* ซึ่งอยู่ใน Phase 11 เต็มรูปแบบ แต่ตาม dependency ต้องทำหลัง kernel (8/9/10) ถึงระดับ  ก่อน

## 7. Test evidence

### Hermetrix

รันใน snapshot นี้และผ่าน:

```text
go build ./...               passed
go vet ./...                 passed
go test ./...                122 test functions, 0 failed
go test -race ./...          passed
node --check internal/web/ui/app.js   passed
./scripts/doc-truth.sh check passed
```

ตัวเลขทั้งหมดในเอกสารนี้ generate จาก `./scripts/doc-truth.sh` ไม่ได้พิมพ์มือ

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
