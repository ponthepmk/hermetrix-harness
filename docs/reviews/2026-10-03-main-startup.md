# ระบบหลัก: เปิดใช้งานต่อเนื่องและตรวจ flow จริง — 2026-10-03

ผู้ใช้ให้พักการทดลองพัฒนาตัวเอง แล้วปรับส่วนใช้งานหลักก่อน รอบนี้แก้ตัวเปิด Hermetrix และตรวจแชต/หน้าหลักฐานจริง ไม่รันทดลอง lab เพิ่มและไม่ติดตั้ง candidate

## ปัญหาที่ทำซ้ำได้

18801 ไม่มี process/listener ในการตรวจครั้งนี้ แต่ 18804/18806 และ Delivery 18920 ยังทำงาน ตัวเปิดเดิม `scripts/start-easiiot-harness.ps1` ล้มเหลวก่อนเปิดเซิร์ฟเวอร์ เพราะบังคับตรวจ Bonsai 8088 ซึ่งออฟไลน์ แม้ฐานข้อมูลมี Qwen profile ที่ใช้ได้อยู่

จุดแยกแรกคือ preflight ใน launcher ไม่ใช่การรับคำสั่งใน agent: ตัว server ตรวจ profile/credential ของ reviewer โดยไม่บังคับต่อโมเดลตอนเปิด จึงเปิด UI/ประวัติได้แม้ reviewer ออฟไลน์ ส่วนสาเหตุที่ process 18801 เดิมหยุดยังไม่ได้พิสูจน์ ไม่กล่าวว่าเกิด crash หรือเกิดจากโมเดล

เทียบ launcher 18804 ซึ่งใช้ `Start-Process -WindowStyle Hidden` และตรวจ readiness แล้วนำเฉพาะวิธีเปิด process/ตรวจความพร้อมมาปรับกับ 18801 ไม่หยุดหรือเปิด instance 18804 ใหม่

## สิ่งที่แก้

- เปิดเบื้องหลังเป็นค่าเริ่มต้น พร้อม PID และ log แยกแต่ละรอบ คำสั่งเปิดจบแล้ว server ยังทำงาน ใช้ `-Foreground` เมื่อต้องการผูกกับ terminal
- ไม่บังคับเปิด Bonsai เพื่อเข้า UI; แจ้ง warning เมื่อ TCP endpoint ของ reviewer ในเครื่องไม่ตอบ การต่อ TCP ได้ไม่ใช่หลักฐานว่าตอบโมเดลได้
- เก็บ reviewer ID ที่เลือกชัดเจน ไม่มีการเลือก gateway อื่นแทนหรือส่งคำขอ remote ระหว่างเปิดระบบ
- ตรวจ service health, executable เจ้าของพอร์ต, data directory, reviewer argument, workspace และ enabled/credential-ready profile ก่อน reuse
- ปฏิเสธ rebuild instance ที่ยังทำงาน และไม่หยุด process เจ้าของพอร์ตอื่น ถ้า startup ของ invocation ใหม่ล้มเหลว/หมดเวลา เก็บ log และหยุดเฉพาะ process ที่เพิ่งเปิด
- แก้ argument quoting สำหรับ Windows/path มีช่องว่างและเก็บ handle เพื่ออ่าน startup exit code ได้ Browser เปิดไม่ได้จะแจ้งให้เปิด URL เองโดย server ยังอยู่

## ผลตรวจจริง

