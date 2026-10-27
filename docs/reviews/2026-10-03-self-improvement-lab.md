# ทดลองให้ Hermetrix ปรับวิธีทำงานของตัวเอง โดยรักษา core เดิม

วันที่: 2026-10-03 · ผล: ทดลองจริงครบแล้ว เก็บรุ่นเดิมไว้

ผู้ใช้อนุญาตให้ลองแนวทางพัฒนารุ่นที่ดีกว่าตัวเอง โดยกำหนดว่า core ต้องยัง
ทำงานดี รอบนี้จึงเพิ่มเครื่องมือทดลองแยก `hermetrix-lab` ให้ Hermetrix เสนอ
checklist จากผลงานของตัวเอง แล้วนำไปเทียบกับวิธีเดิม ไม่เชื่อมการติดตั้ง
รุ่นทดลองเข้ากับระบบที่ให้บริการ

## ผลที่วัดได้

ใช้โปรไฟล์ Qwen ที่มีอยู่จริงผ่าน Harness18801: โจทย์ตั้งต้นหนึ่งครั้ง,
ข้อเสนอ checklist หนึ่งครั้ง และสามกลุ่มงานใหม่ กลุ่มละสองรอบ สองเงื่อนไข
รวม 14 คำตอบที่มีหลักฐาน แชตแต่ละคำตอบแยก session/turn และไม่มี project
หรือเครื่องมือแก้ไฟล์ ทั้งสองเงื่อนไขใช้ provider revision และ Skill catalog
เดียวกัน สลับลำดับการทดลองแต่ละรอบ

| เกณฑ์ | วิธีเดิม | วิธีที่เพิ่ม checklist |
|---|---:|---:|
| ความครอบคลุมตามตัวตรวจ | 130/130 | 109/130 |
| โทเค็นเฉพาะการเปรียบเทียบ | 24,069 | 45,914 |
| เวลารวมเฉพาะการเปรียบเทียบ | 220.003 วินาที | 417.811 วินาที |

130 เป็นจำนวนข้อกำหนดที่ตรวจรวมจากหกคู่การทดลอง ไม่ใช่ 130 งานอิสระ
หรือการทดสอบแอปจริง ตัวตรวจปฏิเสธแผนหนึ่งรายการทั้งแผน เพราะมี
`customer:return_order` ซึ่งไม่อนุญาตในสัญญาบทบาทของชุดทดลอง ดังนั้น 21
คะแนนที่หายไปไม่ได้แปลว่าขาด 21 ฟีเจอร์ในโค้ด เป็นการไม่รับแผนที่ขัดกับ
ขอบเขตบทบาท การตีความชื่อ action ต้องยึดสัญญานี้; ผลนี้ไม่ได้พิสูจน์ว่า
แอปจริงให้ลูกค้าข้ามสิทธิ์หรือว่าคำตอบทั่วไปทุกแบบแย่ลง

checklist เพิ่มภาระโทเค็นประมาณ 1.91 เท่า และไม่มีความครอบคลุมที่ดีขึ้น
จึงได้ `keep_current`, `auto_promoted=false` ตามเกณฑ์ที่กำหนดไว้
ไม่มีการสร้าง active Skill จาก checklist หรือเปลี่ยนรุ่นหลัก

## ยืนยัน core และงานเดิม

- ตรวจ Go suite ก่อนและหลัง: ผ่าน 1,052 รายการทั้งสองครั้ง รวม subtests
  และ Chrome browser workflows ที่กำหนดให้ต้องทำงานจริง
- Frontend Node tests: ผ่าน 135/135 ก่อนและหลัง ไม่มีการข้าม frontend tests
- Go มี 13 รายการที่ข้ามเพราะเป็นการเชื่อมต่อภายนอก/ตัวเลือก/platform fixture
  เช่น Pi, Comfy, Bonsai, embedding, voice และ network opt-in; รายชื่อครบใน
  receipt ห้ามสรุปว่าการเชื่อมต่อเหล่านั้นถูกทดสอบจริงในรอบนี้
