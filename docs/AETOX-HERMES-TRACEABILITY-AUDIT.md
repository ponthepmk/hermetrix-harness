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
| **R-14** | เกณฑ์ถอยของ ADR-7 ยังวัดไม่ได้ในสนามจริง | **medium** | **วัดได้แล้วว่าตาบอด** — verdict `retrieval_blind` แยก "ยังไม่มีหลักฐานพอ" ออกจาก "scorer อ่านไม่ออก" · ทางแก้จริงยังต้องตัดสินใจ<br> รันกับ gateway จริงแล้ว **254 turn**: `turns_with_relevant_skill` = 12 (ต้องการ ≥20) `turns_model_requested` = 0 `rate` = 1.0 verdict `insufficient_evidence`<br>**ตัวหารเล็กเพราะ goal เป็นไทยแต่ catalog เป็นอังกฤษ** — lexical ข้ามภาษาไม่ได้ตามที่ O-20 ระบุ model เรียก `skill_search`/`skill_view` ไป **165 ครั้ง** แต่ไม่ตรงกับ 12 turn ที่ scorer บอกว่ามี Skill ตรงเลยสักครั้ง<br>วัดจริงไม่ได้จนกว่า catalog จะเป็นไทย หรือ retrieval จะข้ามภาษาได้<br>**ปิดแล้ว** — `rankSkillBindings` บวกคะแนน semantic เข้ากับ lexical ต่อครบสามจุดเรียก (freeze contract · `skill_search` · metric) และ `serve` มี `--embed-url` แล้ว cache vector ด้วย hash ของข้อความ · floor เป็นค่าที่สูงกว่าระหว่าง quartile ของ catalog กับค่ากลางของ control ไม่ใช่ค่าคงที่<br>mutation ห้าข้อแดงหมด รวม **ถอด control floor แล้ว goal เรื่องอากาศดึง Skill มาจริง**<br>วัดกับ bge-m3 จริง: 3/3 goal ไทยถึง Skill ที่ถูก · goal ไม่เกี่ยวได้ `[]` ทั้ง catalog 3 และ 8 ตัว |
| **O-7** | documentation drift | medium | มี `scripts/doc-truth.sh` สองชั้นแล้ว (facts + claim registry) claim registry จับ anchor ที่หายได้จริงตั้งแต่รันครั้งแรก; ข้อความเชิงความหมายยังต้องให้คนไล่ |
| ~~**P-3**~~ | ~~exit gate ของ Phase 8–14 หลายข้อยังวัดไม่ได้~~ | — | **ปิดแล้ว** — gate audit ครบใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md) ทุก gate เป็น predicate; ที่ขาด artifact กลายเป็น prerequisite ที่มีชื่อ (P8-A…P14-A) แถวนี้ค้างมาจากรอบก่อน |
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
| **O-21** | export กับ import ไม่ใช่ฟังก์ชันผกผัน และไม่มีอะไรบอก | high | **ปิดแล้ว** — เจ้าของเลือกให้ export ใส่น้อยลง เหลือเฉพาะ skill lifecycle |
| **O-22** | *(ถอน)* — ผมเรียก fidelity ผิด API เอง ไม่ใช่ข้อบกพร่อง | — | ถอน |
| **O-23** | fidelity corpus ที่ seed มากับระบบล้มไม่ได้ | high | **แก้แล้ว** |
| **O-24** | replay สีเขียวที่ทดสอบแค่ manifest ถูกรายงานเหมือนทดสอบ procedure | high | **ปิดแล้ว** — เจ้าของเลือกให้บล็อก promotion |
| **O-25** | หน้า error HTML ของ gateway กลายเป็นบทสนทนาและถูกอัดเข้า checkpoint | high | **แก้แล้ว** |
| **O-26** | token error band ประเมินไม่ได้ — คู่ค่าถูกทิ้ง calibration หายตอน restart ใช้ร่วมข้าม model | **critical** | **แก้แล้ว** |
| **O-27** | band วัดเทียบ *งบ* ไม่ใช่ *prompt* ทำให้ค่าผิดแปรตามขนาด context | high | **แก้แล้ว** |
| **O-28** | calibration ลู่เข้า **รากที่สอง** ของค่าจริง ทิ้ง error ถาวร −10.6% | high | **แก้แล้ว** |
| **O-29** | estimator คิด 1 token ต่ออักษรที่ไม่ใช่ ASCII — ผิดราวสองเท่า | **critical** | **แก้แล้ว** |
| **O-30** | chat template ที่ห่อทุก message ถูกเรียกเก็บเงินและไม่เคยถูกนับ | medium | **แก้แล้ว** |
| **O-31** | metric ตัดสินจากประวัติทั้งชีวิต รวมช่วงก่อน calibrate | high | **แก้แล้ว** |
| **O-32** | compiler จองงบให้ tool arguments ที่ transport ส่งไม่ได้ | high | **แก้แล้ว** |
| **O-33** | `no_change` รวม "ปฏิเสธ" กับ "ตอบไม่ได้" เป็นอย่างเดียวกัน | high | **แก้แล้ว** |
| **O-34** | `.gitignore` กลืนไดเรกทอรี `cmd/hermetrix/` — **repo ไม่มีตัวโปรแกรม** | **critical** | **แก้แล้ว** |
| **O-35** | reviewer ไม่ deterministic — gate ที่อ่านครั้งเดียวตัดสินเส้น 0.60 ไม่ได้ | high | **แก้แล้ว (worst of N)** |
| **O-36** | corpus scoring รีวิวซ้ำต่อ split — หลักฐานเดียวกันได้คำตอบต่างกันในรายงานฉบับเดียว | high | **แก้แล้ว** |
| **O-37** | citation ท้ายประโยคถูกตราว่าประดิษฐ์ | medium | **แก้แล้ว** |
| **O-38** | 502 ครั้งเดียวทิ้งงาน scoring หนึ่งชั่วโมง | medium | **แก้แล้ว** |
| **O-39** | verdict ของ fidelity ไม่อ่าน decision/open-task/file-state recall — run ที่ทิ้ง decision 97% รายงาน `passed` | high | **แก้แล้ว** |
| **P-7** | P9-A วางไว้วัด essential retention ที่วัดไม่ได้ — pinned retention เป็น 1 หรือ compile error ไม่มีค่ากลาง | high | **เปลี่ยนรูปแล้ว** |
| **O-40** | `decision` / `open_task` / `acceptance_criteria` ไม่มีผู้ผลิตนอก fixture — สอง ใน สาม ประธานของ gate Phase 9 ไม่มีอยู่จริง | **critical** | **แก้แล้วหนึ่งในสาม** — `approval_decision` กลายเป็น `decision` ยืนยันในสนามจริง<br>`open_task` ดู V-7 · `acceptance_criteria` รอการตัดสินใจ |
| **V-7** | `open_task` ไม่มีผู้ผลิตที่**เรียกถึงได้** — approval ที่ค้างบล็อก session ไม่มี compile เกิดขึ้นระหว่างนั้น | high | **วัดแล้ว ไม่ปิดบัง** — ถอด producer ที่ยิงไม่ออกทิ้ง |
| **O-41** | qualification โหมด `qualified` เรียกไม่ถึงสำหรับ provider ระยะไกล — ผ่านทุก check เกรด A แต่ `eligible=false` | **low** (ลดจาก high) | **ไม่ใช่ช่องโหว่** — เพดานที่ตั้งค่าไว้บังคับอยู่แล้วที่ `service.go:73` · ที่ขาดคือหลักฐานความลึกเพิ่มเติม ไม่ใช่ความปลอดภัย |
| **O-42** | 6 ตารางมีแต่ schema ไม่มีคนอ่านคนเขียน แต่อยู่ในรายการ backup | medium | **ตรวจจับอัตโนมัติแล้ว** ใน `doc-truth.sh` |
| **V-8** | MCP: เทสต์ทุกตัวตั้ง `TrustAnnotations: true` — สาขา default ที่ป้องกัน operator ไม่เคยรัน | high | **ปิดแล้ว** hostile fixture + mutation |
| **O-43** | compaction ตัดสินด้วยตำแหน่ง ไม่ใช่ความเกี่ยวข้อง — fact ที่ตำแหน่งสุ่มหาย 34.5% | high | **แก้แล้ว** — compact ตามความเกี่ยวข้อง + `context_search` + checkpoint ประกาศความสูญเสีย |
| **V-9** | corpus P9-B วัดความสูญเสียไม่ได้อีกแล้วหลังแก้ compactor — ทุก fact เข้าถึงได้ 100% | medium | **ปิดแล้ว** — เปลี่ยนมิติเป็นระยะห่างของถ้อยคำ far/middle หาย 0/19 |
| **O-44** | `context_search` สืบทอดจุดอ่อนเดียวกับที่ทำให้ fact หาย — retrieval แบบ lexical กู้สิ่งที่ ranker แบบ lexical ทิ้งไม่ได้ | **high** | **แก้แล้ว** — semantic + chunk + ตำแหน่ง · fact เข้าถึงได้ 70/90 → 90/90 |

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

