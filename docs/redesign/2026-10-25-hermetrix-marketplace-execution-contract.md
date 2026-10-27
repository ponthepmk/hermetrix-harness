# สัญญาการควบคุมงาน Hermetrix และโครงการ Marketplace

วันที่ 25 ตุลาคม 2026 · อัปเดต 26 ตุลาคม · เป้าหมายยัง active · local runtime ที่เลือกทำ native generation/review ได้แล้ว ดูหลักฐาน B6

เป้าหมายเต็มคือทำให้ Hermetrix รับงานขนาดใหญ่ วิเคราะห์ข้อกำหนด ถามเมื่อมีข้อมูลที่เปลี่ยนการออกแบบ วางแผน ลงมือทดสอบ แก้จากหลักฐาน พักและกลับมาทำต่อได้ แล้วใช้สร้าง marketplace หลายผู้ขายที่ใช้งานจริงได้ โดยพัฒนาใน Docker Compose และมีเส้นทางรัน Kubernetes ด้วย Go, React/Next.js, PostgreSQL, Redis และ RabbitMQ/worker

Codex รับผิดชอบการออกแบบขอบเขต สั่งงาน review และตรวจรับ Hermetrix เป็นผู้เขียนโค้ดและแก้โค้ด ผู้ใช้กำหนดว่าห้าม Codex ลงมือ code เอง ระหว่างรอความชัดเจนให้ข้อห้ามครอบคลุมทั้ง source ของ harness และ marketplace Codex จัดทำเอกสาร วิเคราะห์ diff และเรียกการตรวจที่ได้รับอนุญาตได้ แต่ไม่แทนที่ Hermetrix ด้วย implementation ของตัวเองหรือ agent อีกตัว

## ผลที่ต้องส่งมอบครบ

| รหัส | ผลปลายทาง | หลักฐานตรวจรับ |
|---|---|---|
| R1 | Hermetrix เรียนรู้จากทั้งข้อผิดพลาดและวิธีที่ได้ผล | มี model-authored candidate, immutable evidence, paired evaluation, rejection/promotion/rollback และผลบนงานใหม่ |
| R2 | รับ requirement ที่ซับซ้อนและแก้ข้อสงสัยก่อนลงมือ | requirement revision, clarification history, dependency plan และ criterion coverage |
| R3 | คุมการลงมือจนจบงานที่ตกลง | งานจริงตั้งแต่ plan ถึง source change, command, review, repair และ acceptance; ไม่ใช่ simulated provider เท่านั้น |
| R4 | หยุด พัก กลับมาทำต่อหลังปัญหา | crash/timeout/Stop/resume tests กับ effect จริง; ไม่มี duplicate write/order/payment |
| R5 | UX/UI ของ harness ใช้งานแบบ dynamic | start/stop/resume/steer/queue, plan/approval/evidence, context และ artifact เชื่อม backend จริง พร้อม recovery ทุกสถานะ |
| R6 | คุม browser และเครื่องได้ในขอบเขต | actual target/window/process identity, fresh observations, action receipts และผลหลัง action |
| R7 | รับ repository/งานขนาดใหญ่ได้เหมาะสม | งานแบ่งเป็น bounded steps, dependency/context indexing, scheduler/load measurements และ per-stage budgets |
| R8 | marketplace หลายผู้ขายทำงานครบ | buyer/seller/admin journeys, stock/order/payment/shipping/returns/payout/support ผ่าน API/DB/queue/browser จริง |
| R9 | local Compose และ Kubernetes ใช้งานได้ | build/start/migrate/seed/test บน Compose, deploy/upgrade/rollback/recovery บน cluster ที่ตกลง |
| R10 | พร้อมปล่อยใช้งานตามขอบเขตจริง | ไม่มี known blocker/P0/P1, acceptance ครบ, restore drill, operational handoff, live integrations ที่เลือกและ owner signoff |

