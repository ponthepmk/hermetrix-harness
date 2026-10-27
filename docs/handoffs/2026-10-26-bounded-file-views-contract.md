# สัญญางานส่วนไฟล์สำหรับแก้ไฟล์ใหญ่

H1D เป็น dependency เพิ่มจากการลงมือจริง ไฟล์ agent/service.go มีขนาด 172,056 bytes และ worker เดิมปฏิเสธก่อนเรียก provider ด้วยข้อความ task exceeds 128 KiB จึงต้องแยก source ที่เก็บไว้ใน host ออกจากส่วนที่ส่งให้โมเดล โดยรักษา hash ของไฟล์เต็มและสิทธิ์เดิม

ความคืบหน้า 26 ตุลาคม: A1 นำเข้า native 7 ไฟล์, A2 อีก 9 ไฟล์ และ A3 worker transport อีก 36 ไฟล์แล้ว พร้อม tests, reviews และ native receipts ดู [ผล A3](2026-10-26-bounded-transport-candidate-results.md) ขั้น B coordinator/reviewer ยังไม่เชื่อม จึงยังไม่รับรองการทำงานกับโปรเจคไฟล์ใหญ่ครบวงจร

หลักฐานอยู่ที่ [ผลทดสอบขอบเขต](../evidence/2026-10-26/h1d-live/H1D-before-large-source.log) งานนี้ไม่ลดขอบเขต H1A, H1C หรือ marketplace และไม่ยกเพดาน request/context โดยเงียบ

## ผลการตรวจแผน

แผนจากโมเดลสองรอบแรกยังมีคำสั่งยืนยัน hash เดิมหลังแก้ไฟล์ ขอบเขตที่ส่งกลับมาจากโมเดล และการเชื่อม transport ที่ไม่ครบ จึงไม่ถูกนำไปลงมือ แผนรอบย่อย H1D-A ปรับโครงสร้างดีขึ้น แต่สร้าง SHA ที่ไม่ตรงรูปแบบ ด่าน validatePlannedChecks เดิมของ task coordinator ปฏิเสธก่อน CreatePlan ได้จริง พร้อม positive control ของคำสั่งที่ถูกต้อง

ดังนั้นผลลัพธ์ที่ PlanWithProviderService คืนมาเป็นเพียงข้อเสนอที่ผ่านรูปแบบ ไม่ใช่แผนที่ตัวคุมงานยอมรับแล้ว ไม่เพิ่มด่านตรวจซ้ำจากการเห็นผลลัพธ์ดิบ และไม่ใช้การตอบของโมเดลแทนหลักฐานว่าแผนรันได้

Codex ในฐานะผู้ควบคุมกำหนดสัญญางานย่อยต่อไปนี้ ส่วน implementation และ tests ให้ Hermetrix เขียนทั้งหมด สัญญานี้เป็นข้อกำหนดการออกแบบ ไม่ใช่โค้ดผลิตภัณฑ์

## A1 ตรวจข้อมูลส่วนไฟล์ที่ host ตรึง

สร้าง helper บริสุทธิ์ใน internal/worker/file_views.go สำหรับไฟล์ที่มีอยู่แล้ว พร้อมข้อมูล Path, FullSHA256, Start/End แบบ byte, Content และ RegionID ที่คำนวณซ้ำได้ ตัวตรวจรับ expected path และ full preimage แยกต่างหาก ไม่ให้ข้อมูลใน view รับรองตัวเอง

ตรวจ path แบบ relative slash ที่เป็น canonical ไม่มี traversal/absolute/backslash/colon/control characters ตรวจ hash เต็มตรงกับ bytes จริง ตรวจช่วงก่อน slice และไม่ตัด UTF8 encoding หรือคู่ CRLF ค่า Content ต้องตรงช่วง source และ RegionID ต้องผูก path/hash/ช่วงอย่างไม่กำกวม

ไฟล์ว่างที่มี SHA จริงและช่วง 0 ถึง 0 เป็นข้อมูลที่ถูกต้อง ส่วน absent marker ไม่ใช่ไฟล์ว่างและต้องถูกปฏิเสธ helper ไม่มี filesystem, network, DB หรือสิทธิ์เขียนไฟล์ และยังไม่เปลี่ยน Task/transport

ตรวจรับด้วยกรณี valid/stale/tampered path/hash/content/ID, bounds, Unicode, CRLF, empty และ absent ก่อนยอมรับ foundation นี้ helper ที่ผ่าน unit tests ยังไม่ใช่ H1D ที่ใช้งานครบ

## A2 ใช้ exact TextEdit เดิมภายในส่วนที่อนุญาต