#### O-30 — chat template ไม่เคยถูกนับ *(แก้แล้ว)*

ทุก message ถูกห่อด้วย template ของ model และ request มี preamble อีกชั้น ทั้งคู่ถูกเรียกเก็บเงินและไม่ใช่ context — estimator ไม่นับทั้งสองอย่าง

**วัดตรงๆ ไม่ได้ fit**: แยก content ก้อนเดิมออกเป็น 1, 3, 5, 9, 17, 33 message ได้ผลเพิ่มขึ้น **ครั้งละ 9 token พอดี** บนค่าคงที่ 45 — request เปล่า 52 token

การ fit บน traffic จริงทำไม่ได้: ที่นั่นจำนวน message, ขนาด content และสัดส่วนภาษาโตไปด้วยกัน regression จึงคืนค่า **−260 token ต่อ message**

ผลที่ซื่อสัตย์: **มันไม่ได้กู้แบนด์** บน 37 request คิดราคา template ได้ p95 7.42% ยุบเข้า rate ได้ 7.85% ผ่านทั้งคู่

สิ่งที่มันแก้คือ **rate** — rate ที่ดูดค่าคงที่ต่อ message เข้าไปถูกต้องเฉพาะที่ความหนาแน่น message ที่มันเรียนมา content เท่าเดิมแต่หนาแน่นสามเท่า โมเดลที่ยุบเข้า rate หลุดแบนด์ ส่วนที่คิดแยกยังตรงเป๊ะ **ความล้มเหลวแบบนี้ไม่มีวันโผล่ใน corpus ที่ผลิต rate ตัวนั้น**

provider ที่ยังไม่วัดถูกคิด **ศูนย์ ไม่ใช่ค่าเดา** — overhead ที่ผิดถูกหักจาก usable context ทุก request

#### O-31 — metric ไม่เคยลืม cold start ของตัวเอง *(แก้แล้ว)*

estimator calibrate ตัวเอง ประวัติทั้งชีวิตจึงมีช่วงก่อนที่มันจะรู้อะไรเลย วัดสดจากศูนย์: **7 request แรกพลาด 9–21% และอีก 31 ตัวถัดมาเข้าแบนด์ทุกตัว** — lifetime p95 19.9% แต่ 15 ตัวล่าสุด 6.5%

ตัดสินจากทั้งชีวิต provider จะ `out_of_band` ตลอดกาลเพราะ request ที่มันยิงตอนยังไม่รู้อะไร — **ตอบคำถาม "เคยผิดไหม" ทั้งที่ gate ถาม "ตอนนี้แม่นไหม"**

verdict มองย้อน 50 ตัวล่าสุด **แต่ overflow ไม่ถูก window** — request ที่ถูกเรียกเก็บเกิน ceiling ของ profile ตัวเองเป็นข้อเท็จจริงเรื่องความปลอดภัย ไม่ใช่สถานะ calibration และมันไม่เลิกเป็นจริง

#### O-32 — จองงบให้ไบต์ที่ส่งไม่ได้ *(แก้แล้ว)*

arguments ของ tool call ถูก replay ผ่าน `replayableArguments` ซึ่งแทนด้วย `{}` เมื่อ provider จะปฏิเสธ compiler นับของเดิม จึงจอง context ให้ไบต์ที่ transport โยนทิ้ง

สด: reasoning model ปล่อย tool call สามครั้งที่ arguments เป็นวรรณยุกต์ไทยตัวเดียวซ้ำ **12,131 อักษร** ตัดกลางคัน compiler คิดราว 6,700 token · request ส่ง `{}` · provider เรียกเก็บ 11,744 เทียบกับที่ทำนาย 16,722 — **63% ของอักษรไทยทั้ง compile อยู่ใน tool call ที่ไม่เคยถูกส่ง**

