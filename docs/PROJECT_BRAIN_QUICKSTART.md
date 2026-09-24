# เริ่มใช้ Hermetrix + Pi Project Brain

คู่มือนี้เป็นขั้นตอนใช้งานของระบบที่เปิดอยู่ในวันที่ 2026-09-24 ไม่ต้องสร้างหรือวาง token ใหม่เพื่อทดลองบนเครื่อง Windows บัญชีนี้ เพราะ Codex และ Hermetrix มี credential แยกกันที่บันทึกไว้แล้ว

## เปิดใช้งาน

1. ดับเบิลคลิก `C:\Users\ZP2E0\AppData\Local\Hermetrix\Start Hermetrix.cmd` แล้วเปิด `http://127.0.0.1:7331/` บนเครื่องเดียวกัน
2. เลือกโปรเจกต์ `hermetrix-harness` และเริ่มแชทใหม่ ลองถามว่า **“ในโปรเจกต์นี้ ถ้าจะเพิ่ม planning task ต้องใช้ Platform Project ID หรือ board project ID? อธิบายวิธีตรวจให้ถูกต้อง”** Hermetrix จะค้นความรู้ที่ Pi รับรองแล้วให้ local model ก่อนตอบ คำตอบที่ผ่านการทดสอบจริงแยก ID สองชนิดนี้ได้
3. หากต้องการดูบอร์ดงาน ให้ดับเบิลคลิก `C:\Users\ZP2E0\AppData\Local\Hermetrix\Show Pi Board Login.cmd` โปรแกรมจะเปิด `https://kanban.go2gether-tech.online/` และแสดงรหัส `owner` ในหน้าต่างของเครื่องนี้ อย่าส่งรหัสในแชท
4. หากต้องการให้ Codex อ่านหรือแก้ planning บน Pi ให้เปิด **Codex session ใหม่** เพื่อโหลด MCP profiles ที่บันทึกไว้ แล้วสั่งด้วยภาษาปกติ เช่น “ดูงานของ hermetrix-harness บน Pi และสร้างแผนสำหรับ …” Codex มีสิทธิ์ `work.read`/`work.write` เฉพาะโปรเจกต์ที่ได้รับ grant; Hermetrix ใช้ profile อ่านความรู้แยกจาก profile `candidate_submit` สำหรับส่งข้อเสนอที่เจ้าของงานยืนยัน

| สิ่งที่ใช้ | ปลายทาง | เจ้าของงาน |
|---|---|---|
| แผนและสถานะงานผ่าน MCP | `https://agent-knowledge.go2gether-tech.online/mcp` | Pi Agent Platform / Kanban |
| ความรู้ที่คัดเลือกแล้วผ่าน MCP | `https://secondbrain.go2gether-tech.online/mcp` | Pi Second Brain / Project Brain |
| หน้าบอร์ดสำหรับคน | `https://kanban.go2gether-tech.online/` | Kanban UI |

สอง MCP ใช้ Bearer token คนละชุดที่เก็บในเครื่องนี้ Cloudflare Tunnel เป็นทางเข้า HTTPS; การตั้งค่านี้ **ไม่ต้องใช้ Cloudflare Access service token** เว็บบอร์ดใช้ Basic login แยกจาก MCP token สำหรับ Hermetrix โทเคน `candidate_submit` เป็นอีกชุดหนึ่ง: ส่งได้เฉพาะข้อเสนอของโปรเจกต์ `hermetrix-harness` อ่านความรู้ เขียนข้อมูลทั่วไป และรับรองข้อเสนอไม่ได้

## เช็กว่าใช้งานได้จริง

- Hermetrix เปิดได้และ `http://127.0.0.1:7331/api/health` ตอบ HTTP 200
- คำถามตัวอย่างข้างบนได้รับคำตอบที่อธิบายว่า **Platform Project ID ใช้กำหนดสิทธิ์/ผูกบอร์ด ส่วน task API ใช้ board project ID** พร้อมขั้นตอน `list_platform_projects` → `list_projects` เทียบ `platform_project_id` → ใช้ board `id` กับ task tools การเปิดหน้าเว็บหรือเห็น MCP `ready` อย่างเดียวไม่ใช่หลักฐานว่าความรู้ถึง local model
- Codex session ใหม่มองเห็น MCP profiles ชื่อ `agent-knowledge` และ `project-brain`; การค้น Project Brain จำกัด `project=hermetrix-harness` และพบหัวข้อ Solution ของบทเรียนเรื่อง board/project ID
- เว็บบอร์ดเปิดด้วย login ที่บันทึกไว้และเห็น board project ของ Hermetrix; ไม่จำเป็นต้องทดสอบด้วยการสร้างงานใหม่ซ้ำ เพราะ [หลักฐานการสร้างและลบงานทดสอบ](project-brain-verified-planning-flow.md) บันทึกไว้แล้ว

