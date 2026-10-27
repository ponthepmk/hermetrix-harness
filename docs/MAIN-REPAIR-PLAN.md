# แผนแก้ระบบหลัก Hermetrix — 2026-10-03

ปรับขอบเขตล่าสุด 2026-10-10: ผู้ใช้ยืนยันให้ Hermetrix และ Project Brain เป็นระบบอิสระ ใช้ร่วมกันผ่าน MCP เท่านั้น ระบบทำงาน/แผน/reviewer/Stop/Queue/Steer/Desktop ให้แก้ภายใน Hermetrix ความรู้เป็น optional reference และไม่ให้สิทธิ์สั่งงาน ดู [ผลตรวจและแผน Hermetrix ล่าสุด](reviews/2026-10-10-hermetrix-plan-review.md) และ [รายงาน Pi แยกต่างหาก](reviews/2026-10-10-project-brain-audit.md)

สถานะล่าสุด 10 ตุลาคม: ยังไม่ครบเจ็ดขั้น แต่คืน Bonsai เดิมตามคำสั่งผู้ใช้แล้ว; Model/Task ตัวอย่างและ Stop→Queue→Steer ของ Chat/Workspace ผ่านกับโมเดลจริง เปิด preview76 ที่ http://127.0.0.1:18810/; ทดลอง migration73→76 ข้อมูลเดิม105ตารางคงเดิม รุ่นหลัก18804ยังเป็น73 ดู [ผลทำต่อและงานที่ยังค้าง](reviews/2026-10-10-bonsai-chat-controls.md)

เป้าหมาย: เปิดระบบได้ต่อเนื่อง รับคำสั่ง/วางแผน/คุมการทำงาน/ตรวจผลได้จริง มีสถานะและทางแก้เมื่อเกิดปัญหา พร้อมรวมความสามารถที่ตรวจแล้วเข้าสู่รุ่นหลัก โดยคงข้อมูลและงานเดิม

## หลักฐานตั้งต้น 3 ตุลาคม (ประวัติก่อนการแก้รอบปัจจุบัน)

- 18801/18804 healthy schema73, 18806 healthy schema74; source ปัจจุบัน schema74 จึงยังไม่ใช่ executable รุ่นเดียวกัน
- Docker `hermetrix-delivery-delivery-1` ที่18920 stopped ExitCode2, OOMKilled=false; log ที่อ่านมีเพียงข้อความเริ่มระบบ ยังไม่พิสูจน์สาเหตุ Compose มี `restart: unless-stopped` อยู่แล้ว
- Qwen personal chat ผ่านจริง (`MAIN-FLOW-OK`,476 token) แต่ไม่ได้พิสูจน์ coding/Task/desktop ทั้งวงจรล่าสุด Bonsai8088 ออฟไลน์และการคืนโมเดลครั้งก่อนถูก policy ปฏิเสธ
- แชตหลักไม่มี Stop/Queue/Steer ที่ตรวจพบ; มี turn lease และ MaxWallTimeSeconds อยู่แล้ว รวมถึง cancel ของ command jobs/team/browser/desktop/Discord ซึ่งต้องใช้เป็นเส้นทางเทียบเคียง
- Desktop คลิกซ้าย/ภาพหน้าจอ/approval receipt ทดสอบแล้วใน18806; live-model command และ vision ยังไม่ได้ยืนยัน
- Discord เคย disabled/unconfigured; protocol fixture ผ่าน แต่ bot จริง Ready/รับคำสั่ง/ส่งผลกลับยังไม่ยืนยัน
- Easiiot task revision31 และ Delivery task revision101 ยัง verifying รอการรับรองจากผู้ใช้

## ลำดับและเกณฑ์ผ่าน