ใช้คำตอบ changes/path/edits old/new เดิม ไม่เพิ่ม View, offsets หรือข้อมูลสิทธิ์ในคำตอบโมเดล สำหรับไฟล์ที่ให้เพียงบางส่วนต้องปฏิเสธ full content replacement ข้อความ old ต้องตรง preimage อย่างไม่กำกวมและอยู่ในช่วงที่ host ตรึง ตรวจการซ้อนทับและรักษา bytes ที่ไม่ได้แสดง ผลลัพธ์เป็น source เต็มพร้อม BeforeSHA256 เดิม

ต้องกำหนดและทดสอบ semantics ของหลาย edits ให้ชัด โดยไม่เปลี่ยนการแก้ไฟล์เต็มแบบเดิม การเดา anchors หรือการส่ง source ที่ถูกตัดแล้วเป็นทั้งไฟล์ไม่ใช่วิธีผ่านเกณฑ์

สัญญา A2 ที่ใช้ลงมือ: เพิ่ม pure helper MaterializeBoundedChange(expectedPath, fullSource, expectedFullSHA string, views []BoundedFileView, content *string, edits []TextEdit) (string, string, error) ในไฟล์ใหม่ โดยยังไม่เรียกจากเส้นทางเดิม รับ views 1 ถึง 32 ช่วง ตรวจทุก view กับ expected inputs ก่อนใช้ ปฏิเสธช่วงซ้ำหรือซ้อนทับ แต่รับช่วงติดกันได้ ช่วงว่างไม่ให้อำนาจแก้ข้อความที่มีความยาว

อนุญาตเฉพาะ edits 1 ถึง 64 รายการและ content ต้องเป็น nil ทุก old ต้องไม่ว่าง, old/new เป็น UTF8 และ old ปรากฏเพียงครั้งเดียวใน source เดิมทั้งหมด รวมการปรากฏที่ซ้อนกันด้วย ทุกช่วงแก้ต้องอยู่ภายใน view เดียวและไม่ตัด UTF8/CRLF ห้ามรวมสอง view ที่ติดกันเพื่ออนุญาตข้อความที่คร่อมขอบ ตรวจ edits ซ้อนทับโดยอิงตำแหน่งใน preimage เดิม จากนั้นประกอบตามลำดับตำแหน่งย้อนกลับ คืน source เต็มและ strategy replace_text โดยไม่แก้ slices ของ caller การแก้แบบ chain ที่อาศัย new ของ edit ก่อนหน้าต้องถูกปฏิเสธในโหมดนี้ ส่วน whole-file mode เดิมยังคง semantics แบบ sequential ตามเดิม

ข้อความเมื่อเกิด error ต้องแนะนำวิธีซ่อมที่อยู่ในขอบเขตจริง ไม่แนะนำ full replacement หรือการเข้าถึง source ที่ซ่อนไว้ซึ่งโหมดนี้ไม่อนุญาต

## A3 เชื่อมกับ worker transport เดิม

Task เก็บ snapshot เต็มไว้ใน host พร้อม view ที่ host ตรวจและตรึงแล้ว การส่ง provider ใช้เฉพาะข้อมูลที่อนุญาตและ hash เต็ม ตรวจขนาด serialized wire จริง เพดาน token และ credential ก่อนส่ง Canonical task/result binding ยังครอบคลุม source เต็มและขอบเขตเดิม ต้องมีเพดานข้อมูล private ที่พอดีกับ artifact/memory ของระบบด้วย

ต้องตรวจ partial result ซ้ำได้หลัง persist หรือ transport ด้วย ปัจจุบัน ValidateResult ตรวจ identity, preimage และ strategy ของ whole-file result แต่ไม่มี proof ของช่วงที่แก้ การเพิ่ม partial mode ต้องเก็บ edits หรือหลักฐานที่ host ประกอบใหม่ได้ แล้วคำนวณผลใหม่ตาม view ที่ตรึงเพื่อยืนยันว่า Content ไม่เปลี่ยน bytes นอกขอบเขต แม้ผู้ส่งจะคำนวณ proposal ID ใหม่เอง ห้ามใช้ content hash ที่ผู้ส่งคำนวณได้เป็นการอนุญาตสิทธิ์

เชื่อมเข้า RunWithProviderService/runWithOptions เดิม ไม่เพิ่มทาง apply ใหม่ ตรวจด้วย adapter ที่อ่าน wire payload และพิสูจน์ว่า hidden source ไม่หลุด รวมกรณี view ปลอม, stale source, malformed reply และการคงพฤติกรรม full-file/new-file เดิม

### ข้อกำหนด A3 ที่ใช้ลงมือ

