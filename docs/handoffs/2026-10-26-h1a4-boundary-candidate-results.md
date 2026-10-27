# H1A4: ผลตรวจขอบเขต prepared run และจุดพัก

ตรวจวันที่ 26 ตุลาคม 2026 ผู้ใช้เลือกใช้ Bonsai Local ต่อ ไม่มีการอนุญาตให้ส่งงานไป 9arm gateway สถานะเป้าหมายที่อ่านจากระบบขณะจัดเก็บผลเป็น `paused` จึงบันทึกผลที่มีอยู่โดยไม่เรียกโมเดลเพิ่มหรือ apply candidate

ชุด candidate ล่าสุดมี 27 ไฟล์: 23 ไฟล์จาก H1A3 เดิมและ test ใหม่ 4 ไฟล์ โค้ดผลิตภัณฑ์ในชุดนี้ยังไม่ได้ review หรือ apply และ source จริงยังใช้ schema 78 หลักฐานอยู่ใน [checkpoint](../evidence/2026-10-26/h1a4-boundary-candidates/checkpoint.json) และ [source manifest](../evidence/2026-10-26/h1a4-boundary-candidates/source-candidate-manifest.json)

## ผลที่ทดสอบได้

ผลด้านล่างเป็น focused tests แยกรอบ ไม่ใช่การรันทดสอบทั้ง repository หรือทั้ง cohort 27 ไฟล์พร้อมกัน

| กรณี | ผลและขอบเขต |
|---|---|
| base ไม่ใช่รุ่นปัจจุบัน | ผ่านบน SQLite จริง: สร้าง version ใหม่ของ Skill เดิม เปลี่ยน current version และตรวจจำนวนแถวที่แก้เป็น 1; Prepare ปฏิเสธ candidate ที่ยังอ้าง base เก่าและไม่มี run ถูกสร้าง |
| Prepare พร้อมกัน | ผ่าน: สอง goroutine ใช้ run ID และ input เดียวกัน ได้ record เท่ากัน มีแถวเดียว และอ่านกลับตรงกัน เป็น concurrent requests บนฐานข้อมูลที่ serialize connection ไม่ใช่ parallel model inference |
| project/session ปิดใช้งาน | ผ่าน: ปิด session และ archive project ของเจ้าของจริงแล้ว Prepare ปฏิเสธ; คืนสถานะแล้ว positive control สำเร็จ แต่ fixture ยังไม่ได้ assert RowsAffected ของ UPDATE อย่างชัดเจน |
| อ่านข้อมูล run | ผ่าน: record จริงที่ completed_at เป็น NULL อ่านกลับตรงกัน; projection ที่เปลี่ยน owner, candidate, revision, hash, empty non-NULL completion และเวลาเริ่มผิดรูปแบบ ถูกปฏิเสธพร้อม zero run; missing ID ตรวจ ErrNoRows ได้ |

ตรวจ assertion เพิ่มพบว่ากรณี missing ID ทิ้งค่าที่ scanner คืนด้วย `_` แล้วเปรียบเทียบตัวแปร `missing` ที่ประกาศเป็น zero ไว้เอง การเปรียบเทียบนี้จึงไม่พิสูจน์ returned zero run แม้ test จะผ่าน ต้องให้ Bonsai แก้เพื่อ capture และตรวจค่าที่คืนจริงก่อนรับ test นี้เป็นหลักฐานเต็ม ไม่เปลี่ยน oracle เพื่อให้ implementation ผ่าน

การสร้าง scanner test มีร่างผิด syntax/API, no-op และ preimage mismatch หลายครั้ง เก็บผลที่ถูกปฏิเสธไว้ ไม่ใช้เป็นโค้ดที่ยอมรับ ร่างสุดท้ายมาจาก `H1A4-scan-explicit-test` ตามด้วย `scan-call-valid`, `scan-call-corrupt`, `scan-call-missing`, `scan-caller-functions` และ `scan-missing-id-literal` ทั้งหมดเป็น prefix H1A4 การซ่อมรอบสุดท้ายแก้ undefined identifier แล้ว focused test ผ่าน แต่ไม่ได้แก้ assertion ข้างต้น

## ทำต่อเมื่อกลับมาดำเนินงาน

1. ให้ Bonsai capture returned record ใน missing-ID test และเพิ่ม RowsAffected=1 ใน fixture ปิด/คืนสถานะ พร้อมตรวจ missing project/session และ session ที่ไม่มี project ตาม contract
2. ทดสอบ cohort สุดท้ายทั้ง skills/store/agent และตรวจ vet/build จากนั้นจัด native review ให้เหมาะกับ context ของ Bonsai โดยแสดงขอบเขต source ที่ผู้ตรวจเห็นตามจริง
3. ตรวจ native ancestry และ preimage ของทุกไฟล์ก่อน apply ผ่าน Hermetrix และตรวจ root suite บน source จริงหลัง apply ห้ามนำ migration เข้าเดี่ยวแล้วอ้างว่า H1A เสร็จ
4. เชื่อม durable dispatch/completion, provenance และ promotion gate กับ trusted run ก่อนทดลอง holdout transfer; Prepare/Get metadata ยังไม่พิสูจน์ self-learning ครบวงจร

ใช้ overlay `H1A4-scanner-compiled-overlay.json` ใน RUNROOT เดิมเป็นจุดต่อ candidate เท่านั้น ไม่ replay generation/materialization prefix ที่มีผลแล้ว หลักฐานสำเนา Go ทั้งหมดใช้ `.go.txt` มี manifest ตรวจ hash 194 รายการ และตรวจว่า candidate H1A3 เดิม 23 ไฟล์กับ actual source preimages ยังตรงก่อนจัดเก็บ เป้าหมาย dynamic UX/control, marketplace ใน Docker Compose และการ deploy Kubernetes จริงยังไม่เสร็จ
