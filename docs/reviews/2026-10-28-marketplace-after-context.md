# Marketplace หลังแก้ historical context: ผลการรันจริง

วันที่ 28 ตุลาคม 2026 · งานหลักยัง ACTIVE · API และ marketplace ยังไม่สำเร็จ

รัน native Harness/Bonsai Local 64k จริงภายใต้ source `b9ad2b8` ซึ่งมีตัวแก้ historical context `70de7de`/`366447f` แล้ว เก็บงานเดิมที่หมดเวลาไว้ครบ ปิด failed proposal attempt ผ่าน API กู้คืนเดิม และเริ่ม goal ใหม่บน task/requirement เดิม ไม่รีเซ็ต counter/deadline ของ goal เก่า รหัสใหม่มีข้อความ 20261029 แต่เวลา created_at/deadline จริงเป็นวันที่ 28 ตามหลักฐาน

## สิ่งที่เกิดจริง

- ใช้ 45 actions, 6 attempts, 3 plans และ 14 model calls รวม prompt 72,880 / completion 23,008 tokens ทั้ง 14 คำตอบจบแบบ structured tool call ไม่มี response ถูกตัดในรอบนี้ ตัวเลขนี้ไม่ใช่สถิติรับรองความเสถียรหรือผลทดลองแบบ paired
- คำขอสร้างโค้ดจริงรายการที่ 2 มี historical source 6,824 ไบต์และ terminal เดิม 210 ไบต์พร้อม trust/provenance ขณะที่ main.go/main_test.go ปัจจุบันยัง absent ตามจริง จึงยืนยันได้ว่าการส่ง context เข้าถึงโมเดลแล้ว
- Native source 6 ชุดถูกนำเข้า รันคำสั่ง Go จริง แล้ว rollback ทุกชุด เพราะ compiler error เช่น variable scope, signal.Notify, httptest request และการใช้ method ของ struct ผิด ยังไม่มี Go แอปสองไฟล์ที่คงไว้ และไม่เคยถึง reviewer/real PostgreSQL acceptance
- การสร้างแผนใหม่มีทั้งการปรับที่ถูกและการคงตัวอย่าง API ที่ผิด จึงยังอ้างไม่ได้ว่า history อย่างเดียวทำให้โมเดลแก้งานจนจบหรือเรียนรู้ได้

## สาเหตุที่หยุดก่อน planner dispatch รอบต่อไป

บันทึก fail ของ build และ test เป็นคนละ check จากคนละ proposal ตัวแนบ history เดิมจำกัด 2 ไฟล์/8,192 ไบต์ต่อ record จึงแนบ source 6,664 + 6,588 = 13,252 ไบต์ในคำขอเดียว ข้อมูลก่อน manifest มี 30,109 ไบต์ แต่ conservative body limit ของ planner-large เป็น 27,952 ไบต์ ระบบจึงปฏิเสธก่อน dispatch ด้วย `planner context cannot retain every explicitly writable source path`

นี่เป็นเพดาน byte ของตัวจัด context ไม่ใช่หลักฐานว่า model context 65,536 tokens เต็ม preset/context ของโมเดลไม่เปลี่ยน การเพิ่ม model context อย่างเดียวจึงไม่แก้เงื่อนไขนี้

Native candidate ใช้ budget ประวัติโค้ดร่วมกันทั้งรายการ fail สูงสุด 2 ไฟล์/8,192 ไบต์ จัดลำดับใหม่→เก่าเหมือนเดิม record เก่าที่ไม่ได้ใส่ source ยังมี wrapper แสดง omitted/truncated พร้อม terminal และ provenance เดิม ไม่เปลี่ยน current source หรือสิทธิ์เขียน