การแก้บั๊กต้องมี regression test และทดสอบเส้นทางใช้งานที่เกี่ยวข้อง เกณฑ์ R10 ไม่ใช่คำรับประกันว่าซอฟต์แวร์จะไม่มีบั๊กใหม่ตลอดไป ผลทุกข้อยังต้องพิสูจน์จากระบบปัจจุบัน ห้ามเรียกโครงการเสร็จเพียงเพราะ build หรือ tests บางส่วนผ่าน

## สถานะตั้งต้นที่ตรวจได้

- Source HEAD `4459036638527a68dec0a14d2fab8d87e08c13e3`; ก่อนเริ่มงานนี้มีเฉพาะเอกสาร Harness² และหลักฐาน probe ที่ยังไม่ commit
- ไม่พบ process ชื่อ Hermetrix/llama/Ollama จาก process inventory และ health ที่ 7331, 18816, 8088, 8089 ติดต่อไม่ได้ ณ การตรวจรอบนี้ จึงยังไม่มี live worker ให้ส่งงาน
- Docker Engine ตอบ `29.9.2 linux` และมี workload ของโครงการอื่นอยู่ ต้องใช้ Compose project/ports/volumes แยก ห้ามยึดว่าระบบเดิมเป็น marketplace acceptance หรือหยุดมันเพื่อให้ tests ผ่าน
- การเปลี่ยน pilot/model runtime เคยถูก automatic approval review ปฏิเสธตาม [บันทึกเดิม](../reviews/2026-10-23-goal-runner.md) ข้อความคือ blocked by policy ไม่มี detailed class ไม่เปลี่ยนพอร์ตหรือเปิดทางอื่นเพื่อเลี่ยง
- Installed executable เดือน ก.ย. เก่ากว่า source ไม่ควรใช้ชื่อ launcher เป็นหลักฐานว่ามี Goal/schema ล่าสุด ต้องอ่าน health/schema/binary identity เมื่อได้รับขอบเขตให้เปิด runtime
- Worker ปัจจุบันรับ 1–32 files และ request สูงสุด 128 KiB; Goal เขียนไฟล์ใหม่ได้เมื่อ parent มีอยู่ สัญญาการสร้าง directory/deletion ยังไม่ครบ
- Command runner allowlist ปัจจุบันไม่มี docker/kubectl โดยตรง ต้องเพิ่ม capability ที่ host คุมก่อนมอบหมาย Compose/Kubernetes ไม่ใช้ Python/Node wrapper เพื่อหลบข้อจำกัด
- มี Goal API สำหรับ start/status/stop/resume แต่การค้น UI รอบนี้ไม่พบการเรียก `/api/goals` จึงยังต้องเชื่อม controls และพิสูจน์จากหน้าจอจริง

หลักฐาน source: [worker](../../internal/worker/proposal.go), [command runner](../../internal/product/commands.go), [Goal API](../../internal/web/goals.go), [Goal operator guide](../GOAL-RUNNER.md)

## การตัดสินใจที่ยังรอ

1. Runtime/model ที่ให้ Hermetrix ใช้: คำถาม async ถูกส่งแล้ว จะไม่เลือก endpoint แทนผู้ใช้หรือเปิด model service ระหว่างรอ
2. ข้อห้าม Codex เขียน code รวม harness ด้วยหรือเฉพาะ marketplace: คำถาม async ถูกส่งแล้ว ระหว่างรอยึดข้อห้ามที่กว้างกว่า
3. Product requirements ที่ต้องยืนยันก่อน go-live: payment/shipping providers, live domain/cluster, seller verification, settlement/tax policies, retention, expected users/load และ operational ownership ยังไม่มีข้อมูล

ข้อ 3 ไม่ขวางการออกแบบ contract และ sandbox integrations แต่ sandbox จะเป็นเพียง milestone ไม่ใช้แทนคำว่า production ready ปลายทาง

Workspace ที่เสนอสำหรับ marketplace คือ `D:/Projects/harness/hermetrix-marketplace` ซึ่งยังไม่มีตอนตรวจ ห้ามปะปน source ของ marketplace เข้ากับ harness และยังไม่สร้าง application files