ครึ่งที่หนักกว่าไม่ใช่ตัวเลขที่ไม่ตรง: ballast ก้อนนั้นถูก replay เข้าทุก compile ถัดไป ยึด active budget ไว้จากบทสนทนาจริง และบังคับให้ compaction ทำงานเร็วขึ้น — **เพื่อส่งอะไรก็ไม่รู้ที่ไม่มีใครอ่าน**

และมันอธิบาย discontinuity ที่ดูเหมือน provider เปลี่ยนใต้เท้าเรา: billed ร่วง 35% ที่ขนาด compile เท่าเดิม **ไม่ใช่ gateway** — probe ตรงยืนยัน tokenizer ไม่เปลี่ยน (ไทย 0.556 token/char, request เปล่า 52 token) สิ่งที่เปลี่ยนคือสัดส่วนของ compile ตัวเองที่ส่งไม่ได้

#### ผลลัพธ์: gate ของ Phase 9 ผ่านแล้ว

วัดสดบน gateway จริง จาก cold start ครบวงจร:

```
samples 50 (window) · lifetime 62
median |error|  4.6%
p95    |error|  6.0%     (gate: ≤10%)
within band     100%
overflows       0
verdict         within_band
```

สูตรที่ใช้: `billed = request_overhead + message_overhead × messages + asciiTokens + rate × nonASCIIChars`
โดย overhead **วัดด้วย probe** และ rate **เรียนรู้ต่อ provider**

**ไม่ต้องมี tokenizer adapter เต็มรูปแบบเพื่อผ่าน gate นี้** — อย่างน้อยสำหรับ provider นี้ ซึ่งลดขอบเขต P9-C ลงมาก

#### บทเรียนจากการวัด: สามครั้งที่ผมวัดผิดติดกัน

กว่าจะ fit ถูก ผมวัดผิดสามรอบ และ**ทุกรอบข้อมูลดูเป็นระเบียบ**

1. จับคู่ usage รวมทั้งเทิร์นกับ snapshot ของ step เดียว → error +226% ดูเหมือนข้อบกพร่องร้ายแรงของ compiler
2. หารด้วยงบที่รวมเงินสำรอง 2,048 → error หดลงเรียบๆ ตามขนาด context ดูเหมือน estimator ที่ดีขึ้นตามการใช้งาน
3. ลืม tool schema ในโมเดล → offset คงที่ 1,200 token อ่านเป็น error 135% ที่ context เล็ก ลดเหลือ 2% ที่ context ใหญ่

ทุกรูปร่างดูเหมือน finding และทุกอันคือ**รูปร่างของตัวแปรที่ผมไม่ได้ใส่** — สัญญาณที่เรียบเกินไปคือสัญญาณของพจน์ที่หายไป ไม่ใช่ของกฎที่ค้นพบ

#### O-34 — repo ไม่มีตัวโปรแกรม *(critical, แก้แล้ว)*

`.gitignore` มีบรรทัด `hermetrix` ตั้งใจให้ ignore ไฟล์ binary — pattern ที่ไม่มี slash แมตช์ **ทุก path component ที่ชื่อนั้น** รวมถึงไดเรกทอรี `cmd/hermetrix/` **main package ทั้งแพ็กเกจไม่เคยถูก commit**

clone ใหม่:

```
$ ls cmd                  → No such file or directory
$ go build ./...          → ผ่าน (ไม่มีอะไรให้ build)
$ go vet ./...            → เงียบ (ด้วยเหตุผลเดียวกัน)
$ go test ./internal/...  → ผ่าน (internal ครบ)
```

**CI เขียวมาตลอดบน repo ที่ไม่ผลิต binary** `go build ./...` จับ main package ที่หายไปไม่ได้ แก้: anchor เป็น `/hermetrix` + CI build ตามชื่อแล้วสั่งให้โปรแกรมพิมพ์ usage ของตัวเอง

#### O-35 — reviewer ไม่ deterministic *(แก้แล้ว)*

`temperature = 0` อยู่แล้ว จึงเป็นความไม่นิ่งของ model เอง วัดตรงๆ 5 รอบบน 12 เคส:

```
review_05dc9fe...  explicit_learn        no no no no yes   ← พลิก
review_01e9b09...  repeated_correction   no yes yes yes yes ← พลิก
review_24cfddf...  repeated_correction   no no no yes yes   ← พลิก
negatives ทั้งหมด                         no no no no no
stable 9/12 · flipped 3/12
```

**ลังเลเฉพาะฝั่งบวก ฝั่งลบนิ่งสนิท** สามรอบบน 100 เคสเดียวกันได้ 31 · 34 · 30 = recall 0.55–0.62 **คร่อมเส้น 0.60**

gate ตัดสินที่ **worst of N** (`--repeats`) ผ่าน = ผ่านทุกรอบ ไม่ใช่ผ่านรอบที่โชคดี และรายงานช่วง + จำนวนเคสที่ตอบไม่เหมือนเดิม เพราะ *สัดส่วนไหนคือวิจารณญาณ สัดส่วนไหนคือ variance* เป็นข้อมูลในตัวมันเอง

#### O-36 ถึง O-38 — เจอจากการเขียนเทสต์และจากการรันจริง

**O-36** scoring รีวิวเคสละครั้งต่อ split — `driven` กับ `all` เป็นชุดเดียวกันแต่ได้ 31 กับ 34 **หลักฐานเดียวกัน คำตอบต่างกัน ในรายงานฉบับเดียว** แก้แล้วรันเร็วขึ้น 33 → 13 นาที

**O-37** identifier มีจุดและ colon อยู่จริง (`skill:<id>@<ver>`, `event:<id>:<tool>:<status>`) pattern จึงกลืน full stop ท้ายประโยค `event:event_real.` จะถูกกล่าวหาว่าแต่งขึ้นทั้งที่มีในหลักฐาน — **เป็นข้อกล่าวหาที่ gate ให้ tolerance = 0 จึงต้องแม่น**

**O-38** run 200 รีวิว 55 นาที ถูกทิ้งด้วย 502 ครั้งเดียว ตอนนี้ retry แบบมีขอบเขต — ต่างจาก tool call ที่ harness ตั้งใจไม่ retry (`automatic_retry:false`) เพราะ review อ่านอย่างเดียว ไม่เปลี่ยนอะไร ถามซ้ำคือคำถามเดิม

#### O-39 และ P-7 — เครื่องมือตัวที่สี่ที่ตั้งไว้จนวัดอะไรไม่ได้

ก่อนสร้าง gold corpus ของ P9-A ผมวัดก่อนว่า instrument ที่มีอยู่ตอบคำถามอะไรได้บ้าง กวาด pressure จาก 82 token ถึง 1,870,882 token บน `compact-32k`:

