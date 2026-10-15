# แผนปรับ Hermetrix และผลแก้ startup — 2026-10-10

รายงานนี้เป็นช่วงตรวจเริ่มต้นก่อนผู้ใช้สั่งทำต่อ สถานะล่าสุดหลังคืน Bonsai และเพิ่ม chat controls อยู่ใน [รายงานผลทำต่อ](2026-10-10-bonsai-chat-controls.md)
## ขอบเขตตามคำยืนยันผู้ใช้

Hermetrix และ Project Brain เป็นระบบอิสระ ใช้ร่วมกันผ่าน MCP เท่านั้น ระบบทำงาน, แผน, reviewer, session/turn, approval, lease, effects, Stop/Queue/Steer และ Desktop อยู่ใน Hermetrix Project Brain เป็นบริการความรู้ร่วมและมีเจ้าของ lifecycle/curation ของตัวเอง ไม่เป็น dependency สำหรับการเปิด UI หรือสิทธิ์ลงมือทำงาน

ตรวจ `MAIN-REPAIR-PLAN.md`, rollout evidence และ source ปัจจุบัน ผู้ใช้ไม่ได้ส่งเนื้อหาแผนอื่น แต่ยืนยันให้ปรับสิ่งที่เหมาะสมภายใน Hermetrix และแยก Project Brain จึงยึดแผนแก้ระบบหลักที่พบเป็นฐานโดยไม่อ้างว่าตรวจแผนอื่นที่ยังไม่ได้รับ

รายงาน Pi แยกอยู่ใน [Project Brain audit](2026-10-10-project-brain-audit.md)

## สถานะล่าสุดและความเป็นไปได้

แผนเจ็ดขั้นทำได้เป็นช่วง ๆ โค้ดมีฐานสำหรับ local authority, no-replay, approval, cancellation ของ jobs/team/browser/desktop และ idle Task pause แล้ว แต่สิ่งเหล่านี้ยังไม่ใช่ active-chat Stop/Queue/Steer แผนต้องรักษา revision binding และ uncertain-effect reconciliation เมื่อเพิ่ม controls ใหม่

ก่อนแก้รอบนี้ 18801/18804/18806/18920/8088 ไม่ตอบ และ Docker engine named pipe ติดต่อไม่ได้ เป็นผล ณ เวลาตรวจ ไม่ใช่หลักฐานว่าโปรแกรม crash หรือ DB เสีย รายงานเดิมที่ว่า instance เหล่านี้ running จึงใช้เป็น historical checkpoint เท่านั้น

`internal/agent/service.go` ใช้ Project Brain แบบ optional read-only, timeout 5 วินาที, error แล้วแจ้ง stream event และทำต่อในเครื่อง ส่วน `internal/taskcoord/project_brain.go` ทำ best-effort เหมือนกันและไม่แก้ authoritative StepPacket ของ Task สิทธิ์และเครื่องมือไม่ได้เพิ่มจากข้อมูลความรู้ จึงไม่ต้องสร้าง coupling หรือ fallback authority ใหม่

`internal/projectbrain/stage.go`, `outbox.go`, `submit.go` มี owner-selected evidence, export approval และ idempotent inert submission อยู่แล้ว งานต่อไปต้องต่อและพิสูจน์ของเดิมผ่าน MCP ไม่สร้างฐานความรู้/curator ใน Hermetrixซ้ำ

## จุดที่แก้แล้ว: Delivery Harness เปิด UI เมื่อโมเดล offline

**Reproduction:** ตัวเปิด Delivery เดิมล้มที่ `Invoke-RestMethod http://127.0.0.1:8088/health` ก่อนเปิด server แม้มี executable และข้อมูลเดิมอยู่

**ต้นเหตุ:** launcher ใช้ inference readiness เป็น prerequisite ของ UI/history ขณะที่ server และตัวเปิดหลักอนุญาตเปิดได้เมื่อ reviewer offline เส้นทางที่ใช้เทียบคือ `start-easiiot-harness.ps1` ซึ่งตรวจ exact executable/data/workspace/reviewer และแจ้ง warning โดยไม่เลือก provider ใหม่

**การแก้:** Delivery wrapper ใช้ launcher เดิมร่วมกัน เพิ่ม optional executable path ใน launcher และใช้ workspace label ที่เป็นกลาง ไม่มีการคัดลอก implementation ซ้ำ ไม่มี model startup/fallback/MCP query ระหว่างเปิด และไม่ rebuild/stop instance ที่กำลังทำงานอัตโนมัติ หากจะ rebuild ต้องหยุดและ reconcile อย่างชัดเจนก่อน

ตัวเลือก `WorkspaceRoot`, `DataRoot`, `ExecutablePath`, `ReviewerProviderId`, `Listen`, `StartupTimeoutSec`, `CheckOnly`, `Open`, `Foreground` ระบุได้ ค่าเริ่มต้นของ Delivery ยังคงข้อมูล/ executable/ reviewer/ port เดิม และ workspace `D:\Projects\hermetrix-delivery`

## ผลที่ตรวจจริง

