# อ่านข้อมูลอัตโนมัติและแก้แชตล้มเมื่อโมเดลส่งหลายเครื่องมือ

นำขึ้น preview เดิมพอร์ต 18810 แล้ว: `.tmp-ui/hermetrix-read-autonomy-20261011.exe`, PID 83928, schema 76 ใช้ data/workspace/reviewer เดิม สำรอง SQLite อย่างสอดคล้องก่อนเปลี่ยน runtime ที่ `.tmp-ui/read-autonomy-before-20261011.db` และตรวจว่าไม่มี active turn ก่อนหยุด PID 55372

เข้าใช้งานที่ https://desktop-nh66lnq.tail0f7f57.ts.net:18810/ โดยใช้ token เดิม รีเฟรชหน้าเพื่อโหลด JavaScript ล่าสุด Main 18804/PID 20260 และ gateway 18811/PID 43076 ยังเป็นตัวเดิม ไม่เรียก Project Brain หรือเปลี่ยนระบบของ Brain

## บั๊กและพฤติกรรมใหม่

ก่อนแก้ ordinary Chat ปฏิเสธทั้งชุดเมื่อโมเดลส่งคำสั่งที่ต้องอนุมัติร่วมกับคำสั่งอื่น วิธีซ่อมเดิมให้โมเดลลองจัดชุดใหม่สองครั้ง จึงยังจบด้วย error ได้

ตอนนี้ตรวจทุกคำสั่งกับ frozen binding ก่อน แล้วเก็บคำสั่งตามลำดับเดิม คำสั่งก่อนงานที่ต้องอนุมัติทำงานตามสิทธิ์เดิม งานที่ต้องอนุมัติรายการแรกเปิดคำขอสำหรับ arguments และ hash ของรายการนั้นโดยเฉพาะ รายการถัดไปมี receipt `deferred` ว่า **ยังไม่ได้ทำ** เมื่อได้ผลอนุมัติ/ปฏิเสธแล้วโมเดลจึงพิจารณาส่งเฉพาะงานที่ยังจำเป็นด้วย call ID ใหม่ ไม่มีคิวลับที่รันคำสั่งเหล่านั้นอัตโนมัติ และไม่ให้สิทธิ์ทั้งชุดจากการอนุมัติครั้งเดียว

แก้ project gate ที่อยู่ผิดตำแหน่งด้วย: personal Chat ค้นหา อธิบาย และเรียก deferred capability ได้โดยไม่ต้องมีโปรเจกต์ ส่วนเครื่องมือ `workspace.*` ยังต้องมีโปรเจกต์และอ่านจาก root ของโปรเจกต์นั้น

| งาน | การอนุมัติ |
| --- | --- |
| อ่าน/ค้น/แสดงรายการไฟล์, ค้น/อ่านสกิล, ค้น context และอ่านหลักฐานใน session | ทำได้ทันที พร้อม runtime policy ให้ลงมืออ่านโดยไม่ถามผู้ใช้ซ้ำ |
| MCP `resources/read` และ `prompts/get` | ทำได้ทันที รวม catalog ที่เก็บจาก runtime เก่า เนื้อหายังเป็น untrusted evidence |
| MCP/plugin `tools/call` ที่ server ได้รับความเชื่อถือและระบุ read-only | ทำได้ทันทีตาม classification ของ capability |
| MCP tool ที่เปลี่ยนข้อมูล ลบข้อมูล หรือผลกระทบยังเป็น unknown | ยังต้องอนุมัติรายการนั้น ไม่เชื่อชื่อเครื่องมือหรือ readOnlyHint จาก server ที่ไม่ได้รับความเชื่อถือ |
| `workspace.write_file` | ขออนุมัติทุกไฟล์แยกกัน พร้อม SHA-256/absent guard เดิม |
| browser read/inspect/audit/source/capture | ใช้สิทธิ์อ่านเดิม; การเปิดปลายทางภายนอกและ click/type ยังใช้ approval/network guards เดิม |

Audit mode ยังตรวจ batch อย่างเข้มงวดและให้โมเดลซ่อมก่อน dispatch ตามสัญญา audit เดิม การแก้ ordinary Chat ไม่เปลี่ยน audit_report, turn budgets, stop, stale observation, หรือ one-use effect grants

MCP read classification แยก protocol reads ออกจาก tool annotations และรวม policy version ใน public capability revision ด้วย executor ตรวจ revision รูปแบบเดียวกัน จึงไม่เปลี่ยนความหมายของ description/grant เก่าอย่างเงียบ ๆ [ข้อกำหนด MCP เรื่อง tool annotations](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/server/tools.mdx) กำหนดให้พิจารณา hints ของ server ที่ไม่ได้รับความเชื่อถือเป็น untrusted

## หลักฐานการตรวจ

- `go test ./... -count=1` ผ่านทั้งชุด รวม real Chrome; หลังแก้ personal gate ตรวจ agent/MCP ทั้ง package อีกครั้งผ่าน
- Regression: batch อ่าน + สองงานต้องอนุมัติ ไม่เกิด turn_failed, อ่านก่อน approval สำเร็จ, หลัง approval มี deferred receipts ครบ และอนุมัติเดิมซ้ำไม่ได้
- สร้างไฟล์สองไฟล์ใน temp project: ก่อนอนุมัติไม่มีไฟล์ อนุมัติแรกสร้างเฉพาะไฟล์แรก ไฟล์ที่สองขออนุมัติใหม่ ปฏิเสธแล้วไม่เกิดไฟล์ที่สอง
- อ่านพร้อมกันห้าประเภทได้ครบโดยไม่มี approval; personal tool discovery ไม่ต้องมีโปรเจกต์ แต่ workspace access ยังถูกปฏิเสธ
- MCP fixture ผ่าน HTTP/registry จริง: resource และ prompt อ่านได้แม้ legacy requires_approval=1, stale revision ถูกปฏิเสธ, server ที่แอบตั้ง destructive tool เป็น read-only ยังไม่ dispatch
- Node 141/141 ผ่าน; vet/build ผ่าน; real HTTPS Chrome ตรวจ 390×640, 768×700, 1024×700, 1440×900 และ 390×420 ไม่มี horizontal overflow ทั้งแชตอ่านใหม่และแชต diagram เดิม
- **Bonsai จริง** ใน session `session_4a7f8743a94c0acc7ddee275922a373a`, ชื่อ “ทดสอบอ่านอัตโนมัติ”: สอง model steps; ส่ง workspace.read_file, skill_search และ context_search ใน batch เดียว ทั้งสาม receipt succeeded, approvals 0, turn_failed 0; คำตอบมี marker `READ_AUTONOMY_20261011` ที่อ่านจากไฟล์จริง สกิลใน fixture ไม่มีผลค้นหาและตอบตามจริง

หลักฐานอยู่ที่ `docs/evidence/2026-10-11/read-autonomy/` การทดสอบ mixed approval ใช้ fixture ที่ควบคุมคำตอบโมเดลเพื่อบังคับบั๊กอย่างแน่นอน ส่วน Bonsai จริงตรวจเส้นทางอ่านอัตโนมัติ ไม่ replay คำสั่งจากแชตผู้ใช้ที่เคยล้ม และไม่ลบ error เก่าออกจากประวัติ