```
fillers=   0  in=     82 essential=1.00 decision=1.00 open=1.00 file=1.00 passed=true
fillers=  40  in=  62442 essential=1.00 decision=1.00 open=1.00 file=1.00 passed=true
fillers= 200  in= 311882 essential=1.00 decision=1.00 open=1.00 file=1.00 passed=true
fillers=1200  in=1870882 essential=1.00 decision=1.00 open=1.00 file=1.00 passed=true
```

ทุก metric เป็น 1.00 ตลอดช่วง 23,000 เท่า เหตุผลอยู่ใน `sliceFor` — อะไรที่ `Pinned` หรือเป็น `user_goal`/`acceptance_criteria` เข้า slice `pinned` และ slice นั้น**ไม่มีการ drop** มันลงได้หรือ `Compile` คืน `ErrPinnedOverflow` เท่านั้น

**P-7** `essential_exact_retention` จึงมีได้แค่ 1 หรือไม่มี run เลย ไม่มีค่ากลาง gold corpus 50 เคสต่อภาษาจะได้ 1.00 ห้าสิบครั้ง แล้วพิสูจน์เรื่อง compiler ไม่ได้สักข้อ — มันพิสูจน์แค่ว่าคนเขียนเคสให้ priority สูงกับ fragment ที่ตัวเองบอกว่าสำคัญ **instrument เกรดตัว fixture ไม่ได้เกรด compiler** ยืนยันสองทางใน `TestPinnedEssentialsAreRetainedExactlyOrTheCompileFails` — ด้านหนึ่ง retention คงที่ 1.00 ใต้ filler 800 ตัว อีกด้าน pinned 50 ตัวได้ `pinned context exceeds profile budget: used=37100 budget=2048`

คำถามที่วัดได้จริงอยู่ที่ kind ที่**ไม่**ถูก pin — decision, open task, file state พวกนี้ผ่าน `selectActive` และหล่นได้จริง เส้นโค้งมีอยู่:

```
decisions ที่ประกาศว่า essential:  5 -> recall 1.000
                                  50 -> recall 0.360
                                 200 -> recall 0.090
                                 600 -> recall 0.030
```

**O-39** และทั้งสี่แถวรายงาน `passed=true` gate ของ Phase 9 เขียนว่า "retention ของ goal/constraint/decision = 100%" แต่ `verify()` อ่านแค่ตัวแรก อีกสามตัวถูกคำนวณ เก็บลง DB แสดงผลบนหน้าจอ แล้วไม่ถูกใช้ตัดสิน — run ที่ทิ้ง decision ไป 97% ผ่าน gate ที่ห้ามมันทิ้งเลย

ตอนนี้ verdict อ่านครบสี่ตัว mutation: ลบ `metrics.DecisionRecall == 1 &&` ออก แล้ว `TestVerdictFailsWhenDeclaredDecisionsAreDropped` กลับมาเขียว ทั้งที่ decision ยังหายไป 97% เท่าเดิม

หมายเหตุที่ตรวจแล้วว่า**ไม่ใช่** defect: `represented()` รับการปรากฏใน checkpoint เป็นการคงอยู่ ซึ่งดูหลวมจนกระทั่งอ่าน compactor — checkpoint เป็น extractive จริง แต่ละบรรทัดเป็น `- [decision:id] <เนื้อหา>` ตัดที่ 520 rune สำหรับ decision/open_task/tool_result ไม่ใช่การอ้าง ID ลอย ๆ

#### O-40 — ประธานของ gate ไม่มีอยู่จริง

พอรู้ว่า instrument วัดอะไรได้บ้าง คำถามถัดไปคือของจริงมีหน้าตายังไง นับ fragment ทุก kind จาก snapshot จริง 772 ครั้งของ session ที่ขับไปแล้ว อ่านทั้ง `compiled_json` (ที่รอด) และ `report_json` (ที่ถูกทิ้ง) เพื่อไม่ให้พลาด kind ที่ออกมาแล้วโดนตัด — `scripts/fragment-census.py`:

```
snapshots: 772
kinds produced: {'tool_call': 5933, 'conversation': 5649, 'tool_result': 5440,
                 'policy': 1496, 'identity': 772, 'user_goal': 772,
                 'artifact_receipt': 493, 'selected_skill': 320, 'checkpoint': 162}
causal pairs: 5933 seen, 0 split

  user_goal              median=  1 p90=  1 p99=  1 max=  1
  acceptance_criteria    median=  0 p90=  0 p99=  0 max=  0
  decision               median=  0 p90=  0 p99=  0 max=  0
  open_task              median=  0 p90=  0 p99=  0 max=  0
  artifact_receipt       median=  0 p90=  3 p99= 10 max= 13

NEVER PRODUCED: acceptance_criteria, decision, open_task
```

ยืนยันจากฝั่ง source: `grep KindDecision|KindOpenTask|KindAcceptanceCriteria` ในโค้ดที่ไม่ใช่เทสต์ทั้งหมดได้ผู้ผลิตศูนย์ราย — ที่เจอคือ `types.go` ที่นิยามมัน, `compiler.go`/`compactor.go` ที่**บริโภค**มัน (acceptance_criteria เข้า slice pinned, decision/open_task ได้โควตา 520 rune ใน checkpoint) และ fixture ของ fidelity เอง ไม่มีเส้นทางไหนใน agent สร้าง fragment สาม kind นี้เลย `acceptance_criteria` ไม่มีแม้แต่ fixture

gate Phase 9 เขียนว่า "retention ของ goal/constraint/decision = 100%" ตอนนี้อ่านตามตัวอักษร:

| ประธาน | สถานะจริง |
|---|---|
| goal | มี 1 ต่อ turn เป๊ะ ๆ · retention 100% แต่เป็น tautology (pinned slice ทิ้งไม่ได้) |
| constraint (`acceptance_criteria`) | **ไม่มีผู้ผลิตที่ไหนเลย** |
| decision | **ไม่มีผู้ผลิตนอก fixture** |
| causal split = 0 | **ผ่านจริง มีประธานจริง** — 5,933 คู่ ใน 660 snapshot แตกศูนย์ |

นี่ไม่ใช่ instrument ที่วัดไม่ได้แบบสามครั้งก่อน มันหนักกว่า — **สิ่งที่จะวัดยังไม่ถูกสร้าง** สร้าง gold corpus กี่เคสก็ปิด gate นี้ไม่ได้ เพราะ corpus จะเป็นที่เดียวในระบบที่ decision มีอยู่