```text
ก่อน: powershell.exe ... -File scripts/start-delivery-harness.ps1
exit=1; Unable to connect to the remote server at model health preflight

หลัง: คำสั่งเดิมเปิด detached process PID 20260
Harness ready: http://127.0.0.1:18804/
warning: local reviewer offline; UI/history available

-CheckOnly: reuse instance เดิม ไม่มี process ใหม่
/api/health: ok=true, schema=73, expected_schema=73
protected_tables_unchanged=true; integrity=ok

go test ./scripts -count=1
ok hermetrix-harness/scripts 44.002s

go test ./internal/projectbrain ./internal/agentplatform
ok internal/projectbrain 4.856s
ok internal/agentplatform 3.112s

go test ./internal/agent -run TestProjectBrainEvidenceFlowsThroughCompilerAsUntrustedReference -count=1
ok internal/agent 2.372s
go test ./internal/taskcoord -run 'TestDurableProjectBrainReadIsExplicitlyScopedAndBestEffort|TestDurable.*Brain' -count=1
ok internal/taskcoord 2.010s
go vet ./scripts: passed

หลังแก้ workspace label:
go test ./scripts -run 'TestDeliveryLauncherOpensOfflineAndPreservesExactInstance|TestLauncherDetachedReuseAndIdentity' -count=1
ok hermetrix-harness/scripts 26.977s
PowerShell syntax: passed; main HTML HTTP200; FILE_MAP missing paths=0
```

Launcher suite มีแปด top-level tests รวม Delivery offline startup/reuse, exact reviewer/data rejection และ rebuild refusal ตรวจด้วย fake harness ใน temp directory ที่มีช่องว่าง พร้อม tests เดิมเรื่อง port ownership, timeout cleanup, missing reviewer/no fallback, CheckOnly และ invalid port

ก่อนเปิดจริงเก็บ consistent SQLite backup `.tmp-ui/plan-review-20261010/delivery-before.db`; hash receipts อยู่ `delivery-before-hashes.json` และ `delivery-after-hashes.json` เปรียบเทียบ durable_tasks 7 แถว, task_steps 58 แถว, task_user_acceptances 1 แถว, task_effect_intents 108 แถวเท่าเดิม ไม่มี Task acceptance ใหม่ Original reviewer เดิมยังเลือกอยู่และ offline

รอบนี้แก้ launcher ไม่แทนที่ serving executable/schema ไม่อ้างว่า instance18804 เป็น sourceล่าสุดทั้งชุด ไม่เปิด model หรือ self-improvement lab ไม่เปิด Discord/public deploy หรือแก้ Delivery application

## แผน Hermetrix ที่ควรเดินต่อ

| ลำดับ | ส่งมอบ | ผ่านเมื่อ |
|---|---|---|
| H0 | Startup/recovery ของรุ่นหลัก | แยก UI/model/reviewer/integration readiness; ระบุ source/binary/schema/data; cold-start/restart ทดสอบข้อมูลสำเนา ไม่ replay; UI เปิดได้ขณะโมเดล offline |
| H1 | Model และ Task ครบวงจร | explicit provider/reviewer; synthetic feature ผ่าน plan→review→apply→verify; mandatory negative check คืน preimage; receipt ผูก exact revision |
| H2 | Durable active-turn Stop | exact owner/session/turn/run; ไม่มี sampling/dispatchใหม่หลัง stop มีผล; stopping/stopped/uncertain แสดงจริง; cancel inference/approval/effect, double-stop และ restart |
| H3 | Draft/Queue | draft ไม่หายเมื่อสลับหน้า/reload; persistent queue ผูก scope/contract/provider; dequeue revalidates; cancel ก่อนเริ่ม; duplicate/restart ไม่ทำ effect ซ้ำ |
| H4 | Steer | สร้าง requirement/plan revision ใหม่ที่ boundary ชัดเจน; invalidate stale proposal/approval; ทบทวน authority ก่อน sampling/dispatch |
| H5 | Desktop/Discord จริง | Windows executor กับหน้าต่างทดสอบ, exact approval/after evidence และ stale/replay/Stop; Discord bot/channel ที่เจ้าของตั้งค่า Ready→ask→approve/deny→cancel→terminal reply |
| H6 | รวมรุ่น | preview source/build fingerprint, DB migration/rollback บนสำเนา, revision/approval/effect invariants และ combined user flow ผ่านก่อน cutover |

MCP ความรู้เป็น optional reference ของ H1 ไม่เป็น gate ของ H0/H2–H6: unavailable ต้องต่างจาก no_match; refs ที่ใช้ต้องตรง project/version, untrusted และไม่มี write authority หากแบ่งปันผล ใช้ candidate API แบบ owner-approved และให้ Project Brain curator รับรองเอง การ retry inert candidate ไม่ใช่การ retry execution effect

เพิ่ม observability ใน Hermetrix: stop latency, queue age, verification failures, proposal acceptance, uncertain effects, readiness reason และ optional MCP lookup outcomes วัด token/time ของ actual Task ก่อนปรับ context budget ไม่ใช้ short-chat success เป็น coding qualification

การเปิด detached process ยังไม่ใช่ Windows service/autostart/supervisor เมื่อจะเพิ่มต้องเลือก designated instance เดียว มี crash backoff, data-directory lock และ no-replay recovery ทดสอบก่อนติดตั้ง ไม่ติดตั้ง recurring automation จากคำขอตรวจแผนนี้

**สถานะ:** startup repair implemented และ real-runtime outcome verified; แผน H1–H6 ยังไม่เสร็จ ไม่มี full model/browser/native/Discord acceptance จากรอบนี้
