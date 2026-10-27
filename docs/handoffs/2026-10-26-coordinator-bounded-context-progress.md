# ความคืบหน้า B: projection และตัวอ่านข้อเสนอ

วันที่ 26 ตุลาคม 2026 · ยังไม่ apply B; เชื่อม durable snapshot/loader เข้ากับ coordinator แล้วใน candidate

Foundation เดิมมี 11 ไฟล์ ต่อมาได้ชุด candidate 21 ไฟล์: implementation 7 ไฟล์และ test/support 14 ไฟล์ พร้อม native authorship ครบ เชื่อม freeze/persist input snapshot, envelope version 2 และ central loader เข้ากับ Propose reuse/Apply/verification/Review/reconcile/rollback แล้ว ยังต้องเพิ่ม automatic view curation และ bounded review endpoint ดู [ผล integration และข้อจำกัดล่าสุด](2026-10-26-durable-proposal-input-results.md) สำหรับสถานะปัจจุบัน หลักฐาน foundation ด้านล่างยังเป็นประวัติของชุด 11 ไฟล์

ดู [สัญญาและ acceptance ของ B](2026-10-26-coordinator-bounded-context-contract.md) และ [receipt](../evidence/2026-10-26/h1d-live/H1D-B-receipt.json)

## หลักฐานที่มี

- Baseline ผ่าน owned project และ Task/Plan/Run/Attempt/Propose จริง: source agent/service.go 172,056 bytes ถูกปฏิเสธก่อน provider ด้วย task exceeds 128 KiB, effect abandoned, provider calls 0 และไม่มี source write
- Worker suite ผ่านบน candidate และ focused tests ใหม่ 5 ฟังก์ชันผ่าน: partial projection, legacy 4 รูปแบบ, input/output alias, forged/recomputed-ID result และ region edge cases
- Region fixture ตรวจหลาย edits จาก original offsets, empty unused view, header ที่ไม่เปลี่ยน, ความยาว UTF8 ที่เปลี่ยน และ CRLF ใหม่ระหว่าง region กับ hidden suffix
- Parser tests 2 ฟังก์ชันผ่าน: frozen cases 29 กรณี และ valid JSON ที่ serialized แล้วต่ำ/สูงกว่า 16 MiB ตัว parser ตรวจรูปแบบเท่านั้น การตรวจ semantic result และ scope ทั้งหมดต้องผ่าน loader/ValidateResult ในขั้นถัดไป
- Vet ของ worker/taskcoord บน candidate ผ่าน แต่ยังไม่ผ่านรีวิวรับ bundle หรือ native apply
- Source ที่เคย apply H1B/A1/A2/A3 ยังตรง reviewed hashes ทั้ง 59 ไฟล์ ไม่มีการเปลี่ยน production Go ใน workspace จาก B รอบนี้

ข้อมูล fixture ใหม่เป็น JSON DATA ที่ Codex ตรึง ส่วน implementation และ standing Go tests ทั้งหมดให้ Hermetrix เขียน Candidate manifest อยู่ที่ [หลักฐานที่มา](../evidence/2026-10-26/h1d-live/H1D-B-passing-candidate-manifest.json)

## สิ่งที่ tests จับได้

Draft แรกประกาศ Hash/UTF8 helper ซ้ำ ตรวจ absent ด้วย hash ของไฟล์ว่าง และใส่ nonpartial paths ลง partial map ด้วย empty regions การรัน legacy fixtures จับข้อผิดพลาดจริงเหล่านี้ได้ หลังแก้แล้ว whole replacement, sequential edits, empty-existing และ absent-new อยู่ใน whole map เท่านั้น

Tests ที่โมเดลเขียนบางรุ่นเองก็ผิด: เทียบ path กับ content, จับ tuple ช่องผิด, เดา NewReviewViewBuilder, คาด status completed/success และตรวจหา hidden text ใน SHA หรือใน full output ที่ควรเก็บ hidden bytes ไว้ กรณีเหล่านี้ถูกแก้ด้วย compiler และ frozen expected data ไม่ใช่ยอมลดเงื่อนไขให้ implementation ผ่าน

Parser draft ใช้ json.Unmarshal จึงรับ unknown fields และคืน partially populated bundle เมื่อ error ชุดข้อมูล 29 กรณียืนยันความผิดพลาด ก่อนเปลี่ยนไปใช้ strict decoders และ named-return error guard มีหลักฐานก่อน/หลังเก็บไว้

Host สร้าง trailing-data fixture ผิดในครั้งแรก เพราะ expression ของ PowerShell ทำให้ suffix ไม่เข้า body ที่ทดสอบ ข้อมูลนั้นถูกแก้แยกจาก product code พร้อม hash ก่อน/หลัง และรัน baseline ใหม่แล้ว หลักฐานก่อนแก้ DATA ไม่ถูกใช้กล่าวอ้างว่า decoder มีบั๊ก trailing-data ดู [บันทึกการแก้ fixture](../evidence/2026-10-26/h1d-live/H1D-B-envelope-fixture-correction.json)

## งานที่ต้องทำต่อทันที

ตัวอ่าน input/top-level loader และ durable persistence เชื่อมแล้วใน candidate 21 ไฟล์ รวม rollback reader ที่ test พบว่าตกหล่น ต้องยืนยัน full taskcoord suite ล่าสุดตาม receipt แล้วเพิ่ม tamper acceptance ของ body/hash/project/registry ให้ครบก่อนรีวิวรับชุด

งานถัดไปคือ host view curation อัตโนมัติและ bounded review endpoint แล้วตรวจ native owned Go project เกิน 128 KiB ครบ proposal/apply/test/separate-review/receipts ตามสัญญา B ห้ามใช้ candidate overlays เก่าที่มี compile/fixture defects แทน overlay ที่ระบุในรายงานล่าสุด

H1A/H1C, UX/UI, computer control และ marketplace/Compose/Kubernetes ยังอยู่ในเป้าหมายเดิม ไม่มีผลรอบนี้พิสูจน์ self-learning อัตโนมัติหรือระบบพร้อมใช้งานทั้งหมด การเปิดบริการรุ่นใหม่ยังติด automatic approval review เดิม และไม่ได้ใช้ช่องทางอื่นเปิดแทน
