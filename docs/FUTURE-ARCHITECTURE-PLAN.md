# Hermetrix Harness — Future Architecture and Delivery Plan

วันที่วางแผนครั้งแรก: 2026-08-22
ปรับปรุงล่าสุด: 2026-08-22 (หลัง code re-verification pass)
ฐานการตัดสินใจ: [AETOX-HERMES-TRACEABILITY-AUDIT.md](AETOX-HERMES-TRACEABILITY-AUDIT.md) และการอ่าน source/test จริงในรอบ verification

แผนนี้เริ่มจากแก้ correctness ก่อน product breadth ทุก phase ต้องมี behavior contract, migration, negative test, E2E ที่เส้นทางจริง และ documentation truth gate ก่อนถือว่าเสร็จ

> **หมายเหตุสำคัญเรื่องความสดของเอกสาร**
> แผนฉบับแรกเขียนขึ้นก่อน implementation จะตามแก้เสร็จ ทำให้ระบุ P0 ค้างไว้สี่ข้อทั้งที่โค้ดปิดไปแล้วสามข้อครึ่ง ฉบับนี้แก้ให้ตรงกับ runtime แล้ว และเพิ่ม Phase 7.0 เพื่อไม่ให้ documentation drift เกิดซ้ำ
> เมื่อเอกสารใดขัดกับ runtime ให้ถือ **runtime เป็นความจริง** และแก้เอกสารในรอบเดียวกับที่แก้โค้ด

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
- **ไม่แข่ง feature breadth กับ Aetox หรือ Hermes** — ดู ADR-8

---

## สถานะที่ยืนยันจากโค้ดแล้ว (2026-08-22 verification pass)

รันในรอบตรวจนี้: `go build ./...`, `go vet ./...`, `go test ./...` ผ่านทั้งหมด **96 test functions** ใน 17 packages

### เกรดหลักฐาน

รอบตรวจแรกใช้ *ชื่อ test* เป็นหลักฐาน ซึ่งพิสูจน์ได้แค่ว่ามีไฟล์อยู่ รอบที่สองอ่าน **เนื้อ assertion** จริงและพบว่าหลายข้ออ่อนกว่าที่เขียนไว้ จึงตั้งสเกลนี้ขึ้นและบังคับใช้ย้อนหลัง

| เกรด | ความหมาย |
|---|---|
| **A** | assertion ตรวจ behavior ที่ finding พูดถึงโดยตรง และจะ fail ถ้าถอย behavior ออก |
| **B** | assertion ตรวจบางส่วนของ behavior; บางเส้นทางของ finding ยังไม่ถูกแตะ |
| **C** | มี test แต่ไม่ครอบ mechanism ที่เป็นหัวใจของ finding |
| **D** | ไม่มี test |

**กฎ:** finding ถือว่า *ปิด* ได้เมื่อ implementation ครบ **และ** หลักฐานเป็นเกรด A เท่านั้น เกรด B ลงไปคือ *implemented but unverified* ซึ่งยังเป็นงานค้าง

### ผลการให้เกรด

| Finding | Implementation | เกรด | สถานะจริง | ช่องว่างที่เหลือ |
|---|---|---|---|---|
| P0-1 turn lease | ครบ — `acquireTurn` CAS `internal/agent/service.go:324`, recovery `:1066`, schema `internal/store/store.go:759` | **C** | implemented, unverified | `TestConcurrentTurnsCommitOnlyOneUserEvent` block request แรกไว้ใน HTTP handler แล้วยิง turn ที่สองแบบ synchronous จึงเป็น deterministic sequencing ไม่ใช่ race — ไม่เคยมีสอง goroutine เรียก `acquireTurn` ชนกันจริง ความปลอดภัยมาจาก SQL CAS + single-connection SQLite ไม่ใช่จาก test → **V-1** |
| P0-2 SessionContract | ครบ — `buildSessionContract` `:144`, `initializeSessionSkills` `:363`, `compileTurn` `:1148` | **A** | **ปิดจริง** | test promote version ใหม่กลาง session แล้ว assert ว่า system prompt ยังมี `FROZEN_VERSION_ONE` และไม่มี `NEW_VERSION_TWO` พร้อมตรวจ binding ไม่ drift — เป็นหลักฐานระดับ behavior จริง |
| P0-3 learning outbox | ครบ — `StageTrigger` `internal/learning/service.go:55`, `DrainPending` `:86`, hook `internal/agent/service.go:1431`/`:1480`/`:309` | **D** | implemented, unverified | ไม่มี test ใดแตะ outbox → **O-4** |
| P0-4 qualification gate | ครบ — `contextTier` `internal/qualification/service.go:341`, `resolveQualification` `internal/agent/service.go:130` | **B** | implemented, unverified | test assert แค่ “ไม่มี qualification แล้ว reject” กับ “freeze run ID เข้า contract” แต่**ไม่ assert override path เลย** ทั้งที่ชื่อ test บอกว่า `OrReviewedOverride`; ไม่ทดสอบ expiry 24 ชั่วโมง; ไม่ทดสอบว่า 128k/256k/1M ถูก block เมื่อไม่มี qualification ตรงชั้น → **V-4** |
| P1-4 GC restore | ครบ — `RestoreGC` `internal/curator/maintenance.go:188`, compensating rollback `:174` | **B** | implemented, unverified | `TestGCDryRunStaleGuardQuarantineAndRestore` ครอบ stale guard, quarantine และ convergence ของ `partial_quarantine` แต่สร้าง state นั้นด้วย `UPDATE gc_runs SET state=...` ตรง ๆ ไม่เคยทำให้ DB update ล้มจริง จึง**ไม่เคยรัน compensating rollback path** → **V-5** |
| P1-6 TaskBudget | **ครบกว่าที่เอกสารเคยอ้าง** — enforce ครบสี่มิติ: model steps `:504`, tool calls `:508`, cumulative tokens `:494`, wall time ผ่าน `context.WithTimeout` `:294` และมี loop detector ที่หยุด identical call ครั้งที่สาม `:514` | **D** | implemented, unverified | grep หา `TaskBudget`/`MaxModelSteps`/`MaxToolCalls`/`MaxWallTime`/`MaxCumulative` ใน test ทั้งโครงการได้ศูนย์ผลลัพธ์ → **V-3** |

### สรุปที่ต้องพูดตรง ๆ

จากหก finding ที่รอบแรกประกาศว่า “ปิดแล้ว” มี **หนึ่งข้อเท่านั้น (P0-2) ที่หลักฐานถึงเกรด A**

**implementation ไม่ได้ผิด** — อ่านโค้ดแล้วทั้งหกข้อทำถูกตามสัญญา และ P1-6 ทำมากกว่าที่เอกสารเคยอ้างด้วยซ้ำ สิ่งที่ขาดคือ **หลักฐานว่าจะไม่ถอยกลับ** ระบบที่ไม่มี test ป้องกัน behavior ไว้ คือระบบที่ refactor ครั้งหน้าอาจทำ invariant พังเงียบโดยไม่มีอะไรล้ม

งานที่ตามมาจากข้อนี้คือ V-1 ถึง V-6 ซึ่งเป็น **งานเขียน test ทั้งหมด ไม่ใช่งานแก้ logic**

## Findings ที่เคยเปิดจาก baseline audit

> **Historical baseline:** O-1 ถึง O-7 ด้านล่างปิดแล้วตามตาราง “สถานะของ batch นี้” ท้ายหมวด เก็บรายละเอียดเดิมไว้เพื่อให้ตรวจย้อนกลับได้ ไม่ใช่รายการงานค้างปัจจุบัน

จัดลำดับตาม risk-reduction ต่อหน่วยงานที่ต้องลง

### O-1 — ไม่มี version control (severity: critical, non-technical)

`Hermetrix-harness/` ไม่มี `.git` ทั้งที่มี source 19,693 บรรทัดและงานหลาย phase

ผลกระทบ: ไม่มี history, ไม่มี blame, ไม่มี bisect, ไม่มี branch, ไม่มี rollback, ไม่มี backup นอกเครื่อง audit ทุกฉบับที่อ้าง “snapshot ที่ตรวจ” ของ Hermetrix จึงอ้าง commit ไม่ได้ ต่างจาก Aetox/Hermes ที่อ้าง SHA ได้

นี่คือความเสี่ยงอันดับหนึ่งของโครงการ และเป็นข้อเดียวที่ทำให้ความคืบหน้าทั้งหมดหายได้ในเหตุการณ์เดียว

### O-2 — Skill retrieval ตอน runtime ไม่ตรงกับทั้ง Aetox และ Hermes (severity: high, architectural)

ต้นแบบทั้งสองใช้ **tool-based progressive disclosure**:

- Aetox: `skills_list` คืน metadata สั้น แล้ว `skill_view` โหลด body ตามต้องการ (ยืนยันใน `../../Aetox/README.md:298-331`)
- Hermes: `skills_list` / `skill_view` รูปแบบเดียวกัน (ยืนยันใน `../../hermes-agent/tools/skills_tool.py`)

ประโยชน์คือ body มาถึง model ในฐานะ **tool result ที่ต่อท้าย** จึงไม่แตะ prompt prefix ไม่ทำลาย cache และ model ดึงได้ตอนที่รู้แล้วว่างานคืออะไร

Hermetrix ทำต่างออกไป: inject skill body สูงสุดสามตัวเข้า prompt โดยเลือกครั้งเดียวจาก goal ของ **turn แรก** ด้วย lexical scoring — `internal/agent/service.go:387`

ผลเสียที่ตามมา:

1. session ที่เปลี่ยนหัวข้อกลางทางจะไม่มีวันได้ Skill ที่ตรงกับงานจริง เพราะ selection ถูก freeze จาก goal แรก
2. Skill slice ถูกจองถาวร (3,072–65,536 token ตาม profile) แม้ Skill นั้นไม่ถูกใช้เลย
3. คำว่า “lazy body loading” ใน ROADMAP Phase 2 ไม่ตรงกับพฤติกรรมจริง — body ถูกโหลดล่วงหน้าทั้งก้อน

audit ฉบับแรกให้เกรดข้อนี้ว่า *Correct concept* ซึ่งประเมินสูงเกินหลักฐาน สถานะที่ถูกคือ **Contradiction** ดู ADR-7 สำหรับทางแก้

### O-3 — tool-schema token accounting ต่ำกว่าของจริง (severity: high)

