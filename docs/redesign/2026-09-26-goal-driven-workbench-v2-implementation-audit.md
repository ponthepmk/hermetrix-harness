# Hermetrix Workbench v2 — หลักฐานเริ่มงานและสถานะจริง

วันที่ 2026-09-26 · ฐานโค้ด `97f530f` · schema เริ่มต้น `52`, ปัจจุบัน `65`

## ความคืบหน้าล่าสุดหลัง baseline

เพิ่ม flow [Requirement → UX/UI preview → plan → confirmation](2026-09-26-requirement-review-flow.md): ช่องเริ่มงานหลักเปิด Goal draft แทนการส่ง agent turn ทันที; หน้า Plans แสดงภาพที่คาดว่าจะได้และแผนในลำดับที่ตรวจง่าย, เก็บ feedback เมื่อส่งกลับแก้และส่งเข้า planner รอบถัดไป; plan approval ยังผูก revision/hash ฝั่ง Task engine. Chrome ทดสอบทางเข้า/ภาพ desktop และ mobile, targeted engine/coordinator/UI ผ่าน. ขอบเขตสำคัญ: แชท “ถามคำถาม” เป็นทางแยกและยังไม่ได้ผูก policy ของ Task plan กับทุก tool/endpoints.

เพิ่ม [เสียงสนทนาใน Windows Preview](2026-09-13-realtime-voice.md) ตามการเลือก local-only ของผู้ใช้: ปุ่มพูดในหน้าเริ่มงาน/แชต, transcript แก้ไขได้, ไมค์ Windows ตรงเมื่อ browser ขอสิทธิ์ค้าง, Whisper และ Windows Speech ในเครื่อง, คำตอบเสียงเป็นช่วงจากโมเดลข้อความที่เลือก และการหยุด/พูดแทรก ใช้ Preview กับ Ollama ในเครื่องและข้อมูลแยกจากแอปที่ติดตั้ง ผลทดสอบ UI จริงเห็นไมค์→ถอดเสียง→ข้อความตอบ→เล่นเสียงครบวงจร ต่อมาพบ local model ถูกพักหลังถูกยกเลิกกลางคำตอบ จึงแก้ให้หยุดเฉพาะเสียงเล่นและรอคำตอบเดิมจบก่อนส่งข้อความใหม่ ปิด auto-send เป็นค่าเริ่มต้นเพื่อไม่ส่งเสียงพื้นหลังโดยไม่ตั้งใจ; provider test หลังรีสตาร์ตผ่าน ยังไม่ทดสอบการพูดแทรกซ้ำหลังแก้และยังไม่มี end-to-end latency benchmark

ผู้ใช้เลือกทดลองร่างคอนเทนต์ในเครื่องก่อน Preview จึงมี Task “ทดลองร่างคอนเทนต์ลดขยะอาหาร” พร้อม Goal/ภาพที่คาด/เกณฑ์, แผน 5 ขั้นที่ **รอผู้ใช้ตรวจ** และ [ร่างข้อความกับสตอรี่บอร์ด](../drafts/2026-09-26-food-waste-local-pilot.md) แยกเป็นตัวอย่างอ่านได้ โมเดลในเครื่องสร้างแผนไม่ครบตามรูปแบบ จึงร่างแผนด้วย Codex แล้วบันทึกผ่าน API ที่ยังบังคับ plan review; ไม่ได้กดยืนยันแทนผู้ใช้ Comfy MCP ใน Preview เชื่อมต่อและรายงาน engine พร้อม แต่ยังไม่ render สื่อของตัวอย่าง ไม่มีบัญชี social หรือ remote post

ตารางด้านล่างและหัวข้อ P1 ด้านล่างเป็น snapshot ณ เริ่มงาน ไม่ใช่สถานะล่าสุด หลังจากนั้นลงมือเพิ่ม Crossref research, claim/content review, video frame evidence, Comfy MCP workflow/job/output, social account connection และตัวส่งออก Facebook/TikTok แล้ว สถานะล่าสุด:

