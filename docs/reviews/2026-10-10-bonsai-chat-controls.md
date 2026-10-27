# Bonsai, Task และการควบคุมแชต — 2026-10-10

ผู้ใช้สั่งทำต่อและอนุญาตชัดเจนให้เปิด Bonsai local เดิมเพื่อทดสอบ ข้อจำกัดเดิมที่ห้ามคืนโมเดลจึงถูกยกเลิกเฉพาะการทดสอบนี้ Hermetrix และ Project Brain ยังแยกกัน ใช้ข้อมูลความรู้ผ่าน MCP เท่านั้น ไม่แก้ Pi เพิ่มในรอบนี้

## ผลที่ใช้งานได้แล้ว

รุ่นทดสอบล่าสุดอยู่ที่ **http://127.0.0.1:18810/**, schema **76**, executable `.tmp-ui/hermetrix-controls-verified.exe`, data `.tmp-ui/chat-controls-release-data` ตัวเปิดตรวจ executable/data/workspace/reviewer ตรงกัน และ health รายงาน schema76/expected76/ok

- **Stop:** ผูกเจ้าของ/session/turn ที่แน่นอน บันทึกคำขอหยุดก่อนยกเลิก executor ตรวจสถานะโมเดลและงานที่เป็นของแชตนั้นก่อนคืนสิทธิ์ทำงาน เก็บผลเครื่องมือที่เกิดขึ้นแล้ว ปิด approval ที่ยังไม่ได้เริ่ม ถ้าผลยังไม่ชัดเจน คง turn ไว้ใน `control_blocked` แทนการบอกว่าหยุดสำเร็จ
- **Queue:** พิมพ์ระหว่างตอบได้ ข้อความรอคิวอยู่ในฐานข้อมูลและผูก owner/project/provider/contract/cache epoch ใช้ turn lease เดิมจองคิวและเริ่มคำสั่งใน transaction เดียว ตรวจเงื่อนไขเดิมอีกครั้งก่อนส่ง ป้องกัน retry ด้วย idempotency key และยกเลิกข้อความที่ยังไม่เริ่มได้
- **Steer ของแชต:** เก็บคำสั่งใหม่ หยุด turn เดิม และเริ่มคำสั่งใหม่หลังยืนยันการหยุดแล้วเท่านั้น พักข้อความเก่าที่อาจขัดกับทิศทางใหม่ไว้ให้ตรวจ ไม่ replay approval หรือ effect เดิม นี่เป็นขอบเขตแชต ยังไม่ใช่การเปลี่ยน Goal/Plan revision ของ durable Task ระหว่าง effect
- **Draft/recovery:** ข้อความที่พิมพ์ระหว่างรอไม่ถูกล้างเมื่องานก่อนหน้าจบ หลัง restart คิวที่ขึ้นกับงานที่ถูกขัดจังหวะต้องตรวจสอบก่อนทำต่อ ข้อความและใบรับผลยังอยู่
- **Models:** ปุ่มตรวจยืนยันว่าโมเดลว่างปลดการพัก inference เมื่อ `/slots` ตอบรายการที่ถูกต้องและทุก slot ว่างเท่านั้น ไม่ถือว่า HTTP cancellation คือหลักฐานว่าตัวโมเดลหยุด และไม่ส่งคำสั่งเดิมซ้ำ มีปุ่มตรวจยืนยันการหยุดในแชตสำหรับกรณีที่ผลยังต้องตรวจ

ต้นเหตุที่พบระหว่าง E2E: scheduler เดิมพัก local inference หลัง cancellation จึงทำให้คำสั่งใหม่ในคิวล้มด้วย `cancelled_runtime_idle_unconfirmed` เพิ่มการยืนยันสถานะจริงจาก runtime ก่อนปล่อยคิว โดยคงการพักไว้เมื่อปลายทางไม่ตอบ/ยังทำงาน/ข้อมูลไม่ครบ/slot ซ้ำ อ้างอิง [llama.cpp server API ปัจจุบัน](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) และตรวจ `/slots` ของ Bonsai ที่รันจริง