เพิ่ม Task.FileViews เป็น map ของ path ไปยัง views ที่ host กำหนด และ Change.Edits สำหรับเก็บต้นฉบับ bounded edits ทั้งสองใช้ JSON omitempty เพื่อไม่เปลี่ยน hash ของงานเก่าที่ไม่มีข้อมูลเหล่านี้ BoundedFileView ใช้ชื่อ JSON path/full_sha256/start/end/content/region_id อย่างชัดเจน ตัว model reply ยังใช้ changes/path/content หรือ edits เดิมและห้ามเพิ่มข้อมูลสิทธิ์

สร้าง snapshot ที่แยก maps/slices ทุกชั้นจาก caller ก่อนเรียก progress หรือ dispatch callbacks ใช้การ copy โครงสร้าง ไม่ใช้ JSON round trip ที่อาจ normalize bytes ของ string เดิม จาก snapshot นี้สร้าง canonical JSON ที่รวม source เต็มสำหรับ InputSHA256 และ wire JSON ซึ่งตัดเฉพาะ Files entries ที่มี FileViews ออก แต่คง views/hash ที่ตรวจแล้วไว้ ไฟล์เต็มที่ไม่ได้ใช้โหมด partial ยังอยู่ใน wire ตามเดิม

เพดาน source คือ 2 MiB ต่อไฟล์และ 8 MiB รวม อิง workbench และ planner snapshot เดิม; canonical JSON ไม่เกิน 16 MiB อิง artifact limit และ wire JSON จริงไม่เกิน 128 KiB ตามเดิม ตรวจ raw metadata lower bound ก่อน copy/encode เพื่อกันการขยายขนาดที่ไม่จำเป็น แล้วตรวจ serialized bytes จริงซ้ำ ไม่เพิ่ม profile context/output budget

แยก shared validator ของ view set จาก A2 เพื่อให้ Task และ materializer ใช้ข้อกำหนด duplicate/overlap/empty เดียวกัน Task views ต้องอ้างไฟล์ที่มีใน snapshot และไม่ใช่ absent marker ทุกเส้นทางยังตรวจ credential จาก snapshot เต็มตามเดิม

ใน Run เลือก bounded materialization เฉพาะ path ที่อยู่ใน FileViews เก็บ Edits เฉพาะ Change ของโหมดนี้เพื่อรักษา legacy proposal ID ส่วน ValidateResult ต้องสร้างผลจาก frozen task และ edits ใหม่ แล้วเทียบ content/strategy รวมทั้ง preimage/identity เดิม การเปลี่ยน hidden bytes, ลบ edits หรือแก้ขอบเขตต้องไม่ผ่านแม้คำนวณ proposalID ใหม่

ข้อมูล golden ก่อนเปลี่ยนถูกบันทึกจาก worker.Run เดิมด้วย fake adapter 4 รูปแบบ: whole-file, sequential edits, empty-existing และ absent-new ใช้ตรวจ wire/input hash/proposal ID แบบ byte-for-byte ไม่ใช่ผลคุณภาพโมเดลจริง การตรวจโหมดใหม่ต้องมีไฟล์เกิน 128 KiB, credential/wire/private-size guards, stale/tampered views, callbacks ที่เปลี่ยน original inputs และ persisted results ที่ถูกแก้พร้อมคำนวณ ID ใหม่

## B เชื่อมตัวคุมงานและพิสูจน์กับโมเดลจริง

หลัง A ผ่าน ให้ task coordinator ใช้ส่วนไฟล์ในเส้นทาง proposal/apply/verify/review เดิม คง root/owner/lease/effect/preimage/snapshot/rollback guards และไม่ลดการตรวจตอนหลัง test/review

ขั้น review ต้องรับบริบทที่จำกัดส่วนพร้อมหลักฐาน hash/snapshot เช่นเดียวกัน เพราะการส่งไฟล์เต็มที่ใหญ่ให้ reviewer ยังชนเพดานเดิม ตัวคุมงานต้องยืนยันส่วนที่ไม่เปลี่ยนเอง และบอก reviewer ว่าตรวจเฉพาะการเปลี่ยนแปลงที่อยู่ในขอบเขต ไม่อ้างว่าเห็น source ส่วนที่ไม่ได้ส่ง

ตรวจรับด้วยไฟล์แยกเกิน 128 KiB: โมเดล local เสนอแก้เฉพาะจุด, native apply, actual test, separate review และ mutation receipts ครบ พร้อมตรวจ source เกินขอบเขต การชนกัน การคืนสภาพ และการเปลี่ยนไฟล์หลังรีวิว การไม่มีโมเดลต้องรายงาน inconclusive การทดสอบ unit หรือ helper ที่ยังไม่ถูกเรียกใช้ไม่ถือว่าผ่านขั้นนี้

จากนั้นจึงใช้ความสามารถที่พิสูจน์แล้วเชื่อม H1A/H1C และทำงานตาม execution contract เดิมต่อ