| Journey/ส่วน | หลักฐานที่ผ่าน | ขอบเขตที่ยังไม่ผ่าน |
|---|---|---|
| J1–J2 Goal/Plan/Done | Goal Contract, plan review/diff, completion report/current evidence, user acceptance, ตัวนับเกณฑ์/วิธีตรวจ/ขั้นตอนจากหลักฐานปัจจุบัน, ป้ายหลักฐานเก่า และ activity ledger ข้าม revision ของ Goal/validation/checkpoint/review/acceptance/Task effect รวม Browser/Windows/Comfy/social; ledger เปิดภาพก่อน/ภาพตรวจผลและไฟล์ Comfy ที่เป็น artifact ได้ รวมสถานะ reconciliation; ตอบ unknown ใน Task สร้าง Goal revision พร้อมเก็บ Q/A และทำให้แผนเดิม stale; ร่างคำตอบอยู่ต่อหลัง reload ในแท็บเดิมและหมดอายุเมื่อ Goal เปลี่ยนฉบับ ผ่าน Chrome จริง | owner/reviewer policy รายละเอียด, คำถามที่ agent สร้าง และ readiness ของทุก pane |
| J3 effect ไม่แน่นอน | native Windows และ managed browser click/type มี Task receipt, fresh screenshot, exact approval และ manual reconciliation; Comfy/social บันทึก operation และไม่ replay อัตโนมัติ; social duplicate test; ทางลัดหน้าหนึ่งของ Task พาไปตรวจ Windows/browser/Comfy/social ที่ผลค้างก่อนปิดงาน | reconciliation UI ของ Comfy/social ให้ครบทุกสถานะ, crash test ระหว่างทุกช่วงของ upload |
| J4 capability ขาด | Comfy preflight แจ้ง missing model; TikTok/Facebook ตรวจ identity/capability, token ไม่อยู่ DB | grant/account จริงของผู้ใช้, context/Skill/team readiness ครบชุด |
| J5 Windows/browser | browser observation hash และ schema 65 one-use prepare/confirm receipt ต่อ Task, ปิดทาง direct click/type, password redaction, same-tab unresolved gate, เปิดแท็บใหม่ในเว็บ/โปรเจกต์เดิมเพื่อตรวจผลหลัง restart; agent browser click/type อ่านภาพ/เป้าหมายก่อนขออนุมัติ, รอมนุษย์อนุมัติ, สร้าง receipt ใช้ครั้งเดียวหลังอนุมัติและปฏิเสธเมื่อหน้าเปลี่ยน, จำกัดแท็บในโปรเจกต์; Windows UIA/screenshot/prepare/confirm, Task binding, stop/cancel, same-window unresolved gate และ manual reconciliation พร้อมภาพผ่าน fixture | agent ควบคุม Windows แบบเดียวกับ UI ยังไม่มี, Windows app จริงชนิดที่สอง, protected-surface/stop acceptance จริง และ browser crash walkthrough ครบชุด |
| P5 วิดีโอ | MP4/WebM player/range, ตัวอย่างภาพสามเวลา, ถอดเสียงด้วย FFmpeg และ Whisper ในเครื่องเป็นข้อความพร้อมเวลา/ภาษา/model hash; real speech fixture ผ่านและ UI มี cancel | การตรวจภาพเชิงความหมาย, การยืนยันความแม่นยำเสียงโดยมนุษย์, WebM และ restart/cancel walkthrough ครบชุด |
| J6 content | ค้น Crossref, เลือกแหล่ง, claim สำคัญผูกบทความสองแหล่ง, draft/rights/approval, Comfy output MP4, Facebook Feed/Reel และ TikTok Direct/draft API จำลอง; status `draft_uploaded` ไม่ผ่าน Done | brief บัญชีจริง, token/grant จริงและ remote pilot ที่ผู้ใช้อนุมัติ |
| P3 Context/Skill | Session detail อ่านรายงาน compiler ล่าสุดจาก snapshot เดิม, หน้า Review แยกจำนวน token ประมาณก่อนส่ง/ใช้จริงจากโมเดล/ยังไม่ทราบ พร้อมรายการที่เลือกและไม่เลือก; Project Brain แสดงจำนวนที่อยู่ใน context เทียบกับเสนอแต่ถูกตัด; Skill picker/Review แยก suggested จากที่อยู่ในคำขอจริงล่าสุดตาม fragment ID และบอกเหตุผลที่แนะนำ (ชื่อ/คำ/ข้อความ/ความหมาย/ปักหมุด); หน้า Context diagnostic แสดงรายการเดียวกัน; snapshot ที่อ่านไม่ได้ไม่ทำให้แชทล่ม | ประมาณการคำขอถัดไป, Project Brain source/scope/lifecycle/applicability เต็มชุด, Skill conflict และ team readiness ครบทุกสถานะ |
| P8 migration/recovery | ฐาน schema 64 ที่มี browser tab อัปเกรดเป็น 65 โดยคงข้อมูลเดิม; สำเนาฐานติดตั้งจริง schema 50 (4 projects, 3 sessions) อัปเกรดเป็น 65 และเปิดซ้ำได้โดย count ไม่เปลี่ยนและไม่มี foreign-key error โดยไม่แตะ vault/ฐานจริง; full recovery package เก็บ browser action ที่ผลไม่แน่นอนพร้อม screenshot ref และ restore ได้ โดย vault ไม่อยู่ใน package; build preview เปิดด้วย data แยก, health รายงาน schema 65 และ social endpoint ว่างตามคาด แล้วปิดได้ | security/performance/restart ครบชุด, เชื่อมบัญชีจริง และ release decision |