Replay packet จริงด้วย candidate ผ่านแล้ว: ก่อน manifest เหลือ 21,507 ไบต์ มี source 6,664 ไบต์/2 ไฟล์ และระบุ omitted 2 ไฟล์ รายการไฟล์จำเป็นใส่ได้ครบ ข้อความ terminal/validation/artifact/task/step/proposal/hash เดิมไม่เปลี่ยน การอ่านไม่เปลี่ยน hash ตาราง task/goal/effects/proposals/validations ตัวแก้นำเข้าและ push `dev` แล้วใน `9077ec6`: 5 Go files + frozen DATA, native review approve ไม่มี findings, 5 write receipts/0 rollback; taskengine/taskcoord/worker 384 pass, 1 existing opt-in skip, 0 fail และ vet/build ทั้ง repository ผ่าน หลังนำเข้าตรวจ source/DATA hash และ replay packet จริงอีกครั้งได้ผลตรง candidate ทุกค่า การอ่านไม่เปลี่ยน task/run/goal/effect/proposal/validation rows ไม่มี active inference ณ post-apply checkpoint ไม่ได้กล่าวว่า full runtime suite ผ่าน เพราะยังมี Windows desktop approval-observation failure เดิม

## หลักฐานและงานต่อ

ผลพร้อม hash อยู่ [validation](../validation/2026-10-28-marketplace-after-context.json) หลักฐานดิบถูก ignore ใน `.tmp-ui/goal-20261023/live-20261026-001100/` รอบทดสอบเป็น operator supervision ใช้ native Harness เขียน Go; ไม่ใช่ Codex เขียน application source

พบ registry skills ใน isolated runtime นี้ว่าง และคำขอ worker ไม่มี selected skill content การเลือก/ตรึง/โหลด skill มีอยู่ใน agent session อีกเส้นทาง แต่ taskcoord ยังไม่ได้เชื่อมกลไกเดียวกัน ข้อนี้เป็นช่องว่างที่ตรวจพบ ไม่ใช่ข้อพิสูจน์ว่าถ้าเติม skill แล้วโค้ดจะผ่านทันที

เก็บ primary API reference จาก `go doc` ของ toolchain ที่ติดตั้งแล้ว 6 symbols รวม 8,722 ไบต์เป็น DATA local สำหรับทดลองการช่วยค้น API ต่อ ยังไม่ใส่ในโปรเจค/คำขอโมเดล และยังไม่ใช่ความสามารถค้น docs อัตโนมัติของ Harness ขั้นถัดไปต้องปรับคำแนะนำจากหลักฐาน API จริง โดยไม่ยอมรับแผนที่บอกให้ทำ syntax ผิดซ้ำ งาน buyer/seller/admin/payment/shipping/live/reports/Thai UI/Compose/Kubernetes ยังอยู่ในขอบเขตเดิมทั้งหมด

## เงื่อนไขก่อนทดลองคำแนะนำ Go รอบใหม่

ยังไม่ได้เริ่ม generation ใหม่หลัง aggregate-budget patch นี้ อย่า resume goal ที่หมด deadline หรือรีเซ็ต counter เดิม ก่อนปรับ requirement จริงให้ตรวจ awaiting_plan escalation ที่ค้างไว้ด้วย: PendingEscalation ปัจจุบันกรองเพียง task และ AutoPlan เรียก BuildNextStepPacket เมื่อต้องแนบ failure แม้การเปลี่ยน requirement จะทำให้ไม่มี active plan ประเด็นนี้ยังเป็นการพบจาก source ไม่ใช่ผลทดสอบ runtime ให้ทำซ้ำใน fixture แยกก่อนเปลี่ยน requirement ของงานจริง

การทดสอบใหม่ถูกแยกเป็นตัวโหลด DATA และตัวตรวจ generic เพราะ native รุ่นแรกยาวจนถูกตัด และรุ่นถัดมาสร้าง callback syntax/old-text anchors ผิด final ancestry ใช้เฉพาะคำตอบที่ผ่าน protocol และแก้ native แล้ว ไม่มีการซ่อม Go ด้วย Codex หรือปรับ expected DATA เพื่อให้ผ่าน รูปแบบสุดท้ายเป็น ordinary loop ตรวจครบ7กรณี14callsในหนึ่ง test function ข้อมูลเดิมไม่เปลี่ยน ไม่ใช้จำนวน test events เป็นจำนวนกรณีทดสอบ