## วงจรสั่งงานและตรวจรับ

1. Codex ส่ง packet ที่ระบุ objective, current evidence, writable paths, read-only material, acceptance, budget และ stop/escalation conditions
2. Hermetrix อ่านและเสนอ clarification/plan ก่อนเขียน เมื่อ requirement เปลี่ยนต้องสร้าง revision ใหม่และผูกแผนให้ตรง
3. Host ตรวจ plan/DAG, capability และ writable scope จากนั้นให้ Hermetrix สร้าง bounded proposal
4. Codex review actual diff และความสัมพันธ์กับ requirement; host apply ด้วย preimage hashes และเก็บ operation receipts
5. Hermetrix/host รัน checks ที่ตรึงไว้ เก็บ stdout/stderr, artifacts และ DB/API/browser outcome ไม่ใช้คำอ้างของโมเดลเป็น test result
6. เมื่อ fail ส่ง failure signature, exact output, input, expected/actual และ prior attempts กลับไปให้ Hermetrix แก้ ห้าม Codexเขียน patch แทน
7. เมื่อ fail ซ้ำสองรอบให้ตรวจ root cause/known-good path ใหม่ ไม่วนแก้แบบเดาสุ่ม; temporary provider failure ต้องพักโดยเก็บ state และคืน scheduler ให้ foreground
8. เมื่อผ่าน checks ต้อง fresh review ของ exact source/evidence และ user/business acceptance ก่อนปิดงาน
9. สกัดทั้ง positive และ negative lesson เมื่อมี causal evidence แล้วทดสอบกับงานใหม่ก่อนขยายการใช้

ยังไม่มี packet ใดถูกส่งไปยัง live Hermetrix ในรอบนี้ การเขียน packet เสร็จไม่ใช่ execution receipt

## ลำดับการยกระดับ harness

| งาน | Dependency | ขอบเขต | ผลตรวจรับ |
|---|---|---|---|
| H0 Runtime qualification | runtime/model choice | ตรวจ binary/schema/provider/context/tool readiness และ seed one bounded coding task | โมเดลจริงสร้าง diff, host apply, actual test และ separate review ครบ |
| H1D Bounded views for large files | H0; ก่อนส่วน H1 ที่ต้องแก้ไฟล์ใหญ่ | host ตรึงส่วน source ที่ส่งโมเดลพร้อม hash ของไฟล์เต็ม คง scope/CRLF/bytes นอกส่วนที่อนุญาต และ guard เดิมทุกขั้น | แก้ส่วนเล็กของไฟล์เกิน 128 KiB ได้จริง; ปฏิเสธ view ปลอม, hidden-region edit, stale preimage/root และ full replacement จาก partial view |
| H1 Learning evidence gate | H0 | run identity/hash/revision, paired criterion results และ source-bound Skill scope | สาม probe เดิมถูกป้องกัน พร้อม positive path และ failure tests |
| H2 Project bootstrap effects | H0 | directory/create/delete contracts แบบ bounded และ rollback-safe | สร้าง tree ใหม่จริง, no-clobber, concurrent change, crash และ rollback |
| H3 Compose/Kubernetes capabilities | H0 | host-controlled tool definitions, exact project/namespace/context, deadlines/cancel/receipts | Compose lifecycle จริงและ cluster ที่เลือก โดยไม่แตะ workload อื่น |
| H4 Durable program orchestration | H2/H3 | DAG, bounded child tasks, queue fairness, cumulative budgets, scheduling/backoff | โปรแกรมหลายส่วนต่อเนื่องข้าม restart โดยไม่ replay uncertain effects |
| H5 Dynamic workbench UX | H4 | actual Goal controls, progress, plan/clarification, review, evidence และ recovery | คนใช้ดำเนินงานครบ desktop/mobile โดยไม่มีปุ่มหลอกหรือ flow ตัน |
| H6 Browser/desktop end to end | H0/H5 | window/process/origin scope, observation/action/result และภาพที่ model เห็นได้จริง | actual browser/native flow; text-only model ไม่ถูกอ้างว่าเห็นภาพ |
| H7 Learning lab | H1/H4 | isolated Propose–Probe–Compose และ experience retrieval | holdout improvement คุ้มค่า, baseline fallback, rollback/rejection จริง |
| H8 Scale qualification | H4/H7 | dependency discovery, context fitting, throughput, memory/latency | measured load ตาม target ที่ตกลง ไม่มี silent context loss |

