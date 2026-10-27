# ผลนำเข้า claim run ก่อนเริ่ม effect

27 ตุลาคม 2026 นำ API claim และ test ที่ Bonsai Local เขียนเข้า source จริงแล้ว 13 ไฟล์ ผ่าน native review 4 ชุดและ native write 13 receipts ไม่มี rollback หลังนำเข้า ทดสอบทั้ง repository โดยไม่ใช้ source overlay ผ่าน 1,365 test events ไม่พบ failure มี 20 opt-in/environment skips และ vet/build ผ่าน

ขั้นนี้ทำให้ host มี API สำหรับขอสิทธิ์เริ่ม run ได้ครั้งเดียวและตรวจ snapshot ซ้ำใน transaction ยังต้องเชื่อม host resolver/orchestrator/runner, completion และ promotion ต่อก่อนเป็นวงจรเรียนรู้ที่ใช้งานจริงครบ ระบบที่ติดตั้งและ serving ไม่ได้ถูกเปลี่ยนในรอบนี้

ดู [receipt](../evidence/2026-10-27/h1a6-claim-applied/receipt.json), [source และ native ancestry](../evidence/2026-10-27/h1a6-claim-applied/source-applied-manifest.json), [ผลทดสอบหลังนำเข้า](../evidence/2026-10-27/h1a6-claim-applied/post-test-summary.json) และ [ต้นทุนที่สังเกตได้](../evidence/2026-10-27/h1a6-claim-applied/observed-cost.json)

## พฤติกรรมที่เพิ่ม

`ClaimBehavioralRun(ctx, runID, expectedManifestSHA256, observedBindings)` อ่าน owned run และตรวจ hash/bindings ทุก field เทียบข้อมูลที่ Prepare เก็บไว้ จากนั้นตรวจ candidate revision/hash/blob/base/scope/readiness และ actual owned active project/session อีกครั้ง พร้อมตรวจ base ว่ายังเป็นรุ่นปัจจุบัน ทั้งหมดอยู่ใน transaction เดียว

เมื่อผ่านจึง CAS state จาก prepared เป็น dispatched และตรวจว่าเปลี่ยนหนึ่งแถว ก่อน commit แล้วคืน record ที่เปลี่ยนเฉพาะ state หากเรียกพร้อมกันมีผู้ชนะเพียงหนึ่งราย dispatched/uncertain ปฏิเสธการ claim ใหม่ ส่วนสถานะปิดงานปฏิเสธด้วย revision conflict การปฏิเสธ input/สิทธิ์/state/freshness ที่ทดสอบไม่แก้ประวัติ และทุก error คืน zero record หากผล commit ไม่ชัดเจน host ยังต้องอ่านและ reconcile run เดิม

Get และ Prepare replay ยังอ่านประวัติเดิมได้ แต่การอ่านประวัติไม่ให้สิทธิ์เริ่ม effect ซ้ำ ขาดช่วงหลัง claim commit แล้วต้อง reconcile run เดิม ไม่สร้างการอนุญาตใหม่อย่างเงียบ ๆ

bindings ใน API นี้ยังมาจาก host caller ไม่ใช่หลักฐานว่าระบบ resolve config หรือสังเกต provider/tool execution จริง ยังไม่มี HTTP endpoint, provider/tool dispatch, completion หรือ promotion authority เพิ่มจากชุดนี้ และไม่รับประกันว่าจะไม่มี candidate/config เปลี่ยนหลัง claim; runner ต้องใช้ package ที่ pin ไว้และ completion/promotion ต้องตรวจซ้ำ

## หลักฐานตรวจรับ

| ส่วน | ผลที่ตรวจได้ |
|---|---|
| Source | production 2 ไฟล์และ test/support 11 ไฟล์ รวม 13; native ancestry 33 ขั้น ตรงผลโมเดลหลัง gofmt |
| Concurrent claim | actual SQLite สอง goroutine ได้ success หนึ่งราย อีกหนึ่งราย uncertain; record/manifest คงเดิมและไม่มี evaluation เพิ่ม |
| History/reopen | Get/Prepare replay คืนประวัติ dispatched เดิม; ปิด/เปิด SQLite และ services แล้ว claim ซ้ำไม่สำเร็จ ไม่ใช่ process-kill proof |
| Freshness | candidate หรือ current base เปลี่ยนหลัง Prepare แล้ว claim ปฏิเสธ; bindings 15 fields และ CaseIDs order รวม 16 variants ถูกปฏิเสธโดยประวัติไม่เปลี่ยน |
| Scope และ input | actual owned project/session ปิดหลัง Prepare แล้ว claim ปฏิเสธ คืน active แล้ว positive control ผ่าน; malformed ID/hash/bindings, missing run และ unknown principal ไม่ได้สิทธิ์ |
| Closed states | uncertain/aborted/stale ที่เก็บใน SQLite ไม่เปิดทาง claim ใหม่; fixture UPDATE ทุกครั้งตรวจ RowsAffected=1 และ Get ตรง snapshot ตามสถานะ |
| Review/apply | approve ทั้ง 4 ชุด ไม่มี blocking findings; native write 13 receipts ไม่มี rollback และ hashes หลังนำเข้าตรงครบ |
| Root checks | ก่อนนำเข้าบน candidate overlay และหลังนำเข้าบน actual source ผ่าน 1,365 events/0 failures/20 skips; vet/build ผ่านทั้งสองรอบ |

