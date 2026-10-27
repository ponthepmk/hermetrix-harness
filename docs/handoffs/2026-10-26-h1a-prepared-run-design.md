# H1A — ขอบเขตการสร้างและอ่าน prepared run

26 ตุลาคม 2026 · แนวทางขั้นถัดไป ไม่ใช่ implementation ที่เสร็จแล้ว

Foundation candidates ปัจจุบันมี models, validation, schemaV79/migration และ standing tests เท่านั้น Actual source ยังเป็น schema78 ไม่ได้ apply H1A และไม่มี prepare/get/dispatch/complete API ที่ใช้ได้จริง ผลทดสอบ validation และ migration ต้องอ่านแยกจากหลักฐาน trusted execution/promotion

## Authority และ transaction

Prepare ต้องสร้าง durable record ก่อน provider/tool effect โดย host orchestrator ตรึง bindings จาก actual resolved configuration/catalog/dataset/environment/budget ห้ามถือ metadata ที่ผู้เรียกส่งว่าเป็นหลักฐานว่าได้รันจริง ในช่วงที่ยังไม่มี trusted runner ห้ามเพิ่ม HTTP complete ที่ให้ caller ประทับ observed/provenance เอง การมี prepared record ไม่ authorize promotion

ใช้ transaction เดียวเพื่ออ่าน owned candidate, ตรวจ readiness/base/scope และ insert snapshot `Store.DB` มี MaxOpenConns1 จึงห้ามเรียก Service.GetCandidate, Store.OwnerPrincipalID หรือเมธอดที่ใช้ DB นอก tx หลังเปิด tx ให้ใช้ ownerPrincipalInTx แล้ว scanCandidate บน tx พร้อม id และ owner_principal_id

Candidate ต้องยัง needs_review และ checks ผ่าน Snapshot จาก persisted owned row ไม่ใช่ response ของ CreateCandidate: ปัจจุบัน insertCandidate รับค่า Candidate แบบ value และเติม owner ก่อนเขียน DB แต่ response ที่คืนอาจยังไม่มี OwnerPrincipalID การทดสอบก่อน/หลังจึงต้อง reload GetCandidate และยืนยัน owner ไม่ว่าง ไม่ลด assertion เพื่อให้ตรง in-memory object

Base ที่ระบุ ต้องอ่าน skill_versions ร่วมกับ skills ใน tx ยืนยัน version เป็นของ target skill, owner เดียวกัน และยังเป็น current_version_id แล้ว pin content_hash/package_blob_ref ห้ามใช้ GetVersion เพียงอย่างเดียว เพราะ query นั้นไม่ผูก owner Candidate/base ที่เปลี่ยนระหว่าง execution ต้องตรวจอีกครั้งตอน completion และ promotion ใน transaction ไม่ใช่ตรวจเฉพาะ preflight

## Project/session scope

ใช้ความหมายเดียวกับ agent/skill_scope.go: user ต้อง scope_ref ว่าง; workspace ต้อง scope_ref เท่ากับ actual project ID; runtime ปัจจุบันยังไม่มี durable agent-scope identity จึงไม่ขยายสิทธิ์ให้ scope ชนิดนั้น

ถ้ามี ProjectID ต้อง query projects ด้วย id, owner_principal_id และ state active ถ้ามี SessionID ต้องมี ProjectID และ query agent_sessions ด้วย session id, owner, actual project และ state ที่ runner รองรับ ห้ามใช้ project marker หรือ session string อย่างเดียวแทน owned objects ฟังก์ชัน format validation ยอม workspace/empty ScopeRef ได้ในฐานะข้อมูล แต่ Prepare ต้องปฏิเสธเมื่อไม่ผ่าน authority check นี้

Tests ต้องสร้าง owned project/session จริงผ่าน service หรือ fixture ที่ครบ constraints และมีทั้ง missing/inactive/other owner/other project/positive paths Context principal มาจาก trusted ingress `identity.WithPrincipal` ไม่ใช่ owner ที่แนบใน request body

## Durable representation และ replay

