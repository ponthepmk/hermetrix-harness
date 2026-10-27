# ผลเชื่อม input snapshot กับข้อเสนอแบบ durable

วันที่ 26 ตุลาคม 2026 · สถานะ candidate 21 ไฟล์ ยังไม่ apply B และยังไม่ผ่าน acceptance งาน Go ขนาดใหญ่ครบวงจร

Hermetrix เขียนการเปลี่ยนแปลงและ Go tests ทั้ง 21 ไฟล์ มี implementation 7 ไฟล์และ test/support 14 ไฟล์ Codex กำหนดงาน ตรวจ source และผลทดสอบ จัดทำ operational drivers และตรึงข้อมูล fixture แต่ไม่ได้เขียน product Go หรือ standing Go tests แทนโมเดล

ชุดนี้ต่อจาก [สัญญา B](2026-10-26-coordinator-bounded-context-contract.md) โดยเชื่อมการเก็บและอ่าน snapshot เข้ากับ coordinator แล้ว ส่วนการเลือก bounded views อัตโนมัติและ endpoint ของ bounded reviewer ยังต้องทำต่อ

## พฤติกรรมที่เพิ่มใน candidate

Propose freeze Task และ canonical bytes ก่อน callback/provider แล้วเก็บ input artifact หลัง Run และ ValidateResult ผ่านเท่านั้น จึงไม่เก็บ snapshot ที่ worker ปฏิเสธเพราะ credential ข้อเสนอใหม่เก็บ envelope version 2 ซึ่งอ้าง input artifact แยกต่างหาก และ registry ผูก hash ของ envelope ทั้งฉบับ

ตัวอ่าน input ตรวจ kind/project, hash ของ body, Result.InputSHA256 และ metadata ของ task/step/attempt/packet/proposal/root ก่อน strict decode, freeze และ ValidateResult อีกครั้ง Metadata ที่หาย ผิดค่า หรือผิดชนิดต้องคืน error และ Task ว่าง ตัวอ่านข้อเสนอชั้นนอกตรวจ registry/body hash และคืน loadedProposal ว่างทุก error

เส้นทางใช้ตัวอ่านกลางมีหกจุด: observed Propose reuse, Apply, prepareVerification, Review, ReconcileProviderArtifact และ rollbackProposal รูปแบบ legacy raw whole-file result ยังอ่านได้ ส่วน retained partial edits ต้องใช้ envelope ที่มี snapshot

Input artifact ใช้ source_operation_id ขณะที่ outer proposal คง operation_id เดิม เพื่อให้การกู้คืนหา response artifact ได้แน่นอนโดยไม่เลือก input แทน response การกู้คืนไม่เรียก provider ซ้ำ

## หลักฐานที่ตรวจแล้ว

| กรณี | ผลและขอบเขต |
|---|---|
| Input roundtrip | Task ตรงกับข้อมูลตั้งต้น และเปลี่ยน returned view แล้วไม่เปลี่ยน fixture ต้นฉบับ |
| Metadata 18 กรณี | หก keys คูณ missing/wrong string/wrong type ถูกปฏิเสธพร้อม zero Task |
| Proposal roundtrip | โหลด envelope/input ผ่าน artifact API และได้ Task ที่ผูกไว้ |
| Missing input reference | outer artifact และ registry hash ถูกผูกใหม่อย่างถูกต้องก่อนทดสอบ; loader คืน error และ zero loadedProposal |
| Database write failure | SQLite trigger ปฏิเสธ input artifact จริง; Propose คืน error, ไม่มี proposal/outer artifact, source hash เดิม และ provider ไม่ถูกเรียกซ้ำใน attempt เดิม |
| แก้งานหลัง tests ล้มเหลว | Goal runner apply/run/rollback/replan จนผ่านตาม assertions เดิม |
| คืนสภาพไฟล์ใหม่ | ทดสอบ rollback กลับเป็น absence แล้ว replan ผ่าน |
| Reviewer reject | คืนสภาพพร้อม rollback receipt ตาม test เดิม |
| Owner แก้ไฟล์ภายหลัง | fixture legacy ที่ครบ status/hash ยืนยันว่า rollback ไม่ทับ manual edit และยังตรวจ idempotence/private-content เดิม |
| Credential guard | ข้อมูลที่มี provider credential ถูกปฏิเสธก่อน dispatch ตาม test เดิม |
| Vet และ build | ผ่านบน candidate; ไม่มีการเปิด binary ที่ build |

