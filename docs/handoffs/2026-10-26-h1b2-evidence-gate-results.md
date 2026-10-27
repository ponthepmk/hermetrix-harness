# ผล H1B2 — paired evidence ก่อนรับบทเรียนไปใช้

วันที่ 26 ตุลาคม 2026 · Native review/apply และ post-qualification ผ่าน · เป้าหมายเต็มยัง ACTIVE

Hermetrix สร้างการแก้ production 1 ไฟล์และ standing test/setup 4 ไฟล์ Codex กำหนด acceptance, ตรึง DATA, ตรวจ/สั่ง native generation, review, apply และทดสอบ โดยไม่เขียน implementation แทน หลักฐานอยู่ใน [final receipt](../evidence/2026-10-26/h1b2-evidence-gate/receipt.json) ส่วน apply-milestone.json เป็น immutable snapshot ก่อน post-check

## พฤติกรรมที่เปลี่ยน

RecordBehavioralEval ยังรับคะแนนรวมเป็น telemetry แต่ผลที่เดิมจะผ่านกลายเป็น inconclusive หากไม่มี paired Result ผล regression/false-success ที่สังเกตได้ยังเป็น failed ตามเดิม ผู้เรียกเดิมที่ส่งเพียง aggregate เพื่อ promote ต้องเพิ่มผลรายกรณีที่ครบ

ก่อน promote ระบบตรวจ schema ของ Result ที่เก็บไว้ใหม่ คำนวณและเทียบ Tasks/BaselinePassed/CandidatePassed/Regressions แล้วตรวจ verdict ซ้ำ การมี stored state=passed และ candidate hash/revision ตรงกันเพียงอย่างเดียวจึงไม่พอ ผลเก่าที่ไม่มีคู่ผลหรือ counters ขัดกับรายละเอียดจะไม่ authorize

API/schema/สิทธิ์/งบ/installed runtime ไม่เปลี่ยน การผูก candidate hash ยังเกิดตอนรับผล H1A immutable run ก่อนเริ่มและ H1C scope ยังต้องทำต่อ comment เดิมที่อ้าง start-time refusal ไม่ใช่หลักฐานว่ามีความสามารถนั้น

## หลักฐานทดสอบ

Host DATA ตรึง 5 aggregate cases, 5 historical stored-pass cases และ positive paired evidence รวม 11 leaf cases Native tests ใช้ actual temporary SQLite, เปลี่ยนแถว evaluation ที่ระบุ ID จริงและตรวจ RowsAffected=1 ก่อนเรียก gate/PromoteCandidate จริง กรณี positive ส่ง counters0 แล้วระบบคำนวณ5/5จาก Result และ promote ได้

ก่อนแก้ source: aggregate4leavesและ historical5leaves ล้มเหลวตาม assertion ของ gate; zero-task aggregate inconclusive อยู่ก่อนแล้ว และ positive paired ผ่าน หลัง candidate: ผ่าน13test eventsรวม parent tests บวก frozen-session integration smoke ผ่าน Assertions ธุรกิจเดิมไม่ถูกลด

รันทั้ง repository ก่อน apply ได้1,327passing events และ1failure ที่ HTTP positive fixture เก่าซึ่งยังส่ง aggregate เพียงอย่างเดียว จากนั้นให้ Hermetrix เพิ่ม paired JSON แก่ setup ของ test เดิม ทดสอบเฉพาะตัวนั้นใหม่ผ่าน คง raw failed run ตามจริง นี่เป็น composite pre-qualification ไม่ใช่ single full-suite green run

Native review อนุมัติ ไม่มี findings เห็น production/new tests/DATA เต็มและ complete named functions ของ helper/HTTP setup ตาม partial hashes Host ตรวจ native ancestry ทุกขั้น, original workspace preimages และผลหลัง gofmt ก่อน native product.WriteProjectFilesAtRoot apply5receipts/0rollback หลังเขียน hashesตรงทั้ง5 B6source67คงเดิม และ source59เดิมรักษาไว้ตาม known updates