H1 ให้ใช้ [แผน Harness²](2026-10-25-harness2-comparison-and-improvement-plan.md) และ [packets สำหรับ worker](../handoffs/2026-10-25-hermetrix-worker-packets.md) เป็นจุดเริ่ม ไม่เลื่อน acceptance ให้เล็กลงเพื่อให้ผ่านได้ง่าย

ความคืบหน้า 26 ตุลาคม: H1B ส่วนการคำนวณและตรวจรูปแบบหลักฐานมีโค้ดจาก Hermetrix ที่นำเข้าจริงแล้ว ดู [ผลและข้อจำกัด](../handoffs/2026-10-26-h1b-live-worker-results.md) ส่วน H1A, provenance, promotion และ scope ยังไม่ผ่านตรวจรับครบ การลงมือเผย dependency เพิ่มเติมคือ agent/service.go ขนาด 172,056 bytes ใหญ่กว่าเพดานชุดข้อมูล worker 128 KiB และไฟล์อื่นยังต้องพอดีกับบริบทโมเดล จึงเพิ่ม H1D สำหรับการแก้ไฟล์แบบจำกัดส่วนพร้อม full preimage binding ไม่ใช้การส่ง source ที่ตัดแล้วเสมือนเป็นไฟล์เต็ม ไม่เพิ่มเพดานหรือข้าม guard โดยเงียบ และไม่ตัด H1 หรือ marketplace ออกจากขอบเขต

H1D A1/A2/A3 และ B67 นำเข้าและตรวจ source จริงแล้ว B6 review 17 ชุดอนุมัติครบ native apply 67 receipts/0rollback หลัง apply ทั้ง repository ผ่าน 1,315 test events (20 opt-in/environment skips), vet/build ผ่าน และ native Go fixture ใหม่ผ่านจริง ดู [ผล B6](../handoffs/2026-10-26-b6-native-apply-results.md) ยังต้องพิสูจน์งานหลายไฟล์ซับซ้อนและ measured scale ตาม R7

หลักฐาน A3/B3/B5 ที่ระบุ candidate/unapplied เป็นสถานะในอดีต ปัจจุบันถูกแทนด้วย B6 applied receipt ส่วน Go fixture มี comment padding และ arithmetic declaration เล็ก ไม่ใช้แทนการตรวจระบบธุรกิจหรือ learning transfer

## สถาปัตยกรรม marketplace ที่เสนอ

เริ่มด้วย Go modular backend ที่แบ่ง domain ชัดและ deploy API/worker แยก process เพื่อพัฒนาได้เร็วและรักษา transaction ของ order/stock โดยไม่แบก distributed transactions ตั้งแต่แรก โครงสร้างต้องเปิดทางแยก service เมื่อมีข้อมูล load/ownership ที่คุ้มค่า การเริ่มเช่นนี้ยังต้องทำทุก functional domain ของปลายทางครบ ไม่ใช่ตัดให้เหลือ demo

React ใช้ผ่าน Next.js สำหรับ storefront และพื้นที่ buyer/seller/admin แยก route/authorization; Go เป็น authoritative business API; PostgreSQL เป็นแหล่งข้อมูลจริงและ financial/order records; Redis ใช้ cache/rate-limit/ข้อมูลชั่วคราว; RabbitMQ เป็น durable asynchronous transport โดย worker ต้อง idempotent ใช้ transactional outbox, publisher confirmation และ acknowledge หลังบันทึกผลสำเร็จ

