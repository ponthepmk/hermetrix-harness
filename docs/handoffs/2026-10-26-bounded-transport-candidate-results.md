# ผล A3: ส่งเฉพาะส่วนไฟล์และตรวจผลที่เก็บไว้

วันที่ 26 ตุลาคม 2026 · นำเข้า workspace ผ่าน native API แล้ว 36 ไฟล์

Hermetrix เขียน implementation 10 ไฟล์และ Go test/support 26 ไฟล์ แล้วผ่านรีวิวแยก 4 กลุ่ม ก่อนนำเข้าด้วย WriteProjectFilesAtRoot มี mutation receipts 36 รายการและ rollback 0 รายการ ตรวจ hash หลังนำเข้าได้ตรง 36/36 ส่วน source ที่ผ่านรีวิวสะสมจาก H1B/A1/A2/A3 ตรงทั้งหมด 59 ไฟล์ ไม่ใช่ 61 ไฟล์ไม่ซ้ำ เพราะ A3 ปรับ file_views.go และ file_views_edits.go จาก foundation เดิม

Codex ทำหน้าที่กำหนดสัญญางาน ตรวจ fixture อ่าน compiler/review results และเรียก API ของ Hermetrix ไม่ได้เขียน implementation หรือ standing Go tests เอง ข้อมูล golden/reply fixture เป็น JSON DATA ที่ host ตรึง ส่วนการจัดรูปแบบ source ใช้ gofmt และเก็บ hash ก่อน/หลังไว้

## พฤติกรรมที่ยืนยันแล้ว

Task.FileViews เป็นส่วนไฟล์ที่ host กำหนดและตรวจเทียบกับ path, hash และ bytes ของไฟล์เต็ม โมเดลได้รับเฉพาะส่วนที่อนุญาต แต่ InputSHA256 ยังผูกกับ snapshot เต็มที่แยกจาก caller แล้ว การแก้ maps/slices ผ่าน progress หรือ dispatch callback จึงไม่เปลี่ยน snapshot ที่ใช้ประกอบผล

Partial path รับเฉพาะ exact edits และเก็บ edits ไว้ในผลลัพธ์ ValidateResult ประกอบผลจาก frozen views และ edits ซ้ำอย่างอิสระ การเปลี่ยน hidden content, ลบ edits, เปลี่ยน strategy, เปลี่ยน proof หรือแก้ hidden region ไม่ผ่านแม้ผู้ส่งคำนวณ proposalID ใหม่ ข้อมูล view หรือ ID ที่คำนวณได้ไม่ใช่สิทธิ์เขียนไฟล์

| การตรวจ | ผล |
| --- | --- |
| Standing tests ใหม่ | 26 ฟังก์ชันผ่าน รวม legacy, snapshot, callbacks, forged IDs, resources และ guards |
| Candidate ที่เกี่ยวข้อง | worker, taskcoord, product, skills, learning, selfimprove และ agent ผ่านครบ 7 แพ็กเกจ |
| Worker race | ผ่าน |
| Vet และ build | ผ่านทั้ง candidate และ workspace หลังนำเข้า; binary ใหม่ไม่ได้เปิด |
| Workspace หลัง native apply | worker และ skills ผ่าน; source ตรง reviewed hashes 36/36 |
| Legacy | Golden ก่อน A3 ครบ 4 แบบ และ nil/empty optional fields รักษา wire/input/proposal identity |
| Private source | 2 MiB ต่อไฟล์และ 8 MiB รวม มี positive control ที่ขอบและ one-byte excess |
| Encoded task/result | JSON ที่ escape แล้วเกิน 16 MiB ถูกปฏิเสธ ทั้งที่ raw source ยังอยู่ในเพดาน |
| Provider wire | raw lower bound ผ่านทั้งสองกรณี แต่ encoded view 20 KiB ผ่านและ 25 KiB ไม่ผ่านเพดาน wire 128 KiB |
| Metadata/proof | ตรวจแต่ละ list 32/33, edits 64/65, TaskID 128/129 KiB, changes 0/1/32/33 และ combined raw metadata |
| ก่อน provider | view content/hash/binding ผิด, view บน absent file และ secret ใน fullsource ที่ไม่ได้ส่งออก ถูกปฏิเสธก่อนเรียก provider |
| หลัง provider | frozen valid reply ผ่าน; content-only, content+edits และ model file_views ถูกปฏิเสธหลังเรียกหนึ่งครั้ง |

## ทดลองกับโมเดลจริงหลังนำเข้า

เรียก Bonsai Local ผ่าน RunWithProviderService จาก source ใน workspace จริง ไฟล์ TEXT DATA ขนาด 140,049 bytes ส่งส่วนเป้าหมายให้โมเดลแก้ return a-b เป็น return a+b ใช้ input 1,089 และ output 203 tokens ผลที่ประกอบเต็มตรงตามคาด เก็บ edit หนึ่งรายการ และ BeforeSHA256 ตรงกับไฟล์ต้นฉบับ

