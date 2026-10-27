# ผล native Goal กับไฟล์ Go ขนาดใหญ่

วันที่ 26 ตุลาคม 2026 · Outcome verified สำหรับ fixture นี้ · candidate ของ harness ยังไม่ review หรือ apply เข้าตัวหลัก

Hermetrix ใช้ local Bonsai จริงเลือกไฟล์ เสนอ bounded edit และตรวจหลังทดสอบอีกครั้งผ่าน provider profile แยก การแก้ใช้ native Goal/worker/apply/verification/review APIs พร้อม source ขนาด 480,099 bytes จาก Go code และ standing test ที่ Hermetrix เป็นผู้เขียน Go test เดิมล้มเหลวเพราะ Add คืนค่าผลต่าง หลังแก้ native command ผ่านและ Task/Goal ปิดได้ตามเกณฑ์ของ fixture

หลักฐานถาวรอยู่ที่ [receipt](../evidence/2026-10-26/h1d-live/b5-native-go/receipt.json) พร้อม provider requests/responses, artifacts ทั้ง 8, completion gates, fixture ก่อน/หลัง และ operational drivers ชุด source ของ harness ที่ใช้ตรงกับ candidate 67 paths ใน [B3 manifest](../evidence/2026-10-26/h1d-live/b3-curation/candidate-manifest.json)

## การทดลองจริง

| รายการ | ผล |
|---|---|
| Model | prism-ml/Ternary-Bonsai-2-27B-gguf:PQ2_0 ที่ local 8088 |
| Context และ saved output cap | 16,384 และ 3,072 ไม่เพิ่มงบของ profile |
| Go source | 480,099 bytes มี Unicode comment prefix และ Add declaration |
| ก่อนแก้ | actual go test ล้มเหลว: Add(2, 3) = -1, want 5 |
| File selection | โมเดลจริง 1 call |
| Code proposal | โมเดลจริง 1 call; ได้ bounded views และไม่ได้รับ hidden prefix |
| Apply | native apply เปลี่ยนเฉพาะ return a - b เป็น return a + b |
| Verification | actual frozen command go test ./... -count=1 -v ผ่าน TestAdd |
| Review | โมเดลจริง 1 call ด้วยคนละ provider profile; ได้ partial before/after hashes และ actual Go stdout |
| Readonly files | go.mod และ sum_test.go คง byte เดิม |
| Goal/Task | completed ทั้งคู่ รวม 8 observed actions |
| เวลา | ตัว operational test ใช้ 15.29 วินาที ไม่ใช่ SLA สำหรับงานทั่วไป |

Writer คือ `provider_e9f035b434118c1048fd8a5f05398b56` ส่วน reviewer คือ `provider_287ee0ae512731154193bf1b12131b6c` ทั้งสองใช้โมเดลและ endpoint เดียวกัน จึงเป็น separate review pass ไม่ใช่ model-diverse evidence

## การทำต่อจาก state ที่เก็บไว้

หลัง proposal มี durable state pending_review และ provider calls รวม 2 ครั้ง ได้ปิดแล้วเปิด SQLite และ product/provider/task/coordinator services ใหม่ ตรวจว่า proposal ID เดิมยังอยู่และจำนวน provider calls ไม่เพิ่ม จากนั้น workflow ทำ approve/apply/verify/review ต่อจนจบ การเรียก AdvanceGoal กับ terminal goal ถูกปฏิเสธและไม่เรียก provider เพิ่ม

นี่เป็นการ reopen ฐานข้อมูลและ services ใน process เดิมที่ observed idle boundary ไม่ใช่การฆ่า process ระหว่าง dispatch หรือพิสูจน์ทุก uncertain-effect scenario งาน resilience ส่วนที่กว้างกว่านี้ยังต้องตรวจตามเป้าหมายเต็ม

ห้าม replay driver เดิมหรือใช้ root เดิมเป็น before fixture อีกครั้ง ตอนนี้ source ใน root ถูกแก้แล้ว และ reservation/Goal/operation receipts เป็น terminal ใช้ factory seed และชื่อ root/output/operation ใหม่สำหรับการทดลองหลัง apply

## Identity ที่ต้องรักษา

- Goal: goal_70b3fb18452b625be06b94c9fddab1b5
- Task: task_25eb9b739bd8930a7893fdc2ebd2e4b7
- Project: project_79504f348b2c3c3d01fd58f4bc5f4bb4
- Proposal: bfdaaab38aa76c29c1f7ff56872fb76045cd4c7107f202704b1564651b22b4b9
- Root: D:/Projects/harness/hermetrix-harness/.tmp-ui/goal-20261023/live-20261026-001100/native-large-go-v1
- Audit store: RUNROOT/readonly-audit-data ไม่ใช่ installed data

Source/test fixture มาจาก native largeGoAcceptanceFixture ส่วน Codex เขียนเฉพาะ operational supervision/export drivers, DATA และเอกสาร การเปลี่ยน implementation ที่ทำให้ test ผ่านเกิดผ่าน native proposal/apply ไม่ได้แก้ fixture ด้วยมือ

## ขอบเขตของผล

ไฟล์ใหญ่ส่วนมากเป็น comment padding และจุดแก้เป็น arithmetic declaration ขนาดเล็ก การทดลองนี้พิสูจน์ bounded transport, exact source preservation, durable artifacts, actual model dispatch, native apply, Go verification และ separate review ในกรณีที่ระบุ ไม่ได้วัดความเข้าใจระบบธุรกิจขนาดใหญ่หรือการออกแบบ marketplace

Goal นี้มีเกณฑ์เฉพาะ Add จึงไม่ใช่ completion ของเป้าหมาย R1–R10 ทั้งหมด ไม่ได้ต่อ learning service เพื่อสร้าง/promote บทเรียนอัตโนมัติจากรันนี้ และยังไม่ได้พิสูจน์การถ่ายทอดบทเรียนสู่งานใหม่

ขั้นถัดไปคือ cohesive native review และ apply candidate 67 paths ตาม hash ที่ตรวจแล้ว จากนั้นตรวจหลัง apply และทำ H1A/H1B/H1C ต่อ พร้อมรักษางาน UX/control และ marketplace Compose/Kubernetes การเปิด serving UI ยังอยู่ภายใต้ automatic approval review เดิมที่ปฏิเสธก่อน launch ไม่มีการเปิดผ่าน route อื่นในรอบนี้
