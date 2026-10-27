# ผลการจัด context อัตโนมัติสำหรับงานไฟล์ใหญ่

วันที่ 26 ตุลาคม 2026 · สถานะ technically verified ใน candidate 67 ไฟล์ ยังไม่ review และ apply เข้าตัว harness หลัก

Propose และ Goal ใช้ตัวจัด context ตามงบ request จริงแล้ว โดยเก็บ source ฉบับเต็มไว้กับ host และส่งช่วงที่ตรวจสอบได้ให้โมเดล เมื่อชิ้นส่วนที่จำเป็นไม่พอดี ระบบปฏิเสธแทนการตัดข้อกำหนดหรือขยายสิทธิ์ Candidate นี้รวม durable input ของ B2, bounded reviewer ของ B4 และ curation ของ B3 หลักฐาน native Go กับโมเดลจริงที่ทำต่อภายหลังอยู่ใน [ผล native Go](2026-10-26-native-large-go-goal-results.md)

## ชุด source ที่ตรวจรับ

- Overlay ต้นทาง `H1D-B3-context-guarded-overlay.json` อยู่ใน `.tmp-ui/goal-20261023/live-20261026-001100`
- มี 67 Go paths ได้แก่ production 33 และ test/support 34 ทั้งหมดมีที่มาจากผล native worker ที่รับไว้ และ mechanical gofmt
- สำเนาถาวรและ replay overlay อยู่ใน [b3-curation](../evidence/2026-10-26/h1d-live/b3-curation/receipt.json) โดย `candidate-manifest.json` ผูก hash, workspace preimage และ native authorship
- Source ที่เคย apply ก่อนหน้า 59 ไฟล์ยังตรง hash ทั้งหมด ไม่มีการนำ candidate B เข้าตัวหลักในรอบนี้
- Receipt ของ B3 บันทึกก่อนรัน B5 จึงยังระบุ live Go ว่ารออยู่ ให้ใช้ receipt ของ B5 สำหรับสถานะ native acceptance ล่าสุด

## การทำงานที่เพิ่ม

`proposalRequest` เป็น constructor เดียวสำหรับ request ที่ส่งจริงและการประมาณขนาด `EstimateRequestTokens` ใช้ estimator เดิมของ provider ชุด golden ที่จับจาก request ก่อน refactor ตรวจ whole, partial และ absent cases การประมาณเป็น preflight เท่านั้น gate ของ worker/provider ยังเป็นตัวตัดสินสุดท้าย

Curator ตรวจจำนวนและขนาด source ส่วนตัวก่อน projection ใช้ทั้ง serialized wire ceiling 128 KiB และ ContextMax ของ preset เมื่อไม่พอดีจะถอด reference ที่เป็น optional ทดลองเลือก source แล้วคืน references ทีละรายการตามงบ จึงไม่ใช้ขนาด canonical JSON ฉบับเต็มเป็นเหตุทิ้ง references ทั้งหมด กรณีทดสอบงบ 1,299 tokens ตรึงจาก request ที่คาดหมายอย่างอิสระ ไม่ใช่การเปลี่ยน saved profile

Go ที่ parse ได้เลือก package/import และ declaration ที่สัมพันธ์กับ step ส่วนข้อความทั่วไปเลือกหน้าต่างบรรทัดจาก anchor โดยนับ offset เป็น UTF8 bytes รองรับ quoted literals, identifier boundaries และภาษาไทย ไม่มีการเลือก prefix ของไฟล์โดยอัตโนมัติเมื่อหา anchor ไม่ได้ ช่วงที่ caller จำกัดไว้ก่อนแล้วไม่ถูกขยายเพื่อให้พอดีงบ

ทุก successful result ผ่าน FreezeTask อีกครั้ง เก็บ source/requirements/constraints/checks/writable paths/budgets เดิมไว้ การเปลี่ยนแปลงที่อนุญาตมีเพียง FileViews และ references ที่เป็น optional ทั้ง map และ slice ของผลลัพธ์แยกจาก input

## ผลการตรวจ