#### causal pair — ครึ่งเดียวของ gate ที่ผ่านจริง และผ่านเพราะอะไร

5,933 คู่ แตกศูนย์ อาจเป็นเพราะโชค — คู่ที่บังเอิญพอดีงบทุกครั้ง ตรวจแล้วไม่ใช่ มีการรับประกันสองชั้นที่เป็นอิสระต่อกัน:

1. **ใน slice เดียวกัน** `makeUnits` ใช้ `pair:<PairID>` เป็นคีย์ คู่จึงเป็นหน่วยเดียว เลือกหรือทิ้งไปด้วยกันเสมอ
2. **ข้าม slice** ไม่มีคีย์ร่วม ช่องว่างนี้ปิดด้วยกลไกคนละตัว — `evaluateIntegrity` **ปฏิเสธการ compile ทั้งก้อน** แทนที่จะคืน context ที่ tool call หลุดจากผลลัพธ์ของมัน

ทั้งสองชั้นมี mutation ใน `TestCausalPairsSurviveTogetherOrTheCompileRefuses`: ถอดคีย์ `pair:` ออกจาก `makeUnits` แล้ว subtest แรกล้ม (ชั้นที่สองจับได้เอง ซึ่งเป็นการสาธิตว่าสองชั้นแยกกันจริง) · ทำให้ `default:` ใน `evaluateIntegrity` ไม่คืน error แล้ว subtest ที่สองล้มด้วย `a split pair compiled successfully`

เทสต์ยังกันการวัดหลอกตัวเองไว้ด้วย — ถ้าคู่ที่ควรถูกทิ้งกลับรอดทั้งคู่ มันฟ้องว่า `the pair was never at risk; the premise is broken, not the guarantee`

#### O-40 (ต่อ) — ผู้ผลิตที่หายไปคือสิ่งที่ compile โยนทิ้งอยู่แล้ว

ไม่ต้องสร้างของใหม่ event log มี `approval_required` กับ `approval_decision` มาตั้งแต่ต้น — ใน session ที่ขับไปแล้วมี 11 กับ 9 ตามลำดับ แต่ `switch` ใน `compileTurn` ไม่มี case ให้ทั้งคู่ **ทุก compile โยนทิ้งหมด**

ผลที่เกิดขึ้นจริงกับ model ไม่ใช่แค่เรื่อง metric: model ที่ถูกบอกตอน step 3 ว่ามีคนอนุมัติการเขียนไฟล์นี้ด้วยเหตุผลอะไร ไม่รู้เรื่องนั้นเลยตอน step 40 · และการเขียนที่ค้างรอมนุษย์อยู่ **มองไม่เห็นจาก model ที่กำลังรอมันอยู่**

การแปลงเป็นแบบ deterministic ไม่ใช้ model สกัด:

| event | kind | priority | pinned |
|---|---|---|---|
| `approval_decision` | `decision` | 90 | ไม่ |
| `approval_required` ที่ยังไม่มีคำตอบ | `open_task` | 86 | ไม่ |

คำขอที่ถูกตอบแล้ว**เลิก**เป็น open task — ไม่งั้น model จะถูกบอกว่าเรื่องที่จบไปแล้วยังค้างอยู่ · และทั้งสอง kind **ไม่ pin** โดยตั้งใจ retention ที่ล้มไม่ได้คือ retention ที่วัดไม่ได้ ซึ่งคือบทเรียนของ P-7 ทั้งดุ้น

mutation: ลบสอง case ออกได้ `decisions = 0, want 1` · ลบการข้าม `decided[approvalID]` ได้ `open tasks = 2, want 1`

#### O-44 (ต่อ) — สามชั้นที่ต้องครบ ไม่มีชั้นไหนพอด้วยตัวเอง

วัดบนคอร์ปัสเดียวกัน เปลี่ยนทีละชั้น:

```
lexical อย่างเดียว                    70/90   far/middle  0/20
semantic ทั้งชิ้นส่วน                  70/90   far/middle  0/20
semantic + หั่นเป็น chunk              70/90   far/middle  0/20
semantic + chunk + ชี้ตำแหน่ง          90/90   far/middle 20/20
```

**สามชั้นแรกไม่ขยับอะไรเลย** และแต่ละครั้งที่ไม่ขยับก็บอกสาเหตุที่ต่างกัน:

**ชั้นที่ 1 ล้มเพราะการเจือจาง** bi-encoder คืน vector เดียวต่อข้อความที่ให้ไป วัดด้วย bge-m3 เทียบกับคำถามที่ fact นั้นตอบ:

```
fact เดี่ยว                   0.567
fact ใน padding ~470 rune     0.406
fact ใน padding ~5,600 rune   0.338
padding ล้วน ~5,600 rune      0.354
```

ชิ้นส่วนที่มีคำตอบได้คะแนน**ต่ำกว่า**ชิ้นส่วนที่ไม่มีอะไรเลย นี่ไม่ใช่เพดานที่ปรับได้ เพราะ vector ไม่ได้แทน fact นั้นแล้ว

**ชั้นที่ 2 แก้อันดับได้ แต่ไม่พอ** หั่นเป็นหน้าต่าง 500 rune ซ้อน 100 แล้วให้คะแนนด้วย chunk ที่ตรงที่สุด — needle ขึ้นจาก 0.339 เป็น 0.449 **และเข้า checkpoint ได้จริง** แต่ `marker` ยังเข้าถึงไม่ได้ เพราะ extract ไม่รู้ว่าต้องเล็งตรงไหนในชิ้นส่วนนั้น

**ชั้นที่ 3 คือชั้นที่ทำให้ทั้งหมดทำงาน** chunk ที่คะแนนสูงสุด**คือ**ย่อหน้าที่สำคัญ ข้อมูลคำนวณไปแล้วแค่ไม่ได้ส่งต่อ เปลี่ยน callback จาก `float64` เป็น `SemanticHint{Score, Start, End}`

บั๊กที่เจอระหว่างทาง ทั้งหมดเจอเพราะวัด ไม่ใช่เพราะอ่านโค้ด:

| | |
|---|---|
| event ที่คะแนน 0 ไม่เคยเข้า map | median คำนวณจากเฉพาะตัวที่ตรง = เทียบตัวที่ดีที่สุดกับตัวเอง |
| หน้าต่างเปิดที่กึ่งกลางช่วงที่ชี้มา | chunk ยาว 500 หน้าต่าง 360 fact อยู่ต้น chunk → เปิดเลยไป |

