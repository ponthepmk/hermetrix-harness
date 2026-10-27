# ผลนำเข้า prepared run และขอบเขตการใช้งาน

วันที่ 27 ตุลาคม 2026 นำโค้ดที่ Bonsai Local สร้างเข้า source ของ Hermetrix แล้ว 28 ไฟล์ ผ่าน native review 6 ชุดและ native write 28 receipts โดยไม่มี rollback การทดสอบทั้ง repository หลังนำเข้า บน source จริงโดยไม่ใช้ overlay ผ่าน 1,353 test events ไม่พบ failure มี 20 opt-in/environment skips และ vet/build ผ่าน นี่เป็นความคืบหน้าของการเก็บหลักฐานก่อนเริ่มงาน ส่วน trusted runner และการเรียนรู้ครบวงจรยังต้องทำต่อ

หลักฐานหลักคือ [receipt](../evidence/2026-10-27/h1a5-prepared-run-applied/receipt.json), [source และ native ancestry](../evidence/2026-10-27/h1a5-prepared-run-applied/source-applied-manifest.json), [ผลทดสอบหลังนำเข้า](../evidence/2026-10-27/h1a5-prepared-run-applied/post-test-summary.json) และ [ผล review และต้นทุน](../evidence/2026-10-27/h1a5-prepared-run-applied/observed-cost.json)

## พฤติกรรมที่เพิ่มเข้า source

`PrepareBehavioralRun` เก็บ candidate revision/hash/blob และ base ของ Skill ปัจจุบันจากฐานข้อมูล พร้อม manifest ที่ตรวจรูปแบบและ hash แล้ว ก่อนมี effect ใด ผู้เรียกต้องบันทึก run ID ที่เลือกไว้อย่าง durable ก่อนเรียกเมธอดนี้ ข้อมูล provider/config/dataset/environment ใน bindings ยังมาจากผู้เรียก จึงต้องมี host resolver และ runner ยืนยันค่าจริงในขั้นต่อไป

Prepare ใช้ transaction เดียว ตรวจเจ้าของ candidate และ project/session ที่ยัง active ตรวจ base ว่ายังเป็นรุ่นปัจจุบัน และเก็บ snapshot ของ CaseIDs เมื่อใช้ run ID, candidate ID และ bindings เดิมจะคืน record ประวัติเดิม การเรียกซ้ำนี้ไม่ใช่สิทธิ์ให้ dispatch effect ใหม่ หาก input ขัดกันจะปฏิเสธ ส่วน `GetBehavioralRun` อ่านประวัติภายใต้เจ้าของเดิมและตรวจความสอดคล้องของข้อมูลที่เก็บไว้

schema 79 เพิ่มตาราง run และ trigger ป้องกันแก้ identity, candidate snapshot, manifest และเวลาเริ่มย้อนหลัง การอ่านข้อมูลตรวจ canonical manifest/hash, denormalized fields, lifecycle และเวลา โดยคืน zero value เมื่อข้อมูลผิด ไม่มี public HTTP completion, provider/tool dispatch หรือ promotion authority เพิ่มจากชุดนี้

## การตรวจรับ

| หลักฐาน | ผลและขอบเขต |
|---|---|
| ที่มาของ source | 28 ไฟล์ เป็น production/types/migration 11 และ test/support 17; ตรวจ native response ancestry 66 ขั้น และตรงกับผล Bonsai หลัง gofmt |
| Review | อนุมัติทั้ง 6 ชุด ไม่มี blocking findings; ใช้ saved reviewer profile แยก pass แต่เป็น Bonsai รุ่นเดียวกัน ไม่ใช่ model diversity |
| Native apply | `WriteProjectFilesAtRoot` ตรวจ root/preimage แล้วเขียนครบ 28 receipts ไม่มี rollback; hash source หลังนำเข้าตรงทั้งหมด |
| ทดสอบก่อนนำเข้า | ทั้ง repository บน candidate overlay ผ่าน 1,353 events, 0 failures, 20 skips; vet/build ผ่าน |
| ทดสอบหลังนำเข้า | ทั้ง repository บน source จริงไม่มี source overlay ผ่านผลเดียวกัน; raw logs และเหตุผล skip เก็บไว้ครบ |
| การรักษางานเดิม | ตรวจ preservation records เดิม 138 รายการ ครอบคลุม 133 paths และตรวจ source ใหม่ครบ 28 hashes |

