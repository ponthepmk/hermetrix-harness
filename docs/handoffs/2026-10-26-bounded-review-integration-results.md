# ผลเชื่อม bounded reviewer และการตรวจ source เต็ม

วันที่ 26 ตุลาคม 2026 · Candidate B สะสม 34 Go files ยังไม่ apply และยังไม่ผ่าน acceptance งาน Go ใหญ่ครบวงจร

Hermetrix เขียน implementation และ standing Go tests ทั้งหมด Codex กำหนดขอบเขต ตรวจ source และ oracle เรียก native worker และเก็บหลักฐาน การจัดรูปแบบใช้ gofmt โดยไม่แก้ Go ด้วยมือ ชุดนี้ต่อจาก [durable input integration](2026-10-26-durable-proposal-input-results.md) ตาม [สัญญา B](2026-10-26-coordinator-bounded-context-contract.md)

## พฤติกรรมใน candidate

ReviewTask รับ PartialFiles แบบ optional ควบคู่กับ Files ตรวจจำนวน path/regions, raw และ encoded wire budget, UTF-8 source, path, hashes, before range, region identity, duplicate และ overlap ก่อน callback หรือ provider dispatch เมื่อเป็น partial review คำสั่งระบุชัดว่า reviewer เห็นเฉพาะ before/after regions ไม่ได้เห็น hidden source และตั้ง SourceState จากข้อมูลที่ host ตรวจแล้วแทนข้อความที่ caller อ้าง

ตัวคุมงาน Review ยังคงอ่าน actual after-source ภายใต้ verification snapshot fence และตรวจ hash เดิม จากนั้นเรียก proposalReviewFiles ด้วย frozen input ที่ durable loader อ่านมา ตรวจ active reviewer credential ใน actual after-source และ original.Files ทุกไฟล์ รวม hidden/readonly source ยืนยันทุก change ตรงกับ actual map แล้วจึงเรียก BuildReviewFiles เพื่อสร้าง bounded projection

การส่ง PartialFiles อยู่ใน request จริงของ coordinator แล้ว ไม่มีการเพิ่มอำนาจ model หรือแก้ different-profile rule, saved-decision recovery, owner/root/source guards, callback, effects, post-provider snapshot fence หรือ rollback policy การตรวจ shape ของ endpoint ไม่อ้างว่าพิสูจน์ hidden bytes ได้เอง การพิสูจน์นั้นอยู่ใน full-source loader/validator และ actual-source guards ของ host

## ปัญหาที่ reproduce และแก้แล้วใน candidate

เดิม reviewer ตรวจ credential เฉพาะ JSON body หลัง marshal หาก key มี angle brackets JSON เปลี่ยนรูปเป็น escape ทำให้ literal-key containment มองไม่เห็น Native-authored test ยืนยัน active dummy key ก่อน ยืนยัน encoded body ไม่มี literal key แล้วเรียก endpoint จริงผ่าน saved profile/deterministic adapter ผลก่อนแก้ได้ nil error จึงล้มเหลวตามที่ควร

Hermetrix เพิ่ม reviewCredentialValues เพื่อเก็บ raw metadata, lists, full map keys/content และ partial map/region strings แล้วตรวจ credential ก่อน marshal โดยคง encoded-body guard เดิม หลังแก้ test ผ่านและไม่เรียก authorization callback หรือ adapter ส่วน test invalid UTF-8 before/after ผ่านเช่นกัน

นี่เป็นการรัน provider boundary จริงกับ local fixture ไม่ใช่การส่งข้อมูลลับจริงออกเครือข่าย Dummy keys อยู่เฉพาะ tests ที่ควบคุมไว้

## หลักฐานการทดสอบ

| กรณี | ผลที่ยืนยัน |
|---|---|
| Endpoint frozen matrix | 38 กรณี: 9 positives dispatch อย่างละหนึ่งครั้ง; 29 negatives คืน zero result และไม่เรียก callback/adapter |
| Encoded credential | ก่อนแก้ test ล้มเหลวเพราะ endpoint ยอมรับ; หลัง raw guard ผ่านพร้อม zero dispatch |
| Invalid UTF-8 | before และ after เสียถูกปฏิเสธก่อน dispatch |
| Genuine large-source projection | ใช้ worker.Run จริงกับ deterministic proposalAdapter สร้าง Result จาก Go source เกิน 128 KiB และ NewBoundedFileView จริง; before/after text และ full hashes ตรง; projection ต่ำกว่า 4 KiB และไม่มี padding ที่ซ่อน |
| Hidden before / readonly | แยก fixture inputs สองกรณีจริง; active reviewer credential ถูกปฏิเสธพร้อม nil maps |
| Hidden after / owner drift / missing actual source | สามกรณีถูกปฏิเสธ; credential และ source-mismatch error แยกกัน; ไม่ dispatch เพิ่ม |
| Worker suite หลัง security fix | ผ่านทั้ง package; final integrated suite มีผลใน test-summary.json |
| Vet / build | ผ่าน worker/taskcoord vet และ build ทุก package โดยไม่เปิด binary |
| Applied source preservation | source ที่ apply ก่อนหน้า 59/59 hash ตรง; B ยังเป็น overlay |