การตรวจชุดใหญ่ครั้งแรกผ่าน worker, skills, learning, selfimprove, agent และ product แต่ taskcoord พบ fixture rollback เดิมที่ไม่มี status และ ResultHash ทั้งสองค่ามีในข้อเสนอจริงจาก Propose/registry อยู่แล้ว Hermetrix เติมสอง fields ใน fixture โดยไม่แก้ assertions หรือผ่อน production loader หลังแก้ full taskcoord suite ผ่านใน 182.756 วินาที ไม่มีการแก้ tests เพื่อปิดหรือข้ามการตรวจ สรุประดับ package เดิมไม่ได้แยกสถานะ opt-in ราย test; ผล JSONL รอบ B4 ที่ใหม่กว่าระบุชัดว่า live-model test หนึ่งรายการถูก skip ตามค่าเดิม จึงไม่ใช่หลักฐาน live-model acceptance

หลังจากนั้นจัดรูปแบบ candidate เดิม 5 ไฟล์ด้วย gofmt โดยทำสำเนาใหม่และเก็บ hash ก่อน/หลัง ไม่มีการแก้ logic ด้วยมือ Worker suite ผ่านอีกครั้งใน 5.348 วินาที และ focused taskcoord ผ่านใน 6.569 วินาที รวม parser, loaders, persistence failure, goal repair และ legacy rollback; vet/build ผ่านและ format failures เป็นศูนย์

Functional fixtures ใช้ adapters เดิมแบบ deterministic แต่ใช้ owned project, SQLite, filesystem และ command processes จริง ส่วนการสร้าง candidate เรียกโมเดล Bonsai local จริง หลักฐานสองประเภทนี้ยังไม่ใช่ผล live-model large-Go flow หรือหลักฐานว่า self-learning อัตโนมัติช่วยงานใหม่ดีขึ้นแล้ว

## ข้อผิดพลาดที่พบระหว่างการกำกับ

- Root ถูก shadow ใน if initializer ทำให้ valid input ถูกปฏิเสธ; positive test จับก่อนแก้
- Loader คืน artifact/bundle บางส่วนเมื่อ input error; corrected missing-reference fixture จับก่อนเลื่อน assignment ไปหลัง validation
- ร่าง integration ลบส่วน Apply มากเกินไป; ถูกกันออกและเปลี่ยน packet ให้แก้ได้เฉพาะช่วง decode โดยส่วนสิทธิ์/preimage/rollback อยู่นอก views
- Model ใช้จำนวน arguments/return values ผิดและร่าง error handler ที่ทับ creation error; compiler และการตรวจ source กันร่างออก ก่อนทดสอบ database failure จริงกับรุ่นที่แก้แล้ว
- รูปแบบ envelope ทำให้ rollback reader เก่าได้ Changes ว่าง; goal test ล้มเหลวก่อนเชื่อม rollback เข้าตัวอ่านกลาง
- Tests ที่โมเดลเสนอเคยไม่ได้เปลี่ยน reference เป็น missing จริง, เทียบ SHA กับ content และยอมให้ receipt key หาย; ต้องตรวจ setup และ oracle ก่อนเชื่อชื่อ test หรือผล PASS

การแก้ทั้งหมดมาจาก native proposals ไม่ดึงบาง edit จาก proposal ที่ host ปฏิเสธ และไม่ salvage completion ที่ถูกตัด

## ขอบเขตที่ยังไม่ผ่าน

ยังต้องเพิ่ม automatic host view curation ที่พอดีกับ preset/wire budget, bounded ReviewTask พร้อม full-source reviewer credential scan, ชุด tamper ของ hash/project/registry เพิ่มเติม และ live-model owned Go project เกิน 128 KiB ครบ proposal/apply/test/separate-review/recovery ก่อนรีวิวและ apply B ทั้งชุด

Source ที่เคย apply H1B/A1/A2/A3 ยังตรง reviewed hashes 59/59 ไฟล์ B ทั้ง 21 ไฟล์ยังเป็น overlay candidate ระบบติดตั้งและข้อมูลเดิมไม่ถูกเปลี่ยน เป้าหมาย H1A/H1C, UX/UI, computer control และ marketplace ที่มี Compose/Kubernetes ยังคงอยู่

## หลักฐานและการทำต่อ

- [Receipt และสถานะ qualification](../evidence/2026-10-26/h1d-live/b-durable/receipt.json)
- [Manifest ล่าสุด 21 ไฟล์พร้อม native authorship](../evidence/2026-10-26/h1d-live/b-durable/formatted-manifest.json)
- [Hash ก่อนและหลัง gofmt](../evidence/2026-10-26/h1d-live/b-durable/formatting-receipt.json)
- [Native integration lineage](../evidence/2026-10-26/h1d-live/b-durable/integration-lineage.json)
- [หลักฐานรักษา source ที่ apply แล้ว](../evidence/2026-10-26/h1d-live/b-durable/applied-source-preservation.json)

Overlay ล่าสุดคือ `.tmp-ui/goal-20261023/live-20261026-001100/H1D-B-final-formatted-overlay.json` ห้ามใช้ overlays เก่าที่มี bad drafts งาน inference/test/check ที่ทราบจบแล้วทั้งหมด ดู NEXT_SESSION สำหรับงานถัดไป
