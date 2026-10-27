# H1A3 — prepared run และประวัติที่ตรึงรุ่นไว้

26 ตุลาคม 2026 · Candidate overlay ผ่านการตรวจแพ็กเกจที่เกี่ยวข้อง · ยังไม่ review/apply · เป้าหมายเต็ม ACTIVE

ชุดร่างรวม H1A2 foundation มี23 Go files: production/types/migration11 และ test/support12 ทุกไฟล์มี native ancestry และ gofmt hashes ตรงกับ output ของ Hermetrix ผ่าน Bonsai Local Codex กำหนด acceptance, DATA และควบคุมการสร้าง/ตรวจ ไม่เขียน implementation หรือ standing tests แทน ดู [candidate milestone](../evidence/2026-10-26/h1a3-prepared-run-candidates/candidate-milestone.json) และ [source manifest](../evidence/2026-10-26/h1a3-prepared-run-candidates/source-candidate-manifest.json)

Actual source ยังเป็น schema78 และไม่มี H1A Go files ชุดนี้ใน source จริง ผลล่าสุดของ actual repository ที่นำเข้าแล้วคือ H1E1 ทั้ง repository1,337passing events ส่วน H1A3 ต่อไปนี้ทดสอบบน candidate overlay เท่านั้น

## พฤติกรรมที่ทดสอบได้แล้ว

PrepareBehavioralRun รับ runID ที่ host ระบุ ตั้ง owned transaction อ่าน candidate ที่พร้อม, pin candidate/base/hash/blob/scope และ bindings ก่อนเขียน prepared record ไม่เรียก provider/tool หรือสร้าง evaluation Host orchestrator ยังไม่ได้บันทึก runID ก่อนเรียกในเส้นทางจริง จึงยังไม่ใช่ durable execution ทั้งวงจร

การเรียก Prepare ด้วยรหัสเดิมและ input เดิมคืน record เดิมโดยไม่เขียนเวลา/สถานะใหม่ ถ้า candidateID หรือ bindings ขัดกันต้อง revision conflict การคืนประวัติเดิมไม่อนุญาต dispatch ซ้ำและไม่ยืนยันว่า candidate ยังปัจจุบัน Get ตรวจ owner และ canonical manifest/hash/denormalized fields ก่อนคืนข้อมูล ไม่มีการ refresh binding ให้เป็นรุ่นล่าสุด

Tests ใช้ actual temporary SQLite และ actual services ยืนยัน deep copy ของ caller CaseIDs, recordเดียวหลัง replay, zero recordเพิ่มจากคำขอที่ปฏิเสธ, missing/unknown principal denial, close/reopen DB/services แล้วได้ recordเดิม หลัง UpdateCandidate เปลี่ยน revision/hash ผลเก่ายังคงเดิม ส่วน runใหม่ pinรุ่นใหม่ นี่เป็น DB/services reopen ไม่ใช่ process-kill recovery

Base positive ใช้ Skill ที่สร้าง/promote ผ่าน serviceจริงและ improvement candidateจริง เทียบ version/content hash/package blob จาก stored version อีก testสร้างและ promote Skillอื่นก่อนแก้ candidate.base_version_id ไปยัง version ที่มีอยู่จริง การแก้แถวสำเร็จ1และ foreign keyยังvalid แต่ Prepare ปฏิเสธ target relationship และไม่สร้าง run

Project/session positive สร้าง projectผ่าน fixtureที่ใช้ production owner trigger และสร้าง session ผ่าน agent.CreateSession จริง ตรวจ returned ownership/project/state แล้ว Prepare workspace candidate ได้ อีก testใช้2ownedprojectsและ sessionจริงของprojectB: ผูก sessionB กับprojectA หรือผูก workspace candidateA กับprojectB ถูกปฏิเสธทั้งคู่และจำนวน runยัง0 ไม่ใช่ marker-only fixture

## Integrity และข้อผิดพลาดที่ตรวจพบ

SQLite test ตรวจ immutable trigger ทั้ง8columnsด้วย UPDATE SET column=column แล้วต้องได้ immutable error, stateที่ไม่ถูกต้องได้ CHECK error และ state CAS ครั้งแรกกระทบ1แถว/ซ้ำ0แถว

Frozen record DATA มี14decoder cases (valid2/invalid12) และ31lifecycle cases (valid9/invalid22) ครอบคลุม malformed/trailing/noncanonical/unknown/duplicate JSON, hash mismatch, zero returnบนerror, ID/time/state/result linkage ก่อนแก้ source decoderคืนpartialmanifestบน8errorcasesและ lifecycleยอมevaluation IDที่มีช่องว่าง/ยาวเกิน หลัง native repair ผ่าน45casesเดิมครบ

