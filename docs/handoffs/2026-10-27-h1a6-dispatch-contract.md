# ข้อกำหนด claim run ก่อนเริ่ม effect

27 ตุลาคม 2026 ต่อจาก prepared-run schema79 ที่นำเข้าแล้ว ขั้นนี้เพิ่ม transaction สำหรับ claim เพียงครั้งเดียวก่อน trusted host เรียก provider/tool ไม่เพิ่ม HTTP endpoint หรือ completion/promotion authority และยังไม่ใช้แทนหลักฐาน actual host execution

ผลภายหลัง: นำ claim-only cohort 13 ไฟล์เข้า source แล้ว ผ่าน review/apply และ actual root checks ดู [ผลและขอบเขต](2026-10-27-h1a6-claim-apply-results.md) ส่วนการเชื่อม host resolver/orchestrator/runner และ completion/promotion ด้านล่างยังต้องทำต่อ

## เส้นทางที่ต้องเชื่อม

host ต้องบันทึก operation/run ID ก่อน Prepare แล้ว resolve actual bindings อีกครั้งก่อน Claim และ effect `ClaimBehavioralRun(ctx, runID, expectedManifestSHA256, observedBindings)` รับข้อมูลจาก host caller ภายใน ไม่ถือการส่ง metadata นี้ว่าเป็นหลักฐาน provenance การสร้าง resolver, durable orchestrator และ runner ที่เรียกใช้จริงยังเป็นงานต่อเนื่องที่จำเป็น

Claim ตรวจ ID/hash/bindings format เปิด transaction ใช้ ownerPrincipalInTx และอ่าน owned run ผ่าน scanner เดิม เมื่อ hash หรือ bindings ไม่ตรงคืน ErrRevisionConflict โดยไม่แก้ record เมื่อไม่พบ owned run คืน ErrNotFound และทุก error ต้องคืน zero BehavioralRun

prepared เท่านั้นที่ claim ได้ dispatched/uncertain คืน ErrBehavioralRunUncertain เพราะการเริ่มใหม่อาจทำ effect ซ้ำ completed/stale/aborted คืน ErrRevisionConflict การอ่านผ่าน Get หรือ Prepare replay ยังคืนประวัติได้ แต่ไม่ทำให้ Claim ครั้งใหม่ผ่าน

ภายใน transaction เดียวต้อง reload candidate ตาม ID/owner ตรวจ readiness/checks และเทียบ persisted ID/revision/hash/blob/base/scope กับ manifest เดิม ตรวจ active owned project/session อีกครั้ง และตรวจ owned current base/hash/blob ผ่าน helper เดิม candidate/base/bindings เปลี่ยนหรือ scope ปิดระหว่าง Prepare กับ Claim ต้องปฏิเสธโดย record เดิมยังอยู่

เมื่อผ่านทั้งหมด ให้ CAS UPDATE prepared→dispatched ด้วย id/owner/manifest hash/state และเงื่อนไขไม่มี evaluation/result/completion ตรวจ RowsAffected=1 แล้ว commit ก่อนคืน record เปลี่ยนเฉพาะ state ทุกข้อมูล immutable ต้องตรงเดิม Claim ที่ชนะเพียงรายเดียวจึงให้ host ไปยัง effect ถ้าขาดช่วงหลัง commit ก่อนตอบ การ retry ต้องไม่ authorize effect ใหม่ ต้องอ่านและ reconcile record เดิม

## หลักฐานตรวจรับ

- actual temporary SQLite: concurrent Claim ได้ success หนึ่งรายและ uncertain อีกหนึ่งราย ไม่มี run/evaluation ซ้ำ และ Get/Prepare replay ตรง record ที่ dispatch
- bindings ทุก field รวม CaseIDs value/order และ manifest SHA ที่เปลี่ยนถูกปฏิเสธก่อน transition โดยข้อมูลเดิมยังเท่าเดิม ใช้ frozen DATA แยกจาก implementation
- candidate เปลี่ยนหลัง Prepare และ base current เปลี่ยนหลัง Prepare ถูกปฏิเสธ ตรวจ premise ว่าเปลี่ยนจริง และไม่มีการเปลี่ยน state
- inactive owned project/session หลัง Prepare, unknown principal/missing ID และ malformed input ไม่เปลี่ยน record
- ปิด/เปิด SQLite/services หลัง Claim แล้วยังอ่าน dispatched เดิม และ Claim ซ้ำไม่สำเร็จ
- native review ของ exact source และ affected tests; native apply ตาม preimages; ตรวจ actual root tests หลังนำเข้า ไม่มี Codex เขียน product implementation หรือ standing test แทน

## งานที่ยังต้องทำให้ครบวงจร

H1A6 claim เป็น durable dispatch intent ไม่ได้ยืนยันว่า effect เกิดหรือสำเร็จ ไม่ใช้ผลนี้ปิด H1A เป้าหมายยังต้องมี trusted resolver/runner, observed provider/command/artifact receipts, completion/replay/uncertain reconciliation และ transaction promotion ที่บังคับ trusted completed run พร้อม paired evidence รวมทั้ง holdout transfer และเป้าหมาย UX/control/marketplace/Compose/Kubernetes เดิม