| กรณี | ผลและหลักฐาน |
|---|---|
| เปิด 18801 ขณะ 8088 ออฟไลน์ | ผ่าน: PID 90352, health schema73 ตรง expected73, ใช้ executable/data เดิม |
| เรียก `-CheckOnly` อีกครั้ง | ผ่าน: reuse PID เดิม ไม่มี instance ใหม่; `live-reuse.txt` |
| แชตส่วนตัวผ่าน UI | เลือก Qwen เดิมอย่างชัดเจน ส่งข้อความสังเคราะห์ ไม่แนบไฟล์/ผูกโปรเจกต์ เห็นกำลังทำงาน → ตอบ `MAIN-FLOW-OK` → ส่งข้อความต่อได้; 476 token |
| เปิด Plans/หลักฐาน Easiiot และ reload | ผ่าน: requirement/plan4, revision31, verifying16/17 ยังรอผู้ใช้รับรอง ไม่มี replay หรือ acceptance ใหม่ |
| ข้อมูลเดิม | SHA ของ durable_tasks ทั้ง6, task_steps ทั้ง10 และ task_user_acceptances ทั้ง1 เท่าเดิม; provider configuration/vault/executable เท่าเดิม |
| Provider row แบบทั้งแถว | เปลี่ยนตาม calibration จากแชตปกติ จึงไม่กล่าวว่าทุกแถวในฐานข้อมูลเหมือนเดิม |
| ระบบข้างเคียง | 18804 schema73, 18806 schema74 และ 18920 `/api/health` ตอบปกติ |
| Windows regression | 7 tests: detached/reuse/identity/rebuild, foreign unhealthy port, timeout cleanup, missing-reviewer/no-fallback, unexpected healthy process, CheckOnly/no-start และ invalid-port/no-start |
| เสริม | `go vet ./scripts` และ `go test ./cmd/hermetrix` ผ่าน; หน้าเว็บที่ตรวจไม่พบ console error/warn |

แชตทดสอบ `session_2e7cd5fa6d4935425bdb84eea50b9a61`, ไม่มี authority สำหรับไฟล์/โปรเจกต์ การเรียก API ปกติยังอาจใช้ global Skill context/calibration ตามเดิม ไม่ใช่ sandbox สำหรับการพัฒนาตัวเอง

หลักฐานและ backup อยู่ที่ `.tmp-ui/main-startup-20261003/`: `easiiot-before.db`, `state-before.json`, `state-after.json`, `launcher-tests.txt`, `live-reuse.txt`, `chat-receipt.json`, `chat-complete.png` ส่วน startup log คือ `.tmp-ui/easiiot-18801-639265889614980504.error.log` ไม่เผยแพร่ฐานข้อมูลหรือ vault

## เปิดใช้งาน

```powershell
.\scripts\start-easiiot-harness.ps1
.\scripts\start-easiiot-harness.ps1 -CheckOnly
# เปิด browser ของระบบปฏิบัติการหลังพร้อมใช้งาน
.\scripts\start-easiiot-harness.ps1 -Open
# โหมดเดิม: Ctrl+C หยุด process
.\scripts\start-easiiot-harness.ps1 -Foreground
```

เปิด http://127.0.0.1:18801/ ได้ทันที แชตใหม่เลือกโมเดลที่พร้อมใช้งานอย่างชัดเจน งาน Easiiot ที่รอรับรองอยู่ใน `easiiot-pilot-project`; งานใหม่ใน checkout จริงเลือก `easiiot_project`

## ขอบเขต

แก้และยืนยันการเปิดระบบ/แชต/เปิดหลักฐาน ไม่ได้ทดสอบใหม่ทุก integration หรือพิสูจน์ full autonomous coding flow รอบนี้ ไม่ได้เปลี่ยน serving core/UI/executable/schema หรือปิดงานแทนผู้ใช้ ไม่ใช่ Windows service/auto-start หลัง reboot และไม่มี supervisor restart เมื่อ process crash

Bonsai8088 ยังออฟไลน์ การคืนโมเดลที่เคยถูก policy ปฏิเสธยังต้องรอ authorization ของเจ้าของตาม checkpoint เดิม จึงไม่ได้เรียกคืน และ background reviewer ที่ผูกกับ Bonsai ยังทำ inference ไม่ได้ Active-chat Stop/Queue/Steer และ Discord roundtrip ยังเป็นช่องว่างเดิม ไม่ถือว่าถูกแก้โดย launcher นี้