`go test ./... -count=1 -timeout 180s` ผ่านที่ schema 65 รวม browser receipt/reconciliation, activity ledger, Goal clarification, Task gate, migration 64→65 และ context snapshot ล่าสุด; targeted Task activity และ Chrome clarification หลัง reload ผ่าน, Node Task/runtime 34 tests ผ่าน, `go vet` ผ่านก่อน slice ล่าสุด; Chrome Task/video/mobile tests และ live local Comfy สีเคลื่อนไหว 8 เฟรมแปลงเป็น MP4 เล่นได้. หน้า Review แยกค่าที่โมเดลรายงานจากค่าประมาณก่อนส่งและค่าที่ยังไม่ทราบ พร้อม reserve/include/omit. คู่มือขั้นตอนบัญชีอยู่ที่ `docs/SOCIAL_PUBLISHING.md`. ห้ามอ้างว่าผ่าน P0–P8 ทั้งหมดหรือเผยแพร่จริงจากการทดสอบ mock.

เอกสารนี้บันทึกสิ่งที่ตรวจจากโค้ดและการทดสอบในรอบเริ่มลงมือ โดยไม่ใช้แผนเป็นหลักฐานว่าฟีเจอร์เสร็จแล้ว Checkout มีการเปลี่ยนแปลงค้างอยู่ก่อนเริ่มงานจำนวนมาก จึงแก้ไฟล์เดิมเฉพาะจุดและยังไม่ลบหรือย้ายไฟล์เหล่านั้น

## สิ่งที่ตรวจได้และขอบเขต