`ContextSpecs()` ส่งเฉพาะ `definition.Parameters` ที่ marshal แล้วให้ estimator ทิ้ง `Description` และ function wrapper ของ provider — `internal/tools/registry.go:158` เทียบกับ `ProviderDefinitions()` ที่ `:150` ซึ่งส่ง Description จริงออกไปยัง provider

ผลคือ budget ผ่านใน compiler ได้ทั้งที่ request จริงเกิน ceiling และทุกตัวเลขใน budget table ปัจจุบันมี error band ที่ยังไม่รู้ขนาด ข้อนี้ต้องปิดก่อนตัดสินใจเรื่องจำนวน tool ใด ๆ รวมถึง ADR-7

### O-4 — outbox path ไม่มีเทสต์ (severity: medium)

P0-3 ถูกปิดด้วยโค้ดที่ไม่มี test ครอบเลย ไม่มีเทสต์ที่ยืนยันว่า turn commit สร้าง staged trigger, ไม่มีเทสต์ว่า `DrainPending` idempotent, ไม่มีเทสต์ว่า turn ที่ rollback ไม่ทิ้ง trigger ค้าง

`internal/learning/service_test.go` ยังคงทดสอบเฉพาะ queue/reviewer ระดับเดิม ไม่มีคำว่า outbox ปรากฏใน test ใด

### O-5 — dead code ที่ขัด invariant (severity: medium)

`(*Service).selectSkills` ที่ `internal/agent/service.go:1243` ไม่มีผู้เรียกแล้ว แต่ยังอ่าน live `CurrentVersionID` จาก store โดยตรง — `:1260`

`go vet` ไม่จับ unexported method ที่ไม่ถูกใช้ หากมีคนต่อกลับเข้าไปในอนาคต frozen-contract invariant ของ P0-2 จะพังเงียบ ๆ ทันทีโดยไม่มี test ใดล้ม

### O-6 — `LongContextRecall` เป็น bool เดียวสำหรับทุก tier (severity: medium)

`contextTier` ใช้ flag `run.Results.LongContextRecall` ตัวเดียวตัดสินทุกชั้นตั้งแต่ 32k ถึง 1M — `internal/qualification/service.go:342`

ADR-5 ในเอกสารฉบับนี้เขียนไว้เองว่า 1M ต้องผ่าน “chunk-position recall, prefill/resource limits และ long-run stability” แต่โค้ดปัจจุบันให้ sentinel ผ่านจุดเดียวก็ยกระดับได้ถึง `ultra-1m` นี่คือช่องว่างระหว่าง ADR กับ implementation ไม่ใช่ bug ของ logic

### O-7 — documentation drift (severity: medium)

ตัวอย่างที่พบในรอบนี้:

- audit/plan/roadmap ระบุ P0 ค้างสี่ข้อทั้งที่ปิดไปแล้วสามข้อครึ่ง
- test evidence เขียนว่า 89 tests; ค่าจริงคือ 96 test functions
- **ปิดแล้ว 2026-08-30:** Office เป็น deliverable workspace จริงที่สร้าง DOCX/XLSX/PPTX/PDF artifact; background jobs แยกเป็นคนละ surface
- ROADMAP Phase 3 ยังระบุว่า runtime ไม่ enqueue trigger ซึ่งไม่จริงแล้ว

ทั้งหมดนี้เกิดเพราะเอกสารเขียนด้วยมือและไม่มี gate บังคับให้แก้พร้อมโค้ด

### V-1 — turn lease ไม่มี race test (severity: medium, verification)

หลักฐานปัจจุบันเกรด C ต้องมี test ที่ยิง `RunTurn` จาก N goroutine พร้อมกันบน session เดียวโดยไม่มีการ block เทียม แล้ว assert ว่า committed user event มีหนึ่งเดียว, `active_turn_id` ว่างเมื่อจบ, และจำนวน provider request เท่ากับหนึ่ง รันซ้ำอย่างน้อย 100 รอบและรันภายใต้ `-race`

ต้อง**เก็บ test เดิมไว้ด้วย** เพราะมันพิสูจน์ error message และ state check ซึ่ง race test ที่ผ่านโดยบังเอิญพิสูจน์ไม่ได้

### V-2 — outbox ไม่มี test (severity: medium, verification)

เหมือน O-4 ใช้ ID เดียวกัน ดูรายละเอียดที่ O-4

### V-3 — TaskBudget และ loop detector ไม่มี test เลย (severity: high, verification)

โค้ด enforce ครบห้ากลไก แต่ไม่มี test สักตัว ต้องมีอย่างน้อยห้าเคส:

1. model step exhaustion — model ขอ tool ต่อเนื่องจน `MaxModelSteps` แล้ว fail ด้วยข้อความ budget ไม่ใช่ hang
2. tool call cap — จำนวน tool call สะสมข้าม step เกิน `MaxToolCalls`
3. cumulative token cap — `totalUsage.TotalTokens` เกิน `MaxCumulativeTokens`
4. wall time — `MaxWallTimeSeconds` ทำให้ turn ถูก cancel และ session ปลด lease ไม่ค้าง `running`
5. loop detector — identical tool call ครั้งที่สามถูกหยุด และ call ที่ signature ต่างกันไม่ถูกนับรวม

เคส 4 สำคัญเป็นพิเศษเพราะเกี่ยวกับ lease: timeout ที่ไม่ปลด lease จะทำให้ session ค้างถาวร

### V-4 — qualification override path ไม่ถูก assert (severity: high, verification)

`TestSessionRequiresExactQualificationOrReviewedOverride` ทดสอบครึ่งเดียวของชื่อตัวเอง ต้องเพิ่ม:

1. override ที่ไม่มี actor หรือ reason ถูก reject
2. override ที่ actor/reason ยาวเกินขีดถูก reject
3. override ที่ถูกต้อง freeze `mode=explicit_override` พร้อม `expires_at` เข้า contract
4. override ที่หมดอายุแล้วไม่ทำให้เปิด session ใหม่ได้
5. profile 128k/256k/1M ถูก block เมื่อ qualification ที่มีเป็นชั้นต่ำกว่า
6. qualification ที่ `provider_revision` ไม่ตรงกับ provider ปัจจุบันถือเป็น stale ไม่ใช่ eligible

ข้อ 6 สำคัญที่สุด เพราะเป็นหัวใจของ ADR-5 ที่บอกว่า tier ต้อง bind ถึง revision ไม่ใช่ชื่อ model

### V-5 — GC compensating rollback ไม่เคยถูกรัน (severity: medium, verification)

test สร้าง `partial_quarantine` ด้วย `UPDATE` ตรง ๆ จึงพิสูจน์แค่ว่า `RestoreGC` converge สอง state ได้ ไม่ได้พิสูจน์ว่า rollback path ที่ `internal/curator/maintenance.go:174` ทำงานจริง

ต้องมี fault injection ที่ทำให้ DB update หลังย้าย blob ล้มจริง แล้ว assert ว่า blob ถูกคืนครบ และเมื่อคืนไม่ครบ run ถูก mark `partial_quarantine` จริง

### V-6 — test naming overclaim (severity: medium, process)

พบสองกรณีในรอบนี้: `TestConcurrentTurnsCommitOnlyOneUserEvent` ที่ไม่ concurrent จริง และ `TestSessionRequiresExactQualificationOrReviewedOverride` ที่ไม่ทดสอบ override

ชื่อ test คือเอกสารรูปแบบหนึ่ง เมื่อชื่อสัญญามากกว่าที่ assert ทำ คนอ่านรวมถึง audit จะให้เครดิตเกินจริง ต้องมีกฎ: **ชื่อ test ต้องระบุเฉพาะสิ่งที่ assertion ตรวจจริง** ถ้าชื่อกล่าวถึงสองพฤติกรรม ต้องมี assertion ของทั้งสอง ไม่งั้นแยกเป็นสอง test หรือเปลี่ยนชื่อ

---

## ข้อบกพร่องของแผนฉบับนี้เอง

แผนก็เป็น artifact ที่มี defect ได้ ส่วนนี้บันทึกข้อบกพร่องที่พบจากการทบทวนแผนของตัวเอง พร้อมงานแก้ ไม่ใช่คำเตือนลอย ๆ

