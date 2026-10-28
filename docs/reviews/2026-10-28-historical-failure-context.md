# เก็บ context ของโค้ดที่ล้มเหลวหลัง rollback และข้ามแผนใหม่

วันที่ 28 ตุลาคม 2026 · นำเข้าและ push `dev` แล้ว: `70de7de` และ `366447f`

ก่อนแก้ การคืนไฟล์หลังคำสั่งล้มเหลวทำให้รอบแก้ไขเห็น error แต่ไม่เห็นโค้ดที่ทำให้เกิด error เมื่อสร้างแผนใหม่ StepID เปลี่ยน การค้นเฉพาะขั้นปัจจุบันยิ่งทำให้ข้อมูลเดิมหาย ผลคือโมเดลต้องเดาโค้ดจากข้อความ compiler อย่างเดียว

ตอนนี้ Harness อ่าน proposal ที่เก็บไว้ผ่านหลักฐาน task/step/attempt/log ที่ตรงกัน ตรวจ project, input และ checksum ด้วยตัวโหลดเดิม แล้วแนบโค้ดเก่าเฉพาะ path ที่อนุญาตในงานปัจจุบัน สูงสุด 2 ไฟล์ รวม 8,192 ไบต์ UTF-8 พร้อม hash/ขนาดเต็มและป้ายตัดทอน โค้ดเก่าเป็นข้อมูลวิเคราะห์ที่ไม่เชื่อถือ ไม่กลายเป็นไฟล์ปัจจุบันหรือสิทธิ์เขียนเพิ่ม

กรณีแผนใหม่จะเลือกขั้นต้นทางได้ต่อเมื่อ validation ที่บันทึกไว้ตรงทั้งหมด: งานเดียวกัน check ที่ประกาศ ผล fail, ID, subject revision รูปแบบมาตรฐาน, actual และ artifact ref แบบตรงตัว หากหลักฐานหาย ผิด หรือซ้ำกำกวม จะคงการค้นขั้นปัจจุบัน

## ผลที่ตรวจได้

| ขอบเขต | หลักฐาน |
|---|---|
| ส่วน same-step 13 Go files | 363 pass, 1 existing opt-in skip, 0 fail; vet/build ทั้ง repository ผ่าน; 13 write receipts, 0 rollback |
| ส่วนข้ามแผน 4 Go files + frozen DATA 19 cases | 383 pass, 1 existing opt-in skip, 0 fail ใน taskengine/taskcoord/worker; vet/build ทั้ง repository ผ่าน; native reviewer approve; 4 write receipts, 0 rollback |
| ทดสอบก่อน/หลังแบบเกิดผลจริง | คำสั่ง Node fail → rollback → CreatePlan revision 2 ในงานเดียวกัน: ก่อนแก้ assertion ประวัติโค้ด fail, หลังแก้ผ่านและ README ปัจจุบันยังคืนสภาพ |
| packet ของ marketplace ที่บันทึกไว้จริง | หลังนำเข้าได้โค้ดเดิม 6,824 ไบต์ + terminal เดิม 210 ไบต์ ตรง candidate ทุกค่า packet/หลักฐานเดิมไม่เปลี่ยน และ hash ตาราง task/goal ไม่เปลี่ยนจากการอ่าน |

DATA 19 กรณีถูกตรึงก่อนสร้าง Go tests ค่าไม่เปลี่ยน ความต่าง LF/CRLF บันทึก hash ทั้ง raw และ normalized ไว้ การปรับ curation test ของส่วนแรกเพิ่ม cap 1,299→1,313 เฉพาะกรณี small-reference ให้คงระยะเหลือ 2 tokens หลัง schema เพิ่ม 14 tokens; กรณี strict 1,299 ยังคงพิสูจน์ว่าตัด reference ที่เกิน และ DATA เดิม 4 MB ไม่เปลี่ยน ไม่มีการเพิ่ม budget โปรไฟล์จริง

Go และ standing tests ทั้งหมดสร้างผ่าน Bonsai ที่ผ่าน protocol validation รวม ancestry ของรุ่นก่อนหน้า โค้ดที่ protocol ปฏิเสธไม่ถูกนำมาใช้ Codex เขียนเฉพาะการออกแบบ ข้อมูล fixture ตัวตรวจ/ตัวนำเข้า และรายงาน

## ข้อจำกัดและงานต่อ

ผลนี้พิสูจน์การส่ง context ให้ขั้นแก้ไข ไม่ได้พิสูจน์ว่า Bonsai แก้ marketplace จนจบแล้ว ยังไม่ได้เริ่ม generation ของ marketplace รอบใหม่หลังตัวแก้นี้ แผนเดิมจบการรันแล้วแม้แถว attempt เก่ายังเขียนว่า running; ต้องตรวจ deadline/effects/authority และใช้กระบวนการ recovery ปกติก่อนเริ่มใหม่ ห้าม reset identity หรือ budget เดิม

การตัด prefix 8,192 ไบต์อาจพลาดบรรทัดท้ายไฟล์ใหญ่ ต้องวัดผลรอบแก้จริงก่อนขยายเป็นการเลือกบริเวณ error ผู้ตรวจเป็น Bonsai เครื่องเดียวกับผู้เขียนจึงไม่ใช่การตรวจต่างโมเดล และยังมี full-suite Windows desktop approval-observation failure เดิมที่ไม่เกี่ยวกับ patch นี้ จึงไม่กล่าวว่าทดสอบทั้งระบบผ่านทั้งหมด

ขั้นต่อไปคือรันงานแก้ API ใหม่ด้วย authority และ identity ใหม่ที่ถูกต้อง ตรวจว่าคอมไพล์และตอบ health/readiness กับ PostgreSQL จริง แล้วค่อยขยาย flow marketplace ส่วน UI/security audit ของ O-SOUL เป็นผลตรวจโดย operator แยกจากความสามารถทำเองของ Harness

ผลพร้อม hash หลักฐาน: [validation](../validation/2026-10-28-historical-failure-context.json) · หลักฐานดิบอยู่ `.tmp-ui/goal-20261023/live-20261026-001100/` ที่ถูก ignore จาก Git โปรเจค marketplace อยู่นอก Harness Git

ภายหลังมีการรัน marketplace จริงเพิ่มเติมและแก้ขีดจำกัด history ให้ใช้ร่วมทั้งรายการ fail ใน `9077ec6` อ่าน [ผลล่าสุดและข้อจำกัด](2026-10-28-marketplace-after-context.md) แทนสถานะที่ยังไม่มี generation รอบใหม่ใน checkpoint เก่านี้
