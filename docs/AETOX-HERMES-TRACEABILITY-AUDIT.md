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
| **O-9** | runtime evidence ไม่มีทางกลายเป็น Skill candidate ได้เลย | **critical** | เปิด |
| **O-10** | system prompt ไม่เคยบอก model ว่ามี Skill catalog อยู่ | high | **แก้แล้ว** |
| **O-11** | output reserve ไม่รู้จัก reasoning token | high | **บรรเทาแล้ว** — turn ที่ถูกตัดถูกติดธงพร้อมสัดส่วน reasoning; การจัดสรร reserve ยังไม่แก้ |
| **O-12** | `/api/` path ที่ไม่ match route คืน HTML 200 | high | **แก้แล้ว** |
| **O-13** | tool-call arguments ที่พังถูก replay กลับไปหา provider ทำให้ทั้ง turn ตาย | high | **แก้แล้ว** |

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

#### O-11 — output reserve ไม่รู้จัก reasoning token *(high)*

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
