# Hermetrix Harness — ADR Ledger

ทะเบียนการตัดสินใจเชิงสถาปัตยกรรม เนื้อหาเต็มของแต่ละ ADR อยู่ใน [FUTURE-ARCHITECTURE-PLAN.md](FUTURE-ARCHITECTURE-PLAN.md) ไฟล์นี้เก็บ **สถานะ**, **เกรดหลักฐาน** และ **งานที่เหลือ**

## สถานะที่ใช้ได้

| สถานะ | ความหมาย |
|---|---|
| `proposed` | เขียนเป็น contract แล้ว ยังไม่ตัดสิน |
| `accepted` | ตัดสินแล้ว ยังไม่มีโค้ด |
| `partial` | มี runtime path แต่ขอบเขตยังไม่ครบตาม ADR หรือหลักฐานต่ำกว่าเกรด A |
| `implemented` | runtime path ครบตาม ADR **และ** หลักฐานเป็นเกรด A |
| `policy` | เป็นข้อตกลงการทำงาน ไม่ใช่โค้ด จึงวัดด้วย compliance ไม่ใช่ test |
| `superseded` | ถูกแทนที่ ต้องระบุว่าโดย ADR ใด |

## เกรดหลักฐาน

| เกรด | ความหมาย |
|---|---|
| **A** | assertion ตรวจ behavior ของ ADR โดยตรง และจะ fail ถ้าถอย behavior ออก |
| **B** | assertion ตรวจบางส่วน; บางเส้นทางยังไม่ถูกแตะ |
| **C** | มี test แต่ไม่ครอบ mechanism ที่เป็นหัวใจ |
| **D** | ไม่มี test |
| **—** | ไม่ใช่สิ่งที่ทดสอบด้วย test ได้ (ADR ประเภท `policy`) |

**กฎ:** สถานะ `implemented` ต้องอ้าง **assertion** ที่ตรวจ behavior จริง ไม่ใช่ชื่อ test การอ้างชื่อ test อย่างเดียวคือเกรด C และสถานะสูงสุดคือ `partial`

## ทะเบียน

| ADR | เรื่อง | สถานะ | เกรด | หลักฐาน / งานที่เหลือ |
|---|---|---|---|---|
| ADR-1 | Immutable SessionContract | `partial` | **A** | `buildSessionContract` — `internal/agent/service.go:144`; `TestSessionUsesFrozenSkillVersionAfterLaterPromotion` promote version ใหม่กลาง session แล้ว assert ว่า system prompt ยังเป็น version เดิมและ binding ไม่ drift — หลักฐานระดับ behavior จริง<br>เหลือขอบเขต: desk/surface ceiling และ memory/identity revision (Phase 11) |
| ADR-2 | Persisted TurnLease | `partial` | **C** | `acquireTurn` CAS — `internal/agent/service.go:324`; recovery — `:1066`<br>test ที่มีเป็น deterministic sequencing ไม่ใช่ race — block request แรกใน HTTP handler แล้วยิง turn ที่สองแบบ synchronous ความปลอดภัยมาจาก SQL CAS + single-connection SQLite ไม่ใช่จาก test<br>เหลือ: **V-1** race test 100 รอบใต้ `-race` |
| ADR-3 | Authority ladder สำหรับ Skill | `partial` | **B** | candidate-only lifecycle, promotion transaction, replay/weakened-test/stale-revision gate มีครบและมี test<br>เหลือ: sandboxed behavioral eval ชั้นก่อน human review (Phase 8); outbox path ไม่มี test เลย (**O-4 / V-2**) |
| ADR-4 | Narrow capability waist | `partial` | **B** | direct primitives 6 ตัว — `internal/tools/registry.go:97`; deferred catalog `tool_search`/`tool_describe`/`tool_call`; 1,500-tool scale test พิสูจน์ว่า catalog ไม่ทำให้ prompt โต<br>เหลือ: token accounting ที่นับจริง (**O-3**) — ตราบใดที่ยังนับต่ำกว่าจริง คำว่า “อยู่ใต้ ceiling” ยังพิสูจน์ไม่ได้ |
| ADR-5 | Certified context, not declared context | `partial` | **B** | `contextTier`/`contextCapacity` — `internal/qualification/service.go:341`; `resolveQualification` + expiring override — `internal/agent/service.go:130`; test assert reject-without-qualification และ freeze run ID<br>เหลือ: **V-4** override path/expiry/tier gating/revision staleness ไม่ถูก assert เลย; **O-6** หลักฐาน recall แยกต่อ tier |
| ADR-6 | Clean-room product implementation | `policy` | **—** | ไม่มี Aetox source/asset/branding ใน tree; requirement source แยกอยู่ที่ [`../../Hermetrix-research`](../../Hermetrix-research/README.md); third-party notices ครบ<br>วัดด้วย compliance review ตอนปิด phase ไม่ใช่ test |
| ADR-7 | Skill retrieval เป็น tool ไม่ใช่ prompt injection | `accepted` | **—** | ยังไม่มีโค้ด ปิด **O-2** งานอยู่ใน Phase 7.2 ต้องรอ 7.1-1 (exact token accounting) ก่อน<br>มีเกณฑ์ถอย: `no_skill_requested_rate` > 50% ต่อ model tier ถือว่า pull ล้มเหลวสำหรับ tier นั้น (**R-14**) |
| ADR-8 | Scope discipline: WIP limit | `policy` | **—** | แก้จากฉบับแรกที่เขียนเป็นกฎห้ามซึ่งใช้ไม่ได้ (**P-2**) ฉบับปัจจุบันเป็น WIP limit สองตัวพร้อมตารางนิยาม `qualified` ต่อ subsystem<br>WIP ที่จัดสรรตอนนี้: Session/turn authority และ Provider/qualification |

## สรุปสถานะ

- `implemented`: **ศูนย์**
- `partial`: ADR-1 ถึง ADR-5
- `policy`: ADR-6, ADR-8
- `accepted`: ADR-7

ADR ที่ใกล้ `implemented` ที่สุดคือ **ADR-1** ซึ่งมีหลักฐานเกรด A แล้ว เหลือเพียงขอบเขตที่ผูกกับ Phase 11

## ข้อตกลงเรื่องเอกสาร

1. commit ที่เปลี่ยน behavior ต้องแก้เอกสารที่อ้าง behavior นั้นใน commit เดียวกัน หรือระบุเหตุผลใน commit message ว่าทำไมไม่ต้องแก้
2. ADR ที่เลื่อนสถานะขึ้นต้องอัปเดตแถวในตารางนี้พร้อมหลักฐานและเกรดในรอบเดียวกัน
3. finding ID (`O-*`), verification ID (`V-*`), plan-defect ID (`P-*`) และ risk ID (`R-*`) ใช้ร่วมกันทั้ง audit และ plan ห้ามตั้ง ID ซ้ำหรือเปลี่ยนความหมายของ ID เดิม
4. เมื่อเอกสารใดขัดกับ runtime ให้ถือ runtime เป็นความจริงและแก้เอกสาร ไม่ใช่แก้โค้ดให้เข้ากับเอกสาร
5. ชื่อ test ต้องระบุเฉพาะสิ่งที่ assertion ตรวจจริง ชื่อที่กล่าวถึงสองพฤติกรรมต้องมี assertion ของทั้งสอง ไม่งั้นแยก test หรือเปลี่ยนชื่อ