รักษา source เดิม 166 preservation records ครอบคลุม 161 paths และ schema ยังเป็น 79 ไม่มี migration เพิ่ม จำนวน test events รวม parent/subtest ไม่ใช่จำนวนกรณีธุรกิจอิสระ และไม่ใช้เป็นหลักฐาน production readiness ของเป้าหมายทั้งหมด

## ข้อผิดพลาดและต้นทุน

รอบนี้มี generation responses 40 ครั้ง ใช้ prompt 93,525 และ completion 39,510 tokens รวมร่างที่ cap/reject/ทิ้ง ส่วน review 4 ครั้งใช้ 36,203 และ 914 tokens เป็น Bonsai รุ่นเดียวกันผ่าน reviewer profile อีก pass ไม่ใช่ model diversity และยังมี Codex กำกับการแยกงาน วิเคราะห์ error และเลือก source ที่ผ่าน

พบร่างที่รับค่าจาก scanner ผิดจำนวน สร้าง helper ซ้ำ อ้างตาราง/คอลัมน์หรือ import ที่ไม่มี ใช้ struct comparison ที่ไม่รองรับ slice และทดสอบ Prepare แทน Claim บางร่างชน output cap 3,072 จึงแบ่ง test/fixture ให้เล็กลง บางการซ่อมหลายจุดแก้ไม่ครบ จึงต้องส่งจุดที่ยังผิดเป็นคำสั่งแยกและตรวจเนื้อหา/ผลจริง ไม่ใช้ summary ของโมเดลแทนหลักฐาน

มีการแก้ test ที่คาดผิดว่า dispatched ต้องมี evaluation/result และที่เรียก Claim ซ้ำแต่ต้องการอ่านประวัติ รวมถึง valid-input row ที่ถูกใส่ใน malformed table โดยยังคง positive control และ nil-CaseIDs negative ไว้ ผลที่ถูกปฏิเสธไม่ถูกนำมาปะติดปะต่อเป็น source

Codex เคยอธิบายเร็วไปว่า DATA preset mismatch ตั้งค่าเท่า base แต่ตรวจจริงพบ base=1/case=2 ถูกต้องอยู่แล้ว ตัวตรวจหยุดก่อนเขียน DATA และไม่มี v2 ถูกสร้าง สาเหตุจริงคือร่าง test กลับเครื่องหมายเปรียบเทียบ แก้ด้วย native response แล้วผ่าน DATA เดิม hash `a8fbccf139b809edcdd171ede3f6865a98c264d3201613b9464cb8c69465fa9d` ดู raw `H1A6-operator-oracle-resolution.json` ใน evidence

หลักฐานนี้ยังไม่พิสูจน์ autonomous repair, causal improvement จากการจัดบริบท หรือการนำบทเรียนไปช่วยงานใหม่ ต้องวัดแยกจากผลการกำกับงาน

## งานต่อเนื่อง

1. ให้ host orchestrator บันทึก operation/run ID ก่อน Prepare และใช้ Claim ก่อน effect จริง พร้อม resolver ที่คำนวณ actual provider/preset/catalog/dataset/environment/budget bindings
2. เชื่อม trusted executor และ host-observed receipts ให้ completion อ้าง run เดิม ตรวจ stale candidate/base/config และ conflicting/exact replay ใน transaction รองรับ uncertain/reopen โดยไม่ resample เงียบ
3. แก้ RecordBehavioralEval และ promotion ให้ใช้ trusted completed run เท่านั้น รักษา paired evidence gate เดิมและตรวจ promotion ใน transaction ปัจจุบัน legacy เส้นทางนี้ยังไม่บังคับ trusted run
4. พิสูจน์งานใหม่และ holdout transfer แล้วทำ dynamic UX/control และ marketplace หลายผู้ขายให้ครบทั้ง Compose และ Kubernetes จริงตาม R1–R10 เป้าหมายเต็มยัง active

หลักฐาน Go ใช้ `.go.txt` ทั้งหมด manifest เริ่มต้น 510 รายการคงเดิม ผลตรวจหลังจัดเก็บและ manifest v2 แยกไว้ ห้าม replay H1A6 apply/review/generation prefix ที่มีผลแล้ว งานใหม่ใช้ actual source และ prefix H1A7