| ลำดับ | งาน | ผ่านเมื่อ |
|---|---|---|
| 1 | หาสาเหตุและกู้ Delivery18920 | เปิดเว็บได้, ข้อมูลเดิมครบ, สี่บทบาททำงานได้, การหยุดและกู้คืนที่ทดสอบไม่สร้าง order/effect ซ้ำ |
| 2 | ตรวจ model route และงานหลักครบวงจร | โมเดลที่เลือกตอบจริง, รับเป้าหมาย→แผน→review→apply→verify สำเร็จในโปรเจกต์ทดสอบ พร้อม negative/rollback case |
| 3 | Stop แชต/งานที่กำลังทำ | คำขอหยุดตรง turn/run, ไม่มี sampling/dispatch ใหม่หลังหยุดมีผล, ผลที่เกิดแล้วมี receipt, ไม่ทราบผลต้อง reconcile |
| 4 | Draft/Queue แล้วค่อย Steer | พิมพ์ต่อได้, คิวเห็น/ยกเลิกได้, ไม่ข้าม session/project/provider scope; Steer ทำ revision ใหม่และทบทวน authority ก่อนทำต่อ |
| 5 | Desktop ผ่านคำสั่ง AI จริง | เลือกหน้าต่าง→ตรวจสภาพ→เสนอ action→อนุมัติ→คลิก→ภาพหลังและตรวจผลสำเร็จ รวม stop/stale/replay rejection |
| 6 | Discord จริง | Ready แล้ว new/ask/status/approve/deny/cancel ผ่าน bot จริง, สิทธิ์ไม่ตรงถูกปฏิเสธ, ตอบจบงานหลัง cancel ได้และไม่ replay |
| 7 | รวมรุ่นหลักและตรวจร่วมกัน | preview ใหม่ผ่าน API/browser/native/Discord ที่ตั้งค่าแล้ว; migration และการกลับรุ่นผ่านบนข้อมูลสำเนา ก่อนเปลี่ยน instance หลัก |

แต่ละช่วงต้องมี reproduction, การแก้ต้นเหตุ, targeted regression และผลจาก flow จริง เก็บ failure receipts ด้วย หาก dependency ยังไม่พร้อม ให้ทำส่วนที่ไม่พึ่ง dependency ต่อและระบุส่วนที่ยังไม่ผ่านตามจริง

## 1. Delivery: แยก containment ออกจากแก้ต้นเหตุ

ตรวจ Docker events/daemon/container exit และเวลาที่หยุด, image/runtime config, filesystem/SQLite integrity และ process lifecycle ก่อนเปลี่ยน source ไม่สรุปว่าเป็น crash จาก ExitCode2 เพียงอย่างเดียว ตรวจว่าเป็นการหยุดโดยผู้ใช้/daemon หรือ application ก่อนตัดสินว่าต้องแก้ตรงไหน

สำรอง SQLite อย่างสอดคล้องพร้อม WAL และเก็บ image digest/config/count/hash ของข้อมูลเดิม กู้ด้วย image/volume เดิมก่อนถ้า integrity ผ่านโดยระบุเป็น containment; ไม่ล้าง volume หรือ seed ทับเพื่อให้ health ผ่าน

ใน preview ใช้ข้อมูลสังเคราะห์ ทดสอบ admin จัดการ→ร้านค้ารับ/เตรียม→ไรเดอร์รับ/ส่ง→ลูกค้าเห็นผล รวมแก้ order ก่อนจัดสรร, rider งานเดียว, failed/retry/return, permission/version conflict, ราคา/บัญชี/CSV และ recovery หลัง restart ตรวจ live เดิมเฉพาะเส้นทางที่ไม่เปลี่ยน order จริง

ขอบเขต: `D:/Projects/hermetrix-delivery`, docker-compose.yml, runtime/store/handlers ตามต้นเหตุจริง หลักฐานเพิ่มใน docs/ ของโปรเจกต์นั้นและเชื่อมจาก Harness

## 2. Model readiness และ Task ที่ทำงานจนตรวจผล

แยกสถานะ UI พร้อม, provider มี config/credential, โมเดลตอบจริง, reviewer พร้อม และ tool/context qualification ไม่รวมเป็นคำว่า ready คำเดียว ไม่มีการเปลี่ยน gateway หรือให้ remote authority แทน local โดยเงียบ ๆ

ใช้ Qwen profile เดิมเป็นเส้นทางที่มีหลักฐานสำหรับทดสอบข้อความสังเคราะห์ ตรวจการใช้กับ planner/coding/reviewer ตาม contract ของแต่ละบริการก่อนเลือกเส้นทางจริง ตรวจ reasoning/output budget สำหรับแผนยาว เพราะ short-chat ผ่านไม่ได้แปลว่า compact profile เหมาะกับ planning และไม่ใช้ override เป็นหลักฐาน qualification

ทำ Task ใหม่ใน checkout ทดสอบที่ย้อนกลับได้: ให้โมเดลวางแผนแก้ feature เล็กที่มี acceptance ชัดเจน ตรวจแผน/สิทธิ์/ไฟล์, proposal review, apply, ผลตรวจ และการคืน preimage เมื่อ mandatory check ล้มเหลว ใช้ fixtures/tests คงที่ ไม่แก้ tests เพื่อให้ผ่าน ตรวจผลผ่าน browser/API/ไฟล์จริงและผูกทุกผลกับ revision