| หลักฐาน | ผล |
|---|---|
| Request golden | ตรงกับ request เดิมทั้งสามรูปแบบ |
| Anchor matrix | 6 term cases และ 3 scoring cases ผ่าน |
| Go scope matrix | 7 cases รวม Unicode prefix, unique/ambiguous declaration และ package only ผ่าน |
| Curation contract | 8 cases ตรวจงบ, optional refs, mixed readonly/absent/empty, input immutability และ frozen result ผ่าน |
| Line boundaries | 10 cases รวม UTF8/CRLF, identifier boundary, literal priority, ties, radius และ 33 regions ผ่าน |
| Raw view builder | 12 cases รวม invalid/nested/empty/split-rune ranges และ aggregate budget ผ่าน |
| Narrowing caller | ใช้ 12 cases เดียวกัน ตรวจ error และไม่ panic ผ่าน |
| Owned Goal integration | source 864,069 bytes; Node ล้มเหลวก่อน, native apply และ Node ผ่านหลัง; readonly test/prefix คงเดิม; reviewer ได้ actual output ผ่าน |

Owned integration ของ B3 จำลองเฉพาะ model transport ส่วน project ownership, SQLite, file writes, command process, review profile และ receipts เป็นของจริง มันไม่ใช่หลักฐานคุณภาพของ local model; B5 เป็นการทดลองที่ใช้โมเดลจริงแยกต่างหาก

ชุดรวม `go test` ของ worker, providers และ taskcoord ผ่าน มี 274 passing test events และ 0 failures ระยะเวลาของ package คือ 6.890, 8.969 และ 172.105 วินาทีตามลำดับ หนึ่ง existing opt-in test `TestBonsaiDecisionAgainstLocalServer` ถูกข้าม ผลนี้ไม่ถูกใช้เป็น live model acceptance `go vet` ทั้งสาม package และ `go build ./...` ผ่าน

## ข้อผิดพลาดที่พบและแก้

Raw regions ถูก merge ก่อนตรวจทีละรายการ ทำให้ containing interval กลบ nested reversed/empty interval ได้ Frozen row 4 ทำให้ builder เดิมล้มเหลว จึงเพิ่ม shared raw validator ก่อน merge และก่อน narrowing slice ทดสอบเดิมผ่านโดยไม่แก้ expected data

Native drafts เคยมี append ที่ไม่ระบุ slice type, API/return arity ที่ไม่มีจริง, struct ถูกใช้เหมือน array, test ที่ตรวจตัวแปรผิด และ malformed Go if initializer ทั้งหมดถูกกันออกจนตรวจผ่าน การซ่อม initializer สองครั้งไม่สำเร็จจึงกลับไปใช้ source ที่ยัง valid และ guard จาก sibling ที่ทำงานได้ แทนต่อยอด source ที่ parse ไม่ผ่าน

Test ของ reviewer เคยตั้งสมมติฐานผิดว่าต้องมี readonly test source ใน review.Files เส้นทางทั้ง whole และ partial เดิมส่ง changed source พร้อม actual command evidence จึงแก้ test ให้ตรวจ frozen StepChecks และ stdout จริง โดยยังตรวจ test.js ที่ filesystem ว่าไม่เปลี่ยน ไม่มีการขยาย production review contract เพื่อให้ test ผ่าน

## ขั้นถัดไป

B5 ผ่าน native Go fixture แล้ว ต้อง review candidate ทั้ง 67 paths เป็นชุดที่เชื่อมกันก่อน native apply ใช้ manifest เดิมและ existing native review/apply drivers เป็นฐาน ห้าม apply rejected/truncated drafts หรือ replay apply รุ่นก่อน หลัง apply ต้องตรวจ source hashes และทดสอบ native Go ใหม่ด้วย fixture/operation ใหม่

งาน H1A การตรึง evaluation run ตั้งแต่เริ่ม, H1B aggregate-only hole, H1C scope resolver, การเรียนรู้และทดสอบบนงานใหม่, UX/control และ marketplace Compose/Kubernetes ยังเป็นส่วนของเป้าหมายเต็ม การผ่านกรณีไฟล์ใหญ่ไม่ใช่การปิดงานเหล่านั้น
