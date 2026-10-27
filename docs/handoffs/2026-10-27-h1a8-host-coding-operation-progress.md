# ผลทดสอบร่างบริการรับคำสั่งและกู้คืนงาน

> Historical candidate32 record. Superseded by actual H1A8b applied37/schema80 and verified native host V2. Current state: [NEXT_SESSION](../NEXT_SESSION.md); actual post-apply proof: [H1A8b result](../evidence/2026-10-27/project-brain-measurement/prior/H1A8b-post-root-result.json). Do not use the candidate-only/not-applied statements below as current state.


27 ตุลาคม 2026 ร่างบริการ CodingOperations เชื่อมการรับคำขอเข้ากับ Task และ Goal ที่รันแก้ไฟล์จริงแล้ว ชุดร่าง 32 ไฟล์ Go มาจาก Bonsai Local ทั้งหมด แบ่งเป็น production 14 และ test 18 ไฟล์ ยังไม่ได้นำเข้าโค้ดหลัก ซึ่งยังอยู่ที่รุ่นฐานข้อมูล 79

บริการบันทึกคำขอเดิมและ Task ใน transaction เดียวก่อนสร้าง Goal การส่ง OperationID เดิมพร้อมคำขอเดิมคืนสถานะงานเดิม ส่วนคำขอที่เปลี่ยนภายใต้ ID เดิมถูกปฏิเสธ Get อ่านสถานะได้แม้ยังไม่มี Goal และ Advance ใช้เส้นทางวางแผน แก้ไฟล์ ทดสอบ และตรวจรับที่มีอยู่

## ผลที่ยืนยันแล้ว

`H1A8-op-functional32.log` ผ่าน 13 ฟังก์ชันทดสอบและ 20 subtests รวม 33 events ใช้ฐานข้อมูล SQLite และการเขียนไฟล์/รัน Node จริง โดยจำลองเฉพาะการตอบของโมเดล

- คำขอพร้อมกันสี่ชุดสร้าง Task, operation และ Goal เพียงชุดเดียว และคืนผลเหมือนกัน
- ความล้มเหลวขณะบันทึก ทั้ง SQL ABORT และการเขียนที่กระทบศูนย์แถว ย้อนกลับ Task และ operation พร้อมกัน รักษาสาเหตุเดิม และส่งคำขอเดิมใหม่สำเร็จได้
- เทสจงใจให้ข้อเสนอแรกผิด ตรวจพบจากคำสั่งจริง ย้อนคืนไฟล์ และส่งข้อมูลความล้มเหลวเข้าสู่การแก้รอบต่อไปจน Task และ Goal completed ไฟล์ทดสอบเดิมคง byte เดิม
- เปิด SQLite และบริการใหม่หลังการวางแผนแล้วส่งคำขอเดิมกลับมา ได้งานและประวัติเดิมโดยจำนวนการเรียกโมเดลไม่เพิ่ม การส่งซ้ำหลัง completed ไม่เปลี่ยนผล ประวัติ หรือจำนวนการเรียก
- เปลี่ยนคำขอ เปลี่ยนโฟลเดอร์หรือโปรไฟล์ก่อนสร้าง Goal ผู้เรียกที่ไม่รู้จัก ข้อมูล UTF-8 เสีย และ envelope ผิดเงื่อนไข ถูกปฏิเสธตามกรณีที่ทดสอบ
- แถว operation แก้ไขหรือลบผ่าน SQL ไม่ได้ การอัปเกรดจากสถานะจำลอง schema79 รักษา Task เดิมและตาราง behavioral_eval_runs พร้อมใช้งานบริการใหม่

รายละเอียดขอบเขตอยู่ใน [สัญญาบริการ](2026-10-27-h1a8-host-coding-operation-contract.md) และ [หลักฐานตรวจรับร่าง](../evidence/2026-10-27/h1a8-host-candidate32/receipt.json) เก็บ1324 artifacts พร้อม manifest ตรวจ SHA ครบ ไม่มีไฟล์ Go ที่เพิ่ม package ในชุดหลักฐาน ชุดทดสอบใช้ functional32 overlay; prototype flow ที่อ้าง API ผิดไม่ได้ถูกเลือกนำเข้า และไม่มีการตัด standing test จริงออก