**ข้อจำกัดที่ยังอยู่:** คอร์ปัสวัดความสูญเสียไม่ได้อีกแล้ว (ครั้งที่สี่) ทุกช่องเข้าถึงได้ 100% การวัดครั้งต่อไปต้องหาเคสที่ยากกว่านี้ ไม่ใช่ถือว่า 90/90 แปลว่าปลอดภัย

#### O-44 — retrieval แบบ lexical กู้สิ่งที่ ranker แบบ lexical ทิ้งไม่ได้

corpus P9-B รอบที่ใช้มิติถ้อยคำ วัด 90 task สามเงื่อนไข ไขว้ผลกับตารางที่ออกแบบไว้:

```
cell            n  reach  compiled ผ่าน   ค้น   เจอ  retrieval ผ่าน
far/head       14     14             13     7     7              14
far/middle     19      0              0    18     3               1
far/tail        9      9              9     4     3               9
near/head      13     13             13     2     2              13
near/middle    12     12             12     5     3              13
near/tail      18     18             18     1     0              18
```

`far/middle` คือช่องเดียวที่ fact หายจริง และมันเล่าเรื่องทั้งหมด:

**ข่าวดี — R-14 กรณีเลวร้ายที่สุดไม่เกิดขึ้น** โมเดล**ค้น 18 จาก 19 ครั้ง** เมื่อมันตอบไม่ได้จริง ๆ พอ checkpoint บอกว่า context สูญเสียและเรียกชื่อเครื่องมือ โมเดลก็ใช้ ต่างจาก `skill_search` ที่ถูกเรียก 165 ครั้งโดยไม่ตรงกับ turn ที่ควรเรียกเลยสักครั้ง — **การประกาศความสูญเสียใน checkpoint ได้ผล**

**ข่าวร้าย — เครื่องมือหาไม่เจอ** ค้น 18 ครั้ง **เจอ 3** ผ่าน 1

เหตุผลตรงไปตรงมาและเป็นเชิงโครงสร้าง: โมเดลค้นด้วย**คำในคำถาม** แต่ fact เขียนด้วย**คำอื่น** ซึ่งเป็นความไม่ตรงกันแบบเดียวกันเป๊ะที่ทำให้ compactor ทิ้งมันตั้งแต่แรก

> **retriever แบบ lexical กู้สิ่งที่ ranker แบบ lexical ทิ้งไม่ได้ เพราะทั้งคู่ล้มด้วยอินพุตเดียวกันด้วยเหตุผลเดียวกัน**

สังเกตว่าในช่องอื่นเครื่องมือทำงานดี — `far/head` ค้น 7 เจอ 7 · `near/head` ค้น 2 เจอ 2 เมื่อถ้อยคำตรงกัน retrieval ได้ผล 100%

**ทางแก้จึงไม่ใช่ "ทำให้โมเดลค้นบ่อยขึ้น"** — มันค้นอยู่แล้ว 95% ของเวลาที่จำเป็น ทางแก้คือทำให้ retrieval ข้ามการใช้คำต่างกันได้ ซึ่งเป็นคำถามเรื่อง semantic/embedding หรือ query expansion และเป็นการตัดสินใจที่ยังไม่ได้ทำ

#### R-14 — ตัวหารถูกผลิตโดย matcher ตัวที่กำลังถูกตั้งคำถาม

`SkillRetrievalMetrics` อ่าน `no_skill_requested_rate` จาก turn ที่ "มี Skill ที่เกี่ยวข้อง" ซึ่งนิยามโดย `selectSkillBindings` — **scorer ตัวเดียวกับที่กำลังถามว่าดีพอไหม** ผลคือ retrieval ที่ล้มสนิทกับ catalog ที่ว่างเปล่าให้ตัวเลขเหมือนกันเป๊ะ แล้ว verdict ค้างที่ `insufficient_evidence` รอ sample ที่ไม่มีวันมา

วัดกับ catalog จริงจาก corpus ที่ขับไปแล้ว (คำอธิบาย Skill เป็นอังกฤษทั้งสามตัว):

```
ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์      -> []
แก้การปัดเศษสตางค์ให้ปัดครึ่งขึ้น        -> []
ออกเลขที่ใบกำกับภาษีให้ถูกรูปแบบ       -> []
round satang half up                  -> [money-rounding-thai satang-rounding]
fix invoice numbering                 -> [invoice-numbering]
```

บรรทัดแรกแปลตรงตัวว่า "round baht money into integer satang" เทียบกับ Skill ที่สรุปว่า `Round Thai monetary values half up using satang integers` — ได้ศูนย์ ส่วนคำแปลอังกฤษของ goal เดียวกันดึง Skill ตัวนั้นมาเป็นอันดับหนึ่ง

ยังไม่แก้ retrieval — นั่นเป็นการตัดสินใจ (catalog เป็นไทย หรือ retrieval ข้ามภาษา) แต่ **หยุดรายงานความบอดว่าเป็นความเงียบ** ได้เลย: นับ `turns_goal_script_unmatched` (goal ไม่ใช่ ASCII · catalog เป็น ASCII ล้วน · scorer ได้ศูนย์) แล้วถ้า turn แบบนี้มากกว่า turn ที่ไม่ตรงด้วยเหตุผลอื่น verdict เป็น `retrieval_blind` ไม่ใช่ `insufficient_evidence`

ต่างกันตรงที่ `insufficient_evidence` บอก operator ว่า *"รออีกหน่อย"* ส่วน `retrieval_blind` บอกว่า *"รอไปก็ไม่มา"* — mutation: ถอดเงื่อนไขออกแล้วได้ `verdict = "insufficient_evidence", want retrieval_blind`

##### ปิดแล้ว — เพิ่ม scorer ตัวที่สอง ไม่ใช่แก้ตัวเดิม

เจ้าของเลือก "ใช้ embedder ที่มีแล้ว" เหตุผลคือกลไกที่พิสูจน์กับประวัติสนทนาไปแล้ว — embed, chunk, ชี้ตำแหน่ง — เอามาใช้กับ catalog ได้ตรง ๆ (reachability 70/90 → 90/90 ในการวัดครั้งนั้น) ราคาถูกกว่าตอนเสนอครั้งแรกมาก

`selectSkillBindings` ไม่ถูกแก้ มันกลายเป็น wrapper บาง ๆ ของ `rankSkillBindings(goal, catalog, semantic)` ที่**บวก** คะแนน semantic เข้ากับคะแนน lexical ไม่ใช่แทนที่ เหตุผลเดียวกับ `context_search`: ชื่อ canonical ที่พิมพ์ตรงเป๊ะเป็น substring ที่ vector ทำได้แค่ประมาณ ส่วน paraphrase ข้ามภาษาเป็นสิ่งที่ trigram ทำไม่ได้เลย ทั้งสองตาบอดคนละที่