DATAรุ่นแรกที่ Codexสร้างมีลำดับ EvaluatorRevision/DatasetSplit สลับจาก native Go struct ทำให้ positivecanonical fail เก็บรุ่นเดิมและ raw failed runไว้ จากนั้นแก้เฉพาะลำดับJSON/associated byte hashes ไม่เปลี่ยนชื่อ/จำนวน/verdictทั้ง45cases Test draftเองใส่ decodevalid9ผิดจากoracle2 จึงให้ nativeซ่อม countก่อน semantic red-v2 ชุด red-v2จึงแสดงเฉพาะข้อผิดพลาดใน implementation จริง ดู raw/H1A3-record-DATA-v2-summary.json และ red-v2 events ภายใน evidence

ร่างที่ถูกคัดออกยังมี DTO/API/import/constant/table names ผิด, sql.Result/RowsAffected usageผิด, scope string projectแทนworkspace, การอ่าน JSON wrapperผิด และข้อความ editไม่ตรง preimage รวม outputถึงcap ไม่ salvage คำตอบที่ถูกปฏิเสธ เปลี่ยนไปใช้ complete working APIs/DTO และงานเล็กกว่าเดิมเมื่อวิธีเดิมไม่ช่วย แต่ยังเป็น operator-guided development ไม่ใช่ autonomous repair

## Qualification และต้นทุน

รันทั้ง internal/skills, internal/store และ internal/agent บน final23 overlay:301passing test events,0fail,4skips การตรวจโค้ดทั้ง3packagesและ buildทั้งrepositoryบนoverlayผ่าน ดู [package results และ skip outputs](../evidence/2026-10-26/h1a3-prepared-run-candidates/affected-test-summary.json) ตัวเลขรวม parent/subtest events ไม่ใช่301independent business journeys ยังไม่ได้รัน full-root tests สำหรับ H1A3 cohort หรือ sourceจริงหลัง apply

4skips คือ aged installed DB fixture, actual capped Bonsai admission/read opt-in, external Skill fixture และ real embedding endpoint ไม่ได้ใช้ผล package testsนี้อ้าง live behavioral quality แม้การสร้างโค้ดใช้ Bonsaiจริง

[Observed attempts](../evidence/2026-10-26/h1a3-prepared-run-candidates/observed-attempts.json) บันทึก45response recordsของ H1A3 รวม reported109,313prompt/41,186completion tokens รวมร่างที่เสีย/คัดออก ไม่รวม H1A2 costsเดิม Source23มี51ancestry responsesรวม H1A2เดิม13และH1A3ที่ใช้ต่อ38 ไม่มี model-diversity หรือ causal speed/cost improvement claim

ตรวจ actual applied source138hash entries (previous59+B6จำนวน67+H1B2จำนวน5+H1E1จำนวน7 พร้อม known updates) ไม่เปลี่ยน เก็บ815initial evidence filesด้วย SHA256 ทุกสำเนา Go เป็น .go.txt และ go listไม่พบ evidence packages Source manifest SHA c1a7e87621377b2a4257b79ed00853e3bf0518cd6edab60c36e279b9a4bc2bcc Installed app/dataไม่เปลี่ยน ไม่เปิด serving และคงBonsai16384/3072 reasoningdisabledตามเดิม

## งานที่ยังต้องทำ

ต้องเพิ่ม stale-base/current-version, concurrent replay, scanner denormalized corruption และ inactive/missing project/session boundary tests ก่อน native review/apply และ full-root qualification จากนั้นเชื่อม durable host operation/runID, dispatch/complete/uncertain/stale/replay, provider/command/artifact provenance และ promotion gateใน transaction

RecordBehavioralEval/promotion เส้นทางจริงยังไม่ผูก trusted run ชุดนี้จึงยังไม่ปิด H1A ทั้งหมด Caller fingerprints เป็น metadata ไม่ใช่หลักฐาน execution และการมี prepared recordไม่ authorize lesson promotion ต้องมี actual paired runner, repeats/holdout transfer/cost/fallbackต่อไป H1C, dynamic UX/control และ marketplace Go/React/Next/PostgreSQL/Redis/RabbitMQ/workers บน Compose และ actual Kubernetes ยังคงเป็นเป้าหมายเต็ม