นี่เป็นการพิสูจน์ native proposal/validation ของ worker ไม่ใช่การแก้โปรเจค Go ขนาดใหญ่ครบ native apply/test/review และไม่ใช่ผล benchmark หรือ flow ของ marketplace ดู [ผลโมเดลหลังนำเข้า](../evidence/2026-10-26/h1d-live/H1D-A3-workspace-live-qualification.json)

## ข้อผิดพลาดและการเปลี่ยนวิธีควบคุมงาน

คำตอบที่ผ่าน protocol บางชุดยังคอมไพล์ไม่ได้ เดา API, สร้าง fake ซ้ำ, ใช้ Task เทียบ nil, ไม่ปิดวงเล็บ หรืออ้างตัวแปรผิด บาง test ไม่เรียกตัวตรวจใน positive case หรือทดสอบ nil ของข้อมูล input แทน output จึงต้องตรวจ fixture และเงื่อนไขด้วย ไม่ใช่อ่านเพียงผล PASS

บริบทที่ผู้ควบคุมส่งให้บางชุดตัดค้างกลางฟังก์ชัน และบางชุดไม่ได้ให้ module import ที่ถูกต้อง ทำให้เกิดข้อผิดพลาดเพิ่ม จึงเปลี่ยนไปใช้ source/ตัวอย่างที่ครบถ้วน งานแก้เฉพาะสาเหตุ และ golden DATA จากรันเดิม การตอบยาวที่หมด output ถูกทิ้งทั้งชิ้น ไม่ซ่อมโดยดึงบางส่วนมาใช้ ไม่เพิ่ม budget หรือส่งไป gateway ภายนอกโดยเงียบ

Ledger ของ A3 ตั้งแต่ draft ก่อนหน้า รวมงานที่ควบคุมและซ่อม มี completion artifacts 109 ครั้ง: proposal 105 และ review 4, บันทึก structured protocol result ได้ 95 ครั้ง, output limit 9 ครั้ง รวม prompt 365,833 และ completion 104,880 tokens จำนวนเหล่านี้ไม่ใช่อัตราความสำเร็จในการเขียนโค้ด และไม่ใช่หลักฐาน self-learning อัตโนมัติ ดู [ledger](../evidence/2026-10-26/h1d-live/H1D-A3-job-ledger.json)

## งานถัดไปและข้อจำกัด

B ต้องเชื่อม coordinator และ bounded reviewer โดยรักษา full snapshot, owner/root/lease/effect/preimage/rollback guards เส้นทางปัจจุบันใน taskcoord/service.go สร้าง worker.Task ที่บรรทัด 577 โดยไม่มี FileViews ส่วนการตรวจ JSON เต็ม 128 KiB ที่บรรทัด 614 ใช้ทิ้ง optional ProjectBrainRefs เท่านั้น ตัวปฏิเสธหลักยังเป็น worker ที่ได้รับ Files เต็ม และ reviewer เริ่มที่บรรทัด 1305 จึงยังห้ามสรุปว่าทั้งระบบรองรับไฟล์ใหญ่แล้ว ดู [สัญญา B จาก baseline จริง](2026-10-26-coordinator-bounded-context-contract.md)

ต้องทดสอบ owned Go fixture เกิน 128 KiB ให้ครบ proposal/apply/test/review และ receipts รวมการเปลี่ยนไฟล์หลัง review และ recovery จากความผิดพลาด จากนั้นทำ H1A/H1C และ execution contract เดิมต่อ การทดลองนี้ยังไม่มีการนำบทเรียนไป promote อัตโนมัติ และยังไม่ใช่ระบบ marketplace พร้อมใช้

การเปิดบริการพอร์ต 18816 ถูก automatic approval review ปฏิเสธก่อน execution ด้วยเหตุผลทั่วไป blocked by policy ไม่มีรายละเอียดเพิ่ม ไม่ได้ลองเปิดผ่าน binary/process/port อื่น บริการและข้อมูลที่ติดตั้งเดิมไม่ถูกเปลี่ยน ส่วน headless source/tests ข้างต้นดำเนินการได้ตามขอบเขตที่อนุญาต

หลักฐานรวมอยู่ใน [receipt](../evidence/2026-10-26/h1d-live/H1D-A3-receipt.json), [native apply](../evidence/2026-10-26/h1d-live/H1D-A3-apply-receipt.json), [ที่มาของ source](../evidence/2026-10-26/h1d-live/H1D-A3-bundle-authorship.json), [hash ของ source สะสม](../evidence/2026-10-26/h1d-live/H1D-A3-all-reviewed-workspace-hashes.json) และ [สัญญาขั้นถัดไป](2026-10-26-bounded-file-views-contract.md)