`skillSemanticBonus` แยกออกมาเป็น pure function เพราะกฎการตัดสินคือส่วนที่ผิดมาแล้วทุกรอบ — **floor แบบ absolute โอนข้ามคำถามไม่ได้** วัดด้วย bge-m3 บนภาษาไทยจริง:

```
คำถามเรื่อง batch size  เทียบ paraphrase ของมัน  0.480
                        เทียบข้อความไม่เกี่ยว     0.419
คำถามเรื่อง plan id     เทียบ paraphrase ของมัน  0.604
                        เทียบข้อความไม่เกี่ยว     0.471
```

การจัดอันดับถูกทั้งสองคำถาม แต่ค่าทับกัน: floor ใด ๆ ที่รับ 0.480 ก็รับ 0.471 ด้วย จึงต้องเป็น floor ที่คิดจากตัวเอง — แต่ **"ค่ากลางของ catalog" ไม่ได้แปลว่า noise floor** และวัดจริงแล้วพังทั้งสองทาง

ทางแรก catalog จริงของ corpus มี 3 ตัว และสองในนั้นเรื่องปัดเศษเหมือนกัน:

```
goal: ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์
      satang-rounding 0.525 · money-rounding-thai 0.547 · invoice-numbering 0.466
```

ค่ากลางคือ 0.525 ซึ่งอยู่**ระหว่างคำตอบที่ถูกสองตัว** ไม่มีใครชนะเกิน margin ผลลัพธ์ว่าง — mutation ยืนยัน: เปลี่ยนกลับเป็นค่ากลาง แล้ววัดกับ bge-m3 จริงได้ `1/3 Thai goals reached the right Skill` เปลี่ยนเป็น quartile ล่างแก้ข้อนี้ได้

ทางที่สองโผล่ตอนขยาย catalog เป็น 8 ตัว **quartile ก็ไม่ใช่ noise floor เหมือนกัน**:

```
goal: วันนี้อากาศเป็นยังไงบ้าง  (ไม่เกี่ยวกับ Skill ตัวไหนเลย)
      release 0.398 · backup 0.405 · pr 0.379 · ... quartile = 0.342
```

goal เรื่องอากาศชนะ quartile ของตัวเองเกิน margin แล้วดึง Skill สองตัวเข้า contract — **การจัดอันดับภายใน catalog มีผู้ชนะเสมอ** มันตอบไม่ได้ว่า "ไม่มีอะไรตรงเลย"

ทางแก้คือ **control** — ประโยคธรรมดา 5 ประโยคสองภาษาที่จงใจไม่เกี่ยวกับ Skill ใด ๆ (แมวนอนบนหลังคา, ตารางเดินรถไฟ, สูตรตุ๋นเนื้อ, ประวัติสะพาน, คอร์ดกีตาร์) embed ครั้งเดียวแล้ว cache เหมือนข้อความอื่น ค่ากลางของ cos(goal, control) คือ "ข้อความที่ไม่เกี่ยวได้เท่าไหร่**สำหรับคำถามนี้**" ซึ่งเป็นตัวเลขที่ catalog ถูกขอให้ตอบแต่ตอบไม่ได้

floor สุดท้ายคือ **ค่าที่สูงกว่าระหว่าง quartile ของ catalog กับค่ากลางของ control** เพราะสองตัวนี้พังคนละทาง: quartile ต่ำเกินเมื่อไม่มีอะไรใน catalog ตรงกับ goal ส่วน control ต่ำเกินเมื่อทุกตัวใน catalog ตรง — catalog ที่เป็นคำอธิบายทางเทคนิคล้วนย่อมชนะประโยคเรื่องแมวทั้งกระดาน

ผลกับ control:

```
วันนี้อากาศเป็นยังไงบ้าง       control med 0.405 · best 0.405  -> []
แมวของผมชอบนอนกลางแดด          control med 0.378 · best 0.354  -> []
เขียนสคริปต์สำรองฐานข้อมูล      control med 0.364 · best 0.655  -> db-backup
```

catalog เล็กแค่ไหนก็ใช้ semantic ได้แล้ว เพราะ floor ไม่ได้มาจากขนาด catalog อีกต่อไป

vector ของ catalog cache ด้วย hash ของข้อความ ไม่ใช่ด้วย skill id — summary ที่แก้แล้วคือข้อความคนละอัน ได้ vector ใหม่โดยไม่ต้องมีใครจำว่าต้อง invalidate และ version ที่ไม่ได้เปลี่ยนคำอธิบายก็ไม่ถูก embed ซ้ำ ผลคือ turn ที่สองจ่ายแค่ embed goal อย่างเดียว

จุดเรียกสามที่ต่อครบ: freeze contract, `skill_search`, และ `SkillRetrievalMetrics` — จุดสุดท้ายสำคัญ เพราะ metric ต้องวัดว่า **ผลิตภัณฑ์** หา Skill เจอไหม ไม่ใช่ว่า scorer ตัวใดตัวหนึ่งหาเจอไหม ถ้าไม่ต่อ metric จะรายงาน `retrieval_blind` ต่อไปทั้งที่ production ไม่บอดแล้ว

mutation ห้าข้อ: ถอด `score += semantic[...]` → goal ไทยกลับไปได้ศูนย์ · baseline เป็นค่าคงที่ 0.40 → noise ของคำถาม plan ได้คะแนน 5 · ปิด cache → batch ที่สองมี 4 ข้อความแทนที่จะเป็น 1 · **ถอด control floor → วัดกับ bge-m3 จริง goal เรื่องอากาศดึง `db-backup` มา goal เรื่องแมวดึง `satang-rounding` มา** · ถอด catalog floor → control ต่ำเกินจนทุกตัวผ่านหมด

**และปิดช่องที่ทำให้ทั้งหมดนี้ไม่มีผลจริง**: `SetEmbedder` ไม่เคยถูกเรียกจาก `main.go` เลย กลไก semantic ทุกอย่างที่สร้างมาทำงานแต่ใน test กับ taskeval เท่านั้น ตอนนี้ `serve` มี `--embed-url` / `--embed-model` / `--embed-api-key-env` / `--embed-dimensions` — credential เก็บเป็น**ชื่อ** env var ตามสถาปัตยกรรม ไม่ใช่ค่า