ผล full worker/taskcoord suite อยู่ใน [test-summary.json](../evidence/2026-10-26/h1d-live/b4-review/test-summary.json) และ raw JSONL ที่อ้างในนั้น การ skip opt-in live-model test ต้องรายงานตามผลจริง ไม่ถือว่าเป็น acceptance โมเดลจริง

Large-source projection fixture ใช้ SQLite และ worker boundary จริง แต่ยังไม่ได้สร้าง owned project ผ่าน native coordinator แล้ว apply/build/test/review ด้วยโมเดลจริงครบวงจร จึงไม่ใช้แทน B5 และไม่ใช่หลักฐานว่า self-learning ทำให้งานใหม่ดีขึ้น

## ข้อผิดพลาดของร่างที่ถูกกันออก

Protocol-accepted proposal หลายร่างยัง compile ไม่ผ่าน เพราะ model เดา import path, API return arity, cleanup methods หรือ field names และบางร่างแก้ syntax ไม่ครบ ตัวตรวจ source/compiler กันออกก่อน qualification ไม่มีการแก้ Go ด้วยมือเพื่อให้ผ่าน

ต้องตรวจ oracle เพิ่มจาก compiler: ร่าง readonly test เคยใส่ key ใน before ทุกครั้ง ร่าง missing-after เคย replace ข้อความที่ไม่มีอยู่แทนการลบ map entry และร่าง error check เคยคาดให้ error เปิดเผย key จริง ทั้งหมดแก้ผ่าน native proposals โดยคงข้อกำหนดเดิมให้ครบ ไม่ลด assertion เพื่อรับพฤติกรรมที่ผิด

ร่าง negative matrix หนึ่งครั้งหมด output budget ก่อนจบ ถูกทิ้งทั้งชุดและแบ่งเป็น packets เล็ก ไม่เพิ่ม saved budget และไม่ salvage partial completion การซ่อมที่ได้ผลใช้ actual API declarations และ FileViews เฉพาะบรรทัด/บล็อกที่เสีย พร้อมตรวจ hidden suffix ว่ายังเดิม

## สิ่งที่ต้องทำต่อก่อน apply B

1. B3 automatic host view curation ใน Propose/Goal ให้เลือกช่วงจาก source จริงและ frozen step โดยพอดี actual preset/wire budget คง criteria/constraints/checks ทั้งหมด Optional brain refs ต้อง fit จาก bounded wire
2. ใช้ request estimator/constructor ร่วมกับเส้นทาง provider จริงในการวัด input อย่าเดางบจาก full canonical JSON หรือเพิ่ม context/output ceilings เพื่อให้ request ผ่าน
3. เพิ่ม input/body/checksum/project/registry tamper acceptance ตามสัญญาเดิม และ review cohesive B ด้วย native path ก่อน apply
4. B5 owned Go project เกิน 128 KiB: local-model proposal, native apply, actual failing/passing command checks, separate-profile review, conflict/recovery และ receipts ครบ
5. H1A evaluation run binding, aggregate-only gap, H1C scope, self-learning holdout, dynamic UX/UI, computer control และ marketplace Compose/actual Kubernetes ยังอยู่ในเป้าหมายเดิม

## หลักฐานและการทำต่อ

- [Candidate manifest 34 files](../evidence/2026-10-26/h1d-live/b4-review/candidate-manifest.json) ระบุ hashes, preimages และ native authorship
- [Native lineage](../evidence/2026-10-26/h1d-live/b4-review/native-lineage.json) แยก protocol status ออกจาก final candidate; ไม่ใช้จำนวน proposals เป็นผลคุณภาพ
- [Durable replay overlay](../evidence/2026-10-26/h1d-live/b4-review/evidence-overlay.json) ชี้ copies ที่ hash ตรวจแล้ว
- [Source preservation](../evidence/2026-10-26/h1d-live/b4-review/applied-source-preservation.json)
- [Receipt](../evidence/2026-10-26/h1d-live/b4-review/receipt.json)

Overlay ที่ใช้จริงล่าสุดคือ `.tmp-ui/goal-20261023/live-20261026-001100/H1D-B4-qualified-overlay.json` มี implementation 12 files และ test/support 22 files โค้ดและข้อมูลระบบติดตั้งเดิมไม่เปลี่ยน ไม่มี server ใหม่ถูกเปิด การเปิด serving ยังอยู่ภายใต้ automatic approval rejection เดิมและไม่ลองช่องทางอื่นเพื่อข้ามข้อจำกัดนั้น