Manifest ต้องคง candidate ID/revision/hash/blob, base ID/hash/blob, owner/scope และ bindings ทั้งชุด Canonical JSON ไม่เกิน64KiB พร้อม SHA256 เก็บก่อน dispatch ข้อมูล slice ต้องไม่แชร์กับ input ที่ caller แก้ภายหลัง และ schema immutable trigger ต้องทดสอบการแก้ทั้ง8columnsจริงด้วย SQLite

Get ใช้ owned run query ตรวจ canonical JSON/hash และเทียบ denormalized candidate/owner/revision/hash ทุกครั้ง ตรวจ timestamps/state/evaluation linkage อย่างชัดเจน ผลเสียหายต้องปฏิเสธ ห้ามอ่าน manifest ที่ไม่ครบแล้วทำเหมือน valid run ประวัติ run ที่ stale ยังอ่านได้ แต่ไม่เปลี่ยน binding ให้เป็น candidate รุ่นปัจจุบัน

Prepared ยังไม่ใช่ dispatched การตัดสินว่า effect เริ่มหรือไม่ต้องใช้ durable dispatch receipt ก่อนเรียก provider/tool และ state CAS การ reopen หลัง uncertain dispatch ต้องไม่ resample เงียบ Completion payload เดิมต้อง idempotent และ payload ขัดกันต้องถูกปฏิเสธ โดยไม่สร้าง evaluation หรือ effect ซ้ำ

## Amendment — รหัสก่อนเตรียมและการอ่านสถานะเดิม

Prepare ต้องรับ runID ที่ host สร้างและบันทึกใน orchestration operation ก่อนเรียก ไม่สร้าง ID ใหม่ทุกครั้งภายใน Prepare เพราะการขาดช่วงหลัง commit แต่ก่อนรับ response จะทำให้ retry ไม่รู้ว่าต้องอ่าน run ใด Signature ที่ตั้งใจคือ PrepareBehavioralRun(ctx,runID,candidateID,bindings) การบันทึกรหัสใน orchestration ยังต้อง implement แยก ไม่ถือว่ามีแล้วเพราะ service รับพารามิเตอร์นี้

ภายใน owned transaction ถ้า runID มีอยู่แล้ว ตรวจ stored manifest/hash/denormalized fields/state ให้ครบ แล้วเทียบ candidateID และ bindings กับ input เดิม หากตรงคืน record เดิมโดยไม่เขียน timestamp/state/evaluation หรือเรียก provider หากขัดกันให้ revision conflict การคืนประวัติเดิมไม่อนุญาต dispatch ซ้ำหรือยืนยันว่า candidate ยังปัจจุบัน ต้องตรวจ current candidate/base/config และ state CAS ก่อน effect อีกครั้ง Get ยังอ่านประวัติหลัง candidate เปลี่ยนได้ ส่วน unknown/foreign principal ไม่ได้สิทธิ์อ่าน record

Record lifecycle validation: prepared/dispatched/uncertain ไม่มี EvaluationID/ResultSHA256/CompletedAt; completed ต้องมีครบพร้อม ID ที่ bounded/trim-exact, SHA lowercase64 และ completion time ไม่ก่อน start; stale/aborted อาจเก็บ triplet ที่สมบูรณ์ของผลเก่า หรือไม่มีทั้งชุด แต่ห้ามบางส่วน Timestamps จาก storage ต้อง canonical UTC ไม่มี silent parsing/defaults State/metadata validation เพียงอย่างเดียวยังไม่พิสูจน์ host execution

## สิ่งที่ยังต้องพิสูจน์

Native standing tests ของ immutable trigger, owned prepare/get, concurrent/stale/replay/reopen และ promotion CAS ยังต้องทำ Actual runner ต้องบันทึก host-observed command/provider/artifact receipts ก่อนจะอ้าง behavioral success แล้วจึงมี paired repeats/holdout transfer/cost และ fallback ตาม contract หลัก H1B2 parser/counts/verdict ยังคงใช้ครบ

ไม่มีการแก้ scope/budget/serving restriction จากเอกสารนี้ ผู้ใช้เลือก Bonsai Local ต่อ เป้าหมาย full learning, dynamic UX/control และ marketplace Compose/actual Kubernetes ยัง ACTIVE
