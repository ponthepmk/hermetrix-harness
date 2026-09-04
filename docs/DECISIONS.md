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
| ADR-2 | Persisted TurnLease | `implemented` | **A** | `acquireTurn` CAS — `internal/agent/service.go:324`; recovery — `:1066`<br>`TestConcurrentTurnsNeverDoubleCommitUnderRace` ปล่อย 4 goroutine พร้อมกัน 100 รอบใต้ `-race`; `TestSecondTurnIsRejectedWhileFirstHoldsLease` ครอบ error message<br>mutation: ปิด rows-affected guard — timeout + FAIL |
| ADR-3 | Authority ladder สำหรับ Skill | `partial` | **A** | candidate-only lifecycle, promotion transaction, replay/weakened-test/stale-revision gate มีครบและมี test; outbox path มี 6 เคสและผ่าน mutation test (O-4/V-2 ปิด)<br>**observation → candidate ต่อครบแล้ว (O-9 ปิด)** — `ModelReviewer` อ่าน digest แล้วเสนอ Skill; parser fail closed; ขับจริงแล้วปฏิเสธงานที่ไม่มี procedure และเสนอเฉพาะงานที่ผู้ใช้แก้ซ้ำ; active store ไม่ถูกแตะ<br>เหลือขอบเขต: sandboxed behavioral eval ชั้นก่อน human review (Phase 8) |
| ADR-4 | Narrow capability waist | `implemented` | **A** | direct primitives 13 ตัว — workspace file 4, session/context/Skill 4, deferred catalog `tool_search`/`tool_describe`/`tool_call` 3 และ runtime `workspace.run`/`browser` 2; 1,500-tool scale test ยืนยันว่า catalog ไม่ทำให้ waist โต<br>token accounting serialize payload จริง: 2,651 จาก budget 3,584 ของ compact-32k (`TestRealPayloadFitsEveryProfileDirectToolBudget`) |
| ADR-5 | Certified context, not declared context | `implemented` | **A** | `contextTier`/`contextCapacity`; `resolveQualification` + expiring override — `internal/agent/service.go:130`<br>override path ครบ 6 เคส (V-4 ปิด); recall probe ปลูก sentinel ห้าตำแหน่งและบังคับคืนครบ (O-6 ปิด)<br>mutation: pin profile lookup, ผ่อน recall เป็น any-position — แดงทั้งคู่ |
| ADR-6 | Clean-room product implementation | `policy` | **—** | ไม่มี Aetox source/asset/branding ใน tree; requirement source แยกอยู่ที่ [`../../Hermetrix-research`](../../Hermetrix-research/README.md); third-party notices ครบ<br>วัดด้วย compliance review ตอนปิด phase ไม่ใช่ test |
| ADR-7 | Skill retrieval เป็น tool ไม่ใช่ prompt injection | `implemented` | **A** | `skill_search`/`skill_view` เป็น direct primitive — `internal/tools/registry.go`; execution ผูก session contract — `executeSkillTool` ใน `internal/agent/service.go`<br>test: session ที่เปลี่ยนหัวข้อค้น Skill ที่ goal แรกไม่ได้เลือกเจอ, version ที่ promote หลังเปิด session ถูกปฏิเสธ, argument ผิดรูปถูก reject<br>mutation: ตัด contract gate, ตัด ranking — แดงทั้งคู่<br>direct tools ปัจจุบัน 13 ตัว = 2,651 จาก 3,584 token ของ compact-32k<br>เกณฑ์ถอยวัดได้แล้ว: `SkillRetrievalMetrics` + `GET /api/skill-retrieval` (**R-14 ปิด**) — denominator ใช้ deterministic scorer ตัวเดียวกับ `skill_search` จึงนับเฉพาะ turn ที่มี Skill ตรงจริง; verdict ต้องมี sample ≥20 turn<br>mutation: ตัด denominator, ตัดการตรวจ tool_name, ปลด sample floor, ขยับ threshold 0.5 — แดงทั้งสี่ |
| ADR-8 | Scope discipline: WIP limit | `policy` | **—** | แก้จากฉบับแรกที่เขียนเป็นกฎห้ามซึ่งใช้ไม่ได้ (**P-2**) ฉบับปัจจุบันเป็น WIP limit สองตัวพร้อมตารางนิยาม `qualified` ต่อ subsystem<br>WIP ที่จัดสรรตอนนี้: Skill learning และ Context compiler (หลัง Phase 7.x ปิด)<br>**ไม่มี phase ใดถูกตัด** — ข้อเสนอให้ตัด Phase 11 ถูกพิจารณาและปฏิเสธ; WIP limit กับ dependency เป็นเครื่องมือคุมลำดับแทนการตัด |

