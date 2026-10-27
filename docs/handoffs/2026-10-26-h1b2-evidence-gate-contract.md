# H1B2 — เกณฑ์ paired evidence สำหรับ promotion

เป้าหมายที่อนุญาตแล้วคือการเรียนรู้ที่วัดผลจริงและไม่รับบทเรียนที่ถดถอย ข้อมูล paired Result ปัจจุบันคำนวณผลรายกรณีได้ แต่ absent Result ยังใช้ aggregate และให้ passed; gate เชื่อ state/hash/revision อย่างเดียว ผล aggregate เก่าจึงยังผ่าน promotion ได้

ขอบเขตย่อยนี้: คงการบันทึก aggregate telemetry และผลล้มเหลว/false-success ที่ตรวจได้ แต่ aggregate ที่จะผ่านต้องเป็น inconclusive เพราะไม่มีคู่ผลรายกรณี ตอน promote ตรวจ paired schema/counts/regressions/verdict ซ้ำจาก result ที่เก็บไว้ รวม historical passed rows ไม่อาศัย state อย่างเดียว ไม่เปลี่ยน schema, public API, permission, scope หรือ model budgets

ยอมรับเมื่อ standing tests ที่ Hermetrix เขียนแสดง red บน source เดิมและ green บน candidate: aggregate tie/gain/perfect/zero/invalid counts ไม่ authorize; historical passed ไม่มีคู่ผล/คู่ผลขัด counts/ซ่อน regression/จำนวนผิดถูกปฏิเสธจาก PromoteCandidate จริง; valid paired evidence ยัง promote ได้ Existing aggregate failures/inconclusive/paired parser tests ต้องผ่าน ปรับเฉพาะ positive test setup เก่าที่ให้ aggregate success ให้ส่ง paired fixtures โดยคง assertion ธุรกิจเดิม

H1A run identity ก่อนเริ่ม, metadata provenance และ H1C scope ยังแยกทำต่อ การแก้นี้ไม่อ้างว่าเป็น full behavioral runner หรือ self-learning transfer แล้ว