| Domain | ขอบเขตเต็ม | ตัวอย่าง invariant ที่ตรวจจริง |
|---|---|---|
| Identity/access | buyer, seller, staff, admin; sessions/recovery; tenant/shop roles | ผู้ขาย A อ่าน/แก้ออเดอร์และ payout ของ B ไม่ได้ |
| Seller/shop | onboarding, verification state, shop profile, policies, staff | ร้านที่ยังไม่ผ่านเงื่อนไขขายไม่สามารถ publish/settle |
| Catalog/search | categories, variants, media, stock, price, moderation, search/filter/sort | variant/price/stock ตรงกันจาก storefront ถึง checkout |
| Cart/pricing | multi-seller cart, shipping quote, coupons/promotions, total breakdown | server คำนวณยอด; coupon/price change ไม่ทำให้เงินผิด |
| Checkout/order | stock reservation, per-seller suborders, state transitions, cancel | concurrent last-item purchase ไม่ oversell; retry ไม่สร้าง order ซ้ำ |
| Payment/ledger | payment intent, webhooks, reconciliation, fees, journal | repeated/out-of-order webhook ไม่ตัดหรือคืนเงินซ้ำ |
| Fulfillment | shipment, tracking, label/provider adapters, delivery confirmation | package status ไม่กระโดดข้าม state ที่จำเป็น |
| Returns/refunds | request/evidence, eligibility, partial return, dispute | คืนเงินไม่เกิน settled amount และไม่เพิ่ม stock ผิด |
| Seller settlement | balances, commission, payout, hold, reconciliation | payout อ้างเงินที่มีสิทธิ์และ reverse ได้ตามกรณี |
| Engagement | reviews/ratings, wishlist, notification preferences, buyer-seller support | review ผูกกับสิทธิ์/การซื้อ; notification job ไม่ส่งซ้ำ |
| Operations | moderation, disputes, support, reports, audit/export, observability | ทุก critical mutation มี actor/history และค้นย้อนหลังได้ |
| Reliability | migrations, backup/restore, secrets/config, recovery/load | actual restore และ queue replay ไม่ทำให้ยอดธุรกิจเปลี่ยนผิด |

Object storage สำหรับ product media ต้องเลือก implementation ที่เหมาะกับ local/cluster; ไม่ให้ binary media อยู่ใน PostgreSQL โดยไม่มีเหตุผล การเลือก library/version ให้ Hermetrixตรวจ official documentation ก่อน pin dependency

## ลำดับส่งมอบ marketplace

| งาน | ผลที่ต้องได้ | ผลจริงที่ต้องตรวจ |
|---|---|---|
| M0 Product contract | roles, state machines, schema boundaries, API/event contracts, flow map | ทุก requirement trace ไปยัง task และ acceptance |
| M1 Reproducible environment | Go API/worker, Next.js, Postgres, Redis, RabbitMQ, migrations/seed และ media storage ผ่าน Compose | cold start และ repeated start บน machine ที่ตกลง โดยไม่ใช้ข้อมูลระบบเดิม |
| M2 Accounts and shops | auth, buyer/seller/admin, shop roles/onboarding | browser login/recovery/forbidden paths และ DB ownership |
| M3 Catalog to cart | publish variant/media/search, wishlist/cart/promotions | ผู้ขายลงสินค้าแล้ว buyer ค้นหาและเพิ่ม variant ที่ถูกต้อง |
| M4 Checkout to paid order | reserve stock, authoritative pricing, payment adapter/webhook | actual concurrent checkout, duplicate request และ payment callback |
| M5 Fulfillment and after sales | shipment/tracking/cancel/return/refund/dispute | UI/API/DB/events สอดคล้องทุก state และ partial refund |
| M6 Settlement and operations | fees/payout/reconciliation/support/reports/audit | ledger reconciliation และ rights tests |
| M7 Complete UX | responsive storefront, seller/admin work areas, all loading/error/empty states | buyer/seller/admin ทำ full journeys ผ่าน UI จริง |
| M8 Resilience and performance | failover/retry/DLQ/restore/load profiling | worker crash ระหว่างงาน, broker/db outage, concurrent load และ recovery |
| M9 Kubernetes | manifests/packaging, config/secrets, ingress, probes, resources, migration jobs, rollout/rollback | cluster deployment จริงของ version เดียวกับที่ผ่าน Compose |
| M10 Release acceptance | selected real integrations/domain/operations/runbooks and owner acceptance | all current acceptance evidence และไม่มี unresolved blocker |