## โมเดลและ Task จริง

Bonsai เดิมโหลดเสร็จและ health8088 ตอบ `ok` ใช้ weights/executable เดิม ไม่เปลี่ยนโมเดลระหว่างงาน การโหลดรอบนี้นานกว่าหน้าต่างรอของตัวเปิด แต่ reuse โปรเซสที่เปิดไว้จนพร้อม ไม่มีการเปิดโมเดลซ้ำ

ทดสอบทั้ง Chat และ Workspace ผ่าน Chrome จริงกับ Bonsai: เริ่ม inference → พิมพ์ระหว่างทำงาน → บันทึกคิว → เปลี่ยนคำสั่ง → หยุดเดิม → รับคำตอบ `CONTROL_ACCEPTANCE_OK` → คิวเก่าถูกพัก มี stop receipt หนึ่งรายการ ตรวจปุ่ม/ช่องพิมพ์และการไม่ล้นหน้าจอที่390/768/1440px ทั้งระบบจำลองและโมเดลจริง รวมถึงตัว executable ที่18810

Task ตัวอย่าง `task_77b93381818aaf4786a4401a62bdfdb9` ใช้โปรเจกต์ `.tmp-ui/bonsai-task-fixture` แยกจากงานผู้ใช้:

1. บั๊กเดิม `add(a,b)` ลบเลขแทนบวก ทดสอบเดิม4กรณีมีผลผิดจริง
2. Bonsai วางแผนหนึ่งขั้น เลือกไฟล์ และเสนอแก้เฉพาะ `sum.js` โดยคง CommonJS exports
3. ตรวจ proposal ก่อนใช้กับ fixture; ผ่าน hash ของไฟล์ทดสอบเดิม, syntax check และทดสอบ4กรณีจริง
4. โปรไฟล์ผู้ตรวจต่างจากผู้เสนอ แต่ใช้ Bonsai weights เดียวกัน ตรวจผลอีกครั้งและให้ `approve` ไม่อ้างว่าเป็นโมเดลคนละตัว
5. Completion API revision6 รายงานทุก gate ผ่าน ยกเว้น user acceptance ซึ่งยังรอผู้ใช้ ไม่รับรองผลหรือปิด Task แทนผู้ใช้
6. สำรอง DB/ย้าย artifact ของ fixture ไป preview76 แล้วเปิดใหม่ ผลตรวจ/รีวิว/current completion ยังอยู่ครบและยังรอรับรองผล

ผลนี้พิสูจน์งานเล็กครบวงจร ไม่ใช่ qualification เต็ม64k หรือหลักฐานว่าทุกงานซับซ้อนทำได้ พบ negative case จริงด้วย: คำขอยาวใน profile compact32k ใช้งบคิดจนหมด4096token และไม่มีคำตอบ ระบบรายงานเหตุผลตามจริง การทดสอบ Stop จึงสังเกต token ที่เริ่มส่งรวมช่วงคิด ไม่รอจนเกิดข้อความคำตอบ

## ข้อมูลเดิมและการย้ายรุ่น

ทดลอง **73→76 บนสำเนาสอดคล้องของ DB เดิม**: integrity `ok`, foreign-key errors0, ข้อมูลเดิมทั้ง105ตารางมีจำนวนแถวและ hash เท่าเดิม Migration75เพิ่มตารางคิว/การหยุด และ76เก็บ job IDs สำหรับยืนยันผลหลัง restart รองรับ preview75 ทั้งก่อนและหลังมีคอลัมน์นี้

ตรวจรุ่นหลัก18804หลังคืน Bonsai: Task7, steps58, user acceptances1, effects108 พร้อม hash เท่า snapshot ก่อนเริ่ม รุ่นหลักยังเป็น binary/schema73 งานเดิมและการรับรองผลเดิมคงอยู่ ไม่เปิด self-improvement lab หรือ promote candidate

Build/evidence อยู่ที่ `docs/evidence/2026-10-10/chat-controls/` และ runtime receipts ที่ `.tmp-ui/bonsai-task-acceptance`, `.tmp-ui/chat-controls-browser` Binary SHA256: `b895fd84be451b9ae0b7eeac2ab6f0b63fd35dc03c5446ef6d408f5e19ebff2f`

