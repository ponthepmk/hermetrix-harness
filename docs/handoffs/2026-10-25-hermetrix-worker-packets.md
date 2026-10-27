# งานสำหรับ Hermetrix จากผู้ควบคุมโครงการ

วันที่ 25 ตุลาคม 2026 · เริ่ม dispatch ขั้นย่อย H1B แล้ว 26 ตุลาคม ดู [ผลการลงมือจริง](2026-10-26-h1b-live-worker-results.md) · ขอบเขตเต็มตาม [execution contract](../redesign/2026-10-25-hermetrix-marketplace-execution-contract.md)

ผู้ลงมือคือ Hermetrix ที่ runtime/model ผู้ใช้เลือก Codex จัด packet, ตรวจแผน/diff/test และส่ง defect กลับ ไม่เขียน implementation แทน ทุก packet ต้อง pin requirement, writable paths, baseline source และ checks ก่อน dispatch ชุดต่อไปนี้เป็น requirements ไม่ใช่ code patch

## H0 พิสูจน์เส้นทางลงมือด้วยโมเดลจริง

Objective: ให้ actual Hermetrix รับงาน coding ที่มี executable acceptance ใน workspace แยก ตั้งแต่ clarification/plan, generated change, apply, actual test, review จนแสดง evidence

Preconditions: runtime/model choice ชัดเจน; health/schema/binary identity ตรง; writer/reviewer profiles พร้อมและ context qualified; model/control actions ผ่าน execution boundary; temporary workspace และตรวจว่าไม่มี source ลูกค้า

Acceptance: มี raw model dispatch/response, exact source diff, operation/apply receipt, process exit/output, review artifact และผลที่แก้จริง ถ้า reviewer เป็น model/endpoint เดียวกันต้องเรียกว่า separate pass ห้ามอ้าง model diversity งานนี้พิสูจน์ transport/workflow ก่อนส่ง H1 ไม่ใช่ใช้แทนโครงการใหญ่

Stop: unavailable provider, malformed proposal, scope mismatch, uncertain effect หรือ manual approval pending ให้หยุดตาม state และส่งหลักฐาน ห้ามเปลี่ยน endpoint หรือ code เอง

## H1A ผูก evaluation กับรุ่นที่เริ่มรันจริง

Observed: `internal/skills/behavioral.go` รับผลโดย candidate ID แล้วอ่าน hash/revision ณ ตอนบันทึก ผลเก่าที่ส่งหลัง edit จึงถูกประทับให้รุ่นปัจจุบันได้

Read: `internal/skills/behavioral.go`, `internal/skills/service.go`, `internal/web/server.go`, `internal/store/` ส่วน schema ที่เกี่ยวข้อง และ existing tests

Objective: สร้าง immutable evaluation run ก่อน dispatch ที่ตรึง candidate revision/hash, base version/hash, provider/catalog/evaluator/environment และ dataset identity รับ completion ตาม run ID ที่ host ออกเท่านั้น; hash/revision ที่เปลี่ยนต้อง refuse/inconclusive ตามสัญญาที่ออกแบบ

Acceptance cases: unchanged candidate completion; edit between start/completion; delete/archive target; changed baseline; late duplicate completion; competing completion; process interruption; mismatched result; new run valid positive path Existing stored passing result ที่มาก่อน edit ต้องยังใช้กับรุ่นใหม่ไม่ได้

Scope proposal: ให้ Hermetrixเสนอ exact files/migration ก่อนเขียน ห้ามแก้ hash ของ evidence เก่าให้ผ่าน และห้ามลบ historical evaluations เพื่อปิดอาการ

## H1B ประเมิน regression เป็นคู่รายกรณี

Observed: aggregate `baseline_passed == candidate_passed` ให้ regressions=0 แม้ task เดิมเสียแล้วไปได้คะแนนจากอีก task; result details ไม่ถูกคำนวณใหม่

Known-good sibling: `internal/selfimprove/compare.go` เปรียบเทียบ missing requirements เป็นคู่และไม่ให้ข้อที่ดีขึ้นหักล้างข้อที่ถดถอย

Objective: รับ host-generated case/criterion evidence ผูกกับ run จาก H1A คำนวณ totals/regressions/false completion ใหม่ และทำให้งานที่ไม่มีหลักฐานหรือ malformed ไม่กลายเป็น pass

