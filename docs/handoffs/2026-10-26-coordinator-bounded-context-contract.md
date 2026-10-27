# สัญญา B: เชื่อม bounded context เข้าตัวคุมงาน

วันที่ 26 ตุลาคม 2026 · สถานะ: ออกแบบจาก source จริงและ baseline ที่รันแล้ว; implementation B ยังเป็น candidate และยังไม่ apply

งานนี้ต่อจาก [A3 ที่นำเข้าแล้ว](2026-10-26-bounded-transport-candidate-results.md) ไม่ลดเป้าหมาย H1A/H1C หรือ marketplace และไม่ถือว่าการผ่าน helper tests เท่ากับรองรับงานใหญ่ทั้งระบบ

## สิ่งที่พบจากเส้นทางจริง

Propose ใน taskcoord/service.go อ่าน source ผ่าน SnapshotProjectFileAtRoot ของ project ที่มีเจ้าของ แล้วสร้าง worker.Task ที่ยังไม่มี FileViews การตรวจ JSON 128 KiB ที่บรรทัด 614 เดิมมีไว้ทิ้ง ProjectBrainRefs ที่เป็น optional เท่านั้น ไม่ใช่ตัวปฏิเสธหลักที่ควรถอดออก การปฏิเสธไฟล์ใหญ่เกิดใน worker เมื่อ coordinator ยังส่ง Files เต็ม

รัน baseline ผ่าน Task/CreatePlan/BeginRun/BeginStepAttempt/Propose จริงใน audit DB ที่แยกจากระบบติดตั้ง โดยใช้ project ที่มีอยู่จริงและไฟล์ internal/agent/service.go ขนาด 172,056 bytes ได้ invalid_task: task exceeds 128 KiB, provider calls 0 และ effect abandoned ไม่มีการเขียน source ตัว adapter ห้าม network ไว้อีกชั้น จึงเป็นหลักฐานขอบเขต native ก่อนเรียกโมเดล ไม่ใช่ผลคุณภาพโมเดล

Apply เดิมผูก artifact checksum กับ registry และตรวจ root/preimages/writable paths แต่ไม่ได้เก็บ worker.Task เต็มไว้ตรวจ bounded proof ข้าม process ส่วน Review อ่าน source เต็มหลังทดสอบ จึงต้องเชื่อมการเก็บ snapshot และ review context ด้วย ไม่ใช่แก้ gate ขนาดเพียงจุดเดียว

Known-good ที่ต้องรักษาคือ A3 Freeze/ValidateResult/MaterializeBoundedChange, owned snapshots ของ product, observed effect reuse/reconciliation, และ review_snapshot tests ที่ปฏิเสธ source/root/ผลจาก step ก่อนหน้าซึ่งเปลี่ยนระหว่าง review

## ลำดับการเชื่อม

### B1 สร้างข้อมูลก่อน/หลังสำหรับ reviewer

ส่งออก FreezeTask เป็น wrapper ของ prepareWorkerTask เดิม ไม่คัดลอกตัวตรวจหรือเปลี่ยน budget เพื่อให้ coordinator ได้ independent Task, full canonical JSON และ bounded wire จากกลไกเดียวกับ worker

BuildReviewFiles ต้อง freeze และ ValidateResult ก่อน แล้วแยก full Files ของ nonpartial path ออกจาก bounded review ของ partial path ทุก legacy strategy รวม create_file ต้องอยู่ใน full map เท่านั้น และใช้กติกา absent ของ ValidateResult ไม่เทียบ absent กับ Hash(empty)

BoundedReviewFile มี BeforeSHA256/AfterSHA256 ของไฟล์เต็มและ Regions แต่ละ region มี Before เป็น BoundedFileView ที่ตรึงเดิม และ AfterContent เป็นข้อความในส่วนเดียวกันหลังใช้ edits ต้องหา matches จาก ORIGINAL source แล้วใช้ MaterializeBoundedChange เดิมกับ subset ในแต่ละ region ไม่สร้างตัวประกอบ edits ชุดใหม่

