# H1A — immutable evaluation run ก่อนเริ่มงาน

วันที่ 26 ตุลาคม 2026 · ข้อกำหนดสำหรับขั้นถัดไป · ยังไม่ใช่ implementation หรือหลักฐานว่าผ่าน

เป้าหมายคือไม่ให้ผลของ candidate รุ่นเก่าถูกประทับเป็นผลของรุ่นใหม่ แม้ paired cases จะดูผ่าน H1B2 ปิด aggregate/historical-state bypass เท่านั้น ยังไม่แก้การผูกผลตั้งแต่เริ่ม

## สิ่งที่อ่านพบ

internal/skills/behavioral.go เรียก GetCandidate ภายใน RecordBehavioralEval ตอนรับผล แล้วกำหนด CandidateRevision/CandidateHash และ StartedAt/CompletedAt ในคราวเดียว ไม่มี durable run identity ก่อน execution comment เดิมที่อ้างว่าปฏิเสธการแก้ candidate ระหว่างรันจึงไม่ใช่หลักฐานว่ามีความสามารถนั้น

schemaV7 ใน internal/store/store.go มี candidate_behavioral_evals พร้อม ID, candidate/base/hash/revision, runner revision, state, result, started/completed timestamps แล้ว ต้องใช้ migration conventions ปัจจุบัน ไม่แก้เฉพาะ schemaV7 เพราะฐานข้อมูลเดิมจะไม่รันใหม่ API ปัจจุบันมี GET/POST /api/candidates/{id}/behavioral-eval ใน internal/web/server.go ไม่มี start/complete แยก

GetCandidate ใน service.go ผูก owner principal ที่ store กำหนด ต้องรักษาขอบเขตนี้และ project/session scope ที่ host ยืนยัน ข้อมูล owner/project ที่ runner ระบุเองไม่สร้างสิทธิ์เพิ่ม

## เกณฑ์ปลายทาง

1. Host สร้าง durable run ก่อนเรียก provider/tool ใด โดย pin candidate ID/revision/hash และ package blob, base version/hash, owner/project/session ที่มีสิทธิ์, actual provider/model configuration, Skill catalog, dataset/split/evaluator และ environment fingerprints ค่าเหล่านี้ห้ามประทับย้อนหลังตอนรับคะแนน
2. Completion ต้องอ้าง run เดิมและ immutable bindings ผลคนละ candidate/revision/base/config/dataset ถูกปฏิเสธอย่างชัดเจน ไม่แก้ binding ให้เข้ากับ candidate ปัจจุบัน ผลที่ไม่มี start identity เก็บเป็น legacy telemetry ได้แต่ห้าม authorize promotion
3. Start/complete/reopen เป็น durable state machine ที่ตรวจ race และ replay ใช้ CAS/transaction ตามแบบเดิม Completion ซ้ำที่ payload ตรงกันต้องไม่เกิด evaluation/effect ซ้ำ; payload ขัดกันต้องปฏิเสธ ไม่ resample model โดยเงียบ
4. Paired schema/counts/regression/verdict จาก H1B2 ยังมีผลเต็ม รวม provider errors/uncertain/minimum sample/false success และตรวจซ้ำก่อน promote การมี RunID อย่างเดียวไม่ใช่หลักฐาน behavioral success
5. Promotion ต้องผูกทั้ง candidate และ run ที่เสร็จจริง ฐานข้อมูลเก่าหรือ historical state=passed ห้ามยกระดับเอง ต้องเห็นขั้นตอนขอผลใหม่โดยไม่ลบหลักฐานเดิม
6. ระบุแหล่ง provenance ตามจริง: API ที่ caller ส่ง metadata เองไม่ใช่หลักฐานว่า host สังเกตการรันจริง ต้องเชื่อม runner ที่ host ควบคุมและ artifacts/command/provider receipts ก่อนอ้างการเรียนรู้อัตโนมัติครบวงจร

## หลักฐานตรวจรับ

ใช้ actual temporary SQLite และ owned candidate/project/session ผ่าน API จริง ตรวจ start ก่อน effect; แก้ candidate/base/config ระหว่างรันแล้ว completion ถูกปฏิเสธ; unknown/mismatched/replayed RunID; concurrent completion และ candidate edit; reopen DB/services ไม่เสีย run identity; legacy telemetry ยังอ่านได้แต่ไม่ promote; positive paired run รุ่นที่ pin ไว้ผ่าน promotion; active version/session catalog เดิมไม่เปลี่ยนย้อนหลัง

การพิสูจน์ actual model execution และ holdout transfer ยังต้องมี runner trials แยกจาก service fixtures ห้ามใช้ผล API/unit tests แทน live quality/learning claim

## ลำดับลงมือ

หลัง H1B2 review/apply/post-check สำเร็จ: สำรวจ migration/latest ownership conventions และ working sibling CAS tests → ตรึง DATA acceptance/runner authority contract → ให้ Hermetrix สร้าง standing tests และ native implementation ทีละ bounded step → red/green/integration review/apply → actual learning/holdout trials ตาม R1 การแก้นี้ไม่เปลี่ยน serving restriction, budgets หรือสิทธิ์ของ candidate