Acceptance cases: win/loss สลับกันแต่ totals เท่าเดิม; critical invariant regression; equal quality; verified gain; missing/duplicate/unknown case; impossible counters; provider failure; score ที่ model กล่าวเอง; malformed evidence; measured token cost over budget

Promotion: automated create และ improve ต้องมีเกณฑ์สอดคล้องกับระดับอำนาจ ห้ามสร้าง create ใหม่เพื่อหลบ gate ของ improve รักษา manual review และ rollback ของ policy เดิม

## H1C Scope ของ Skill ทุก producer

Observed: direct `skill_manage` create ตั้ง user scope แม้ session ระบุ project ขณะที่ background path ใช้ `boundSuggestionToSource`

Known-good sibling: `internal/learning/source_scope.go` และ `internal/agent/skill_scope.go`

Objective: host derive scope/owner จาก authoritative session/project ทั้ง direct/background path ป้องกัน target ข้าม scope และแยก proposal สำหรับยกระดับเป็น user-wide ออกจาก write ปกติ

Acceptance cases: project A create/retrieve; project B ไม่เห็น A; personal session; archived/deleted/missing project; stale Skill version; target skill cross scope; background/direct parity; automated promotion/undo; user-wide promotion เฉพาะเมื่อได้รับอำนาจและหลักฐานครบ

หลักฐานต้นเหตุ: [behavioral probe](../evidence/2026-10-25/harness2/behavioral-probe.go.txt), [scope probe](../evidence/2026-10-25/harness2/scope-probe.go.txt) Probes เดิม assert ช่องว่างเพื่อวัด state ต้องออกแบบ regression tests ที่ assert พฤติกรรมหลังแก้ แล้วเห็น old source fail ก่อนรับ fix

## H2 โครงสร้าง project และผลข้างเคียงไฟล์

Objective: ให้ goal สร้าง directory tree และ source หลายส่วนได้อย่าง bounded โดยรักษา project root identity, allowlist, preconditions, no-clobber, owned rollback/quarantine และ recovery

Acceptance: nested missing parents, symlink/reparse/path escape, concurrent owner file, partial multi-file failure, crash before/after each state, safe empty-directory rollback และการรักษาข้อมูลที่ไม่ได้สร้างเอง เริ่ม create contract ก่อน; deletion ต้องมี ownership และ explicit scope แยก ห้ามเพิ่ม unrestricted shell เพื่อสร้าง project

## H3 รัน Compose และ Kubernetes ตามขอบเขต

Objective: ใช้ docker/kubectl ผ่าน capability ที่ host ตรวจ ไม่ใช่ general shell หรือ wrapper หลบ allowlist ตรึง Compose project/config/root และ kube context/namespace/resource scope

Acceptance: ตรวจ config → build/start → migrate/seed → actual tests → stop/cleanup เฉพาะ owned resources; deadline/cancel/restart มี receipts; uncertain apply ต้อง inspect cluster/resource identity ก่อน retry; ปฏิเสธ privileged host mounts/foreign resources ตาม policy; workload เดิมไม่เปลี่ยน

## H4 ถึง H8 และ M0 ถึง M10

ให้แตกเป็น packets ที่มี acceptance เช่นเดียวกันตาม execution contract หลัง H0 พิสูจน์ได้ รักษา dependencies และ link original requirements R1–R10 ทุกข้อ ห้ามส่ง marketplace ทั้งระบบใน prompt เดียวหรือยัดเกินขอบเขตไฟล์ของ worker

M0 ต้องส่ง domain/state/API/event/UX architecture กลับมา review ก่อน application coding M1 ต้องพิสูจน์ cold-start Compose ในชื่อ project แยก M4/M5/M6 ต้องมี concurrency/idempotency/ledger tests ตั้งแต่แรก M9 ต้องมี cluster execution evidence และ M10 ยังต้องยืนยัน real integrations/operational ownership

## รูปแบบผลที่ต้องรายงานกลับ

- requirement และ packet revision ที่ลงมือจริง
- ข้อสงสัยและสมมติฐานที่มีผลต่อ design
- exact files/changes กับเหตุผลและ source hashes
- command/job/effect IDs, raw output และ exit status
- ผล acceptance รายข้อ พร้อม failure/skip/inconclusive
- reviewer findings และ repair attempts ที่ตอบ findings เดิมได้จริง
- current known limitations และงานถัดไปใน DAG
- learning candidate/negative lesson พร้อม evidence; ห้ามอ้างว่ารุ่นใหม่ดีขึ้นก่อน paired holdout evaluation