ถ้า Hermetrix เปิดไม่ได้ ให้เช็กว่า Start script ยังรันอยู่และพอร์ต `7331` ไม่ถูกโปรแกรมอื่นใช้ ถ้าแชทตอบได้แต่ไม่ใช้ความรู้ Pi ให้ตรวจว่าเลือกโปรเจกต์ `hermetrix-harness`, MCP profile `Project Brain` พร้อมใช้งาน และ credential ยังไม่หมดอายุ ถ้า Codex เจอ `403` ตอนเพิ่ม task ให้แยก Platform Project ID ออกจาก board project ID ก่อน อย่านำ ID ตัวแรกไปส่งให้ `add_task`

## ความรู้ใหม่เข้าระบบอย่างไร

Codex ส่ง `submit_knowledge_candidate` พร้อมผลตรวจและหลักฐานที่อ้างอิงแบบ immutable ได้ ส่วน Hermetrix มีทางส่งจากงานในเครื่องที่ **เสร็จแล้ว** ดังนี้:

1. เปิด **Plans** แล้วเลือก Task ที่ขึ้นสถานะ `completed` เลื่อนมาที่หัวข้อ **แชร์วิธีแก้ที่ตรวจแล้ว**
2. หากโปรเจกต์ งาน หรือ artifact ยังเป็นส่วนตัว ให้กดอนุญาตเฉพาะรายการที่ต้องการ ระบบถามยืนยันทุกครั้ง การเปิดสิทธิ์เลือกส่งออกยังไม่ส่งข้อมูลไป Pi
3. เลือกผลตรวจ `pass` ของเกณฑ์งานฉบับปัจจุบัน เลือก artifact ที่ต้องการอ้างอิง 1–8 รายการ และเขียนวิธีแก้ที่นำกลับมาใช้ได้
4. กด **ตรวจสิ่งที่จะส่ง** เพื่อดูปัญหา วิธีแก้ ผลตรวจ และ digest ของหลักฐาน แล้วกด **ยืนยันส่งข้อเสนอนี้ให้ Pi ตรวจ** อีกครั้ง ระบบไม่ส่งอัตโนมัติ
5. ดูสถานะใน Task: **รอส่งไป Pi** หมายถึงอยู่ใน outbox ทนทานของเครื่อง; **Pi รับข้อเสนอแล้ว** หมายถึงส่งถึง Pi แต่ยังไม่ผ่านการรับรอง ถ้าส่งไม่ได้ให้ดูสาเหตุและตรวจสิทธิ์แชร์/ผลตรวจอีกครั้ง การส่งซ้ำไม่รันงานหรือ Effect ใหม่

candidate ยังไม่เป็นความรู้ที่ระบบหยิบไปใช้ ผู้ดูแล Pi แยกจากผู้ส่งต้องตรวจหลักฐาน ความซ้ำ และความขัดแย้งในคิว แล้วจึงรับรอง หลังรับรอง Hermetrix/Bonsai และ agent อื่นจึงค้นหน้าเดียวกันได้ ขั้นตอนรับรองยังเป็นงานคน; ยังไม่มีการดึงบทเรียนจากทุก session หรือ promote อัตโนมัติ

Credential อ่าน Project Brain ชุดเดิมหมดอายุ **2026-12-22 19:30:19 UTC** และ credential ส่ง candidate ใหม่หมดอายุ **2026-12-23 04:14:13 UTC** ต้องหมุนเวียนก่อนวันดังกล่าว Hermetrix ยังไม่มีสิทธิ์เขียน planning หรือรับรองความรู้บน Pi; ระบบยังไม่มี Platform Router หรือ managed remote execution ดูรายละเอียดการดูแลและขอบเขตที่ตรวจจริงใน [คู่มือปฏิบัติการ](PROJECT_BRAIN_OPERATIONS.md) และ [ผลทดสอบหลัง rollout](project-brain-rollout-review.md)