การทดสอบฐานข้อมูลจริงครอบคลุม migration/reopen, immutable columns, lifecycle, historical replay หลัง candidate เปลี่ยน, current/foreign/stale base, Prepare พร้อมกัน, project/session ที่เป็นเจ้าของจริง, cross-project, inactive และ missing references รวมถึง scanner ที่ปฏิเสธ owner/candidate/revision/hash/time/NULL ที่ขัดกัน จำนวน passing events รวม parent/subtest จึงไม่ใช่จำนวนกรณีธุรกิจอิสระหรือคำรับประกันว่าไม่มีบั๊ก

รอบนี้แก้ test สองช่องว่างที่พบใน H1A4 โดยให้ Bonsai จับค่าที่ scanner คืนจริงในกรณีไม่พบรายการ และตรวจ RowsAffected=1 ทั้งตอนปิดและคืนสถานะ project/session test ใหม่สำหรับ missing references เคย compile ไม่ผ่านจากการเรียก helper และเปรียบเทียบ struct ผิด หลังแก้ยังพบชื่อตัวแปรที่ไม่ประกาศ จึงตรวจจุดเรียกจริงและให้แก้การอ้าง ID ให้ตรงกัน เก็บร่างและผลที่ไม่ผ่านไว้ ไม่แก้ oracle เพื่อลดเกณฑ์

เฉพาะ H1A5 มี generation responses 5 ครั้ง ใช้ prompt 9,413 และ completion 3,636 tokens; review 6 ครั้งใช้ 55,412 และ 1,736 tokens ตามลำดับ ตัวเลขนี้ไม่รวมต้นทุน H1A2/H1A3/H1A4 ที่เป็นบรรพบุรุษของ source ยังมี Codex กำกับขอบเขตและวิเคราะห์ failure จึงไม่ใช้เป็นหลักฐาน autonomous repair หรือ learning transfer

## หลักฐานจัดเก็บและสถานะ runtime

เก็บสำเนา Go เป็น `.go.txt` และตรวจหลังจัดเก็บว่าไม่มี Go evidence packages แทรกใน build การจัดเก็บครั้งแรกติดการ hash log ที่ model server เปิดเขียนอยู่ จึงเก็บสำเนาที่ได้เป็น partial snapshot พร้อมหมายเหตุ และแยก log สดออก ผล native completion/review/test เก็บแยกครบ manifest เริ่มต้น 420 รายการคงเดิม ส่วนผลตรวจหลังจัดเก็บอยู่ใน addendum และ manifest v2

ใช้ Bonsai Local เท่านั้น context 16,384 และ output cap 3,072 เดิม ไม่มีการส่งไป 9arm gateway ไม่มีการเปิด serving ของ Hermetrix หรือแก้ installed app/data ไม่มี commit/PR ใหม่ Source ที่นำเข้าแล้วไม่เท่ากับการอัปเดตแอปที่ติดตั้ง

## งานถัดไปตามเป้าหมายเต็ม

1. เชื่อม durable run ID ใน orchestrator และ dispatch CAS ที่ตรวจ candidate/base/config ปัจจุบันก่อน effect; งานที่ dispatch แล้วแต่ไม่ทราบผลต้องเก็บ uncertain และไม่สั่งซ้ำเงียบ ๆ
2. เชื่อม trusted host runner และ completion ที่ผูกกับ run/receipts เดิม ทดสอบ concurrent completion, conflicting replay, reopen และ candidate/base/config เปลี่ยนระหว่างงาน
3. ให้ evaluation/promotion ต้องใช้ trusted completed run พร้อม paired evidence เดิม ผล legacy ที่ caller ส่งเองไม่ authorize promotion และต้องทดสอบ holdout transfer ก่อนอ้างว่าระบบเรียนรู้แล้วทำงานใหม่ดีขึ้น
4. ทำ dynamic UX และ computer/browser control พร้อมเส้นทางใช้งานจริง แล้วพิสูจน์ marketplace หลายผู้ขายใน Docker Compose และ deploy/upgrade/rollback บน Kubernetes จริงตาม R1–R10 เป้าหมายทั้งหมดยัง active และยังไม่ครบ

ห้าม replay prefix H1A5 ที่มี intent/result แล้ว หรือใช้ candidate overlay เก่าทับ source ปัจจุบัน งานใหม่เริ่มจาก actual source schema 79 และ prefix ใหม่ H1A6
