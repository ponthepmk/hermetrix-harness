# ผล H1E1 — schema ตรงกับขอบเขตไฟล์ที่อนุญาต

26 ตุลาคม 2026 · นำเข้าและตรวจ source จริงแล้ว · เป้าหมายเต็มยัง ACTIVE

Hermetrix ผ่าน Bonsai Local สร้าง production 2 ไฟล์และ test/support 5 ไฟล์ ผ่าน native review และ `product.WriteProjectFilesAtRoot` 7 receipts/0 rollback Codex กำหนด acceptance, DATA, ตรวจสายที่มาและควบคุมการทดสอบ โค้ดผลิตภัณฑ์และ standing tests เป็น native output พร้อม mechanical gofmt ทั้งหมด ดู [final receipt](../evidence/2026-10-26/h1e1-scope-schema/receipt.json) และ [source manifest](../evidence/2026-10-26/h1e1-scope-schema/source-applied-manifest.json)

## ปัญหาและผลที่เปลี่ยน

ระหว่าง H1A โมเดลส่งการแก้ 2 รายการที่ path เดียวกันต่อเนื่อง แม้งานอนุญาตเพียงไฟล์เดียว Validator ปฏิเสธถูกต้อง แต่ request schema เดิมยังยอมให้รูปแบบนี้เกิดได้ จึงสร้าง schema ใหม่แต่ละ request โดยกำหนด changes.maxItems ตามจำนวน actual allowed paths และ path.enum ตาม writablePathAllowed เดิม เรียงลำดับคงที่ ขอบเขตสิทธิ์และ validator ไม่เปลี่ยน หลายไฟล์ยังต้องตรวจ duplicate ที่ parser เพราะ schema นี้ไม่ได้รับประกัน path uniqueness ทุกกรณี

Schema เพิ่ม token estimate ของ frozen small-reference case จาก 1,299 เป็น 1,307 และทำให้ reference หลุด budget จึงให้ Hermetrix ย่อ description เป็น `Bounded edits for review; no apply.` ผลสุดท้าย 1,297 tokens ภายใต้งบเดิม 1,299 พร้อม reference เดิมและ source view 1,163 bytes ไม่เพิ่ม context/output cap และไม่ลดข้อมูล oracle

## หลักฐานตรวจรับ

Scope matrix 6 cases ครอบคลุม single, subset/readonly, sorted/legacy, absent/Unicode และขอบเขต 32 ไฟล์ Golden transport เดิม 3 cases เปรียบเทียบทั้ง request ยกเว้น schema 2 fields และ description ที่ตั้งใจเปลี่ยน ใช้ estimator จริง Original fixture และ curation DATA 8 cases คง hash เดิม Fresh-schema test ตรวจการไม่แชร์ mutable map และ guard test ส่ง duplicate/outside-writable output ผ่าน mock provider แล้วตรวจการปฏิเสธจริง

Red บน schema เดิมล้มเหลวตาม expected matrix/fresh assertions ส่วน source สุดท้ายผ่าน affected packages รวม 283 events ก่อน review Review เป็นอีก profile/pass ของ Bonsai ตัวเดียวกัน ไม่ใช่ความหลากหลายของโมเดล

หลัง native apply และหลังจัดเก็บ evidence รัน `go test ./... -count=1 -json -timeout 10m` กับ actual working tree ไม่มี source overlay: ผ่าน 1,337 test events, 0 failed, 20 opt-in/environment skips และ 8 packages ไม่มี tests `go vet ./...` และ `go build ./...` ผ่าน ดู [post-test summary](../evidence/2026-10-26/h1e1-scope-schema/post-test-summary.json) และ [เหตุผล skip ครบจาก raw events](../evidence/2026-10-26/h1e1-scope-schema/skip-details.json) Source 7 hashes ตรงทั้งหมด B6 ตรวจครบ 67 paths โดย 2 paths เปลี่ยนตาม H1E1 ตั้งใจ ส่วน H1B2 5 paths คงเดิม ดู [preservation](../evidence/2026-10-26/h1e1-scope-schema/source-preservation.json)

ระหว่างตรวจพบความผิดพลาดในการจัด evidence ของรอบก่อน: Codex คัดลอก source H1B2 เป็น `.go` ทำให้ Go มองเป็น packages เพิ่ม จึงเปลี่ยนเฉพาะชื่อสำเนา 5 ไฟล์เป็น `.go.txt` โดย bytes เดิมทุกไฟล์ เก็บ [amendment](../evidence/2026-10-26/h1b2-evidence-gate/evidence-storage-amendment.json) และ manifest v2 โดยไม่แก้ historical receipt การตรวจทั้งระบบข้างต้นทำหลังแก้การจัดเก็บแล้ว Evidence H1E1 ใช้ `.go.txt` ตั้งแต่ต้น ไม่มี evidence package ปะปนใน `go list ./...`

## ข้อจำกัดและงานถัดไป

Live trial ใช้ import-repair task เดิม: schema ใหม่ทำให้ได้ 1 change ที่ตรง scope แต่โค้ดยังผิดไวยากรณ์ จึงไม่ apply และไม่นับเป็นการซ่อมสำเร็จ อีก native repair ของ matrix test ผ่าน syntax/tests ได้ ทั้งสองกรณียังไม่พิสูจน์ผลเชิงสาเหตุว่าลด retry หรือต้นทุนทั่วไป

[Observed cost](../evidence/2026-10-26/h1e1-scope-schema/observed-cost.json) มี generation responses 19 ครั้ง รวม 44,400 prompt/11,480 completion tokens และ review 8,554/179 tokens รวมร่างที่ล้มเหลวและ trial ที่ผ่านเพียงรูปแบบ ผู้ควบคุมยังช่วยแบ่งงานและชี้สาเหตุ ไม่มี autonomous learning/holdout transfer claim

ผู้ใช้ยืนยันใช้ Bonsai Local ต่อ เก็บ context 16,384/output 3,072 ตามโปรไฟล์เดิม ไม่มี remote 9arm consent H1A ยังเป็นร่างที่ไม่ apply ต้องผูก immutable run ก่อน execution และเชื่อม trusted runner/provenance/promotion; H1C scope และ actual learning/holdout ยังไม่เสร็จ เป้าหมาย dynamic UX/control และ marketplace ครบระบบบน Compose/actual Kubernetes ยังต้องทำต่อ Installed app/data ไม่เปลี่ยน และไม่ได้เปิด serving ที่เคยถูกปฏิเสธ