วัดกับ bge-m3 จริงแล้ว ไม่ใช่แค่ fake — `TestRealEmbedderCrossesScripts` ข้าม test ไปเงียบ ๆ ถ้าไม่ตั้ง `HERMETRIX_EMBED_URL` เพราะ test ที่ผ่านตอนไม่มีโมเดลคือคำอ้างที่ไม่มีหลักฐาน:

```
HERMETRIX_EMBED_URL=http://127.0.0.1:11434/v1 go test ./internal/agent/ \
  -run TestRealEmbedderCrossesScripts -v

ปัดเศษเงินบาทเป็นจำนวนเต็มสตางค์  -> money-rounding-thai
แก้การปัดเศษสตางค์ให้ปัดครึ่งขึ้น   -> satang-rounding
ออกเลขที่ใบกำกับภาษีให้ถูกรูปแบบ    -> invoice-numbering
```

สามในสาม goal ที่เคยได้ `[]` ทั้งหมด และ goal ที่ไม่เกี่ยวได้ `[]` ทั้งกับ catalog 3 ตัวและ 8 ตัว

`conceptEmbedder` ยังอยู่ มันพิสูจน์การต่อสายและกฎการตัดสินโดยไม่ต้องมีโมเดล ส่วน test ตัวจริงพิสูจน์ว่าโมเดลจริงจับคู่ข้ามภาษาได้ — **สองอย่างนี้ตอบคนละคำถาม อันไหนหายไปก็เหลือคำอ้างที่ไม่มีหลักฐานครึ่งหนึ่ง**

#### V-7 — producer ที่ยิงไม่ออก คือโรคเดียวกับที่กำลังไล่ปิด

รอบแรกผมทำ `approval_required` ที่ยังไม่มีคำตอบให้กลายเป็น `open_task` ด้วย ดูสมเหตุสมผล มี mutation ผ่าน แล้ว census จากสนามจริงหลังต่อ producer ได้:

```
decision   max=1     <- เดิม 0 จาก 772 snapshot
open_task  max=0
```

`decision` โผล่จริง `open_task` ไม่โผล่ ตรวจว่าทำไม ไม่เดา:

```
approval_858d45f7 session_state=awaiting_approval events_after_request=0
approval_d1e7eba7 session_state=awaiting_approval events_after_request=0
```

pending approval ทั้งสองตัวใน corpus อยู่ใน session ที่ค้าง **ไม่มี event เกิดหลังคำขอเลย** แล้วลองกับ gateway จริง — ปล่อย approval ไว้ไม่ตอบ แล้วเปิด turn ที่สอง:

```
turn2 -> failed  session is awaiting_approval with active turn turn_54e37bb1...;
                 only one turn may commit
```

การขอ approval ทำให้ session ค้างที่ `awaiting_approval` โดยยังถือ turn lease อยู่ **ไม่มี compile เกิดขึ้นเลยระหว่างที่คำขอค้าง** fragment ที่ผมเขียนจึงไปถึง request ไม่ได้ตลอดกาล

ถอดทิ้ง **producer ที่ยิงไม่ออกแย่กว่าไม่มี** เพราะมันทำให้ kind ดูเหมือนมีคนดูแล — ซึ่งคือโรคเดียวกับที่ไล่ปิดมาทั้ง session และผมเพิ่งเป็นเอง เหลือไว้เป็นเทสต์ที่ยืนยันข้อเท็จจริงเรื่อง reachability แทน: ถ้าวันหนึ่ง `TestNoCompileRunsWhileAnApprovalIsOutstanding` ล้ม แปลว่า flow เปลี่ยนแล้ว `open_task` มีที่มาจริง

`approvalCase` ใน fidelity corpus ก็ถอด open task ออกด้วย — corpus ที่ถือ kind ที่ระบบไม่เคยผลิต คือวิธีที่ corpus นี้เคยกลายเป็นที่เดียวใน Hermetrix ที่ decision มีอยู่

**ยังเหลือ `acceptance_criteria`** — ตัวนี้ไม่มีแหล่งที่ deterministic และมีอันตรายเฉพาะตัว: `sliceFor` ส่งมันเข้า slice `pinned` ซึ่ง**ทิ้งไม่ได้และ fail-closed** ถ้าเกิน `PinnedBudget` (2,048 บน compact-32k) `Compile` คืน `ErrPinnedOverflow` แล้ว **ทั้ง turn ล้ม** ไม่ใช่แค่ตัดของทิ้ง ผู้ผลิตอะไรก็ตามที่ต่อเข้ากับ kind นี้ต้องมีเพดานของตัวเอง เป็นเรื่องที่ต้องตัดสินใจ ไม่ใช่เลือกเอง

#### P8-A ปิดแล้ว — gate ของ semantic reviewer ผ่าน

100 เคส · 4 family · 31 evidence shape · label โดยผม audit โดยเจ้าของ **20/20 เห็นด้วย**

อ่านสองรอบกับ gateway จริง ตัดสินที่รอบที่แย่กว่า:

```
positives 55 · proposed 52 · recall 0.94   เกณฑ์ 0.60   ผ่าน
false proposals 4 / 45 = 0.09              เพดาน 0.10   ผ่าน
invented evidence 0                        ไม่มี tolerance ผ่าน
reviewer errors 0
recall 0.94–0.96 · 5 เคสตอบไม่เหมือนเดิม
```

การแก้คำสั่ง reviewer ทำครั้งเดียวและวัดครั้งเดียว:

| family | recall ก่อน | หลัง | false |
|---|---:|---:|---:|
| explicit_learn | 0.64 | **1.00** | 0 |
| repeated_correction | 0.56 | **0.96** | 0 |
| successful_milestone | 0.20 | 0.60 | 2 |
| skill_failure | — | — | 2 |

**margin ด้าน precision เหลือเคสเดียว** — 4/45 = 0.089 อีกหนึ่งเป็น 0.111 ซึ่งตก

false ทั้งสี่เป็นรูปแบบเดียวกันจากสอง family: หลักฐานที่**พูดถึง**ขั้นตอนโดยไม่ได้**ทำ** สองอันเป็นคำถามเชิงขั้นตอนซึ่งคำสั่งบอกให้ปฏิเสธชัดเจน อีกสองคือการสำรวจที่ล้มกลางคัน

นี่คือภาพสะท้อนกลับด้านของความล้มเหลวเดิม corpus จึงกลายเป็นบันทึกของ reviewer ที่**เอียงไปทางกระตือรือร้นเกินเล็กน้อย** แทนที่จะเข้มเกินไป

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