## การตรวจและข้อจำกัด

Go full-suite ของ schema76 ผ่านครบ ดู receipt `chat-controls-go-results-schema76.txt` ประกอบ Node UI138/138ผ่าน, vet ผ่าน, browserจริงและ Bonsai ทั้งสอง composer ผ่าน และตรวจ migration จากทุก lineage, runtime-idle, pending-approval stop, foreign owner/stale turn, FIFO/idempotency/scope drift, restart/no-replay, persisted-owned-command reconciliation

Gateway fixture ทดสอบ agent/approval/denial จริงของ core แต่ mock Discord transport พบ deadline20ms ของ fixture ทำให้ heartbeat หลุดเมื่อรันชุดรวม ปรับเฉพาะ fixture เป็น200ms และ focused testผ่าน3รอบ ไม่เปลี่ยน production heartbeat

ระบบอนุมัติอัตโนมัติปฏิเสธ `Stop-Process` ของ preview เก่าเพื่อเปลี่ยน binary โดยระบุว่า action ถูกนโยบายบล็อก จึงเก็บโปรเซสเดิมและเปิดพอร์ตแยก Previewก่อนหน้า18807/18808/18809ยังเป็นหลักฐานเก่า ใช้18810เป็นผลล่าสุด ไม่มีการฝืนปิดโปรเซสหรือสลับ18804

## ยังไม่เสร็จในแผนใหญ่

ภายหลังผู้ใช้ลองเข้าจาก Tailscale ได้ตั้งทางเข้า **https://desktop-nh66lnq.tail0f7f57.ts.net:18810/** แล้ว ผ่าน gateway HTTPS ที่127.0.0.1:18811และ Tailscale Serve TCP18810 ไปยังรุ่นเดิม ไม่มีการเปิด DB/worker ซ้ำ ต้องใช้ token login เดิมของ Hermetrix โดยเก็บรหัสและ key เฉพาะในโฟลเดอร์ส่วนตัวที่ไม่เข้า Git ตรวจจาก Pi ผ่าน HTTPS จริง, Chrome ผ่าน certificate/login/project hydration/assets/cookie และตรวจ401/403/421/streaming/disconnect ผ่าน ตัวเปิด `scripts/start-tailnet-preview.ps1` reuse เฉพาะ gateway/config ที่ตรงกัน ยังไม่มี gateway autostart หรือ supervisor ต่ออายุ certificate อ่าน receipt `tailnet-preview-verification.json` และ `tailnet-preview-tests.txt`

- Cutover รุ่นหลัก18804 และรวม18801/18806พร้อม operational supervisor/autostart ยังไม่ทำ; ขณะนี้เป็น previewที่ตรวจแล้ว
- Steer ของ durable Task ต้องสร้าง Goal/Plan revision และทดสอบ invalidate proposal/effect ที่ถูกผูกไว้ ต่างจาก chat Steer ที่ผ่านแล้ว
- Desktop ด้วยโมเดลภาพและหน้าต่างทดสอบครบวงจรยังไม่ตรวจ ไม่ใช้ผลตอบข้อความของ Bonsai อ้างว่าควบคุม Windows ได้
- Discord bot/channel จริงยังไม่ตรวจ;18804รายงาน disabled การทดสอบ gateway fixture ไม่ใช่การส่งข้อความไป Discord จริง
- Delivery application18920/Docker recovery ยังแยกจากการซ่อมตัวเปิด Harness ไม่อ้างว่ากู้ application แล้ว
- Project Brain ยังมี backlog ในรายงาน Pi แยก ไม่เอาความพร้อมของ knowledge service มาแทน local execution readiness

อ่านคู่กับ [แผนหลัก](../MAIN-REPAIR-PLAN.md), [audit Hermetrix ก่อนทำต่อ](2026-10-10-hermetrix-plan-review.md) และ [audit Project Brain](2026-10-10-project-brain-audit.md)