Bonsai ที่เคยถูกปฏิเสธไม่ถูกเปิดซ้ำจากแผนนี้ Background reviewer ต้องเลือก/configure อย่างชัดเจนก่อนทดสอบ ใช้ synthetic evidence เท่านั้น ไม่ขยายการส่งข้อมูลโปรเจกต์หรือคัดลอก vault ข้าม instance โดยอัตโนมัติ

ขอบเขต: providers, agent, taskcoord/taskengine, startup reviewer binding และ UI model/status ตาม defect ที่ reproduce ได้ ไม่เปิด self-improvement lab หรือ promote Skill เพื่อเป็นทางลัด

## 3. Stop ที่สอดคล้องกับ turn lease และผลของเครื่องมือ

ตรวจ known-good cancellation ของ jobs/team และคำตอบจบหลัง cancel ใน discordbridge เทียบกับ RunTurn/approval continuation แล้วกำหนด contract ร่วมให้ session owner สั่งหยุด exact turn/run ได้อย่าง idempotent

เพิ่ม durable stop request/receipt และ cancellation ของ inference/tools ที่รองรับ การกดส่ง request ไม่เท่ากับหยุดสำเร็จ UI แสดงกำลังหยุด→หยุดแล้ว/ยังรอเครื่องมือ/ต้องตรวจผล พร้อมเวลาล่าสุดและสิ่งที่เกิดไปแล้ว ปลด lease เมื่อ execution ยืนยันจบอย่างถูกต้อง ไม่ทำให้ session active ทั้งที่ tool ยังทำงาน

ทดสอบก่อน sampling, ระหว่าง stream, ก่อน dispatch, ระหว่าง command, รอ approval, หลัง effect, double-stop, turn เก่า, ผู้ใช้ผิดคน, disconnect และ restart การตัด HTTP connection ไม่ใช่กลไก Stop เพียงอย่างเดียว และต้องไม่ dispatch approval ที่ถูกหยุดแล้ว

ขอบเขต: internal/agent/service.go และ runtime/tools ที่เกี่ยวข้อง, internal/store, internal/web/server.go และ chat UI ใช้ preview schema migration ถ้าจำเป็น

## 4. Draft/Queue ก่อน Steer

เก็บ draft ให้พิมพ์ได้ขณะทำงาน ทดสอบสลับหน้า/refresh/back โดยข้อความไม่หาย เพิ่มคิวที่เก็บถาวร มีสถานะ queued/running/completed/cancelled/needs-review และยกเลิกก่อนเริ่มได้

ผูก queue item กับ owner, exact session/project, provider/capability contract, idempotency key และ revision ที่เกี่ยวข้อง ตรวจ scope/budget/qualification ใหม่เมื่อถึงคิว เมื่อ contract เปลี่ยนให้รอตรวจ ไม่ย้ายคิวไปโปรเจกต์ที่เปิดอยู่โดยอัตโนมัติ ทุก item ยังต้องผ่าน turn lease เดิม ทดสอบส่งซ้ำ/ลำดับ/restart/active turn stopped โดยไม่ทำ effect ซ้ำ

Steer ทำหลัง Stop/Queue ผ่าน: แสดงว่าคำสั่งแก้ทิศทางจะใช้เมื่อไร สร้าง requirement/plan revision ใหม่, invalidate proposal/approval ที่ไม่ตรง และทบทวนก่อน sampling/dispatch ถัดไป ไม่ฉีดคำสั่งใหม่ลง authority ของแผนเก่า

ทดสอบ UI desktop1280×720 และ mobile390×844, keyboard/focus, loading/empty/error/reduced-motion โดยใช้สถานะจริง

## 5. Desktop: ปิดช่องว่างของคำสั่งจริงก่อนเพิ่ม input

เริ่มกับหน้าต่างทดสอบที่เราเป็นเจ้าของ โดยต่อ model ที่มี capability/qualification เหมาะสม Session ใหม่ต้องเห็น desktop tool ใน frozen contract ใช้ UIA/text เป็นเส้นทางแรกได้ หากต้องใช้ pixels ต้องมี qualified image provider และ approval ของข้อมูล/ปลายทางที่ชัดเจน Qwen text profile เดิมไม่ถูกถือว่ารองรับภาพโดยไม่มีหลักฐาน

พิสูจน์คำสั่งภาษาธรรมชาติหนึ่งงานจริงให้ครบ screenshot/inspect→action preview→exact approval→mouse action→after screenshot→ตรวจผล ทดสอบ window ปิด/เปลี่ยน, frame เก่า, replay, cancel, stop และ timeout

หลังวงจรนั้นผ่านจึงเพิ่ม right-click, drag และ typing/key controls ทีละชนิด พร้อม bounds/window binding/one-use receipts และ negative cases ห้ามใช้การคลิก native fixture เพียงอย่างเดียวอ้างว่าโมเดลเข้าใจภาพหรือทำงานได้ครบ