## สรุปสถานะ

- `implemented`: ADR-2, ADR-4, ADR-5, ADR-7
- `partial`: ADR-1 (เกรด A แล้ว เหลือขอบเขต desk/memory revision ที่ผูกกับ Phase 11), ADR-3 (เกรด A แล้ว เหลือ behavioral eval ใน Phase 8)
- `policy`: ADR-6, ADR-8
- `accepted`: ไม่มี

ADR ทุกข้อที่มีโค้ดตอนนี้มีหลักฐานเกรด A และผ่าน mutation test แล้ว สองข้อที่ยัง `partial` ติดที่ **ขอบเขตของ ADR** ไม่ใช่คุณภาพหลักฐาน

## คำถามเชิงออกแบบที่เปิดอยู่ (จากการขับใช้งาน 2026-08-24)

สองข้อนี้เป็นการตัดสินใจของเจ้าของผลิตภัณฑ์ ไม่ใช่บั๊ก โค้ดถูกทำให้**รายงานสถานะจริง**แล้ว แต่ยังไม่ได้เปลี่ยนนโยบาย

1. **export/import ไม่ใช่ฟังก์ชันผกผัน (O-21)** — export serialise 42 ตาราง import อ่านสอง ทางเลือก: (ก) export ใส่น้อยลงและเปลี่ยนชื่อให้ตรง (ลด blast radius เพราะไฟล์พกบทสนทนาทั้งหมดไปได้) หรือ (ข) import คืนได้ครบจริง (เป็นฟีเจอร์ใหญ่และเสี่ยงทับข้อมูล) ตอนนี้ผลลัพธ์ระบุตารางที่ถูกทิ้งแล้ว
2. **implicit-only replay ควรบล็อก promotion ไหม (O-24)** — Skill ที่ไม่มี fixture ได้ replay ที่ตรวจแค่ manifest ทางเลือก: ปล่อยผ่านพร้อม warning (สถานะปัจจุบัน) · หรือบังคับให้ `improve` ต้องมี fixture อย่างน้อยหนึ่งตัวก่อน promote (เข้มขึ้น แต่กันคนแก้ Skill ที่ยังไม่มี test)

## ข้อตกลงเรื่องเอกสาร

1. commit ที่เปลี่ยน behavior ต้องแก้เอกสารที่อ้าง behavior นั้นใน commit เดียวกัน หรือระบุเหตุผลใน commit message ว่าทำไมไม่ต้องแก้
2. ADR ที่เลื่อนสถานะขึ้นต้องอัปเดตแถวในตารางนี้พร้อมหลักฐานและเกรดในรอบเดียวกัน
3. finding ID (`O-*`), verification ID (`V-*`), plan-defect ID (`P-*`) และ risk ID (`R-*`) ใช้ร่วมกันทั้ง audit และ plan ห้ามตั้ง ID ซ้ำหรือเปลี่ยนความหมายของ ID เดิม
4. เมื่อเอกสารใดขัดกับ runtime ให้ถือ runtime เป็นความจริงและแก้เอกสาร ไม่ใช่แก้โค้ดให้เข้ากับเอกสาร
5. ชื่อ test ต้องระบุเฉพาะสิ่งที่ assertion ตรวจจริง ชื่อที่กล่าวถึงสองพฤติกรรมต้องมี assertion ของทั้งสอง ไม่งั้นแยก test หรือเปลี่ยนชื่อ