| ส่วน | สถานะ | หลักฐาน/ทางเดินปัจจุบัน | งานถัดไป |
|---|---|---|---|
| Task, Goal, Plan, evidence | MODIFY | Goal Contract และการแก้ revision ในหน้า Task, คำตอบ unknown ถูกเก็บใน contract ฉบับใหม่โดยคำตอบเก่าแก้ทับไม่ได้; plan review ผูก hash, เปรียบเทียบแผนฉบับก่อน, user acceptance, completion gate เดียวกัน; ledger แสดง validation/checkpoint/review/acceptance/Task effect และ Browser/Windows/Comfy/social พร้อม current/stale; ร่างคำตอบ reload ได้ในแท็บเดิม; ทดสอบ engine/API/Chrome | owner/reviewer policy, agent clarification และ recovery เส้นทางที่เหลือ |
| Workbench page | MODIFY | `internal/web/ui/app.js`, `codex-theme.css`; Task cockpit แสดง gate, next action, ตัวเลือกงาน, ปุ่มทางลัดมือถือ; มี preset คุย/วางแผน/ลงมือ/ตรวจผล/ทบทวน และจำ layout ต่อโปรเจกต์ | เชื่อมสถานะ loading/offline/stale ทุก pane, keyboard path และ deep link |
| Browser | REUSE/MODIFY | CDP browser เพิ่ม observation hash; click/type ต้อง prepare + ยืนยัน exact action one-use; Task receipt และภาพก่อน/หลัง, redaction password, uncertain reconciliation ผ่านแท็บใหม่ของเว็บ/โปรเจกต์เดียวกัน | พิสูจน์ interrupt/restart แบบ kill จริงและ user journey ครบ J5 |
| Windows app control | NEW/MODIFY | เพิ่มรายการหน้าต่าง, UI Automation, ภาพ, handle/PID; กดและพิมพ์ผ่าน prepare/confirm พร้อม durable receipt, stale/duplicate/restart gate; ทดสอบกับหน้าต่างตัวอย่างจริง | stop/reconciliation UI, protected-surface state และทดสอบสองแอปจริง |
| Video | NEW/MODIFY | นำเข้า MP4/WebM สูงสุด 256 MiB/10 นาที/4K ด้วย ffprobe, บันทึก CAS แบบ stream, player และ HTTP range; เฟรม 3 เวลาและถอดเสียงเป็นช่วงเวลาผ่าน local Whisper พร้อม durable media job/cancel; ทดสอบ MP4/seek/ไฟล์เสีย/เสียงพูดจริง | semantic vision, WebM/browser walkthrough และ full restart/cancel acceptance |
| MCP/Comfy | MODIFY | มี MCP catalog; Codex host ติดต่อ Comfy MCP ในเครื่องได้ แต่ยังไม่ใช่การเชื่อมจาก Hermetrix; workflow วิดีโอที่ลองสองแบบขาดโมเดล | readiness และ preflight ใน Hermetrix, durable job, output artifact และ recovery |
| Article/content | NEW | ยังไม่มี flow ค้นบทความ → claim → copy/storyboard ที่ตรวจพบ | source manifest, claim ledger, preview และ review แยกแพลตฟอร์ม |
| Facebook/TikTok publishing | NEW | ยังไม่พบ social publisher; บัญชีมีตามผู้ใช้ แต่สิทธิ์ API ยังไม่ตรวจ | ตรวจ grant จริง, approval ต่อชิ้น/ปลายทาง, upload/reconcile/remote receipt |
| Skill/Context/Team | REUSE/MODIFY | มี Skill, Project Brain, task/decision records | applicability/readiness, context actual/estimate, team activity/stall UI |
| ขอบเขตที่ห้ามทับ | DO NOT TOUCH | dirty checkout ที่มีงานเดิมและ credential vault | รักษา diff เดิม; migration ต้องเป็น additive และตรวจ schema ก่อนทุกครั้ง |

## Flow และหน้า: เกณฑ์ตรวจรับของแต่ละเส้นทาง

| Journey | หน้า/การตัดสินใจที่ต้องเห็น | Feedback และ recovery ที่ต้องพิสูจน์ | ตอนนี้ |
|---|---|---|---|
| J1 โค้ด/UI | Project → Goal → Plan → Build → Verify → Review → Done | check ล่าสุด, review ล่าสุด, effect receipt, ปุ่มปิดงานเมื่อ gate ผ่าน | ส่วน Task/Plan/Done พร้อม browser test; Build/Review end-to-end ยังไม่ผ่านเป็น journey เดียว |
| J2 งานกำกวม | Goal → คำถาม → requirement revision → plan ใหม่ | คำตอบใหม่ทำให้หลักฐาน/approval เก่า stale; draft ไม่หาย | UI/API/Chrome ตอบ unknown สร้าง Goal revision, เก็บ Q/A, ยกเลิกแล้วยังเห็น draft; ยังต้องตรวจ recovery หลัง reload และคำถามจาก agent |
| J3 ผล action ไม่แน่นอน | preview → exact approval → dispatch → reconcile | operation ID และสถานะ uncertain; ห้ามส่งซ้ำจนทราบผล | engine มี effect records; UI/restart proof ยังไม่ครบ |
| J4 capability ขาด | Goal/Plan → readiness → ทำส่วนที่ทำได้ | บอก prerequisite เฉพาะจุด, เก็บ draft, กลับมาทำต่อ | หน้า Task บอก provider ไม่พร้อม; MCP/reviewer/media ยังไม่ครบ |
| J5 ควบคุมเครื่อง | เลือก window/tab → observation ล่าสุด → action preview → ยืนยัน → receipt | target เปลี่ยน, lock, denied, stop, uncertain | browser CDP เพิ่ม hash/approval; Windows observe/action exact gate และ durable receipt ผ่าน fixture จริง; Task link และ reconciliation UI ยังไม่มี |
| J6 คอนเทนต์ | brief → sources/claims → copy/video → preview → approval ต่อปลายทาง → upload → remote status | conflicting claim, Comfy model ขาด, token หมด, draft upload, uncertain post | ยังไม่สร้าง journey จริง |