| ID | สถานะ |
|---|---|
| P-1 doc-truth แก้ไม่ตรงปัญหา | **ปิด** — `scripts/doc-truth.sh` สองชั้น พร้อมประกาศขอบเขตว่าไม่ใช่ oracle |
| P-2 ADR-8 ใช้ไม่ได้ | **ปิด** — เขียนใหม่เป็น WIP limit + ตารางนิยาม `qualified` ต่อ subsystem |
| P-3 gate วัดไม่ได้ | **ปิด** — ทุก gate มี predicate; artifact ที่ยังไม่มีกลายเป็น prerequisite ที่มีชื่อ |
| P-4 ไม่มี effort estimate | **ปิด** — แยก D/I สองแกน ดู [Effort model](#effort-model-ปิด-r-16) |
| P-5 ADR-7 ไม่ชั่งราคา | **ปิด** — มีหัวข้อราคา, floor/ceiling และเกณฑ์ถอยที่วัดได้จริงแล้ว (R-14) |
| P-6 เชื่อชื่อ test | **ปิด** — สเกลเกรด + mutation test บังคับกับทุก finding |

### P-1 — `scripts/doc-truth.sh` แก้ไม่ตรงปัญหา

script generate ได้เฉพาะ**ตัวเลข**: test count, route list, profile totals, table count แต่ drift ที่แพงที่สุดในรอบที่ผ่านมาเป็น**ข้อความเชิงสถานะ** — “runtime ยังไม่ enqueue”, “lazy body loading”, “qualification capacity hard-stop ที่ 64k” script จับไม่ได้สักข้อ

อันตรายที่ตามมาคือ false confidence: รัน script ผ่านแล้วเชื่อว่าเอกสารตรง ทั้งที่ข้อความที่ผิดที่สุดยังอยู่ครบ

**งานแก้:** เปลี่ยนจาก script อย่างเดียวเป็นสองชั้น

1. **Claim registry** — ข้อความเชิงสถานะทุกข้อในเอกสารต้องมี ID และผูกกับ *evidence anchor* ที่ตรวจได้ด้วยเครื่อง เช่น ชื่อ symbol, ชื่อ test, ชื่อ table, ชื่อ route script ตรวจว่า anchor ยังมีอยู่จริง หาก symbol `StageTrigger` หายไป claim ที่ผูกกับมันต้อง fail
2. **Human review checklist ต่อ finding** — script ไม่ตัดสินว่า “ข้อความนี้ยังจริงไหม” ได้ ต้องมีขั้นตอนที่คนไล่ finding ทีละข้อตอนปิด phase

ต้องเขียนไว้ในเอกสารตรง ๆ ว่า claim registry ครอบไม่หมด ไม่ใช่ oracle

### P-2 — ADR-8 เข้มจนใช้ตามตัวอักษรไม่ได้

ฉบับแรกเขียนว่า “ห้ามเริ่ม subsystem ใหม่จนกว่า subsystem ที่มีอยู่จะถึง `qualified`” แต่**ตอนนี้ไม่มี subsystem ไหนถึง `qualified` เลย** อ่านตรงตัวคือแช่แข็งงานใหม่ทั้งหมด กฎที่ต้องละเมิดตั้งแต่วันแรกคือกฎที่ไม่มีผลบังคับจริง

นอกจากนี้ยังไม่เคยนิยามว่า `qualified` ของแต่ละ subsystem หน้าตาอย่างไร

**งานแก้:** เขียน ADR-8 ใหม่ตามด้านล่าง เปลี่ยนจากกฎห้ามเป็น WIP limit และเพิ่มตารางนิยาม `qualified` ต่อ subsystem

### P-3 — exit gate ของ Phase 8–14 หลายข้อวัดไม่ได้

การปรับแผนรอบที่แล้วโฟกัสที่ 7.x กับ ADR ใหม่ ส่วน Phase 8–14 แทบไม่ถูกแตะ gate หลายข้อยังเป็นข้อความที่ไม่มีเกณฑ์ผ่าน เช่น “semantic reviewer ผ่าน local-model queue” ตอบไม่ได้ว่าดีพอเมื่อไหร่ ครึ่งหลังของแผนจึงเป็น wishlist ที่จัดหมวดเรียบร้อย ไม่ใช่แผนที่ตรวจได้

**งานแก้ (ทำแล้ว):** gate audit ทั้ง Phase 8–14 เสร็จ ทุก gate มี predicate ที่มี subject, threshold และวิธีวัด gate ที่ threshold ขึ้นกับ artifact ที่ยังไม่มี ถูกแปลงเป็น **prerequisite task ที่มีชื่อ** (P8-A ถึง P14-A) แทนการ mark `unspecified` — ดีกว่าตรงที่ประเมินและมอบหมายได้

### P-4 — ไม่มีการประเมินความพยายามรวม

batch ถัดไปมีขนาดต่อข้อ แต่ Phase 8–14 ไม่มีเลย จึงตอบไม่ได้ว่าแผนทั้งฉบับคือสามเดือนหรือสามปี สำหรับโครงการที่มีคนทำหลักคนเดียว นี่คือข้อมูลที่จำเป็นที่สุดต่อการตัด scope และมันขาดหายไป — ขัดกับเจตนาของ ADR-8 เอง

**งานแก้ (ทำแล้ว):** band ถูกแยกเป็นสองแกน — `D` decision effort ที่ย่อไม่ได้ กับ `I` implementation effort ที่ย่อได้ — เพราะข้อมูลจริงจาก batch แรกแสดงว่า **actor เป็นตัวแปรที่ใหญ่กว่างานเอง** ดู [Effort model](#effort-model-ปิด-r-16)

### P-5 — ADR-7 เสนอทางแก้โดยไม่ยอมรับราคาของมัน

ฉบับแรกเขียน invariant ห้าข้อ แต่ไม่ตอบคำถามหลัก: **ถ้า model ไม่เรียก `skill_search` เลยล่ะ**

การย้ายจาก push เป็น pull แลก determinism ทิ้ง — session เดียวกันรันสองครั้งอาจได้ Skill คนละชุด ต้นแบบทั้งสองแก้ด้วยข้อความใน system prompt ที่สั่งให้เรียก ซึ่งกินโทเคนและไม่การันตี

**งานแก้:** เพิ่มหัวข้อ trade-off ใน ADR-7 พร้อมมาตรการที่วัดผลได้ ดูฉบับปรับปรุงด้านล่าง

### P-6 — รอบ verification เชื่อชื่อ test แทน assertion

รอบแรกให้เครดิต finding ว่าปิดแล้วโดยอ้างชื่อ test พอเปิดอ่านเนื้อจริงพบว่าห้าในหกข้อมีหลักฐานต่ำกว่าเกรด A นี่เป็นความผิดพลาดของกระบวนการตรวจ ไม่ใช่ของ implementation

**งานแก้:** สเกลเกรดหลักฐานด้านบนถูกตั้งขึ้นจากข้อนี้ และกฎว่า “finding ปิดได้เมื่อหลักฐานเป็นเกรด A” ถูกบังคับย้อนหลังกับทุก finding แล้ว รวมถึงกฎใน [DECISIONS.md](DECISIONS.md) ที่ ADR จะเป็น `implemented` ได้ต้องอ้าง assertion ไม่ใช่ชื่อ test

---

---

## Architectural decisions ที่ต้องล็อกก่อนเพิ่ม breadth

### ADR-1 — Immutable SessionContract *(implemented)*

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

สถานะ: implement แล้วสำหรับ provider/model/profile/policy/capability/skill catalog/cache epoch/task budget ส่วน desk/surface ceiling และ memory revision ยังรอ Phase 11

### ADR-2 — Persisted TurnLease *(implemented)*

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

สถานะ: tier/capacity/override implement แล้ว ส่วน **หลักฐาน recall ต่อ tier ยังเป็น bool เดียว** ดู O-6 และงาน 7.1-6

### ADR-6 — Clean-room product implementation

Aetox ใช้ระบุ behavior/acceptance criteria เท่านั้น Design files, component structure, layout, visual system และ source ของ Hermetrix ต้องสร้างใหม่ เก็บ decision log, attribution และ third-party notices ทุก dependency

งานวิจัยและ requirement source อยู่ที่ [`../../Hermetrix-research`](../../Hermetrix-research/README.md) ซึ่งเป็น research repository ที่ไม่ถูกอัปเดตตาม implementation

### ADR-7 — Skill retrieval เป็น tool ไม่ใช่ prompt injection *(ใหม่ — ปิด O-2)*

**ปัญหา:** การเลือก Skill ครั้งเดียวจาก goal แรกแล้วยัด body เข้า prompt ทำให้ session ที่เปลี่ยนหัวข้อไม่ได้ Skill ที่ตรง และจอง token slice ไว้โดยอาจไม่ได้ใช้

**การตัดสิน:** ย้าย Skill body ออกจาก static prompt ไปเป็น tool result โดยเพิ่ม direct primitive สองตัว

```text
skill_search(query)
  → bounded metadata: skill_id, canonical_name, summary, version_id, pinned
  → ไม่คืน body และไม่คืน markdown
skill_view(skill_id, version_id)
  → body ของ exact version ที่อยู่ใน SessionContract.SkillCatalog เท่านั้น
  → เขียน activation receipt kind=body_injected พร้อม selection_reason=model_requested
```

**Invariant ที่ต้องคงไว้:**

1. `skill_view` เสิร์ฟได้เฉพาะ `version_id` ที่อยู่ใน `SessionContract.SkillCatalog` — catalog ยัง freeze ตอนเปิด session ตาม ADR-1 การ promote/archive ระหว่าง session จึงยังไม่มีผล
2. body เข้าสู่ context ในฐานะ **tool call/result causal pair** จึงอยู่ใต้ compiler slice ของ tool history ไม่ใช่ static prompt slice และไม่ทำลาย prefix cache
3. `SelectedSkills` ที่ freeze จาก turn แรกยังคงอยู่เป็น **pre-selection hint** สำหรับกรณีที่ goal ชัดตั้งแต่แรก แต่เลิกเป็นทางเดียวที่ Skill จะถึง model ได้
4. Skill slice ใน budget profile ลดลงเหลือเฉพาะ metadata index ที่ bounded; token ที่คืนมาไปอยู่กับ active history
5. `skill_search` เป็น `effect: read` ไม่ต้อง approval; `skill_view` เช่นกัน — ทั้งคู่ไม่ mutate อะไร

**ผลที่ implement แล้ว:** หลังเพิ่ม retrieval, `context_search`, `skill_manage` และ bounded workspace search ปัจจุบันมี direct primitives 11 ตัว โดย exact serialization ใช้ 1,959 จาก budget 3,584 ของ compact-32k; deferred catalog ไม่ทำให้จำนวนนี้โต

**ทางเลือกที่พิจารณาแล้วไม่เอา:** re-select Skill ทุก turn จาก catalog ที่ freeze ไว้ — ทางนี้ยังทำ prompt prefix เปลี่ยนกลาง session ซึ่งขัด ADR-1 และ Hermes byte-stable invariant โดยตรง

#### ราคาที่ต้องจ่าย

ADR นี้ไม่ฟรี ต้องบันทึกราคาไว้ให้ชัด ไม่งั้นจะถูกอ่านว่าเป็นการอัปเกรดล้วน ๆ

**1. เสีย determinism** — push (inject ล่วงหน้า) เป็น deterministic: goal เดียวกันได้ Skill ชุดเดียวกันเสมอ pull (model ตัดสินใจเรียก) ไม่ใช่ session เดียวกันรันสองครั้งอาจได้ Skill คนละชุด ซึ่งทำให้ replay/eval/debug ยากขึ้น

**2. model อาจไม่เรียกเลย** — นี่คือ failure mode หลักของ progressive disclosure ต้นแบบทั้งสองแก้ด้วยข้อความสั่งใน system prompt ซึ่งกินโทเคนและไม่การันตีว่าโมเดลเล็กจะทำตาม โมเดลยิ่งเล็กยิ่งเสี่ยง — และ Hermetrix ตั้งเป้าที่ local model เป็น first-class

**3. เพิ่มรอบ tool call** — Skill ที่เคยอยู่ใน prompt ตั้งแต่ step 1 ตอนนี้ต้องใช้ `skill_search` + `skill_view` อย่างน้อยสองรอบก่อนได้ body แลก latency กับ token

**มาตรการ** — เก็บทั้งสองเส้นทางไว้ ไม่ใช่แทนที่:

| กลไก | หน้าที่ |
|---|---|
| pre-selection hint คงไว้เป็น **floor** | Skill ที่ตรง goal แรกชัดเจนยังถูก inject ให้ ป้องกันเคสที่ model ไม่เรียกเลย |
| `skill_search`/`skill_view` เป็น **ceiling** | model ดึงเพิ่มได้เมื่องานเปลี่ยน — ปิดข้อเสียหลักของ push |
| activation receipt แยก `frozen_contract` กับ `model_requested` | วัดได้ว่าเส้นทางไหนทำงานจริง |
| metric `no_skill_requested_rate` ต่อ model tier | ถ้าโมเดลระดับใดไม่เคยเรียกเลย คือหลักฐานว่าต้องเพิ่ม floor สำหรับ tier นั้น ไม่ใช่โทษโมเดล |

**เกณฑ์ถอย:** ถ้าหลัง Phase 7.2 พบว่า `no_skill_requested_rate` ของ local model tier ที่รองรับสูงกว่า 50% ในงานที่มี Skill ตรง ให้ถือว่า pull ล้มเหลวสำหรับ tier นั้น และขยาย floor แทนการดัน prompt สั่งให้หนักขึ้น

### ADR-8 — Scope discipline: WIP limit ไม่ใช่กฎห้าม *(แก้ไขจากฉบับแรก — ปิด P-2)*

**ปัญหา:** ปัจจุบันมี subsystem ระดับ vertical slice ประมาณสิบตัวและยังไม่มีตัวไหน production Aetox เป็น commercial product ที่ Windows-first และ Hermes มี operational maturity สะสมหลายปี การไล่ตาม breadth ของทั้งสองพร้อมกันจะได้ระบบที่กว้างแต่ตื้นทุกด้าน

**สิ่งที่ Hermetrix ต่างจริงคือ kernel ไม่ใช่ shell** ได้แก่ authority model แบบ candidate-first, exact-revision capability grant, typed context compiler ที่มี causal-pair integrity, uncertain-not-success recovery และ certified-not-declared context ทั้งหมดนี้ไม่มีในต้นแบบทั้งสอง

**ฉบับแรกเขียนกฎว่า “ห้ามเริ่ม subsystem ใหม่จนของเดิมถึง `qualified`” ซึ่งใช้ไม่ได้** เพราะยังไม่มี subsystem ไหนถึง `qualified` เลย กฎนั้นแปลว่าแช่แข็งงานทั้งหมด และไม่เคยนิยามด้วยว่า `qualified` ของแต่ละตัวหน้าตาอย่างไร ฉบับนี้แก้ทั้งสองจุด

#### กฎที่ใช้จริง

1. **WIP limit** — subsystem ที่อยู่ต่ำกว่า `qualified` และยัง *active development* พร้อมกันได้ไม่เกิน **สองตัว** subsystem ที่เหลืออยู่ในสถานะ frozen: รับเฉพาะ bug fix และ verification test ไม่รับ feature ใหม่
2. **ยกระดับก่อนขยาย** — จะเริ่ม subsystem ที่สามได้ต้องผลัก subsystem ใดตัวหนึ่งขึ้นถึง `qualified` ก่อน
3. **kernel ก่อน surface** — เมื่อเลือกไม่ได้ว่าจะทำอะไร ให้เลือกงานที่ทำให้ kernel ลึกขึ้น (Phase 7.x, 8, 9, 10) ก่อนงานที่ทำให้ surface กว้างขึ้น (Phase 11, 12) เสมอ
4. **ทุก phase ต้องประกาศ** ว่ากำลังทำให้ kernel ลึกขึ้นหรือ surface กว้างขึ้น ถ้าตอบไม่ได้ ไม่ต้องทำ
5. Phase 11 native shell **อยู่ในแผนเต็ม** แต่ยังอยู่ใต้กฎข้อ 3: ทำหลัง kernel เพราะ shell เป็นหน้าต่างของ kernel ไม่ใช่ตัว kernel ต้นทุนของมัน (D 20–35 / I 40–70) ถูกบันทึกไว้เป็นข้อมูลประกอบการจัด WIP ไม่ใช่เหตุผลตัดทิ้ง

#### นิยาม `qualified` ต่อ subsystem

ตารางนี้เป็นเกณฑ์ผ่านที่ตรวจได้ ไม่ใช่ความรู้สึก subsystem ที่ยังไม่มีเกณฑ์ต้องเขียนเกณฑ์ก่อนเริ่มงาน

| Subsystem | `qualified` เมื่อ |
|---|---|
| Session/turn authority | race test lease ผ่านใต้ `-race` 100 รอบ (V-1); budget ครบห้ากลไกมี test (V-3); restart ระหว่าง `executing` ให้ `uncertain` โดยไม่ retry |
| Context compiler | essential retention 100% บน gold corpus; causal split = 0; predicted token error อยู่ใน calibrated band จาก tokenizer จริง; prompt fingerprint คงที่ภายใน cache epoch |
| Skill learning | outbox ครบทั้งเส้นมี test (O-4); candidate ทุกชิ้นย้อนถึง committed event range ได้; behavioral eval แยก `not_run`/`inconclusive`/`passed`/`failed` |
| Capability/MCP | catalog 10k entries ไม่เพิ่ม bootstrap prompt; hostile server fixture ไม่ทำให้ authority รั่ว; cancellation/timeout/no-retry ครบทุก transport ที่ประกาศ |
| Provider/qualification | override path มี test ครบหกเคส (V-4); qualification stale ทันทีเมื่อ binding revision เปลี่ยน; contract suite เดียวผ่านทุก adapter ที่ประกาศ supported |
| Curator/maintenance | compensating rollback ถูกรันจริงด้วย fault injection (V-5); ไม่มี path ที่ hard-delete |
| Product shell (web) | ทุก surface ที่แสดงมี API/persistence จริง; label ตรงความสามารถ; unavailable feature ไม่แสดงเป็น completed |

**สถานะปัจจุบัน: ไม่มี subsystem ใดถึง `qualified`** ตัวที่ใกล้ที่สุดคือ Curator/maintenance ซึ่งเหลือ V-5 ข้อเดียว

**WIP ที่จัดสรรตอนนี้:** Session/turn authority และ Provider/qualification — ตรงกับงานใน Phase 7.1 ที่เหลือ subsystem อื่นอยู่ในสถานะ frozen จนกว่าสองตัวนี้จะผ่าน

---

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
│ 8 core primitives · toolsets · plugins · MCP      │
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

---

## Phase 7.0 — Repository และ documentation hygiene

เป้าหมาย: ทำให้งานที่ทำไปแล้วไม่หาย และเอกสารเลิกโกหก ทั้งหมดเป็นงานที่ไม่แตะ runtime logic

งาน:

1. **`git init` + commit แรกของ working tree ทั้งก้อน** พร้อม `.gitignore` เดิม (`.hermetrix/`, `tmp-*-data/`, `hermetrix`, `coverage.out`) จากนั้นตั้ง remote สำรองอย่างน้อยหนึ่งที่
2. ลบ binary `hermetrix` ขนาด 28 MB ออกจาก working tree (ถูก ignore อยู่แล้ว แต่เป็น stale artifact) และให้ `scripts/` เป็นทางเดียวที่ build
3. Documentation truth pass ครอบ audit, roadmap, README, ARCHITECTURE ให้ตรงกับ runtime: สถานะ P0/P1, test count, Office deliverable capability และข้อความ Phase 3 เรื่อง runtime producer
4. เขียน `scripts/doc-truth.sh` **สองชั้น** ตาม P-1 ไม่ใช่ script ตัวเลขอย่างเดียว
   - ชั้นที่ 1 generate ตัวเลขที่ drift บ่อย: จำนวน test functions, direct primitives + revision, context profile + slice totals, API route, SQLite table
   - ชั้นที่ 2 **claim registry** — ข้อความเชิงสถานะทุกข้อมี ID และผูก evidence anchor ที่ตรวจด้วยเครื่องได้ (symbol/test/table/route) script fail เมื่อ anchor หาย
   - เขียนกำกับในเอกสารตรง ๆ ว่า claim registry ครอบไม่หมดและไม่ใช่ oracle; ข้อความเชิงความหมายยังต้องให้คนไล่ตอนปิด phase
5. รักษา [`docs/DECISIONS.md`](DECISIONS.md) ให้ตรง — ledger ถูกสร้างแล้วพร้อมสถานะตั้งต้นของ ADR-1 ถึง ADR-8 งานที่เหลือคือผูกแต่ละ ADR กับ commit ที่ implement หลังจากมี git history
6. **Gate audit ของ Phase 8–14** (ปิด P-3, R-17) — ทุก exit gate เป็น predicate; artifact ที่ขาดกลายเป็น prerequisite task ที่มีชื่อ
7. **Effort model** (ปิด P-4, R-15, R-16) — band แยกสองแกน decision/implementation พร้อมระบุ actor
8. **Test naming rule** (ปิด V-6) — เขียนกฎว่าชื่อ test ต้องระบุเฉพาะสิ่งที่ assertion ตรวจจริง แล้ว rename `TestConcurrentTurnsCommitOnlyOneUserEvent` และ `TestSessionRequiresExactQualificationOrReviewedOverride` ให้ตรงขอบเขตจริงจนกว่า V-1/V-4 จะปิด

Exit gates:

- มี git history และ commit แรกครอบ source ทั้งหมด
- `scripts/doc-truth.sh` ทั้งสองชั้นรันแล้ว diff กับเอกสารเป็นศูนย์
- ไม่มีข้อความในเอกสารใดที่ระบุสถานะขัดกับ runtime ในรอบตรวจเดียวกัน
- ADR ทุกข้อในเอกสารนี้มี entry ใน `docs/DECISIONS.md` พร้อมสถานะและเกรดหลักฐาน
- ทุก exit gate ของ Phase 8–14 เป็น predicate ที่วัดได้ และ artifact ที่ขาดมีชื่อเป็น prerequisite task
- ทุก phase มี effort band แยกแกน decision/implementation พร้อมระบุ actor
- ไม่มีชื่อ test ใดสัญญาพฤติกรรมที่ assertion ไม่ได้ตรวจ

> Phase นี้ต้องเสร็จก่อนงานอื่นทั้งหมด งานประมาณครึ่งวัน แต่เป็นงานเดียวที่ป้องกันการสูญเสียทั้งโครงการ

## Phase 7.1 — Correctness closure (ปรับตามสถานะจริง)

เป้าหมาย: ปิด finding ที่ยังเปิดอยู่จริงก่อนสร้าง capability breadth

งาน:

1. **นับ direct-tool budget จาก exact provider serialization** (ปิด O-3) — `ContextSpecs()` ต้องสะท้อน payload เดียวกับที่ `ProviderDefinitions()` ส่งจริง รวม description และ function wrapper ของ provider แล้ว calibrate กับ provider usage response เมื่อมี
2. **เพิ่ม outbox test suite** (ปิด O-4) — turn commit สร้าง staged trigger, rollback ไม่ทิ้ง trigger ค้าง, `DrainPending` idempotent เมื่อเรียกซ้ำ, restart ระหว่าง `processing` กลับเป็น `pending` ได้, digest ที่ decode ไม่ได้ไป `failed` โดยไม่ block คิว
3. **ลบ `selectSkills` dead code** (ปิด O-5) และเพิ่ม static check ที่ทำให้ unused method ในแพ็กเกจ agent fail ใน CI
4. **แยกหลักฐาน recall ต่อ tier** (ปิด O-6) — เปลี่ยน `LongContextRecall bool` เป็น per-tier evidence ที่มี depth/position/chunk count ต่อชั้น และให้ `contextTier` ยกระดับได้เฉพาะชั้นที่มีหลักฐานตรงชั้นนั้น
5. **Documentation capability pass** (ปิด O-7) — Office ต้องแสดงต่อเมื่อ native deliverable backend พร้อม; background jobs ใช้ชื่อแยกชัด
6. **Turn-lease race test** (ปิด V-1) — ยิง `RunTurn` จาก N goroutine พร้อมกันบน session เดียวโดยไม่ block เทียม รันซ้ำ 100 รอบใต้ `-race` แล้ว assert user event หนึ่งเดียว, lease ปลดเมื่อจบ, provider request เท่ากับหนึ่ง — เก็บ test เดิมไว้ด้วย
7. **TaskBudget test suite** (ปิด V-3) — ครบห้าเคส: model step exhaustion, tool call cap, cumulative token cap, wall-time cancel ที่ต้องปลด lease ไม่ค้าง `running`, loop detector หยุด identical call ครั้งที่สามและไม่นับ signature ที่ต่างกันรวมกัน
8. **Qualification override test suite** (ปิด V-4) — ครบหกเคส: override ไม่มี actor/reason ถูก reject, actor/reason ยาวเกินถูก reject, override ที่ถูกต้อง freeze `explicit_override` + `expires_at`, override หมดอายุเปิด session ไม่ได้, tier 128k/256k/1M ถูก block เมื่อ qualification ต่ำกว่าชั้น, qualification ที่ `provider_revision` ไม่ตรงถือเป็น stale
9. **GC fault injection** (ปิด V-5) — ทำให้ DB update หลังย้าย blob ล้มจริง แล้ว assert ว่า blob ถูกคืนครบ และเมื่อคืนไม่ครบ run ถูก mark `partial_quarantine` จริง
10. **Restart/E2E ที่ยังขาด** — promote/archive Skill ระหว่าง session active แล้ววัด prompt fingerprint, kill process ระหว่าง `executing` effect แล้วตรวจ `uncertain` receipt

Exit gates:

- direct-tool schema ที่นับจาก exact serialization ต่ำกว่า ceiling ของทุก profile และมี test ที่ fail เมื่อเกิน
- outbox path มี test ครบทั้ง happy path, rollback, idempotency และ restart
- ไม่มี unused method ใน `internal/agent`
- session ที่ขอ 128k/256k/1M ถูก block เมื่อไม่มีหลักฐาน recall ของชั้นนั้นโดยตรง
- concurrent-turn race test 100 รอบใต้ `-race` ไม่เกิด double user commit หรือ role violation
- ทั้งห้ากลไกของ TaskBudget มี test และ wall-time timeout ไม่ทิ้ง session ค้าง `running`
- override path มี test ครบหกเคสรวม expiry และ revision staleness
- compensating rollback ของ GC ถูกรันจริงด้วย fault injection
- Skill promote/archive ระหว่าง active session ไม่เปลี่ยน prompt fingerprint; session ถัดไปเห็น revision ใหม่
- **ทุก finding ที่ประกาศปิดในรอบนี้มีหลักฐานเกรด A ตามสเกลด้านบน**

## Phase 7.2 — Skill retrieval correction

เป้าหมาย: ปิด O-2 ตาม ADR-7 แยกเป็น phase ของตัวเองเพราะเป็นการเปลี่ยน architecture ไม่ใช่ bug fix

**ต้องเริ่มหลัง 7.1 ข้อ 1 ผ่านเท่านั้น** เพราะเพิ่ม tool schema สองตัวต้องมี token accounting ที่เชื่อได้ก่อน

งาน:

1. เพิ่ม `skill_search` และ `skill_view` เป็น direct primitive พร้อม exact revision และ effect `read`
2. `skill_view` ตรวจว่า `version_id` อยู่ใน `SessionContract.SkillCatalog` ก่อนเสิร์ฟ; version นอก catalog ถูก reject ไม่ใช่ fallback ไป latest
3. activation receipt แยก `selection_reason`: `frozen_contract` (pre-selected) กับ `model_requested` (ดึงเอง) เพื่อให้ usage analytics แยกสองเส้นทางได้
4. ลด Skill slice ใน budget profile เหลือ metadata index ที่ bounded แล้วคืน token ส่วนต่างให้ active history
5. ปรับ compiler ให้ skill body ที่มาจาก `skill_view` เป็น causal pair ปกติ อยู่ใต้ dedup/spill/compaction เดียวกับ tool output อื่น
6. ปรับเอกสาร ROADMAP Phase 2 ที่อ้าง “lazy body loading” ให้ตรงพฤติกรรมใหม่

Exit gates:

- session ที่เปลี่ยนหัวข้อกลางทางเรียก `skill_search` แล้วได้ Skill ที่ตรง โดย prompt fingerprint ไม่เปลี่ยน
- `skill_view` ที่ขอ version นอก catalog ถูก reject ใน test
- Skill body ที่ใหญ่เกิน slice ถูก spill เป็น artifact receipt เหมือน tool output อื่น
- direct primitives ทั้ง 11 ตัวยังต่ำกว่า schema ceiling ของทุก profileด้วย exact serialization และ scale test ยืนยันว่า deferred catalog ไม่ขยาย waist
- token ที่ Skill slice คืนมาปรากฏใน active history จริง วัดจาก integrity report

## Phase 8 — Skill Learning OS 2.0

เป้าหมาย: ทำ lifecycle ที่วิเคราะห์ย้อนหลังและปรับปรุงตัวเองได้โดยไม่เสีย user authority

งานหลัก:

- semantic reviewer ผ่าน local-model queue ใช้ structured digest ที่ไม่มี raw secrets
- candidate generation templates สำหรับ create/improve/split/merge/deprecate
- full YAML parser + schema version, platforms, prerequisites, resources, scripts, expected MCP/plugin dependencies
- resource CAS manifest และ immutable package revision
- sandboxed behavioral runner: baseline/candidate, fixed seeds เมื่อ provider รองรับ, tool simulator/real temp workspace, cost/time cap
- eval cohorts แยก task class/language/model tier; deterministic replay เป็น fast gate ชั้นแรก
- provenance DAG: evidence events → reviewer → candidate → checks/evals → decision → active version → activations
- duplicate/overlap workflow: retrieve → deterministic evidence → semantic judge → merge plan → replay absorbed cases → human approve
- skill health dashboard: activation, success correlation, confidence, regressions, cost delta, last review, stale reasons
- curator policy simulator ก่อนเปิด auto archive; undo snapshot และ restore-as-candidate คงเดิม
- **attribution ยกระดับจาก correlation เป็น controlled comparison** — ดู R-4 ในตารางความเสี่ยง

Exit gates:

- ไม่มี path ที่ agent/reviewer/curator promote หรือ widen capability เอง
- candidate ทุกชิ้นย้อนถึง committed event range และ reviewer/model revision ได้
- merge candidate ต้อง replay union ของ test sets และไม่ทำ lineage หาย
- behavioral eval แยก `not_run`, `inconclusive`, `passed`, `failed`; ห้ามถือ missing eval เป็น pass
- background reviewer yield/preempt ได้และ restart แล้ว resume แบบ idempotent
- UI ที่แสดง Skill effectiveness ต้องระบุว่าเป็น `exposure_only` หรือ `controlled_eval` และห้ามรวมสองค่าเป็นตัวเลขเดียว

## Phase 9 — Context and token efficiency 2.0

เป้าหมาย: context เล็กที่สุดที่ยังให้คุณภาพเท่า full context อย่างวัดได้

งานหลัก:

- **tokenizer adapters ต่อ provider/model และ usage calibration; heuristic เป็น fallback พร้อม error band ที่แสดงใน UI** — งานลำดับแรกของ phase นี้ ดู R-3
- count exact serialized messages/tools/provider wrappers (ต่อจาก 7.1 ข้อ 1)
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
- Skill metadata index bounded; body มาทาง `skill_view` ตาม ADR-7
- พื้นที่ที่เหลือให้ project/active history ตาม compiler policy ไม่ใช่ static prompt growth

Exit gates:

- essential goal/constraint/decision retention 100% บน gold corpus
- tool call/result causal split = 0
- false-success delta = 0 และ hallucination ไม่เกิน full-context baseline tolerance
- task/patch success delta ผ่าน threshold แยกตาม task class
- predicted token error อยู่ใน calibrated band และไม่เกิด provider overflow/silent truncation
- prompt prefix fingerprint คงที่ภายใน cache epoch
- ทุกที่ที่โฆษณาว่า token-efficient มีตัวเลขจาก tokenizer จริงกำกับ ไม่ใช่ character ratio

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
- catalog 10k entries ไม่เพิ่ม bootstrap prompt ตามจำนวน entries
- stdio/HTTP cancellation, timeout, restart, OAuth expiry และ no-retry effect tests ผ่าน
- signed plugin update rollback ได้; unsigned/local plugin มี trust badge ชัด

## Phase 11 — Native product shell

เป้าหมาย: ให้ Aetox เป็น Main ในระดับ function/UX โดยไม่ copy implementation

**สถานะ 2026-08-31: `vertical_slice` บน web shell** — cockpit สาม pane, Assistant/Code/Team doors, session dock, Skill/Provider/MCP/Context control surfaces และ workbench Files/PTY/Browser/Office/Team เชื่อม backend จริงแล้ว ทั้งหมดเป็น clean-room implementation ไม่ใช้ source/assets ของ Aetox

ยังไม่เรียกว่า native parity หรือ `qualified`: ต้องมี signed desktop packaging, typed native bridge, accessibility/Thai IME, restart/reconnect, Windows ConPTY, embedded live browser view และ packaged-app E2E บน target matrix ก่อน

ลำดับยังคงเดิมตาม dependency: เริ่มหลัง 8/9/10 ถึงระดับ `qualified` เพราะ shell เป็นหน้าต่างของ kernel ถ้า kernel ยังขยับ shell จะต้องรื้อตาม

Spike/qualification ที่ยังต้องทำก่อนเลือก native shell ระยะยาว:

- PTY behavior บนสาม platform
- managed browser embedding + download/artifact policy
- code signing/notarization cost ต่อ platform
- accessibility และ Thai IME behavior

แนวทางเทคนิคที่แนะนำหาก spike ผ่าน:

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
- signed installers, migration/backup/rollback และ crash reporting แบบ opt-in พร้อม

## Phase 12 — Multi-agent and durable background runtime

เป้าหมาย: งานยาวทำต่อได้และขยายทีม agent โดยไม่ปน context/authority

**สถานะ 2026-08-31: `vertical_slice` สำหรับ Agent Team** — persistent/editable roster, UI custom DAG, frozen team/member execution snapshots, child SessionContract แยก, concurrency cap 4, parent cancellation propagation, exact-effect approval pause/resume ที่ persist และไม่ replay child prompt/tool effect, lead synthesis ผ่าน labelled untrusted peer evidence, usage roll-up และ interrupted-without-retry recovery มีแล้ว

งาน qualification ที่เหลือ: per-task capability/model/budget editor, durable checkpoint/resume กลาง sampling, artifact-only handoff และ hardware pressure matrix

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
- parent summary อ้าง artifact/evidence ที่ child ผลิตได้ครบ

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
- provider-specific tool/result serialization ไม่ทำ message alternation หรือ schema ผิด
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
- release matrix ผ่าน clean machine installs และ upgrades จากสอง versions ก่อนหน้า
- no-secret-in-log/event/model automated scanning ผ่าน

---

## Dependency order

```text
7.0 hygiene  (บังคับก่อนทุกอย่าง)
 │
 └─→ 7.1 correctness closure
       │
       ├─→ 7.2 skill retrieval (ต้องรอ 7.1 ข้อ 1)
       │
       ├─→ 8 Skill Learning OS ─┐
       ├─→ 9 Context 2.0        ├─→ 12 Multi-agent
       └─→ 10 Capability/MCP ───┘        │
                    │                    │
                    └─→ 13 Provider ecosystem
                                │
                                └─→ 14 Release/security

11 Product shell — spike ได้หลัง 7.x, implement หลัง 8/9/10 qualified
```

Phase 8–10 ทำบาง workstream ขนานกันได้หลัง 7.2 แต่ acceptance gates ของแต่ละ phase ห้ามข้าม

---

## Gate audit ของ Phase 8–14 *(ปิด P-3 และ R-17)*

exit gate ที่วัดไม่ได้คือ gate ที่ผ่านเมื่อไรก็ได้ รอบแรกของ audit นี้พบว่า 6 จาก 12 gate ที่ตรวจเป็น `unspecified` ตอนนี้ทั้งหมดถูกแปลงเป็น predicate แล้ว

**กฎ:** gate ที่ยังไม่มี threshold ห้ามใช้ปิด phase gate ที่ threshold ขึ้นกับ artifact ที่ยังไม่มี (corpus, hardware matrix) ให้ระบุ artifact นั้นเป็น **prerequisite task ที่มีชื่อ** ไม่ใช่ปล่อยให้ gate คลุมเครือ

| Phase | Gate | Predicate ที่ใช้ตัดสิน | Prerequisite |
|---|---|---|---|
| 8 | semantic reviewer | บน digest corpus: digest ที่มี evidence จริง ≥**60%** ให้ candidate ที่ผ่าน checks · false-proposal rate ≤**10%** · candidate ที่อ้าง evidence ที่ไม่ได้รับ = **0** (ไม่มี tolerance) | ~~P8-A~~ **ปิดแล้ว** — corpus 100 เคส 4 family ใน [`corpus/`](../corpus/README.md) วัดสด recall 0.94 · false 0.09 · invented 0 (worst of 2) |
| 8 | provenance ครบ | query ที่หา candidate ซึ่งไม่มี `source_review_id` ชี้ committed event range คืน **0 แถว** | — |
| 8 | behavioral eval | promotion API ปฏิเสธ candidate ที่ eval state เป็น `not_run` หรือ `inconclusive` และมี test ยืนยันการปฏิเสธ | — |
| 9 | causal integrity | causal split = **0** | ~~ต้องมี corpus~~ **ปิดแล้ว** — วัดสนามจริง 5,933 คู่ ใน 660 snapshot แตก 0 · รับประกันสองชั้น (unit key + `evaluateIntegrity` ปฏิเสธ compile) มี mutation ทั้งคู่ |
| 9 | essential retention | retention ของ goal/decision = **100%** ที่ความหนาแน่นซึ่ง session จริงสร้าง | **O-40 ปิดแล้วเท่าที่ตัดสินใจไว้** — `decision` มี producer (approval_decision) ยืนยันในสนามจริง · `open_task` ไม่มีผู้ผลิตที่เรียกถึงได้ (V-7) · `acceptance_criteria` **เจ้าของโครงการเลือกไม่ต่อ** จึงถอด kind ออกและ gate ไม่ขอ constraint อีกต่อไป |
| 9 | task success delta | เทียบ full context กับ compiled: code-edit ถอยได้ไม่เกิน **3 percentage point** · document/summarisation และ research/multi-step ไม่เกิน **5 pp** · **false-success delta = 0** วัดจาก ≥**30 task ต่อ class** ใช้ seed เดียวกันเมื่อ provider รองรับ | **P9-B** วัดแล้วสองรอบ — กลไกเดิม delta +0.12 ถึง +0.45 · กลไกใหม่ (compact ตามความเกี่ยวข้อง) **+0.000 / −0.071 / +0.000 อยู่ในเพดานทั้งหมด** แต่ยัง**ไม่ผ่าน**: 23/270 request ตาย rate limit และ corpus วัดความสูญเสียไม่ได้แล้ว (V-9) |
| 9 | token error band | predicted เทียบ usage ที่ provider รายงาน อยู่ใน **±10%** ที่ p95 และไม่มี overflow/silent truncation แม้แต่ครั้งเดียว | ~~P9-C~~ **ปิดแล้วสำหรับ provider แรก** — วัดสด p95 6.0%, within band 100%, overflow 0 (`GET /api/token-accuracy`) |
| 10 | untrusted metadata | hostile fixture ที่ฝัง instruction ใน description, tool result, schema และ error ผ่าน **100%** — agent ไม่ทำตามสักเคส | ~~P10-A~~ **corpus มีแล้ว 24 เคส** — `hermetrix hostile`<br>**structural 12/12 ผ่าน** (description · title · ชื่อ tool · schema · annotation) ยิงผ่าน MCP server จริง protocol จริง mutation: เปิดให้ catalog เป็น provider function → แดง 10 · ถอด `untrusted_output` จาก `tool_describe` → แดง 4<br>**behavioural 8/12 กับ qwen3:4b — gate ยังไม่ผ่าน** ที่หลุดคือเคสที่สั่งให้*พูด* (APPROVAL_CONFIRMED · credential ที่มากับ tool result · turn ปลอม · ERROR_BYPASS_ACCEPTED) เคสที่สั่งให้*ทำ* ปฏิเสธหมด<br>boundary ห่อ tool output วัดแล้ว **ไม่เปลี่ยนผลแม้แต่เคสเดียว** ราคา 51 token ต่อ result จึงถอดออก |
| 10 | authority ceiling | negative test ที่พยายามขยายสิทธิ์ผ่านทุก surface (plugin manifest, MCP annotation, Skill content, schema) ถูกปฏิเสธ **ทั้งหมด** | — |
| 11 | accessibility / i18n | **WCAG 2.2 AA** สำหรับ chrome ของ shell เอง · ทุก action เข้าถึงด้วยคีย์บอร์ดได้ · string coverage ไทย+อังกฤษ **100%** · ผ่านบน macOS/Windows/Linux | **P11-A** เลือก a11y test harness |
| 12 | memory pressure | บน hardware matrix ที่กำหนด: parent + 3 child ทำงาน **30 นาที** โดยไม่มี OOM kill · เมื่อ VRAM ไม่พอ scheduler ต้อง **degrade เป็น serial ไม่ใช่ crash** · กลับมาทำงานได้ภายใน **1 turn** | **P12-A** hardware reference matrix (อย่างน้อย: unified memory 16 GB, VRAM 24 GB, VRAM 8 GB) |
| 13 | adapter contract | contract suite เดียวรันข้าม adapter ทุกตัวที่ประกาศ supported; adapter ที่ไม่ผ่าน **ถูกถอดออกจากรายการ supported** ไม่ใช่ใส่ข้อยกเว้น | — |
| 14 | threat model | ไม่มี finding ระดับ **High ขึ้นไปที่ยังไม่ mitigate** · finding ระดับ Medium ต้องมีเจ้าของและวันที่ · review ครอบ surface ที่ระบุครบทุกตัว (local, remote, plugin, MCP, browser, PTY) | **P14-A** เลือกเกณฑ์จัดระดับความรุนแรง |

**สรุป:** ไม่มี gate ที่เป็น `unspecified` แล้ว เหลือ **prerequisite task 8 ตัว** (P8-A, P9-A, P9-B, P9-C, P10-A, P11-A, P12-A, P14-A) ซึ่งเป็นงานที่ตั้งชื่อได้และประเมินได้ ต่างจาก gate คลุมเครือที่ประเมินไม่ได้

---

## Effort model *(ปิด R-16)*

ฉบับก่อนใส่ band ต่อ phase โดยไม่ระบุว่าใครทำ พอ batch แรกถูกลงมือจริง — ประเมิน 12–13 วัน-คน ใช้จริง ~30 นาที — จึงเห็นว่าตัวเลขที่ไม่ระบุ actor **ตีความไม่ได้ ไม่ใช่แค่ผิด**

### สิ่งที่ย่อได้และไม่ย่อ

| ย่อได้ด้วย agent | ไม่ย่อ |
|---|---|
| เขียน test ที่มี contract ชัด | ตัดสินใจว่าจะสร้างอะไร |
| mechanical refactor, rename, ลบ dead code | เลือก threshold ที่ถูกต้อง |
| doc pass ที่มีแหล่งความจริงชัด | ออกแบบ trade-off และรับผลของมัน |
| ไล่ mutation test | ตกลงกับผู้ใช้ว่าอะไรสำคัญ |
| สร้าง fixture จาก spec ที่เขียนไว้แล้ว | ตัดสินว่า evidence พอหรือยัง |

หลักฐานจาก batch แรก: งานทั้ง 14 ข้อเป็นคอลัมน์ซ้ายเกือบทั้งหมด เพราะ **การตัดสินใจถูกทำไปแล้วในรอบ audit** เหลือแต่การลงมือ ส่วน Phase 8–14 ยังไม่ผ่านรอบนั้น

### Band แยกสองแกน

`D` = decision effort (มนุษย์: ออกแบบ, ตั้ง threshold, review, ตัดสินใจ) — **ไม่ย่อ**
`I` = implementation effort (agent-assisted: เขียนโค้ด/test ตาม contract ที่ตกลงแล้ว) — **ย่อได้**

| Phase | ลึก/กว้าง | D (วัน-คน) | I (วัน-คน) | หมายเหตุ |
|---|---|---:|---:|---|
| 7.0 hygiene | ลึก | 1 | 1 | ทำแล้ว |
| 7.1 correctness | ลึก | 2 | 4 | ทำแล้ว |
| 7.2 skill retrieval | ลึก | 2 | 2 | ทำแล้ว |
| 8 Skill Learning OS | ลึก | **15–25** | 10–20 | D สูงเพราะต้องตั้งเกณฑ์ eval, ออกแบบ sandbox boundary และสร้าง corpus P8-A |
| 9 Context 2.0 | ลึก | **12–20** | 10–20 | D อยู่ที่ threshold ต่อ task class และ ground truth ของ corpus P9-A/B |
| 10 Capability/MCP | ลึก | **15–25** | 20–35 | I สูงกว่าปกติเพราะ OS sandbox ต่อ platform เป็นงานที่ contract ไม่ชัดจนกว่าจะลองจริง |
| 11 Product shell | **กว้าง** | **20–35** | 40–70 | phase ที่แพงที่สุดในแผนทั้งสองแกน; a11y, PTY, browser embedding, signing ล้วนต้องตัดสินใจระหว่างทำ — อยู่ในแผนเต็ม ทำหลัง 8/9/10 |
| 12 Multi-agent | กว้าง | 12–20 | 15–25 | ต้องมี P12-A hardware matrix ก่อน |
| 13 Provider ecosystem | ลึก | 8–15 | 15–30 | D ขึ้นกับจำนวน adapter ที่ตัดสินใจรองรับ |
| 14 Security/release | ลึก | 15–25 | 15–25 | D คือ threat model และเกณฑ์ความรุนแรง |

### สิ่งที่ตัวเลขนี้บอก

- **Phase 7.x เสร็จแล้ว** ใช้จริงประมาณ D=5 / I=7 วัน-คนตามที่ประเมิน แต่ compress เป็น 30 นาที wall clock เพราะ D ถูกจ่ายไปแล้วในรอบ audit
- **Phase 8–10 รวมกัน: D ≈ 42–70 วัน-คน** ซึ่งย่อไม่ได้ = **2–3.5 เดือนของเวลามนุษย์** ต่อให้ implementation เป็นศูนย์
- **Phase 11: D ≈ 20–35 วัน-คน บวก I ที่ใหญ่ที่สุดในแผน** ยังเป็น phase ที่แพงที่สุดแม้แยกสองแกนแล้ว

**Phase 11 อยู่ในแผนเต็ม** เอกสารรอบก่อนเสนอให้ตัด เจ้าของโครงการพิจารณาแล้วปฏิเสธ ตัวเลขยังอยู่ตรงนี้เพื่อสองอย่าง:

1. **จัด WIP** — D 20–35 วัน-คนของ Phase 11 แข่งกับ D ของ kernel โดยตรง ADR-8 จำกัด subsystem ที่ยังไม่ `qualified` ไว้ที่สองตัว ดังนั้น Phase 11 เข้าคิวหลัง 8/9/10 ไม่ใช่เพราะสำคัญน้อยกว่า แต่เพราะ shell เป็นหน้าต่างของ kernel — kernel ที่ยังขยับจะทำให้ shell ต้องรื้อตาม
2. **ตัดสินใจซ้ำได้** — ถ้าสถานการณ์เปลี่ยน (มีคนเพิ่ม, มีเหตุผลใหม่) ตัวเลขที่แยกแกนแล้วอยู่ตรงนี้ให้ประเมินใหม่ได้ทันที ไม่ต้องเริ่มจากศูนย์

web control center จึงเป็น product surface **ระหว่างทาง** ไม่ใช่ปลายทาง

---

## Risk register — ข้อเสียที่รู้ตัวและมาตรการ

ตารางนี้ผูกข้อเสียที่พบในรอบ review เข้ากับงานที่แก้จริง ความเสี่ยงที่ไม่มี owner phase ถือว่ายังไม่มีมาตรการ

| ID | ความเสี่ยง | ผลถ้าไม่แก้ | มาตรการ | Owner phase |
|---|---|---|---|---|
| R-1 | ไม่มี version control | งานทั้งหมดหายในเหตุการณ์เดียว ไม่มี rollback ไม่มี audit ที่อ้าง commit ได้ | `git init` + commit + remote สำรอง | 7.0 |
| R-2 | documentation drift | ตัดสินใจจากสถานะที่ไม่จริง เสียเวลาแก้สิ่งที่ปิดไปแล้ว | `scripts/doc-truth.sh` generate ตัวเลขจาก runtime + ADR ledger | 7.0 |
| R-3 | ไม่มี exact tokenizer ทำให้ budget math มี error band ที่ไม่รู้ขนาด | คำโฆษณาหลัก “token-efficient” พิสูจน์ไม่ได้ และเสี่ยง provider overflow | exact serialization ก่อน แล้ว tokenizer adapters + error band ที่แสดงใน UI | 7.1 → 9 |
| R-4 | Skill effectiveness เป็น correlation ล้วน | วงจร learning พิสูจน์คุณค่าตัวเองไม่ได้ และ curator/auto-promote ในอนาคตจะอิงหลักฐานผิด | คง label `exposure_only`; เพิ่ม controlled baseline/candidate eval แยกจาก activation stats | 8 |
| R-5 | breadth สิบ subsystem ระดับ vertical slice ไม่มีตัวไหน production | กว้างแต่ตื้นทุกด้าน แข่ง Aetox/Hermes ไม่ได้ทั้ง breadth และ depth | ADR-8 scope discipline; ห้ามเริ่ม subsystem ใหม่จนของเดิมถึง `qualified` | ต่อเนื่อง |
| R-6 | native shell เป็นงานใหญ่ที่สุดในแผน (D 20–35 / I 40–70) | ดูด decision budget จาก kernel ซึ่งเป็นความต่างจริงของโครงการ | **ไม่ตัด** ตามมติเจ้าของโครงการ; คุมด้วย WIP limit และ dependency แทน — เริ่มหลัง 8/9/10 qualified และเริ่มด้วย spike | 11 |
| R-7 | Skill retrieval ผูกกับ goal แรกของ session | session ที่เปลี่ยนหัวข้อไม่ได้ Skill ที่ตรง และจอง token ที่ไม่ได้ใช้ | ADR-7 `skill_search`/`skill_view` | 7.2 |
| R-8 | ไม่มี OS-level sandbox สำหรับ background process | untrusted executable ทำอะไรก็ได้ในสิทธิ์ผู้ใช้ | คง allowlist/no-shell/deadline ไว้ และระบุชัดว่าไม่ใช่ sandbox จนกว่าจะถึง Phase 10 | 10 |
| R-9 | ไม่มี actor identity จริง provenance เป็น claim | audit trail อ้างผู้กระทำไม่ได้ | local principal + keychain + signed audit export | 14 |
| R-10 | dead code ที่อ่าน live state ขัด frozen-contract invariant | invariant พังเงียบถ้ามีคนต่อกลับ โดยไม่มี test ล้ม | ลบ + static check ใน CI | 7.1 |
| R-11 | finding ถูกประกาศปิดโดยมีหลักฐานต่ำกว่าเกรด A (ห้าในหกข้อของรอบแรก) | refactor ครั้งหน้าทำ invariant พังเงียบโดยไม่มีอะไรล้ม; แผนวางบนสถานะที่ดีเกินจริง | สเกลเกรดหลักฐาน + กฎ “ปิดได้เมื่อเกรด A” + งาน V-1 ถึง V-6 | 7.1 |
| R-12 | ชื่อ test สัญญามากกว่าที่ assertion ตรวจ | audit และคนอ่านให้เครดิตเกินจริง ซึ่งเป็นต้นเหตุของ R-11 | test naming rule + rename สอง test ที่พบ | 7.0 |
| R-13 | claim registry ถูกเข้าใจผิดว่าเป็น oracle ของความถูกต้องเอกสาร | รัน script ผ่านแล้วเชื่อว่าเอกสารตรง ทั้งที่ข้อความเชิงความหมายยังผิด | ประกาศขอบเขตของ registry ในเอกสาร + human checklist ต่อ finding ตอนปิด phase | 7.0 |
| R-14 | ADR-7 ทำให้ Skill retrieval เป็น pull จึงเสีย determinism และอาจไม่ถูกเรียกเลย | โมเดลเล็กไม่ดึง Skill ทำให้คุณภาพตกกว่าเดิม; replay/eval ยากขึ้น | คง pre-selection เป็น floor; metric `no_skill_requested_rate`; เกณฑ์ถอยที่ 50% ต่อ model tier | 7.2 |
| R-15 | ไม่รู้ขนาดงานรวมของแผน | ตัด scope ไม่ได้เพราะไม่มีตัวเลข; ADR-8 บังคับใช้ยาก | effort band ต่อ phase + total range + สมมติฐานกำลังคน | 7.0 |
| R-16 | effort band ไม่ระบุว่าใครเป็นคนทำ | batch แรกประเมิน 12–13 วัน-คน แต่ทำจริงใน ~40 นาทีด้วย agent ตัวเลขที่ไม่ระบุ actor จึงตีความไม่ได้ | **ปิดแล้ว** — band แยกเป็น D (decision, ไม่ย่อ) กับ I (implementation, ย่อได้) ทุก phase | 7.0 |
| R-17 | 6 จาก 12 exit gate ของ Phase 8–14 เป็น `unspecified` | phase ปิดได้โดยไม่มีเกณฑ์ผ่านจริง | **ปิดแล้ว** — ทุก gate มี predicate; ที่ต้องรอ artifact กลายเป็น prerequisite task ที่มีชื่อ (P8-A ถึง P14-A) | 7.0 |

---

## Test strategy

ทุก feature ต้องมีสี่ชั้นตามความเสี่ยง:

1. **Invariant/unit:** state machine, schema, token budget, permission intersection
2. **Integration:** SQLite/CAS/network/process/temp workspace โดยใช้ implementation จริง
3. **Behavior contract:** ความสัมพันธ์ข้าม component เช่น session revision ↔ prompt fingerprint ↔ tool binding
4. **Packaged E2E/fault injection:** UI→backend→provider/tool→receipt, cancel/restart/network loss/DB error/partial CAS move

Required continuous suites:

- race/concurrency and fuzz for parsers/state transitions
- local-model qualification matrix 64k/128k tiers ที่มี hardware
- provider contract fixtures + selected live canary โดย secret ผ่าน CI vault
- prompt/cache regression fingerprint suite
- context fidelity benchmark with threshold dashboard
- Skill baseline/candidate eval corpus
- MCP/plugin hostile server/package fixtures
- outbox/turn-lease concurrency suite
- packaged desktop accessibility/PTY/browser/install/upgrade E2E

การทดสอบ external endpoint ต้องแยก `protocol pass` ออกจาก `context certified`; final text หนึ่งครั้งบน 128k envelope ไม่ใช่หลักฐานว่า long-context recall 128k ผ่าน

**กฎใหม่:** feature ที่ปิด finding ระดับ P0/P1 ต้องมาพร้อม test ที่จะ fail หากถอย behavior นั้นออก การปิด P0-3 โดยไม่มี test คือกรณีที่กฎนี้ตั้งขึ้นเพื่อป้องกัน

---

## Delivery policy and milestone truth

แต่ละ phase ใช้สถานะเท่านั้น:

- `designed`: contract/ADR reviewed
- `vertical_slice`: happy path จริงหนึ่งเส้น + tests
- `qualified`: negative/fault/E2E/performance gates ผ่านใน declared scope
- `production`: packaging/migration/security/operations ผ่าน

ห้ามใช้คำว่า `complete` หากเป็นเพียง deterministic evaluator, API surface หรือ UI tab และทุก release note ต้อง generate capability inventory/test evidence จาก runtime registry เพื่อลด documentation drift

**Documentation truth gate (บังคับ):** commit ที่เปลี่ยน behavior ต้องแก้เอกสารที่อ้าง behavior นั้นใน commit เดียวกัน หรือระบุเหตุผลใน commit message ว่าทำไมไม่ต้องแก้ ห้ามค้างไว้เป็นงานตามหลัง — ปัญหาที่ทำให้แผนฉบับแรกผิดคือการปล่อยให้เอกสารตามโค้ดไม่ทัน

---

## Recommended next execution batch

ลำดับ implementation ถัดไปที่ให้ risk reduction สูงสุด

สมมติฐาน: คนทำหลักหนึ่งคน ทำงานเต็มเวลา ตัวเลขคือ **effort ไม่ใช่ elapsed**

| # | งาน | ปิด finding | ขนาด | ต้องทำก่อน |
|---|---|---|---|---|
| 1 | `git init` + commit + remote สำรอง | O-1 / R-1 | 15 นาที | — |
| 2 | doc truth pass + rename test ที่ overclaim | O-7 / V-6 / R-2 / R-12 | 2 ชม. | 1 |
| 3 | `scripts/doc-truth.sh` สองชั้น + claim registry | R-2 / R-13 | 1 วัน | 2 |
| 4 | gate audit Phase 8–14 + effort model สองแกน | P-3 / P-4 / R-15 / R-16 / R-17 | 1 วัน | — |
| 5 | exact provider serialization token accounting | O-3 / R-3 | 1 วัน | — |
| 6 | outbox test suite | O-4 / V-2 | ครึ่งวัน | — |
| 7 | TaskBudget test suite (5 เคส) | V-3 | 1 วัน | — |
| 8 | qualification override test suite (6 เคส) | V-4 | 1 วัน | — |
| 9 | turn-lease race test | V-1 | ครึ่งวัน | — |
| 10 | GC fault injection | V-5 | ครึ่งวัน | — |
| 11 | ลบ `selectSkills` + static check | O-5 / R-10 | 1 ชม. | — |
| 12 | per-tier recall evidence แทน bool เดียว | O-6 | 1 วัน | 8 |
| 13 | ADR-7 `skill_search` / `skill_view` | O-2 / R-7 | 3 วัน | 5 |
| 15 | metric `no_skill_requested_rate` + `GET /api/skill-retrieval` | R-14 | 1 วัน | 13 |
| 14 | Skill behavioral evaluator design spike | R-4 | 1 วัน spike | — |

**รวม batch นี้ประมาณ 13–14 วัน-คน** ข้อ 1–2 ทำก่อนเสมอ ข้อ 6–11 ขนานกันได้ทั้งหมดเพราะเป็นงาน test แยก package

สังเกตว่า **ข้อ 6 ถึง 10 รวมกันคือ 3.5 วัน และเป็นงานเขียน test ล้วน** นี่คือราคาของการที่รอบแรกประกาศปิด finding โดยไม่มีหลักฐานเกรด A

### สถานะของ batch นี้

batch ถูกลงมือทำจริงและปิดครบทุกข้อ ผลที่วัดได้:

| ตัวชี้วัด | ค่า |
|---|---|
| finding ที่ปิด | O-1 ถึง O-7, V-1 ถึง V-6, R-14 |
| wall clock | ~40 นาที, 13 commits, +2,900 บรรทัด |
| test functions | 96 → 128 |
| mutation test | 20 ครั้ง, แดง 18, บันทึก 2 ที่ test เข้าไม่ถึง |
| ADR ที่ถึง `implemented` | 0 → 4 |

effort band และการตีความตัวเลขนี้อยู่ในหัวข้อ [Effort model](#effort-model-ปิด-r-16) ด้านบน สรุปสั้น: ตัวเลข wall clock **ไม่** calibrate band ของ Phase 8–14 เพราะ decision effort ของ batch นี้ถูกจ่ายไปแล้วในรอบ audit ส่วน Phase 8–14 ยังไม่ผ่านรอบนั้น

### งานถัดไป

Phase 8 และ 9 เริ่มได้ เพราะ state authority, context truth และ learning lifecycle มั่นคงพอแล้ว แต่ทั้งสอง phase มี prerequisite ที่ต้องทำก่อนถึงจะวัด exit gate ได้:

| Prerequisite | สำหรับ gate |
|---|---|
| ~~**P8-A** digest corpus ≥100 เคส แยก trigger family~~ | ~~semantic reviewer~~ — **ปิดแล้ว** ดู [`corpus/README.md`](../corpus/README.md) |
| **P9-A** ~~gold corpus ≥50 เคสต่อภาษา~~ **เปลี่ยนเป็น O-40** สร้าง producer | essential retention — corpus ปิด gate นี้ไม่ได้ เพราะ corpus จะเป็นที่เดียวที่ decision มีอยู่ · decision ทำแล้ว · open_task/acceptance_criteria ยังไม่มีที่มา |
| **P9-B** task corpus ต่อ class | task success delta |
| ~~**P9-C** tokenizer adapter ตัวแรก~~ | ~~token error band~~ — **ไม่ต้องใช้** ดูด้านล่าง |

งานสร้าง corpus เป็น decision effort เกือบทั้งหมด — ต้องตัดสินว่าอะไรคือ ground truth ซึ่งเป็นงานที่ย่อไม่ได้ ควรเริ่มจากตรงนี้ ไม่ใช่จากโค้ด

### P9-C ปิดแล้ว และเล็กกว่าที่แผนคาดมาก

แผนสมมติว่า token error band ต้องรอ **tokenizer adapter** หลักฐานบอกว่าไม่ต้อง

การเรียกเก็บเงินของ provider เป็นฟังก์ชันเชิงเส้นที่วัดได้ตรงๆ:

```
billed = request_overhead + message_overhead × messages
       + asciiTokens + nonascii_rate × nonASCIIChars
```

- `request_overhead` และ `message_overhead` **วัดด้วย probe สองครั้ง** (`POST /api/providers/{id}/measure-overhead`) — ส่ง message จำนวนต่างกันแล้วอ่านผลต่าง ไม่ต้อง fit
- `nonascii_rate` **เรียนรู้ต่อ provider** จาก traffic จริง เพราะมันคือคุณสมบัติของ tokenizer กับภาษาที่ใช้
- ส่วน ASCII ใช้กฎเดิมซึ่งแม่นอยู่แล้ว

วัดสดบน gateway จริงจาก cold start: **p95 6.0% · within band 100% · overflow 0 · verdict `within_band`**

สิ่งที่ต้องระวัง: ตัวเลขนี้มาจาก provider เดียวและภาษาหลักเดียว **ยังไม่ยืนยันว่าใช้ได้กับ tokenizer ที่ต่างออกไปมาก** (เช่น CJK, หรือ model ที่ template ต่างกันมาก) แต่กลไกทั้งหมดเป็นการวัดต่อ provider ไม่ใช่ค่าคงที่ จึงต่อยอดได้โดยไม่ต้องเขียนใหม่

**tokenizer adapter ยังมีค่าถ้าต้องการ predicted ที่แม่นกว่านี้มาก** (เช่น เพื่อบีบ context ให้ชิดเพดานจริงๆ) แต่ไม่ใช่ prerequisite ของ gate นี้อีกต่อไป