ทดสอบ end-to-end ระหว่างแต่ละช่วง ไม่รอให้ M7 เริ่มแล้วค่อยเปิด browser การใช้ sandbox payment/shipping ในช่วงต้นต้องแสดงชัด และยังคงงาน live integration ใน M10

## UX ของ harness และ marketplace

สำหรับ harness ผู้ใช้หลักคือผู้สั่งงานที่ต้องเข้าใจว่างานอยู่ขั้นไหน ตัดสินใจอะไร และกู้สถานการณ์อย่างไร หน้าหลักควรเห็น goal/current stage/next decision, แผนและ dependency, actual activity, diff, evidence และ Stop โดยคงตำแหน่งและ draft เมื่อมี streaming updates

Clarification ต้องแสดงสิ่งที่ยังไม่รู้และผลต่อแผน; approval ต้องแสดง exact target/change; error ต้องมีผลที่เกิดขึ้นแล้วและ action ที่ทำต่อได้; waiting ต้องบอกว่ารออะไร; completed ต้องเปิดหลักฐานได้ Mobile ต้องเข้าถึง primary action/Stop/approval โดยไม่มี overflow หรือ modal ที่กดไม่ได้

สำหรับ marketplace แต่ละ journey ต้องมี entry → decision → action → feedback → recovery → completion: ค้นสินค้าไม่เจอต้องปรับ filter ได้, สินค้าหมดต้องคืนทางเลือก, ราคาหรือที่อยู่เปลี่ยนต้องทบทวนยอด, payment uncertain ต้องดูสถานะเดิมได้โดยไม่สร้าง payment ซ้ำ, ผู้ขายแก้ shipment/refund ตามสิทธิ์ได้ และ admin แก้ข้อพิพาทโดยมี audit

ตรวจ keyboard/focus/forms/accessibility, desktop/mobile viewport, slow/offline/timeout/stale/concurrent states และ UI ที่สะท้อน backend จริง ไม่รับ screenshot หน้าสวยเป็นหลักฐานว่า checkout/refund/payout ทำงานแล้ว

## สถานะ verification รอบเริ่มควบคุมงาน

- ชุด tests ที่รันใหม่: `go test ./internal/learning ./internal/skills ./internal/selfimprove ./internal/taskeval ./internal/worker -count=1` ผ่านทั้งห้า packages
- กู้ terminal log `.tmp-ui/goal-20261023/fixture-profile-final-ci-race.jsonl`: 44 package terminal records, started without terminal=0, fail events=0; 36 package pass และ 8 package skip ซึ่งเป็น packages ไม่มี tests ดูสรุปหลักฐานใน docs/evidence
- ผล race เป็นรอบวันที่ 23 ต.ค. มี external-service tests บางรายการ skip ภายใน package; ไม่ใช้แทน live model, desktop workflow หรือ marketplace acceptance
- ยังไม่ dispatch งาน coding จริง ไม่เขียน source ผลิตภัณฑ์ ไม่เปิด model/runtime ไม่เปลี่ยน Docker workloads ไม่ deploy และไม่ claim ว่างานปลายทางสำเร็จ

ก่อนดำเนินการที่ติดอยู่ให้ revalidate runtime เดิมเสมอ ถ้าไม่มีคำตอบเลือก runtime และขอบเขต Codex ให้คง goal active และใช้ blocked audit ตามสถานะจริง ห้ามวนสร้างแผนใหม่แล้วนับว่าเป็นการลงมือสร้างระบบ
