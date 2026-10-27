# ผลนำเข้า Goal start ด้วย ID ที่ host บันทึกล่วงหน้า

27 ตุลาคม 2026 นำ source ที่ Bonsai Local เขียนเข้าโครงการจริง 17 ไฟล์ (production 5, test 12) ผ่าน native review 5 ชุดและ native write 17 receipts ไม่มี rollback หลังนำเข้าทดสอบทั้ง repository โดยไม่ใช้ source overlay ผ่าน 1,385 test events, 0 failures, 20 opt-in/environment skips และ vet/build ผ่าน Schema ยังเป็น 79 และ Go source อีก 695 ไฟล์นอกชุดแก้ไขตรงกับ hash เดิม

หลักฐาน: [receipt](../evidence/2026-10-27/h1a7b-goal-start-applied/receipt.json), [source/ancestry](../evidence/2026-10-27/h1a7b-goal-start-applied/source-applied-manifest.json), [ผลหลังนำเข้า](../evidence/2026-10-27/h1a7b-goal-start-applied/post-test-summary.json), [โมเดลจริง](../evidence/2026-10-27/h1a7b-goal-start-applied/live-probe-summary.json), [ต้นทุน](../evidence/2026-10-27/h1a7b-goal-start-applied/observed-cost.json)

## ปัญหาและพฤติกรรมใหม่

เดิม CreateGoal สร้าง ID ภายใน หากสร้างสำเร็จแต่คำตอบหาย host ระบุ Goal เดิมเพื่อส่งซ้ำไม่ได้ ขณะที่ ServeGoals อาจเริ่มงานนั้นแล้ว เพิ่ม taskengine.Service.CreateGoalAtID และ optional goal_id ใน taskcoord.StartGoalInput ให้ host บันทึก ID พร้อมคำขอเดิมก่อนเรียก

ระบบคำนวณ hash ของ task ID, revision ที่ขอเริ่มครั้งแรก, actor ที่ตัดช่องว่าง และ effective frozen GoalConfig โดยล้าง marker ที่ caller ส่งมา เก็บใน config_json โดยไม่เพิ่ม migration ส่ง ID/คำขอเดิมซ้ำแล้วคืน current owned historical Goal รวมถึงหลัง task เดินหน้าหรือ completed โดยไม่สร้างงานใหม่หรือรีเซ็ตงบ/เวลา ต่าง task/actor/original revision/config จะ conflict และคืน zero record หลัง insert error จะ reconcile ID เดิม ไม่มี ID สำรอง Stored config ถูก hash ตรวจซ้ำ ไม่เชื่อ marker อย่างเดียว

StartGoal ยังตรวจ owned task/root/enabled profiles/local egress/defaults ก่อนเลือก API Caller ที่ไม่มี goal_id ยังคง random ID แบบเดิมและไม่มี marker **UI และ host evaluation runner ยังไม่ได้บันทึก/ส่ง ID ใหม่นี้อัตโนมัติ** Host ต้อง persist ID และ original input ก่อน effect ขั้นนี้จึงเป็น prerequisite ของ runner จริง

## หลักฐานตรวจรับ

| ส่วน | ผลจริง |
|---|---|
| Concurrent | SQLite จริง สี่ goroutine ได้ Goal เดียวกันครบทั้งสี่และมีหนึ่งแถว |
| Request identity | 11 config fields รวม actor/task ID/original revision เป็น 14 กรณี conflict โดย history/count คงเดิม |
| Boundaries | malformed ID, foreign/unknown principal ถูกปฏิเสธและ zero; ขอบเขต 160 bytes และภาษาไทยผ่าน |
| Legacy/error | random allocation และ cleared marker เดิมคงอยู่; legacy record authorize named replay ไม่ได้; creation error ไม่ถูกกลบเป็น not-found |
| Integrity | เปลี่ยน stored config แต่คง marker เดิมไว้ replay ถูกปฏิเสธ |
| Reopen | ID-file-before-create, discarded creation return, consumed budget, SQLite/services reopen และ recover; replay คืน waiting/stopped record เดิม |
| Coordinator | effective defaults replay ได้; หลัง actual fixture failure/recovery/write/Node verification/completion คืน record/actions/provider counters เดิม |
| Encoding | หก invalid-UTF8 identity cases เคยถูกยอมรับ ก่อนแก้; หลังแก้ปฏิเสธครบ Valid Thai และ ignored invalid caller marker ยังผ่าน |
| Qualification | final candidate และ actual post-apply ต่างผ่าน 1,385 events/0fail/20skips, vet/build; native ancestry 54 ขั้นและ source hashes ตรงครบ |

Events รวม parent/subtests ไม่ใช่จำนวนกรณีธุรกิจอิสระ การ reopen เป็น SQLite/services ใน process เดิม ไม่ใช่ process kill ระหว่าง dispatch

## Bonsai จริง