ทุก journey ต้องตรวจ `loading`, `empty`, `error`, `denied`, `offline`, `stale`, `interrupted`, `cancelled` และทางไปต่อที่หน้าจอจริง ก่อนเรียกว่าเสร็จ

## สิ่งที่แก้และพิสูจน์ใน P1 รอบแรก

- `CompleteTask` และ `next-action` ใช้ completion report เดียวกัน: state/plan, effect ค้าง, proposal review, ทุก step/check และ criterion ล่าสุดของ revision ปัจจุบันต้องผ่านพร้อม evidence ref
- การสร้างแผนปฏิเสธ criterion ที่ไม่มี step/check รองรับ; dependency cycle เดิมมีการตรวจอยู่แล้ว
- แก้ลำดับ review ให้ proposal เปลี่ยนเป็น verified ก่อนพยายามปิด Task ซึ่งตรงกับ gate ใหม่
- หน้า Task/Plans แสดง gate รายข้อ, เหตุผล/หลักฐาน, next action และยืนยันก่อนปิดงาน; เปิด artifact หลักฐานจากรายการได้; แสดง goal และ criteria mapping; ปุ่มทางลัดมือถือไปยัง action ปัจจุบัน
- ทดสอบ `go test ./... -count=1 -timeout 180s` ผ่าน และ real Chrome ทดสอบสร้าง Goal, มือถือไปสร้างแผน, การยกเลิก/ยืนยันปิด Task ผ่าน

ผลนี้พิสูจน์เฉพาะ P1 slice ข้างต้น ยังไม่ใช่การผ่าน P0–P8 ทั้งหมด และยังไม่ได้ทดสอบการเผยแพร่จริงหรือการควบคุม Windows app

P2 รอบแรก: เพิ่มชุดจัดหน้า Workbench ห้าแบบโดยไม่เปลี่ยน Task state/สิทธิ์; การเลือกและการบันทึกตามโปรเจกต์ผ่าน real Chrome รวมหน้าจอมือถือ

P1 รอบต่อมา: Goal Contract เก็บ outcome/scope/assumptions/journeys/verification/policy; การแก้ Goal สร้าง requirement revision ใหม่และทำให้แผนเดิมใช้ไม่ได้; แผนใหม่ต้องรอ user review เมื่อ Task สร้างจาก UI, review ผูก plan hash และมี diff เทียบฉบับก่อน; Done ต้องผ่าน current-revision evidence และ user acceptance ถ้า Goal กำหนดไว้.

P4/P5 บางส่วน: browser click/type บังคับ fresh observation hash และ exact approval; Windows ใช้ UI Automation และภาพผูก handle/PID, กดปุ่ม/พิมพ์ใน fixture จริงด้วย prepare/confirm, cancel/stale/replay/restart เป็น durable receipt; วิดีโอ MP4 ขนาดเล็กที่สร้างด้วย FFmpeg นำเข้าและขอช่วง byte เพื่อ seek ได้, ไฟล์เสียถูกปฏิเสธ. Chrome จริงโหลดและเลื่อนวิดีโอได้. Video metadata เป็นการตรวจ container/codec/duration/dimension เท่านั้น; ยังไม่มีการเข้าใจภาพเคลื่อนไหวหรือเสียง.

## ลำดับต่อไป

1. ปิด P1: evidence/progress ledger, owner/reviewer policy รายละเอียด และ J1/J2 recovery จริง
2. P2–P3: workbench flow ทุกหน้า, context/MCP/Skill/team readiness พร้อม browser acceptance
3. P4–P7: Windows, video/Comfy, research/content, publishing ทีละ vertical slice โดย gate อนุมัติยังอยู่ฝั่ง server
4. P8: migration, security/race, real browser/Windows acceptance และ remote receipt สำหรับ pilot ที่ผู้ใช้อนุมัติรายชิ้น