หลัง apply รัน go test ./... -count=1 -json -timeout 10m กับ actual source ไม่มี source overlay: ผ่าน 1,328 test events,0failed,20opt-in/environment-dependent skips และ8packagesไม่มีtests go vet ./... และ go build ./... ผ่าน รายชื่อ/เหตุผล skip และ package timings อยู่ใน [post-test summary](../evidence/2026-10-26/h1b2-evidence-gate/post-test-summary.json) Native/source checks และ jobs ปัจจุบันจบทั้งหมดแล้ว

## ข้อจำกัดและต้นทุนที่พบจริง

การสร้าง test ต้องมีการกำกับแก้หลายครั้ง ทั้ง type/SQL/preimage/no-op และวงเล็บผิด ร่างที่ compile ไม่ผ่านหรือ protocol ปฏิเสธไม่นับเป็นความสำเร็จและไม่ salvage คำตอบที่ถูกปฏิเสธ DATA matrix และการให้ complete syntactic view ช่วยให้รันได้ในครั้งหลัง แต่เปลี่ยนหลายปัจจัยพร้อมกันจึงยังไม่พิสูจน์ causal improvement แบบ controlled trial

[reflection](../evidence/2026-10-26/h1b2-evidence-gate/reflection.json) เป็นบันทึกจากผู้ควบคุม ยังไม่ใช่ lesson ที่ระบบสร้าง/evaluate/promote เอง ไม่อ้าง autonomous repair หรือ holdout transfer จากรอบนี้ [ต้นทุนที่สังเกต](../evidence/2026-10-26/h1b2-evidence-gate/observed-cost.json): generation responses19ครั้ง, review1ครั้ง รวม reported usage69,454prompt/22,004completion tokens เฉพาะ native generation test wall time รวมประมาณ509.15วินาที ไม่รวมเวลาควบคุม/ทดสอบทั้งหมด จึงยังต้องลดรอบซ่อมและจำกัดต้นทุนก่อนขยายงานใหญ่ ตัวเลขนี้ไม่ใช่ SLA หรือ proof ว่างานเร็วขึ้น

เป้าหมาย R1–R10/H0–H8/M0–M10 ยัง ACTIVE รวม H1A/H1C, actual learning/holdout, dynamic UX/control และ marketplace Compose/actual Kubernetes

## Evidence storage amendment และการตรวจภายหลัง

หลังจัดเก็บผลรอบนี้พบว่าสำเนา Go 5 ไฟล์ใต้ evidence ใช้ extension `.go` ทำให้ root Go wildcard พบเป็น packages เพิ่มและ build ไม่ผ่าน เป็นข้อผิดพลาดของการจัดเก็บโดย Codex แก้เฉพาะชื่อสำเนาเป็น `.go.txt` และตรวจ hashes คงเดิมทั้งหมด ไม่เปลี่ยน product source หรือเขียนทับ historical receipt ดู [amendment](../evidence/2026-10-26/h1b2-evidence-gate/evidence-storage-amendment.json) และ [manifest v2](../evidence/2026-10-26/h1b2-evidence-gate/source-applied-manifest.v2.json) Original manifest 225 entries ตรวจกลับผ่าน amendment แล้ว

หลังแก้ storage และนำ H1E1 เข้า รัน actual full repository ใหม่ผ่าน 1,337 events/0fail/20skips พร้อม vet/build ผ่าน Source H1B2 ทั้ง 5 paths คงเดิม ดู [H1E1 final receipt](../evidence/2026-10-26/h1e1-scope-schema/receipt.json) และ [source preservation](../evidence/2026-10-26/h1e1-scope-schema/source-preservation.json) ผลภายหลังนี้เป็นหลักฐาน global build หลังจัดเก็บ evidence ส่วนผล 1,328 ข้างบนเป็นประวัติของช่วงก่อนจัดเก็บ