ไม่ใช้ offsets เดิมเป็น offsets ของไฟล์หลังแก้ เพราะความยาว UTF8 เปลี่ยนได้และ edits อาจทำให้เกิด CRLF ใหม่ข้ามขอบ region ใช้ before-range กับ after-text ที่มีความหมายชัดเจนแทน เก็บ empty unused view/unchanged context ได้ ไม่ให้ข้อมูล review เป็นสิทธิ์เขียน

### B2 ผูก input snapshot แบบ durable

ใช้ artifact ของ full canonical worker.Task แยกต่างหาก และ content hash ต้องตรง result.InputSHA256 เก็บหลัง Run และ ValidateResult ผ่านแล้ว เพื่อไม่ persist credential ที่ตัวตรวจเพิ่งจะปฏิเสธ แต่ต้อง freeze ตัว Task ก่อน provider dispatch

เก็บ proposal ใหม่เป็น envelope version 2 ซึ่งมี Result และ InputArtifactID ใน CONTENT ที่ registry.ResultHash ผูกไว้ ไม่ใช้ metadata ที่ไม่มี content hash เป็นหลักฐานเพียงอย่างเดียว การชี้ไป snapshot อื่นจึงต้องผ่านทั้ง registry checksum, envelope, snapshot checksum/InputSHA และ ValidateResult

สร้าง loader กลางสำหรับ proposal artifact ซึ่งคำนวณ hash ของ bytes จริง ตรวจ kind/project/task/step/attempt/packet/root binding, strict JSON และ trailing data แล้วใช้ร่วมกันใน replay, apply, prepareVerification, review และ reconciliation ทุกจุดที่เคย decode worker.Result ตรง ๆ ต้องตรวจให้ครบ

รองรับ v1 ที่เป็น whole-file legacy โดยคงด่านเดิมและตรวจ artifact body hash จริงเพิ่ม ห้ามยอมรับ retained partial edits ใน v1 ที่ไม่มี frozen input snapshot รุ่นใหม่ที่ขาด version/result/input reference ต้องไม่กลายเป็น legacy โดยเงียบ

Input snapshot artifact ห้ามใช้ metadata key operation_id เดียวกับ proposal response เพราะ FindArtifactByOperationID ต้องพบ response artifact ที่ใช้ reconcile เท่านั้น ใช้ source_operation_id ได้ถ้าจำเป็น การ crash ก่อน response persist ต้องยังเป็น uncertain/lost response ตามเดิม ไม่ replay provider อัตโนมัติ

### B3 ใช้ host-curated views ใน Propose และ Goal

ตัวคุมงานอ่าน source เต็มจาก owned project/root เดิม แล้ว host เลือกบริบทที่เกี่ยวข้องกับ frozen step เมื่อทั้งไฟล์ไม่พอดีกับ wire/preset input budget การเลือกช่วงต้องเกิดจาก source จริงที่ host ถืออยู่ เช่น declaration หรือ line window ที่ผูกกับ symbol/anchor ของ step ไม่รับ offsets, hash, content หรือ region authority จาก model reply

ต้องมีเส้นทางอัตโนมัติใน Goal ด้วย การรับช่วงที่ผู้ควบคุมกำหนดได้เพียงอย่างเดียวยังไม่ถือว่าจบ B Host คง path/writable scope เดิมและ narrow visibility/edit regions เท่านั้น ถ้าเก็บ anchor/context ที่จำเป็นไม่ได้ต้องส่งเหตุผลที่ใช้ replan/ขอบริบทเพิ่มได้ ไม่เลือก prefix ที่ไม่เกี่ยวข้องเพียงเพื่อให้ request ผ่าน

ใช้ preset.ContextMax และเพดาน 128 KiB เดิมเป็นขอบบน ตรวจ serialized wire จริงและให้ provider preflight ตัดสิน request สุดท้าย ห้ามเพิ่ม context/output หรือ discard criteria/constraints/checks โดยเงียบ Optional ProjectBrainRefs fit จาก bounded wire จริง ไม่ใช่ขนาด private source

Freeze final Task และ canonical bytes ก่อน callback แล้วหลัง Run ตรวจผลกับ Task เดิมก่อน persist snapshot/envelope การ apply ต้องโหลด binding นี้และ rederive ผลซ้ำก่อนเปิด write effect พร้อมคง preimage/root/owner/lease/approval/rollback guards เดิม

