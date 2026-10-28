# การวางแผนต่อหลังเปลี่ยน requirement

วันที่ 28 ตุลาคม 2026 · นำเข้าและ push `dev` แล้วใน `3109f2d` · งานหลักยัง ACTIVE

ก่อนเพิ่มเอกสาร Go ให้ task marketplace พบว่า PendingEscalation ค้นเพียง task ทำให้ escalation จาก requirement เก่ายังถูกส่งเข้า AutoPlan หลัง owner เปลี่ยน requirement แล้ว ซึ่งการเปลี่ยน requirement ยกเลิก active plan เดิมตามที่ควรเป็น ผลคือ AutoPlan หยุดด้วย `task has no active plan` ก่อนส่ง provider

ทดสอบซ้ำใน fixture แยกด้วย SQLite และ task APIs จริง ยืนยันสองสาเหตุ: โค้ดเดิมติดเพราะหยิบ escalation เก่า; ถ้าแก้เฉพาะการค้น จะวางแผนใหม่ได้แต่ ResolvePendingEscalation กลับปิด escalation เก่าด้วย plan ใหม่ จึงต้องแก้ทั้งการค้นและการ resolve

สอง query ตอนนี้ผูก escalation → step ใน task เดียวกัน → plan revision ของ step → requirement ปัจจุบัน เก็บแถวเก่าไว้ครบ ไม่ลบ เปลี่ยน state หรือผูกกับ plan ใหม่ ไม่เพิ่ม failure threshold ไม่รีเซ็ต counter/deadline และไม่เปลี่ยน schema กรองด้วย requirement ไม่ใช่ active plan เพื่อให้ escalation จากขั้นใน plan ก่อนหน้าของ requirement เดียวกันยังใช้วิเคราะห์และ resolve ได้

## หลักฐานผลลัพธ์

- Standing regression native: req1 ล้มเหลวสองครั้ง → owner เปลี่ยนเป็น req2 → AutoPlan สร้าง plan2 ได้ โดย state/evidence/planner binding/plan revision/resolved timestamp ของแถว req1 ไม่เปลี่ยน → req2 ล้มเหลวอีกสองครั้ง → AutoPlan สร้าง plan3 และ resolve แถว req2 ที่มาจาก plan2 ได้ถูกต้อง แถว req1 ยังไม่เปลี่ยน
- ก่อนแก้ assertion pending หลังเปลี่ยน requirement fail; แก้ Pending อย่างเดียว assertion แถวเก่าไม่เปลี่ยน fail; แก้ทั้งสองผ่าน ทั้งสามครั้งคอมไพล์ได้และเกิดผลทดสอบจริง ไม่ใช่ compiler error แทนผล red
- taskengine/taskcoord/worker: 385 pass, 1 existing opt-in skip, 0 fail; vet/build ทั้ง repository ผ่าน native reviewer approve ไม่มี findings; guarded apply 2 receipts/0 rollback; หลังนำเข้าตรวจ hash สองไฟล์และ targeted tests ผ่าน
- อ่าน task marketplace จริงหลังนำเข้า: escalation ที่ยังอยู่ใน requirement1 ถูกอ่านได้ตรงเดิม task revision42 และ hash ตาราง task/run/goal/effect/proposal/validation/escalation/plan/requirement ไม่เปลี่ยนจาก observer ไม่มี active inference ณ post-apply checkpoint

การทดสอบ lifecycle ใช้ fixture provider และ failure records ผ่าน APIs ไม่ได้อ้างว่ามี shell jobs จริงใน fixture นี้ ส่วน actual marketplace trial ก่อนหน้ามี compiler failures/rollback จริง 6 ชุดตาม [รายงานก่อนหน้า](2026-10-28-marketplace-after-context.md)

ข้อจำกัด: reviewer เป็น Bonsai เครื่องเดียวกับผู้เขียน ไม่ใช่ความหลากหลายของโมเดล และถ้อยคำ “active plan” ใน rationale หนึ่งประโยคไม่ตรงกับ SQL ให้ยึด source/regression ที่พิสูจน์การ resolve ข้าม plan ภายใน requirement เดียวกัน ยังไม่กล่าวว่า full runtime suite ผ่านเพราะมี Windows desktop observation failure เดิม และ patch นี้ไม่ทำให้ marketplace หรือการเรียนรู้ของ Harness สำเร็จครบแล้ว

ถัดไปจะเพิ่ม primary Go reference ให้ task เดิมผ่าน requirement ใหม่และเริ่ม bounded trial ใหม่ โดยเก็บ goal เก่า/deadline/counters/failed outcomes ไว้ครบ ตรวจจาก request จริงว่า planner/worker ได้รับเอกสารใด และแยกผล operator เตรียม docs ออกจากการเลือก skill/ค้น docs อัตโนมัติที่ยังไม่ครบ

ผลพร้อม hash: [validation](../validation/2026-10-28-requirement-escalation.json) หลักฐานดิบใน `.tmp-ui/goal-20261023/live-20261026-001100/C64-20261028-requirement-escalation-v1/` ถูก ignore ไม่เก็บโปรเจค marketplace เข้า Harness Git
