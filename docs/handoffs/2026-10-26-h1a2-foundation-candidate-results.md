# H1A2 — foundation candidates ที่ทดสอบแล้ว แต่ยังไม่ apply

26 ตุลาคม 2026 · เป้าหมายเต็ม ACTIVE · ใช้ Bonsai Local ต่อ ตามคำตอบผู้ใช้

Hermetrix สร้างร่าง Go 7 ไฟล์สำหรับ run/manifest/bindings models, format/hash/size validation, schemaV79 พร้อม migration hook และ standing tests 3 ไฟล์ ผ่านการทดสอบใน candidate overlay เท่านั้น ยังไม่ review/apply และ actual source ยังเป็น schema78 ไม่มี H1A prepare/get/dispatch/complete/runner/promotion ที่เสร็จแล้ว ดู [candidate milestone](../evidence/2026-10-26/h1a2-foundation-candidates/candidate-milestone.json) และ [7-file native ancestry](../evidence/2026-10-26/h1a2-foundation-candidates/source-candidate-manifest.json)

## สิ่งที่พิสูจน์ได้

Frozen binding DATA 34 กรณี: ถูกต้อง5/ผิด29 ก่อนแก้ native hash predicate ทั้ง5positiveถูกปฏิเสธเพราะใช้ OR ของเงื่อนไขผิด หลังให้ Hermetrix แก้เฉพาะ predicate ผ่าน DATA เดิมครบ ไม่แก้ oracle

Manifest DATA 25 กรณี: ถูกต้อง5/ผิด20 ครอบคลุม candidate/base/owner/scope bounds และ nested bindings ผลที่ถูกต้องต้องได้ canonical json.Marshal bytes พร้อม hash จาก independent SHA256 ของ test อีกกรณี bindings แต่ละส่วนยัง valid แต่ JSON หลัง escape เกิน64KiB ต้องถูกปฏิเสธและไม่คืน encoded/hash

Upgrade test ใช้ temporary SQLite และ candidate ที่สร้างผ่าน actual service ลด fixture DB เป็น78โดยลบเฉพาะตารางใหม่แล้ว reopen: original migration ยังได้78และ testล้มเหลวตาม version assertion ส่วน candidate migration ได้79 มีตารางใหม่ และ candidate ID/hash/blob/revision/owner ที่อ่านจาก persisted rowก่อนเริ่มคงเดิม หลังพบว่า CreateCandidate response มี owner ว่าง จึงแก้ setup ให้ reload GetCandidate และยืนยัน owner ไม่ว่างก่อน downgrade คง assertion หลัง migration ทุกตัว

ทดสอบทั้ง internal/skills และ internal/store บน final7candidate overlay ผ่าน69 test events/0fail/2skips `go vet` ทั้ง2packages และ `go build ./...` บน overlay ผ่าน [ผลแพ็กเกจ](../evidence/2026-10-26/h1a2-foundation-candidates/affected-test-summary.json), [เหตุผล skip](../evidence/2026-10-26/h1a2-foundation-candidates/skip-details.json) ไม่มี aged installed DB fixture หรือ external Skill fixture จึงไม่ได้ทดสอบสองส่วนนั้น ไม่มีการเปิดหรือเปลี่ยนข้อมูล installed app

## ข้อผิดพลาดและต้นทุน

ร่างที่ตัดออกมีทั้ง syntax/import errors, duplicate/no-op changes, path ของ DATA ผิด, decode object เป็น array, สมมติ API ที่ไม่มี และ testใหญ่ที่ไม่ทำ migration ตามที่กำหนด แยกเป็นงานเล็กและให้ complete source/working API ก่อนขอคำตอบใหม่ ไม่ salvage คำตอบที่ protocol ปฏิเสธ ร่างที่ accepted แต่ compileไม่ผ่านเก็บไว้เป็นประวัติ และใช้ต่อได้เฉพาะผ่าน native repair chain ที่ตรวจ preimageครบ

[Observed attempts](../evidence/2026-10-26/h1a2-foundation-candidates/observed-attempts.json) มี response19ครั้ง รวม reported38,901prompt/13,917completion tokens; final ancestryใช้13responsesสำหรับ7files ตัวเลขไม่รวม H1E1 trial ที่บันทึกต่างหาก Codex ยังต้องชี้ต้นเหตุ/แบ่งงานหลายครั้ง จึงไม่มี autonomous repair หรือ model-quality improvement claim และยังต้องจำกัดรอบซ่อมก่อนขยายงานใหญ่

## สิ่งที่ยังขาด

ต้องมี native tests สำหรับ immutable trigger ทุก8columns, owned prepare/get กับ project/sessionจริง, candidate/base/configที่เปลี่ยน, stale/concurrent/replay/reopen และ promotionที่ตรวจrunใน transaction Actual host runner ต้องผูก observed provider/command/artifact receipts ก่อนรับผล ห้ามใช้ caller metadata หรือการมี RunID แทน execution proof แล้วจึงทดสอบ paired repeats/holdout transfer/cost/fallback

อ่าน [prepared-run design](2026-10-26-h1a-prepared-run-design.md) และ [acceptance contract](2026-10-26-h1a-evaluation-run-contract.md) ก่อนสร้าง service methods ร่างทั้งหมดเก็บ `.go.txt` ไม่มี evidence Go package ปะปนใน build ตรวจ ancestry และ final gofmt hashes ครบ7paths พร้อม [source59เดิม](../evidence/2026-10-26/h1a2-foundation-candidates/previous59-actual-source-preservation.json) ที่คงตาม known updates

Actual source ล่าสุดที่นำเข้าแล้วคือ H1E1: whole repositoryผ่าน1,337events/0fail/20skips,vet/buildผ่าน แยกจาก69eventsของ H1A2 candidates นี้ Native review/apply H1A2 ยังไม่เริ่ม เป้าหมาย dynamic UX/control และ marketplace ทั้งระบบบน Compose/actual Kubernetes ยังไม่เสร็จ