## 6. Discord: เชื่อม bot จริงและ reuse core controls

ใช้ bot/guild/channel/user allowlist ที่เจ้าของระบุ บันทึก token ผ่าน app vault ไม่ส่ง token ในแชต ต้องยืนยัน model binding ที่ bridge รองรับ; ปัจจุบัน bridge ผูก local model การใช้ Qwen remote ผ่าน Discord เป็นขอบเขตใหม่ที่ต้องออกแบบ policy/approval โดยชัดเจน ไม่เปิดด้วย flag ลัด

เมื่อ settings/credentials พร้อม ทดสอบใน channel ทดสอบที่ได้รับอนุญาต: Gateway Ready, new/ask/status, approved/denied action, cancel ระหว่าง inference/command และ terminal reply ทดสอบ reconnect, duplicate interaction, expiry, unauthorized user/channel และ configuration revocation ทุก remote action ใช้ core contract/receipt เดียวกับเว็บ

DM/mention/thread, attachments และ voice เป็นงานต่อยอดหลัง slash-command flow ผ่าน ไม่ถือว่ารอบนี้ทำ Hermes parity ทั้งหมด

## 7. รวมรุ่นและตรวจ acceptance ร่วมกัน

เก็บ executable/source/data fingerprint ของ18801/18804/18806 แยกกันก่อนรวม; build รุ่นใหม่ลง path ใหม่ ใช้ preview port และ database สำเนา ไม่เริ่ม binary ใหม่ทับ live schema73 ขณะยังไม่ตรวจ migration

ทำ migration73→รุ่นใหม่และ restart/recovery บนสำเนา ตรวจ Task31/101, versions/approvals/effects, vault access, row/hash invariants และ browser history/forms จากนั้นตรวจ combined flow รับคำสั่ง→วางแผน→review→ลงมือ→Stop/Queue→recover→verify พร้อมกรณี offline/reasoning budget/permission/rejected plan/test failure

ก่อนสลับรุ่นตรวจว่าไม่มี planned/dispatched/uncertain effect ที่ยังไม่ reconcile สำรอง data อย่างสอดคล้อง เก็บ binary คู่ schema และเปิดหลักหลัง readiness ผ่าน ให้ข้อมูลใหม่หลัง cutover มีแนวทาง reconcile/export; ห้าม rollback ด้วย backup เก่าจนข้อมูลใหม่สูญหาย

เกณฑ์ปิด: automated checks ที่เกี่ยวข้องผ่าน และ user-visible business outcomes ผ่านพร้อม receipts/skips ที่ระบุ ไม่มี replay/duplicate/authority widening และปุ่มรับรองผลยังเป็นการตัดสินใจของผู้ใช้ Easiiot31/Delivery101 ไม่ถูก auto-accept จากการทดสอบนี้

## ขอบเขตที่พัก/ขึ้นกับข้อมูลภายนอก

- Self-improvement, autonomous successor และ promotion ของ candidate ยังพัก
- Token/bot binding, model restoration ที่เคยถูกปฏิเสธ และการส่ง pixels ออกภายนอกต้องมี authorization/configuration ที่ตรงขอบเขตก่อนทดสอบจริง ขอเฉพาะตอนมีผลให้ตรวจและพร้อมดำเนินการ ไม่ขอซ้ำสำหรับสิ่งที่ได้รับอนุญาตแล้ว
- Public rollout/GPS/payment/notifications/account recovery/native app ของ Delivery เป็นงาน integration แยกจากการกู้18920และแก้ core รอบนี้ ต้องกำหนดบริการ/บัญชี/ขอบเขตจริงก่อนอ้างว่าพร้อมใช้งานภายนอก
- ไม่ commit/PR/deploy สาธารณะจากคำสั่งวางแผนนี้

งานต่อไปใน Hermetrix: ทบทวน preview76 ที่ผ่าน Model/Task และ chat Stop→Queue→Steer แล้ว ก่อน cutover designated main instance; เพิ่ม Task Goal/Plan steer และตรวจ Desktop/Discord จริง; ตรวจDocker/Delivery applicationแยกจากHarness launcher เพราะตอนตรวจ10ตุลาคม Dockerengineไม่พร้อม หากกู้ด้วยimage/volumeเดิมต้อง consistent snapshot และรายงานcontainmentเมื่อยังไม่พิสูจน์เหตุที่หยุด ห้ามถือว่าProjectBrainพร้อมแล้วเท่ากับlocalexecutionพร้อม