## Regression ที่พบและแก้แล้วในร่าง

การทดสอบทั้ง repository รอบแรกพบว่า migration ใหม่แทนขั้น79ด้วย80 ทำให้ฐานข้อมูลใหม่ขาด behavioral_eval_runs แม้เทสเส้นทางแก้โค้ดจะผ่าน Native repair คืนขั้น79และต่อ80ตามลำดับ เทส TestBehavioralRun ใน agent และ skills กลับมาผ่าน การตรวจ user_version ของ migration78 เปลี่ยนจากเลข79ตายตัวเป็น store.CurrentSchemaVersion โดยคงการตรวจตารางและข้อมูลเดิม

ผลตรวจทั้ง repository ของร่าง32ล่าสุดผ่าน1418 test events,0failures,20skips ตามเงื่อนไข พร้อม vet/build ผ่าน เป็นการตรวจ candidate overlay ยังไม่มี native review หรือ source apply Source-manifest SHA45f85870ab779682b04e6df87634d5109f4200ba48faa41511df176e16b77d1c และ qualification SHA590ec17d1077ae24207cc89aee11727c9fec374a00a7b12e7aedaf6bdb82c144 ตรวจ ancestry105ขั้นยืนยัน source จาก native output+gofmt และ Go source จริง716ไฟล์กับ frozen DATA5ชุดคง hash เดิมทั้งหมด

## ต้นทุนการสร้างร่าง

จาก raw completions118คำขอ ใช้ prompt218900/output90252 tokens โดย109คำตอบผ่าน protocol,105ขั้นถูกใช้ใน ancestryสุดท้าย และ4prototypeที่ผ่าน protocol ไม่ถูกเลือกใช้ อีก9คำตอบถูกปฏิเสธและไม่มีการ salvage หลายร่างต้องแก้ชนิดข้อมูล API ชื่อ field syntax และ oracle ของเทส รวมทั้ง regression migration ที่ตรวจพบจากการรันทั้ง repository ตัวเลขนี้เป็น observed adapter usage ยังต้อง reconcile กับ ledger ก่อนนำเข้า เป็นงานที่ Codex กำกับ ไม่ใช่หลักฐาน autonomous learning

## การทดลองโมเดลจริงและงานที่เหลือ

เตรียม fixture Go จาก native seed เดิมแบบ byte ตรงกันแล้ว ฟังก์ชันบวกเลขผิดและ TestAdd ล้มเหลวจริงก่อนเริ่มแก้ source480099bytes ส่วนใหญ่เป็น comment padding จึงไม่ใช่หลักฐานความซับซ้อนทางธุรกิจ ยังไม่ได้ส่งการทดลองผ่าน host ใหม่ไปให้โมเดลจริง

ขั้นต่อไปคือทดลอง host ด้วย Bonsai Local ผ่านการวางแผนของโมเดลจริง บันทึกคำขอก่อน effect เปิดบริการใหม่แล้วกู้คืน และตรวจการส่งซ้ำเมื่อ completed ใช้ฐานข้อมูลแยกเพื่อไม่ให้ร่าง schema80 อัปเกรดฐานข้อมูล authoring79 ก่อนนำเข้าโค้ดหลัก จากนั้นจึงทำ native review, native apply และตรวจหลังนำเข้า

เป้าหมายเต็มยังต้องมีวงจรประเมินการเรียนรู้ที่รันจริงและตรวจสอบหลักฐานได้ก่อน promotion การวัดผลกับงานใหม่ UX ควบคุมเครื่อง และ marketplace เต็มระบบทั้ง Compose และ Kubernetes ร่างบริการนี้เป็นส่วนเชื่อมการทำงาน ไม่ใช่หลักฐานว่าระบบเรียนรู้เองหรือพร้อม production แล้ว