ใช้ exact native Go arithmetic fixture ที่มี comment padding รวม 480,099 bytes ก่อนแก้ TestAdd ล้มเหลว หลังโมเดลเลือกไฟล์ เสนอ bounded edit, native write, actual Go check และ separate-profile review ผ่าน Task/Goal complete ด้วย 8 observed actions และ 3 provider calls รักษา go.mod และ standing test byte เดิม

Final17 trial ใช้ Goal goal_h1a7b_named_lifecycle_v2 และ task task_7055df0c6c96fe574b1f487d90bcb64d บันทึก ID/คำขอก่อน start แล้วจงใจทิ้ง successful return ปิด/เปิด SQLite/services ก่อน retry และอีกครั้งเมื่อมี pending proposal ตรวจ named replay ระหว่างงานและหลัง completed โดย history/งบ/เวลา/Goal count/provider calls ไม่เพิ่ม Original revision2, current Goal revision6

นี่เป็นการจำลองคำตอบหายด้วยการทิ้ง return ไม่ใช่ network fault งานมีแผนจาก operator และจุดแก้ arithmetic เล็ก ไม่พิสูจน์ business complexity, autonomous planning/repair หรือ learning transfer รุ่นก่อน encoding guard ทดลองแยกและเก็บเป็นประวัติ Final source-manifest SHA5c31176b0427c84e81ee75cea1f30b7779cef7916922ba582a388d01704e94f5

## ข้อผิดพลาดและต้นทุน

64 generation requests ใช้ prompt128978/output58179 tokens;57 protocol accepted,54 final ancestry steps และ3 abandoned accepted test prototypes 7 rejected outputs รวม cap/no-op/ผิด bounded protocol ไม่ถูก salvage Review5 requests prompt43724/output1332 ทั้งหมด ledger reconciled ด้วย actual usage Live fixture สองครั้งเก็บต้นทุนแยก Same local model ผ่านคนละ profile ไม่ใช่ model diversity

หลายร่างอ้าง helper/import/table/field ที่ไม่มี ใช้ return values ผิด ประกาศตัวแปร nil ทับค่ามาตรฐาน หรือแก้หลายข้อไม่ครบ บาง test oracle คาด caller marker หรือ initial record เท่ากับ completed ซึ่งผิดและแก้ผ่าน native response DATA เดิมสี่ชุดไม่เปลี่ยน hash

Initial15-source cohort เคยผ่าน1377events แต่ยังไม่ครอบคลุม UTF8 เพราะ JSON แทน malformed bytes ด้วย replacement character เพิ่ม negative tests แล้วพบยอมรับครบหกกรณีจริง Native guard แรกคืน nil error จึงยังไม่ปฏิเสธ ชุดทดสอบเดิมจับได้และแก้ก่อนตรวจเต็มใหม่

Operator มีข้อผิดพลาดด้วย: narrow suffix ขาด syntax scope, PowerShell scalar indexing สร้าง pattern ผิดและหยุดก่อน dispatch, เรียก fixture check ผิด working directoryจนเริ่ม root run ที่ได้หยุดและไม่นับ qualification ไฟล์ผลสองไฟล์ที่ไป D:/ ย้ายกลับ workspace แบบไม่ overwrite Operational probe v1 แก้ชื่อ sample ซ้ำก่อน dispatch Global Anaconda startup ล้มเหลว จึงใช้ bundled Python อ่าน ledgerแบบ read-only โดยไม่แก้ environment ทั้งหมดมี raw evidence งานนี้มี Codex กำกับ ไม่ใช่ autonomous learning

Product/standing tests ทั้งหมดจาก Bonsai ตรวจ native ancestry และใช้เพียง gofmt จัดรูปแบบ Codex ออกแบบ/กำกับ/ตรวจ และเขียนเฉพาะ operational drivers, DATA และ prose

## งานถัดไป

1. ให้ host persist operation/run ID และ original input ก่อน effect ใช้ named Goal ใน runner จริง Resolve actual provider/preset/catalog/dataset/environment/budget และ materialize pinned candidate/base
2. เชื่อม trusted executor/observed receipts/completion ตาม immutable run เดิม Reconcile uncertain/lost response โดยไม่สร้างงานใหม่
3. บังคับ trusted completed run ก่อน behavioral evaluation/promotion และตรวจ paired evidence/freshness ภายใน promotion transaction ปัจจุบัน legacy caller-result และ preflight-outside-TX ยังไม่ปิดช่องว่างนี้
4. พิสูจน์ held-out transfer/cost ทำ dynamic UX/control และ marketplace buyer/seller/admin ให้ครบ local Compose และ Kubernetes จริงตาม R1–R10 เป้าหมายเต็มยัง active

Installed app/binary/data ไม่เปลี่ยน Serving เดิมถูก automatic approval review ปฏิเสธ “blocked by policy” และไม่มีการเปิดผ่าน port/binary/route อื่น รอบนี้ใช้ headless APIs/tests ไม่มี branch/commit/PR ใหม่ ห้าม replay prefix ที่มีผลแล้ว หลักฐาน Go ทุกสำเนาเป็น .go.txt