### B4 เชื่อม endpoint review

เพิ่ม ReviewTask.PartialFiles แบบ optional omitempty คู่กับ Files โดย key ต้องไม่ซ้ำกัน รวมไม่เกิน 32 path ตรวจ shape/hash/range/region identity และ raw/encoded size ก่อน provider ไม่เชื่อข้อมูลเหล่านี้เป็น authority นอก host-built flow

เมื่อมี partial context ให้ SourceState และคำสั่ง reviewer ระบุว่าเห็นเฉพาะ before/after regions พร้อม full hashes; Files ที่เหลือเป็น after-source เต็ม ห้ามอ้างว่า reviewer เห็น hidden source ทั้งไฟล์ Host ตรวจ hidden bytes และ full output เองจาก snapshot/proof และ actual verification

Review ใช้ loader ของ B2 กับ before snapshot และผลที่เก็บไว้ ใช้ BuildReviewFiles สร้างข้อมูล จากนั้นยืนยัน actual after-source ตรงกับ full hashes ภายใต้ verification snapshot fence เดิม ตรวจ credential ของ reviewer จาก full source/result ก่อนตัดเหลือ bounded wireด้วย คง different-provider-profile policy, provider revision binding, effect dispatch/reuse, saved decision recovery และ source/root fence หลัง provider ตอบ

### B5 ทดสอบครบวงจรกับ Go จริง

Hermetrix ต้องสร้าง fixture Go ที่มีเจ้าของและไฟล์เกิน 128 KiB พร้อม actual failing/passing test ให้ native coordinator เลือก/เสนอ/apply/test/separate-review และสร้าง receipts ครบ ใช้ saved local model/profile budgets เดิม Reviewer ใช้ profile แยกบน local model เดียวกันได้ แต่ต้องรายงานว่าไม่ใช่ model-diverse evidence

ห้ามใช้ TEXT DATA fixture ของ A3, helper/unit tests, หรือ operator direct file write แทน acceptance นี้ การสร้าง source Go และ tests ของ fixture ให้ Hermetrix เขียน; Codex คุมสัญญางาน ตรวจข้อมูลและเรียก API เท่านั้น

## Acceptance ที่ต้องมีหลักฐาน

| เส้นทาง | หลักฐานผ่าน |
| --- | --- |
| Baseline ก่อน B | owned172056byte source ถูกปฏิเสธก่อน provider, effect abandoned, source ไม่เปลี่ยน — ทำแล้ว |
| Projection | partial before/after exact, full hashes, no hidden markers, no alias; mixed full/new/partial scope ถูกต้อง |
| Region edge cases | หลาย edits จาก original offsets, empty unused view, UTF8 length change และ CRLF ใหม่ข้ามขอบ |
| Forged result | เปลี่ยน hidden content แล้วคำนวณ ID ใหม่ ต้องได้ error และ nil maps |
| Durable binding | wrong/missing input ref, body/checksum/packet/project/attempt/root drift ถูกปฏิเสธก่อน write/provider |
| Legacy | whole replacement, text edits, empty-existing และ absent-new คง flow และ encoding เดิมเมื่อไม่มี partial data |
| Native large Go | actual local-model proposal, source scope ตรง, native apply receipts, actual test pass, separate post-test review, final state ถูกต้อง |
| Failure/recovery | test fail หรือ review reject คืนสภาพอย่างมี receipts; conflict/root drift ไม่เขียนทับ owner bytes; observed work ไม่ถูก replay |
| Review fence | owner เปลี่ยน source/root/ผล step อื่นระหว่าง review แล้วต้องไม่ complete |
| Public behavior | ไม่เพิ่ม model/tool authority หรือ saved budgets; รายงานสิ่งที่ reviewer ไม่เห็นอย่างตรงไปตรงมา |

ยังไม่อนุญาตให้เรียก B ว่าเสร็จจน integration และ native Go acceptance ครบทั้งหมด การเปิดบริการ/หน้าจอจริงยังติด automatic approval review เดิม และไม่มีการเปิดผ่านช่องทางอื่น