- source fingerprint และ test fingerprint ของ core เดิมตรงกันก่อนและหลัง
  ไม่มีการเปลี่ยนไฟล์ core, serving routes, schema หรือ executable ที่ให้บริการ
- `go vet ./...`, `go build ./...` และ race check ของ `internal/selfimprove` ผ่าน
- Harness18801/18804/18806 ตอบ health200; Delivery18920 `/api/health` ตอบ200
  และ Docker container ยัง healthy
- Easiiot Task revision31 และ Delivery Task revision101 ยัง `verifying`;
  JSON ของทั้งสองงานตรงกับก่อนทดลองทั้งหมด ไม่ยอมรับงานแทนผู้ใช้
- Skill สองรายการยังมี version, state, enabled และขอบเขตเดิม การทดลองผ่าน
  API ปกติมีแชตใหม่, activation receipts และ calibration ตามปกติ

## ปัญหาที่ reproduce และแก้ในขอบเขตทดลอง

รอบแรกใช้ compact32k แล้วโมเดลใช้ reasoning จนไม่เหลือคำตอบในงบ4096
ตัวตรวจหยุด ไม่ให้คะแนนว่าผ่าน ไม่ retry คำตอบเดิม และตรวจ core ซ้ำผ่าน
เก็บหลักฐานรอบที่ไม่สำเร็จไว้ครบ

รอบใหม่กำหนด extended96k และบันทึก reviewed context override เฉพาะ
แชต Lab เพื่อเพิ่มงบคำตอบ ใช้ provider configuration/presets เดิม
ไม่ได้อ้างว่า override เป็น qualification ที่วัดแล้ว และไม่ได้เปิดโมเดล
Bonsai ที่เคยถูกปฏิเสธให้เปิด รอบนี้ทั้ง14คำตอบเสร็จ ภายในงบ observed
74,573 tokens จากเพดาน dispatch220,000; usage ของรอบ compact ที่ล้มเหลว
ไม่รวมในยอดนี้ เพราะไม่มี final usage receipt

## หลักฐานและขอบเขต

- [รายงานจริง](../evidence/2026-10-03/self-improvement/report.json)
- [รอบ compact ที่ไม่สำเร็จ](../evidence/2026-10-03/self-improvement/compact-failure.json)
- [งานเดิมและ Skill ไม่ถูกเปลี่ยนรุ่น](../evidence/2026-10-03/self-improvement/state-preservation.json)
- [race check](../evidence/2026-10-03/self-improvement/lab-race.txt)
- [วิธีใช้และสถาปัตยกรรม](../SELF-IMPROVEMENT.md)

Executable ของรอบเปรียบเทียบ:
`0af821af20c8c676fc0e392d05ffb70e15f9a86d156a0f8f20db6dbb03334fe5`

Core source fingerprint:
`c6f6b6f207a3fbfee5fa4fef21f7796f965bd12ab4cb3231d87d66042e350d0c`

Suite hash:
`f74b325ca2d0d65ad395a84e74c5128fa8bda11c765e1f6d5c2b57b52bdafac3`

Raw logs และ executable เก็บใน `.tmp-ui/self-improvement-20261003/`:
`live-trial` คือรอบ compact, `context-trial` คือการเปรียบเทียบที่เสร็จ
ไม่มี helper server หรือโปรเซสทดลองที่ต้องเปิดค้างจาก feature นี้

ผลที่ยืนยันคือวงจรเสนอ → เปรียบเทียบ → ปฏิเสธข้อเสนอที่ไม่ผ่าน → รักษา
core เดิม การให้คะแนนเป็น structured planning coverage เท่านั้น ยังไม่ได้
พิสูจน์การสร้างและติดตั้ง successor code, การเรียนรู้ที่ทำให้งานจริงดีขึ้น,
หรือ model-weight training ขั้นต่อไปควรเพิ่มการแก้โค้ดในสำเนา source/data
แยก และทดสอบ browser/API/database จากงานจริงก่อนขยายอำนาจระบบ
