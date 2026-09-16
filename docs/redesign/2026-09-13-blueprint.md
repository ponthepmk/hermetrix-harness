# Hermetrix redesign — Working blueprint

วันที่: 2026-09-13 · สถานะ: **ข้อเสนอระหว่างเก็บ requirement — ยังไม่ใช่สเปกอนุมัติให้ implement ทั้งหมด**

อ่านคู่กับ [requirement registry](2026-09-13-requirements.md), [source baseline](2026-09-13-baseline.md) และ [Pi Knowledge Hub contract](2026-09-13-knowledge-hub.md) เอกสารนี้ไม่แทน roadmap/spec เดิมโดยอัตโนมัติ และไม่มี runtime changes ในรอบนี้

ความสามารถอนาคตที่เพิ่ม: [Realtime voice extension](2026-09-13-realtime-voice.md) — ออกแบบให้พูด/โต้ตอบ streaming และพูดแทรกได้ โดยไม่ผูก task engine กับ text chat หรือ speech provider ตัวเดียว

## 1. ทิศทางของผลิตภัณฑ์

Hermetrix ควรเป็นพื้นที่ทำงานของ developer ที่เชื่อม **ความต้องการ → แผน → ลงมือ → หลักฐาน → review → บทเรียน** เป็นงานเดียว ไม่ใช่ chat ที่เพิ่ม tools กับหน้าต่างย่อยไปเรื่อย ๆ

ข้อจำกัดที่ยืนยัน: ใช้คนเดียว, native Windows/macOS, Windows i7 Gen 12 F + RTX 5070 Ti 16 GB + RAM 64 GB, ใช้ Ollama ได้ในอนาคตแต่เริ่มด้วย remote gateway/model ที่ผู้ใช้เลือก, Mac ไม่รัน inference, เป้าหมาย local context96k ยังคงอยู่, Go/Fiber + React/Next.js + Python + PHP, workflow เพิ่มเองได้, tester/security ครอบคลุม local/staging/production และ Pi5 RAM16/M.2 ผ่าน LAN/VPN เป็น Knowledge Hub ข้าง Hermes

ลำดับที่เสนอ: coding/reproduction/review เป็นเส้นทางหลักแรก ตามด้วย browser/API customer-flow testing และ security assessment; media/research/desktop control ใช้พื้นฐานเดียวกัน ผู้ใช้ยังต้องยืนยันอันดับและ project ตัวอย่าง ไม่ได้ตัด feature ใดออกจากภาพรวม

หลักออกแบบ:

1. Requirement และผลตรวจเป็นข้อมูลที่มี revision ไม่ใช่สิ่งที่โมเดลต้องจำจาก chat ยาว
2. เปิดดูเหตุผลแบบสรุป หลักฐาน และการตัดสินใจได้ โดยไม่ต้องเก็บ hidden chain-of-thought
3. ขนาด context เป็นเพดาน ไม่ใช่เป้าหมายที่ต้องเติมให้เต็มทุกครั้ง
4. Source, state, process, artifact และ credential มีเจ้าของชัดเจน ไม่ผูกกับ pane ที่กำลังเลือก
5. การเรียนรู้ต้องมีทั้งผู้เขียน ผู้ตรวจ และผู้ใช้ในงานถัดไป พร้อมวัดผลจริง
6. Native app, terminal และ IDE ต้องผ่านการใช้งานบนทั้งสอง OS ตั้งแต่ฐานแรก
7. ใช้ของเดิมที่มีหลักฐานว่าดี; เปลี่ยนทีละเส้นทางงาน ไม่ rewrite ทั้งระบบพร้อมกัน

## 2. ขอบเขตที่ต้องแยก: client, inference, execution, data

| บทบาท | หน้าที่ | ที่ตั้งที่เสนอ / สิ่งที่ยังเปิด |
|---|---|---|
| Desktop client | UI, buffers/drafts, local history/cache, user controls | Windows และ MacBook |
| Voice session layer (future) | mic/playback, streaming turns, ASR/TTS หรือ native-audio adapters, interruption | capture/playback บน desktop; inference ที่ provider/worker ที่เลือก ไม่บังคับ Mac/Pi รัน speech model |
| Inference host | สร้างคำตอบ/แผน/review, embedding/vision ตาม capability | เริ่ม gateway.9arm.co / qwen3.8-27b-fp8 ตามผู้ใช้; Windows/Ollama เป็น local track ภายหลัง; Mac ไม่รันโมเดล |
| Execution host | checkout, terminal, debugger, browser, test/API jobs, media apps | เครื่องที่มี source/runtime/app ที่เลือก ไม่อนุมานจากที่ตั้ง LLM |
| Shared data hub | task checkpoints, knowledge, evidence catalog, device/project registry | Pi5 RAM16/M.2: ออกแบบ Knowledge Hub เพิ่มข้าง Hermes; bridge เข้า Hermes หลังทราบ API |

Mac สามารถเปิดแอปและรันโค้ดบน Mac โดยใช้ remote gateway เริ่มต้น หรือ LLM ที่ Windows ภายหลังได้ โดยต้องแสดง `Project · Execution: Mac · Model: Gateway` ให้ชัด การเลือก model endpoint ไม่ได้ให้สิทธิ์รันคำสั่งบน host นั้น ถ้าต้อง remote execution ต้องเป็นอีก capability ที่ตั้งค่าและรับรองตัวตนต่างหาก

### 2.1 Raspberry Pi + Hermes: integration ที่เสนอ

ใช้ MCP เป็น façade สำหรับข้อมูล/ความรู้ เช่นค้น task, ดึงบทเรียน, เขียน evidence manifest แต่ให้ sync ที่จำเป็นต่อความถูกต้องเป็น deterministic application service ไม่ฝากให้ LLM จำเรียก `save_memory` เอง

ผู้ใช้ขอให้วาง MCP tools ใหม่ได้ จึงเสนอ service `Knowledge Hub` ที่ติดตั้งข้าง Hermes โดยไม่แก้/แทน service เดิม ความเข้ากันได้กับ Hermes เป็น adapter ที่ต้องตรวจภายหลัง รายละเอียด tool contracts/auth/installation gates อยู่ในเอกสาร Hub ไม่จำเป็นต้องรอ Hermes มี feature ครบจึงออกแบบ domain ของเราได้

MCP นิยามการเชื่อม tools/resources/prompts และการเจรจาความสามารถ ไม่ใช่ database replication protocol งาน sync ต้องกำหนด contract เพิ่มด้านล่าง และตรวจตาม Hermes ที่ติดตั้งจริง ไม่ถือว่า server รองรับ optional features ทุกตัว ดู [MCP specification](https://modelcontextprotocol.io/specification/2026-07-28)

ขอบเขตเก็บข้อมูลที่เสนอ:

- Pi: project/device registry, versioned requirement/plan/checkpoint, incident/lesson metadata, artifact hashes/locations, sync cursors และ audit records
- Windows/Mac: local transactional store + pending sync outbox, document drafts, working copy, installed tools, local execution receipts และ cache
- วิดีโอ/trace ขนาดใหญ่: managed asset store บน disk ที่เลือกหรือ external storage; Pi เก็บ catalog ได้โดยไม่ต้องถือเนื้อไฟล์ทั้งหมด
- Credentials, browser cookies, OS permissions, live PTY/debug handles และ executable approval grants: ไม่ sync เป็นข้อมูลทั่วไป
- Source code: ใช้ Git/explicit patch handoff ไม่ใช้ knowledge sync คัดลอก working tree เงียบ ๆ

ต้องตรวจ Hermes ก่อนเลือก implementation ว่ารองรับสิ่งใดแล้ว:

1. Stable IDs, schema version, project/customer scope และ authenticated device identity
2. Durable write acknowledgement, operation ID สำหรับ deduplication และอ่านผลเดิมได้เมื่อ response หาย
3. Mutable record ใช้ expected revision/compare-and-set; conflict ต้องแสดง ไม่ blind last-write-wins
4. Immutable evidence append-by-ID; manifest ตรวจ hash/size และรู้ว่า assets ดาวน์โหลดครบหรือยัง
5. Incremental sync cursor, pagination, backpressure, retry เฉพาะ operation ที่มี safe dedup contract
6. Delete tombstones, derived-data invalidation, retention และ backup/restore ที่ทดสอบจริง
7. Project-scope filtering ก่อน search และก่อนโหลดเนื้อหา; sanitized general lesson ต้องยกระดับ scope อย่างชัดเจน

ถ้า Hermes มีเพียง semantic memory search จะให้ทำหน้าที่นั้นก่อน ไม่อ้างว่าเป็น task database ครบ หากต้องเพิ่ม bridge ให้เป็น module เล็กที่ใช้ contract นี้ และเลือกว่าจะเก็บ authoritative records ใน Hermes หรือ store ข้างเคียงหลัง audit ไม่สร้างสำเนาความจริงสองชุดที่แก้พร้อมกันได้โดยไม่มี owner

**Offline proposal:** บันทึกงานใหม่เฉพาะเครื่องได้ คิว sync ไว้และแสดงสถานะ `local / pending / synced / conflict` แต่ห้ามสองเครื่องรับช่วง task เดียวกันอัตโนมัติในช่วง partition งานที่ Pi รับรอง owner ไม่ได้ให้ทำเป็น task fork หรือรอ handoff ตาม policy ที่ผู้ใช้เลือก ความพร้อมทำงาน offline ไม่เท่ากับรับประกัน shared ownership ได้ระหว่าง offline

Pi เป็น dependency ของ shared sync ไม่ควรเป็น dependency ของทุก keystroke/model token ต้องมี retry backoff, health panel และ backup ไปคนละ failure domain ส่วน LAN/VPN/TLS, device enrollment และ disk encryption ยังรอข้อมูล network/storage ของ Pi; ไม่เปิด raw unauthenticated MCP/LLM endpoint สู่ public internet

### 2.2 Project/task portability

`ProjectID` เป็น identity ถาวร แยกจาก `DeviceCheckout{device, checkout, path, repository, branch/worktree}` เช่น path Windows กับ Mac ต่างกันแต่แทน project เดียวกันได้ ต้องจัดการ case sensitivity, separators, CRLF/LF, Unicode paths และ symlink containment ราย OS

Handoff ส่ง checkpoint พร้อม source revision, dirty patch ที่ยินยอมส่ง, required capabilities, evidence availability และ pending/uncertain effects ปลายทางตรวจทั้งหมดแล้วสร้าง run ใหม่ งานหนึ่งมี active execution owner เดียว ใช้ lease/fencing ในกรณีมี coordinator และห้าม stale owner สร้าง effect ใหม่ ไม่อ้างว่าย้าย process/debug session จาก transcript ได้

Fencing ต้องตรวจที่ host/effect admission boundary ไม่ใช่แค่ Pi database และ late callbacks ห้าม advance current run ถ้า external target/tool ไม่รองรับ fencing/idempotency ต้องยืนยันหยุดหรือ reconcile งานเก่าก่อน handoff รันต่อ มิฉะนั้นแสดง blocked การ fork task offline ก็ไม่ให้สิทธิ์แก้ external target เดียวกันพร้อมกันโดยอัตโนมัติ ต้องยึด resource ownership แยกจาก TaskID

## 3. โครงสร้างโปรแกรม: modular core ก่อน distributed services

ข้อเสนอคือเก็บ Go application core และ local SQLite ต่อ device แล้วแยกขอบเขตเป็น modules ที่มี typed contracts ยังไม่แยกทุก module เป็น microservice จำนวน packages ไม่ใช่เป้าหมาย; ownership และ dependency direction สำคัญกว่า

| ขอบเขต | เจ้าของข้อมูล/การทำงาน | ไม่ควรทำ |
|---|---|---|
| Desktop shell | window/menu/dialog, clipboard, file drop, lifecycle, update integration | ตัดสิน task policy หรือประกอบ shell commands |
| UI workbench | typed state, pane/tab layout, interaction, status | เป็นเจ้าของ process หรือเก็บความจริงของ task เพียงที่เดียว |
| Application services | project, task, requirements, documents, evidence, knowledge use cases | import UI/DOM |
| Orchestration | plan/run/step state machine, checkpoints, scheduler, bounded context | ถือ global current project/session เพื่อ dispatch effects |
| Host runtime | Unix PTY/ConPTY, managed processes, filesystem/watch, runtime discovery, permissions | อนุมานสิทธิ์จาก cwd หรือผู้ใช้เลือก pane |
| Capability adapters | providers, MCP, LSP, DAP, browser/API, media/computer control | ข้าม run/policy/receipt contract |
| Persistence/sync | transactions/outbox, blobs/index, migration, Hermes bridge | ให้ embedding index เป็น source of truth |

UI เสนอ TypeScript + component boundaries + explicit state/actions แทน `app.js` ที่ rerender หลายส่วนรวมกัน ให้เลือก framework ตามทีม/maintainability ใน ADR ไม่เลือกเพราะหน้าตาอย่างเดียว API versioning และ contract tests ต้องใช้ร่วม native bridge/headless service เพื่อไม่เกิด behavior สองชุด

### Native shell decision spike

Wails เป็น candidate แรกเพราะทำ desktop ด้วย Go + web และรองรับ Windows/macOS; ยังไม่ล็อกจนกว่าจะผ่าน terminal/editor/IME/lifecycle spike บนทั้งสองเครื่อง ดู [Wails introduction](https://wails.io/docs/introduction/)

ต้องพิสูจน์ packaged assets/CSP และ runtime styles, Thai IME, clipboard shortcuts, file dialogs/drop, scaling, suspend/reopen, streaming/backpressure และ versioned native bridge หากไม่ผ่านค่อยเปรียบเทียบ shell อื่นโดยใช้ tests เดียวกัน ไม่เปลี่ยน core contract ตาม framework

คำว่า native ในข้อเสนอนี้หมายถึง installable desktop app ที่มี OS integration และใช้ web renderer ได้ ไม่ได้หมายถึงเขียนทุก control ใหม่ด้วย SwiftUI/WinUI ต้องยืนยันเพิ่มเติมหากผู้ใช้ต้องการ native widgets ทั้งหมด Signing/notarization/update channel, minimum OS และ offline installer เป็น release decisions ที่ต้องเก็บเพิ่ม

## 4. UX ที่เน้นงาน ไม่บังคับดูข้อมูลเทคนิคตลอดเวลา

Navigation หลัก: Projects/Tasks, Workspace, Runs & Findings, Knowledge, Connections/Settings เมนูย่อยพับได้ sidebar เลื่อนแยกจากเนื้อหา; user ไม่ต้องเลือก project ซ้ำในทุก pane

หนึ่ง task มีมุมมอง Brief/Plan, Chat, Code, Changes, Debug, Terminal, Browser, Tests, Evidence, Report เลือกเปิดเฉพาะที่ใช้ `Review` ควรหมายถึง code/requirements findings; session contract ย้ายไป inspector ไม่กินพื้นที่หลัก

Workspace ใช้ persistent split-tree: node เป็น split แนวนอน/แนวตั้งหรือ tab group จึงจัดบนสองล่างหนึ่ง/บนหนึ่งล่างสอง/สี่ช่องได้ ลาก tab/header มี drop target preview, resize handles และ keyboard move/split/reset layout ปิด pane ไม่ปิด process และย้าย pane ไม่ทำ buffer/undo หาย; มุมมอง document เดียวกันอ้าง buffer เดียว ไม่ clone เป็นไฟล์คนละฉบับ

หน้าเริ่ม task เลือกชนิดงานพร้อม brief template: Build / Fix bug / Review / Test flow / Security / Media / Research ใช้ความต่างนี้เลือก input/checklist ไม่สร้าง engine แยกกันทุกชนิด รายละเอียด model/permissions/context แสดงเป็น status สั้น ๆ และเปิดเพิ่มได้

Workflow เพิ่มเองได้ผ่าน versioned definition: input form/schema, steps/dependencies, required capabilities, expected artifacts, validation checks, pause points และ budgets แยก workflow definition จาก run instance รองรับทั้ง deterministic step และ LLM step; validate graph/types ก่อนรัน การ import workflow ไม่ติดตั้ง executable/MCP หรือเพิ่ม permissions โดยอัตโนมัติ รุ่นแรกใช้ template/form editor ก่อนตัดสินว่าจะทำ visual flow builder เต็มรูปแบบ

เกณฑ์ผ่าน extension: สร้าง/แก้ template และ rerun ด้วย input ใหม่ได้จาก UI/config โดยไม่แก้ application source, report template อ้าง check/evidence IDs ได้, schema/version/graph errors อธิบายวิธีแก้ และ run เก่ายังอ้าง definition revision เดิม

Acceptance ด้าน UX: ไม่มีเมนูเข้าไม่ถึงเมื่อ window เตี้ย, ทุก pane มี scroll/min-size ที่ถูก, keyboard focus เห็นชัด, readable placeholder/contrast, IME ไทยไม่ส่งก่อน composition จบ, error อยู่ใกล้ action พร้อม recovery ไม่หายไปใน toast อย่างเดียว ต้องทดสอบ screenshot + interaction +ข้อมูลจริง ไม่รับ test ว่ามี DOM/สีต่างจากพื้นหลังเพียงอย่างเดียว

## 5. IDE และ terminal ที่ใช้ทำงานจริง

### Document service

เปิดไฟล์จาก project checkout ปัจจุบัน; tree มี pagination/virtualization/search และแสดงรายการที่โหลดไม่ครบ มี tabs, dirty markers, undo/redo, selection/scroll restore, autosaved drafts และ recover หลัง crash

Buffer มี document version; save/AI patch/format ต้อง compare กับ disk hash + buffer version ไม่ overwrite สิ่งที่ user พิมพ์ระหว่างรอ เมื่อ external edit ชนให้ merge/reload/keep draft อย่างชัดเจน จัดการ encoding, line endings, read-only, binary และ large files แยกจาก text editor

### Language, debug และ changes services

- LSP สำหรับ diagnostics, completion, definition/references, symbols, rename และ formatting ตาม capability ของ server ดู [LSP](https://microsoft.github.io/language-server-protocol/)
- DAP สำหรับ launch/attach, breakpoint, pause/step/continue, stack, variables/watch และ exception state ดู [DAP](https://microsoft.github.io/debug-adapter-protocol/)
- Git service สำหรับ status, selected base revision, diff hunks, worktree, conflict และ review anchors; stage/commit/publish ไม่เกิดจากการเปิด review
- Run/Test/Format/Debug ใช้ `RunConfiguration{checkout, revision, executable, argv, cwd, envRef, runtimeVersion, inputs}` ผ่าน managed execution ไม่ส่ง string เข้า human terminal ที่อาจกำลังรันอย่างอื่น
- Format รอผลสำเร็จจริงและตรวจ document version; run panel แสดง exit code/stdout/stderr/artifacts/diagnostics ของ run นั้น

เป้าหมายภาษาที่ขอ: Go/Fiber, React/Next.js, Python และ PHP เสนอพิสูจน์ end-to-end ด้วย Go/Fiber + Next.js ก่อน แล้วเพิ่ม Python/PHP บน contracts เดียวกัน ทั้งหมดยังอยู่ใน scope แต่ลำดับต้องยืนยันจาก repo จริง ไม่ประกาศ feature parity ของทุกภาษาเพียงเพราะมี LSP/DAP interfaces สิ่งที่ต้องส่งมอบรวม runtime discovery, installation policy, version pinning, per-project configs และ tests ของ adapter นั้น

ทั้งหมดเป็น stacks ของ **customer projects** ไม่ใช่คำสั่งให้เปลี่ยน backend Hermetrix เป็น Fiber หรือ desktop UI เป็น Next.js Readiness matrix ต้องส่งพร้อม release:

| Project stack | Edit/LSP + Format | Run/Test | Debug acceptance | สถานะ redesign |
|---|---|---|---|---|
| Go/Fiber | semantic navigation/diagnostics + Go formatter | package tests + configured API server | breakpoint/request handler/stack/variables | planned; ยังไม่ทดสอบ |
| React/Next.js | TS/JS/TSX project intelligence + project formatter | dev/build/test scripts ตาม repo | server และ browser/client debug configs แยก | planned; ยังไม่ทดสอบ |
| Python | project environment-aware LSP + formatter | selected venv/runtime/test config | breakpoint/step/variables ใน environment เดียวกับ run | planned; ยังไม่ทดสอบ |
| PHP | project/runtime-aware LSP + formatter | configured server/test runner | debug adapter + debugger extension/server path mapping | planned; ยังไม่ทดสอบ |

รายชื่อ server/adapter/version เลือกหลังสำรวจ repo และทดสอบ licenses/installability; IDE slice ที่ผ่านสอง stacks ยังไม่เรียกว่า complete IDE release ของทั้งสี่

### Terminal

เปิดแล้วได้ shell ที่ project root ทันที advanced options ซ่อนไว้ มี raw interactive input, ANSI/cursor, selection/copy, paste, scrollback, resize, Ctrl-C, Unicode และ multiple sessions

Windows ใช้ ConPTY; Mac ใช้ Unix PTY และทดสอบ semantics ร่วม ไม่เอา command form มาทดแทน terminal ดู [Windows pseudoconsole](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session)

Host runtime ต้องควบคุม owned process tree และ drain I/O ระหว่าง cancel/close; Windows ประเมิน Job Objects, Mac ใช้ process group/lifecycle ที่เหมาะสม การปิด pane, ปิด window, หยุด run และออกจาก app เป็นคนละ event หลัง restart แสดง terminated/interrupted อย่างตรงไปตรงมา ไม่เรียก transcript ว่า terminal ที่ยังรันอยู่

## 6. Task engine และ requirement-driven coding

ข้อมูลหลักที่เสนอ:

| Record | สิ่งที่ต้องรักษา |
|---|---|
| Task / RequirementRevision | objective, original request, constraints, unknowns, stable criteria IDs และสิ่งที่ถูก supersede |
| PlanRevision / Step | dependencies, bounded input, expected output, checks, effect scope, status และเหตุผล replan แบบสรุป |
| Run / StepAttempt | task/plan/source revision, device/model/tool fingerprints, resource budget, timings และ stop reason |
| EffectIntent / Receipt | operation ID, action/target, expected state, authority, observed result และ uncertainty |
| Checkpoint | completed/pending steps, evidence refs, unresolved effects, next action และ resume prerequisites |
| Validation / Finding | requirement/check ID, exact subject revision, expected/actual, pass/fail/blocked/unknown, evidence, severity/confidence |
| ArtifactManifest | content hash, type/size, producer, project/sensitivity, location/availability และ retention |

Task states เสนอ `draft → ready → running → waiting_for_input / paused / verifying → completed / failed / cancelled` โดย validation failure เปิด revision/replan ได้ ไม่เขียนทับประวัติเดิม Effect state มี `planned / dispatched / observed / uncertain / reconciled` แยกจากสถานะ task

Crash หลัง tool ทำ side effect แต่ก่อนตอบต้อง reconcile ด้วย operation ID/result lookup หรือถามผู้ใช้เมื่อพิสูจน์ไม่ได้ ไม่ retry arbitrary effect อัตโนมัติ ไม่สัญญา exactly-once กับ external tool ที่ไม่มี idempotency contract

Ordering contract: persist intent + attempt identity → dispatch → persist observed receipt → validate → advance step/checkpoint ด้วย expected revision โดย intent/initial attempt และ receipt/step transitions ที่ต้อง atomic อยู่ใน transaction เดียวตาม boundary ของ store Tool call ไม่อยู่ใน DB transaction ข้าม network ขาด receipt หลัง dispatch = uncertain ไม่ใช่ unexecuted แม้ client timeout Late receipt เก็บเป็น evidence แต่ไม่ advance attempt ใหม่ ต้อง inject crash ระหว่างทุก boundary ใน tests

### Golden workflow A: Build / fix / review

1. Intake: บันทึกโจทย์จริงและข้อกำหนดที่ห้ามหลุด แยกความเข้าใจที่ยืนยันกับที่ AI อนุมาน
2. Baseline: ระบุ repo/commit/dirty state, สำรวจระบบและรันทดสอบเกี่ยวข้อง; pre-existing failure ไม่ถูกนับเป็นผลของ patch ใหม่
3. Plan: แต่ละ change ผูก requirement/check, อธิบายผลกระทบ/rollback และสิ่งที่ยังต้องรู้
4. Implement: ใช้ isolated worktree เมื่อเหมาะสม; จำกัด change scope และสร้าง checkpoint หลังงานย่อย
5. Verify: targeted tests → integration → customer flow ตามความเสี่ยง; exit 0 ไม่พอถ้า assertion ไม่ตรวจ outcome
6. Review: fresh bounded context มี requirements + final diff + surrounding code + checks; findings อ้าง revision/line, mechanism, impact, evidence และวิธีแก้
7. Resolve: แก้ finding ที่รับ → rerun check → report requirement coverage; review-only task หยุดที่รายงาน ไม่แก้เอง
8. Learn: เก็บเฉพาะข้อสรุปที่มีหลักฐานตาม scope และมีวิธีตรวจว่าใช้ต่อได้

Reviewer อีก pass จาก model เดิมอาจจับข้อผิดพลาดเพิ่ม แต่ไม่ใช่การรับรองอิสระหรือการรับประกันความถูกต้อง ต้องใช้ explicit requirements, deterministic validators และ user acceptance ร่วมกัน

### Golden workflow B: Customer bug → reproduce → fix

เก็บ original report, affected version/environment, roles/data, steps, expected/actual, attachments และ reproducibility แยก `not reproduced / intermittent / reproduced / environment blocked` ให้ชัด

สร้าง hypotheses พร้อม supporting/contradicting evidence และ next discriminating experiment สาเหตุยังเป็น hypothesis จนพิสูจน์ได้ เก็บ failure ก่อนแก้ → regression case → fix → ตรวจเคสเดิมและผลข้างเคียง ถ้า reproduce ไม่ได้ให้รายงานสิ่งที่ลองและข้อมูลที่ขาด ไม่แต่ง root cause

Incident = เหตุการณ์ที่พบ; Defect = ปัญหาที่รวบรวมและยืนยันจาก incident ได้ อาการคล้ายกันไม่ merge เป็นบั๊กเดียวโดยอัตโนมัติ แยก product defect, fixture/tester error, environment failure และ harness/model failure เพื่อไม่เรียนรู้วิธีแก้ผิดประเภท

## 7. AI tester, security และ research

### Customer-flow tester

Scenario มี requirement revision, environment, actors/test accounts, fixtures/reset, steps, assertions และ cleanup Browser/API ใช้ scenario เดียวกันได้ เช่นสร้างข้อมูลผ่าน API →แก้ผ่าน UI →ตรวจ persisted state ผ่าน API

ผู้ใช้ยืนยัน local/staging/production: ต้องแสดง environment ตลอด run, แยก credentials/fixtures, target allowlist และ mutation budget Production preset เสนอเริ่มด้วย read-only/passive หรือ synthetic test accounts/data ที่ตกลง และต้องมี explicit scope สำหรับ action ที่เปลี่ยนข้อมูล/เพิ่มโหลด; ห้ามนำ staging reset/cleanup recipe ไปใช้ production อัตโนมัติ เปิด production support ไม่เท่ากับอนุญาตสแกน/เปลี่ยนข้อมูล production ใดในรอบออกแบบนี้

เกณฑ์ผ่าน: เปลี่ยน URL ของ staging scenario ให้ชี้ production แล้ว reset/cleanup helper ต้องถูกปฏิเสธ หากไม่มี production-specific authorized configuration และ cleanup จำกัดเฉพาะข้อมูลที่ run เป็นเจ้าของ

LLM ออกแบบ/สำรวจ flow ได้ แต่ expectation ต้องมาจาก requirement/schema/approved example ไม่ใช้ AI ตัดสินว่าทุกอย่างถูกจากสมมติฐานของตัวเอง เก็บขั้นตอนที่ rerun ได้เป็น structured scenario หรือ test code; exploration trace อย่างเดียวไม่เรียก regression test

Evidence รวม checkpoint screenshots/DOM, console/network, redacted API request/response, timing และ assertion results พร้อม source/build/env fingerprint Report แสดง passed/failed/blocked/flaky/not-tested และสร้าง defect/fix task ได้ โดยไม่ทำให้ failed assertion กลายเป็น pass เพราะ browser click สำเร็จ

### Security assessment

เป็น workflow สำหรับระบบที่ได้รับอนุญาต: assessment scope ต้องมี targets/environment, authorization reference, methods, credential profile, rate/concurrency, data changes ที่อนุญาต, stop conditions และที่ส่งรายงาน Discovery พบโดเมนใหม่ไม่ขยายขอบเขตเอง

เลือก static code/dependency/secret analysis, local proof และ active browser/API tests ตามระบบจริง จัด coverage ตาม threat model และ test catalog ที่ pin version เช่น [OWASP WSTG](https://owasp.org/www-project-web-security-testing-guide/) ไม่ใช้จำนวน payload หรือคำว่า “hack ทุกอย่าง” เป็นเกณฑ์ผ่าน

Report แยก confirmed exploitability, suspicious pattern, false positive และสิ่งที่ยังไม่ทดสอบ พร้อม affected revision, minimal reproduction/evidence ที่ redacted, impact, remediation และ retest ป้องกันส่ง customer secrets/payloads เข้า shared general memory; รายงานไม่อ้างว่าระบบปลอดภัยสมบูรณ์

### Research

เก็บ query/question, source URL/document identity, fetched/published dates เท่าที่มี, claim/evidence links, conflicting evidence และข้อจำกัด Retrieval/compaction ต้องรักษาที่มา เนื้อหาจากเว็บ/MCP/docs เป็น untrusted data ไม่ได้เพิ่มสิทธิ์หรือเปลี่ยน task requirements

## 8. Local LLM 96k และ provider strategy

### Remote-first ตอนนี้, local96k เป็น qualification track แยก

Provider เริ่มต้นที่ยืนยันคือ `https://gateway.9arm.co` model `qwen3.8-27b-fp8` ใช้ได้ในเชิงแบบระบบและ existing custom-provider architecture แต่ยังไม่ได้ตรวจ endpoint path/authenticated availability, advertised limits หรือ task quality ในรอบนี้ ไม่เติม `/v1` หรือรับรอง96k จากชื่อโมเดล ต้องทำ bounded smoke/behavior tests เมื่อมี credential ที่จัดเก็บปลอดภัยและ scope งานจริงชัดเจน

Mac จึงไม่ต้องโหลดโมเดล และ remote coding release ไม่ถูก block ด้วยการยัง benchmark local96k ไม่เสร็จ การเลือก gateway อนุญาต provider ตั้งต้น แต่ยังต้องกำหนดว่าข้อมูลลูกค้าใดส่งออกได้ ไม่มี API key ในเอกสาร/config version control/knowledge; credential ที่ถูกส่งในแชทควร rotate แล้วลง secret store

### Local runtime baseline ภายหลัง

ยังไม่มีหลักฐานว่าต้องเปลี่ยน Ollama ให้ benchmark โมเดลเดียว/quantization เทียบ workload เดียวก่อน Context ที่ตั้งสูงขึ้นใช้ memory เพิ่ม และดู actual allocation/offloading ได้ด้วย `ollama ps` ตาม [Ollama context documentation](https://docs.ollama.com/context-length)

llama.cpp เป็นทางเลือกสำหรับทดสอบ runtime ที่ปรับแต่งได้ มี OpenAI-compatible server, CUDA/Metal และ CPU/GPU hybrid แต่ไม่ได้แปลว่าจะเร็วกว่า Ollama บนเครื่องนี้โดยอัตโนมัติ ดู [llama.cpp](https://github.com/ggml-org/llama.cpp) ยังไม่เลือก model size/quantization หรือสั่ง download จากชื่อ GPU อย่างเดียว

VRAM 16 GB ต้องแบ่งให้ weights + KV cache + runtime/compute buffers; RAM 64 GB ไม่ได้กลายเป็น VRAM ความเร็วเท่ากัน ยังไม่รับรอง 96k จนรู้ model/tag/quantization, backend/version, allocation, offload และ latency ที่ผู้ใช้รับได้ Mac capability รอ chip/RAM

### Capacity และ context compilation

เก็บ `advertised maximum`, `verified runtime allocation`, `qualified task envelope`, `normal working target`, `max output/reasoning` แยกกัน ใช้ registry เดียว โดย `extended-96k` หมายถึง 98,304 tokens ไม่แปลงเป็น 128k เพื่อผ่าน guard

Compiler ต้องรักษา `actual serialized input + requested output/reasoning + safety reserve <= allowed capacity` โดยคิด tool schemas/templates/transport และ multimodal tokens ตาม provider ไม่ซ้ำและไม่ลืม hidden overhead ที่นับได้ Unknown overhead ต้องมี conservative bound หรือจำกัดสถานะ qualification ไม่ถือว่าเป็นศูนย์โดยไม่มีหลักฐาน

ตัวอย่างเดิมด้านล่างเป็น **ภาพประกอบก่อนปิด decision เท่านั้น** เมื่อ C = 96,000; production registry ปัจจุบันใช้ 98,304 และ budget จริงอยู่ใน `internal/context/profile.go`/ตาราง Architecture:

| ส่วน | งบตัวอย่าง tokens |
|---|---:|
| System/policy + task/requirements/plan + selected tool schemas | 12,000 |
| Recent working history | 18,000 |
| Active code/document slices | 22,000 |
| Retrieved lessons/project facts | 8,000 |
| Bounded test/tool evidence | 10,000 |
| Output รวม reasoning ตาม semantics ของ provider | 12,000 |
| Token estimate/serialization safety | 6,000 |
| Headroom สำหรับผล tool รอบถัดไป | 8,000 |
| รวม | 96,000 |

Headroom ไม่ใช่ output reserve อีกชุด และเมื่อ tool result มาแล้วต้อง recompile ก่อน model call ถัดไป ห้ามใช้ตารางนี้เป็น production preset โดยไม่วัด Normal work ควรใช้ packet สั้นตามงาน เช่นทดลองช่วง 24–56k ใน benchmark และขยายเมื่อคุณภาพดีขึ้นจริง ไม่เติม dummy context ให้เต็ม

Retrieval: scope/validity filter → lexical/symbol search → optional embeddings → rank/dedup → bounded evidence hydration เก็บ full logs/source/media นอก prompt; โหลดเฉพาะ chunk พร้อม ref Requirements/plan/checkpoint เป็น authoritative structured state ไม่ปล่อย summary แทนจนข้อห้ามหาย

ถ้า brief/requirements ใหญ่เกิน budget: เก็บฉบับเต็มแบบ immutable, แยก task-wide invariants กับ active-step requirement/dependency IDs แล้วตรวจ constraint coverage ก่อนสร้าง packet การเก็บครบใน DB ไม่ได้แปลว่าโมเดลเห็นครบ หาก mandatory constraints ยังใส่ไม่พอให้ split/replan หรือรายงาน overflow ห้ามตัดทิ้งเงียบ ๆ Tests ต้องมี oversized brief, contradictory/superseded updates และ resume หลัง compaction หลายรอบ

### Scheduling และ custom providers

แต่ละ provider profile มี protocol adapter, endpoint/model/credential reference, capabilities, tokenizer/budget evidence, health, cost/privacy policy และ resource-group identity endpoint คนละ URL อาจใช้ GPU เดียวกัน ต้องตั้งกลุ่มได้

Windows local generation เสนอเริ่ม concurrency 1; planner/coder/reviewer/learning เข้า queue เดียวตาม resource group ปล่อย generation slot ขณะรอ tool งาน background เรียนรู้มี budget และ yield เมื่อ interactive ต้องการ ไม่ทำหลาย agent พร้อมกันโดยสมมติว่า VRAM เพียงพอ

Queue ต้อง bounded พร้อม fairness/aging, queue timeout แยก execution timeout และ per-job budgets Gateway ใช้ explicit adjustable admission/rate limits จากข้อมูลที่ได้ ไม่อนุมาน GPU ของ server การ cancel stream ฝั่ง client ไม่พิสูจน์ว่า remote generation หยุดหรือคืนทรัพยากร ต้องบันทึก cancellation acknowledgement/result lookup เท่าที่ provider รองรับ และไม่อ้าง preemption สำเร็จหากสถานะ upstream unknown

Provider switch ต้องเปิดเผยและบันทึก run binding ใหม่; model fallback กับ tool-effect retry เป็นคนละเรื่อง ห้ามเปลี่ยนไป cloud/ส่งข้อมูลลูกค้าเงียบ ๆ Remote gateway ที่ไม่มี host telemetry ใช้ black-box task/context qualification และแสดง runtime allocation ว่า unknown ได้ ไม่ block การใช้งานจากการขาดสิทธิ์ probe ส่วนการอ้างว่า verified local/remote-owned runtime allocation ต้องใช้ trusted host probe/fingerprint ไม่รับรองจาก model list หรือ context ที่ประกาศเท่านั้น

## 9. Learning ที่ตรวจว่าช่วยจริง

แยก knowledge types: user preference, project fact, episodic incident, diagnostic procedure, reusable test, validated fix pattern และ executable skill ไม่บีบทุกอย่างเป็น skill หรือยัด chat ทั้งหมดเข้า vector database

Pipeline ที่เสนอ:

1. Observe: task/tool/model/user-correction events บันทึก transactionally พร้อม failure cause ไม่พึ่ง skill ถูก activate ก่อน
2. Normalize: incident + expected/actual + env/revision + bounded evidence; redaction ก่อนส่ง model หรือ sync
3. Propose: lesson/counterexample/applicability/expiry และข้อสรุปที่ยังไม่แน่ใจ
4. Validate: schema/provenance + targeted replay + held-out workflow checks; new lesson/skill และ improve ใช้ gate ที่เหมาะกับความเสี่ยงเหมือนกัน
5. Promote: authority ตาม policy ผู้ใช้; provisional ไม่ถูกเสิร์ฟเป็นข้อเท็จจริงที่พิสูจน์แล้ว และ lesson ไม่สร้าง permissions ใหม่
6. Retrieve: filter customer/project/revision/validity **ก่อน** search และก่อนอ่านเนื้อหา ให้เหตุผลสั้น ๆ ว่าบทเรียนใดเกี่ยวกับงานนี้
7. Measure: task/check outcomes, recurrence, false positives, latency/tokens และ user corrections เทียบ baseline ที่ไม่ใช้ lesson
8. Retire: supersede/withdraw/revalidate เมื่อเงื่อนไขเปลี่ยน; delete ต้อง invalidate derived indexes และ descendants ตาม lineage

อย่าใช้ `exit 0`, assistant กล่าวว่าเสร็จ หรือ skill ถูกโหลด เป็นหลักฐาน causality ของความสำเร็จ Evidence ที่แข็งกว่าคือ reproduce ก่อนแก้ + regression หลังแก้ + requirement outcome เมื่อทำได้ Negative evidence ต้องเก็บเงื่อนไขที่ทำให้ล้มเหลว ไม่สร้างข้อห้ามสากลจาก transient network error ครั้งเดียว

บทเรียนข้ามลูกค้าเสนอให้แชร์ได้เฉพาะ sanitized general procedures ที่ผู้ใช้กำหนด policy; facts/source/evidence อยู่ project/customer scope ส่วน fine-tuning/LoRA เป็นงานแยกในอนาคตหากผู้ใช้ต้องการ ต้องมีสิทธิ์ข้อมูล, curated dataset, held-out eval และ rollback ไม่ถือว่าอนุมัติจากคำว่า “เรียนรู้” อย่างเดียว

## 10. MCP, media และ computer control

Capability manifest ระบุ version, schemas, host requirements, data/effect scope, readiness, cancellation, idempotency/result lookup, progress, output/artifact types และ limits ตัว model เห็นเฉพาะ tools ที่จำเป็นและ hydrate schema ก่อนใช้

MCP transport ต้อง bounded ทุกช่วง: admission/lock, start, write, frame read, decode, callback, cancel และ shutdown; immutable request identity, stderr/diagnostics ที่ redacted, hostile server tests และ no automatic side-effect replay Optional async-task support ต้องตรวจ server จริง หรือ adapter สร้าง managed job handle โดยไม่อ้างว่าโปรโตคอลรองรับสิ่งที่ไม่มี

### Media workflow

Source clips → streamed ingest/metadata/proxy/transcript → structured edit plan ที่ผูก asset/time ranges → preview → app-specific MCP/adapter job → export → ตรวจ duration/codec/resolution/audio/edits + human creative acceptance

Asset store ต้องรองรับ streaming/range reads, resumable transfer ตามที่เลือก, byte quotas, hash verification และ missing-local-asset state ไม่ส่ง multi-GB video ผ่าน JSON/base64/context Pi เก็บ metadata/catalog ก่อน; render ใช้ Windows/Mac ที่มี app/codec/GPU เหมาะสม

MCP เป็นตัวเชื่อม ไม่ได้ทำให้ทุก video editor มี timeline API เหมือนกัน รอ Q11–Q12 ก่อนเลือก adapter/app การสร้าง NLE เต็มตัวใน Hermetrix ไม่ใช่ default scope แต่ต้องมี preview/report และ export artifacts ใช้งานได้

### Computer control

Browser automation กับ native desktop control เป็นคนละ capability Native ต้องตรวจ target app/window, fresh observation, permissions และ stop เมื่อ user เปลี่ยน target/lock screen มีปุ่มหยุดที่เข้าถึงได้ทันที ไม่ให้ stale screenshot action ไปทำงานผิดแอป

Policy ต้องแยก read/write/network/credential/desktop input โดย capability และ host; Unix process group/Windows Job Objects ช่วย lifecycle ไม่ใช่ sandbox และ MCP child/human PTY ไม่ได้ปลอดภัยเพียงเพราะ agent command runner มี sandbox

### Realtime voice (future extension)

เพิ่ม input/output modality ผ่าน Voice Session Service ไม่สร้าง task engine อีกชุด เผื่อทั้ง streaming ASR → task/LLM → streaming TTS และ native speech-to-speech adapter ที่ยืนยัน capability แล้ว Text gateway ปัจจุบันไม่ได้พิสูจน์ว่ารับ/ส่งเสียงได้ ต้องเลือก voice provider แยกได้

Voice session ผูก device/project/task/turn/revision ชัดเจน Partial transcript ใช้แสดงระหว่างพูด ไม่ dispatch side effects; finalized instruction เข้ากระบวนการ requirement/plan/policy เดิม Phrasing ระหว่างรอ tool ต้องอิง run status ไม่พูดว่าเสร็จก่อน evidence

พูดแทรกให้หยุด playback/ยกเลิกคำตอบเก่าและ discard late chunks ก่อน แต่ไม่เท่ากับย้อนคืนหรือหยุด external effects; UI แยกปิด mic, หยุดเสียง, หยุดงาน และจบ voice session พร้อมแสดง cancellation acknowledged/unknown ตามจริง

ไม่เอา raw audio ผ่าน MCP/SQLite event log/Pi sync ทุก frame; Hub รับเฉพาะ finalized/redacted records ที่ policy อนุญาต เสียงสดมี bounded buffers และ consent/retention แยก Voice egress ไม่ถูกอนุมัติตาม text gateway อัตโนมัติ รายละเอียด contracts, scheduler, privacy และ acceptance อยู่ใน voice design

## 11. Data lifecycle, reliability และ scale

ใช้ SQLite สำหรับ transactional task metadata ในแต่ละ device, immutable evidence/blob manifests, append/chunk logs และ rebuildable search indexes Raw media แยกจาก DB การ sync SQLite/WAL file ข้ามเครื่องตรง ๆ ไม่ใช่ข้อเสนอ

แยก authoritative record ownership กับ local cache/outbox ให้ชัด มี schema migrations, operation dedup, read/write revision checks และ artifact lineage Backup ต้องทดสอบ restore task/knowledge/config/selected artifacts จริง ไม่เรียก skill export ว่า full backup

Retention เป็น policy ต่อข้อมูล: customer logs/screenshots, source snippets, media, lessons, caches และ backups พร้อม quota/private-task mode ตามที่ผู้ใช้เลือก การ “ลืม” ต้องอธิบายว่าถอนจาก search/reuse แล้วหรือ physical deletion สำเร็จแล้ว; offline replicas รับ tombstone ภายหลัง และ backup restore ต้องนำ tombstones กลับมาบังคับก่อน reuse

วัด queue wait, generation latency/token usage, tool duration/errors, context spill, retrieval utility, sync lag/conflicts, disk/WAL growth, orphan processes และ checkpoint recovery หลีกเลี่ยง secrets/raw customer content ใน metrics

Scale รุ่นแรกคือคนเดียวสองเครื่องและ Pi: bounded queues, per-host resource budgets, pagination, incremental indexing/streaming, quota และ failure recovery ก่อน เพิ่ม remote workers/บริการแยกเมื่อมี load evidence; multi-tenancy/RBAC/billing/large cluster ไม่ใช่ค่าใช้จ่ายเริ่มต้นโดยอัตโนมัติ

## 12. แผนส่งมอบทีละขั้นและเกณฑ์ผ่าน

ทุกช่วงจบด้วย demo ที่ทำซ้ำได้และ evidence bundle ไม่ปิดงานด้วย checklist ว่า “เพิ่มปุ่ม/มี endpoint แล้ว” ยังไม่ประมาณวันจนรู้ stack, model, Hermes API, staffing และขอบเขต release

### Astra-led delivery — วิธีพัฒนาที่เลือก (D01)

ผู้ใช้เลือก Astra เป็น lead ให้ sub-agents ลงมือทำงานย่อย การกำหนดนี้เป็น delivery workflow ของการสร้าง Hermetrix ไม่เปลี่ยน product provider/routing, ไม่อนุมัติ deploy/production effects เพิ่ม และไม่ถือว่า implementation ของทุก phase เริ่มแล้ว

แนวทาง OpenAI Docs รองรับการสั่ง Astra ให้ delegate งานอิสระแบบขนาน และแนะนำระบุขอบเขต/ผลลัพธ์ให้ชัด; ใช้เป็นแนวทางแบ่งงาน ไม่ใช่หลักฐานว่าคุณภาพเท่ากันอัตโนมัติ ดู [Astra delegation guidance](https://developers.openai.com/api/docs/guides/latest-model#subagent-delegation)

| บทบาท | หน้าที่ | ผู้ตรวจรับ |
|---|---|---|
| Astra lead | สะท้อน requirements, architecture/interfaces, แบ่งงานตาม dependencies/risks, integration และ final review | ตรวจ against user criteria และรายงานข้อจำกัด |
| Implementation sub-agent | แก้ module/ไฟล์ที่รับผิดชอบตาม brief พร้อม targeted tests/evidence | reviewer + Astra lead |
| Review/verification sub-agent | อ่าน actual diff/เส้นทางรัน, negative cases และ integration risks โดยไม่เชื่อ implementer summary เป็นหลักฐาน | Astra lead ตรวจ findings และผลทดสอบ |

ใช้ reviewer แยกจาก implementer เมื่อความเสี่ยงสมควร แต่ไม่อ้างว่าต่าง agent = อิสระจาก model blind spots งานเล็กใช้ lead review กับ relevant tests ได้ ไม่เพิ่ม agents/reviews เพื่อให้จำนวนดูครบ

**Model policy:** sub-agent ไม่ได้หมายถึง small model หากไม่กำหนด override จะ inherit parent configuration ตาม [OpenAI Docs: Subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents#choosing-models-and-reasoning) สำหรับงานนี้ยังไม่ได้เลือกโมเดลรอง จึงคง effective model/reasoning configuration ตรวจจริงก่อนอ้างชื่อ และไม่ลดระดับเงียบ ๆ หากต้องลดค่าใช้จ่าย ให้ทดลอง model รองกับงานขอบเขตแคบและใช้ acceptance เดียวกันก่อนเลือกใช้จริง ไม่อ้างว่า delegation ประหยัด tokens เสมอ

ขั้นตอนต่อหนึ่งงาน:

1. **Define:** lead จัด brief มี requirement IDs/revision, input/source state, interfaces, files allowed, non-goals, acceptance/negative cases, time/retry/resource budget และ evidence ที่ต้องส่ง ก่อนเริ่ม implementation
2. **Delegate:** แบ่งเฉพาะงานที่รันอิสระได้; shared file/schema มี owner เดียว, contract เปลี่ยนต้องแจ้ง dependent tasks; shared checkout ห้ามแก้ไฟล์ซ้อน ถ้าต้องแยก worktree ให้มี integration point ชัด
3. **Implement:** worker ตรวจ baseline, ลงมือเฉพาะ scope, ทดสอบส่วนที่เปลี่ยน และส่ง actual diff/ไฟล์/commands/exit outcomes/environment พร้อมสิ่งที่ยังไม่ทดสอบ โดย preserve changes ของผู้ใช้
4. **Review:** reviewer อ่านโค้ดที่เปลี่ยนและผลรันจริง ทดสอบปฏิเสธ/ผิดพลาด/ขอบเขตตามความเสี่ยง Bug fix ต้องพิสูจน์ reproduction ก่อนแก้และหลังแก้เมื่อทำได้ ไม่สร้าง tests ที่แค่ยืนยันคำตอบของ implementation
5. **Integrate:** Astra lead ตรวจ actual integrated diff และ invariants ข้าม modules รัน relevant integration/user workflow กับ revision รวมที่ส่งมอบ ตรวจว่า parallel results ไม่ชนกันหรือใช้ stale contract
6. **Accept:** แสดง requirement coverage, findings disposition, passed/failed/skipped/not-run checks และข้อจำกัด หากไม่มี Windows/Pi/model environment จริง ห้ามใช้ mock/cross-compile อ้างว่าผ่าน real environment

Worker ต้องคืนงานหรือ escalate เมื่อ brief กำกวม, ต้องเปลี่ยน architecture/authority/schema นอก scope, ownership ชน, test evidence ขัดกัน หรือแก้ซ้ำแล้วยังหาสาเหตุไม่ได้ ให้ lead ตรวจ root cause/ปรับ task packet/รับช่วง critical portion แทนการเพิ่ม agent ต่อไม่สิ้นสุด

ไม่ลด tests/ซ่อน findings เพื่อปิดงานตาม token/เวลา หาก quota หรือเครื่องทดสอบไม่พร้อม ให้ checkpoint และระบุงานที่ยังไม่ตรวจจริง การ delegate แบ่งงานได้แต่ไม่ได้เลี่ยง account limits หรือรับประกันใช้ tokens น้อยลง ตาม [OpenAI Docs: Subagent availability](https://learn.chatgpt.com/docs/agent-configuration/subagents#availability)

นิยามคุณภาพ: ใช้เกณฑ์ตรวจรับเดียวกันไม่ว่างานเขียนโดย lead หรือ worker ประเมิน paired/held-out tasks เทียบ Astra-only baseline เมื่อมีโจทย์/งบทดสอบที่อนุมัติแล้ว วัด requirement satisfaction, escaped defects, review false positives, user rework, verified workflow success, total cost/tokens และ elapsed time ให้ lead review ตัดสินจากหลักฐาน ไม่ใช่คะแนนรวม/จำนวน reviewers อย่างเดียว ยังไม่รับประกันความเท่ากันทุกงานหรือกำหนดเปอร์เซ็นต์โดยไม่มีการวัด

### P0 — Requirements + feasibility proofs

- ปิดคำถามที่เปลี่ยนโครงสร้าง: Hermes/Pi/sync, exact model/Mac, project stacks, data/autonomy, test environment และ 3 workflows แรก
- เก็บ fixture repo/bug/customer flow ที่ใช้งานได้ พร้อม expected outcomes และสำรวจ Hermes tools/auth/storage โดย read-only ก่อน
- ทดสอบ native shell/editor/terminal integration ทั้ง Windows/Mac และ selected gateway บนงานสั้นจริง; แยก local-model benchmark เมื่อเลือกรุ่นแล้ว ก่อน long context
- ผลส่งมอบ: approved product brief, capability matrix, architecture decisions, baseline failures และ backlog ของ P1
- Gate: ไม่สรุปว่า native/96k/Hermes sync พร้อมหากมีเพียง compile/protocol handshake

### P1 — Cross-platform foundation + durable contracts

**สถานะ 2026-09-16:** ส่งมอบ durable task spine และ execution boundary รุ่นแรกถึง schema v36 แล้ว ครอบคลุม immutable requirement/plan revisions, automatic planner ที่ bind exact revision/criterion coverage, durable no-retry dispatch state, dependency/cycle gates, exact-subject validation evidence, optimistic task/step transitions, checkpoints, leased runs, exact step attempts และ local execution API

Exact step packet ถูก persist กับ attempt และ criterion mapping ถูก persist กับ step จึง resume/verify โดยไม่สร้าง authority ใหม่จาก task state ที่เปลี่ยนแล้ว Effect action ถูกบังคับด้วย frozen step scope และแยก pre-dispatch abandoned ออกจาก post-dispatch uncertain พร้อม block resume จน reconcile

Proposal coordinator เชื่อม exact packet/attempt กับ provider และไฟล์ที่เลือกแบบ proposal-only พร้อม credential egress guard, immutable artifact, durable single-decision review, optimistic apply/rollback, direct-exec frozen-command verification ที่ปฏิเสธ shell control syntax และ immutable evidence bundle หลัง test Provider profile ที่ต่างทั้ง identity และ model/endpoint ต้อง review exact source/evidence ก่อน promote requirement validation และปิด task ส่วน reject/failure จะ rollback และ fail attempt/run/step Managed command มี operation-ID result lookup และ batch reconciliation โดยไม่ replay

มี unit/race/integration tests และ Task cockpit แบบ project-scoped รองรับ create/list/inspect/auto-plan/start/propose/review/apply/verify/post-review แล้ว ยังไม่ถือว่า P1/P2 เสร็จ—ขาด orchestration loop ที่เลือกไฟล์เอง, lookup adapter สำหรับ provider/browser/MCP effects และ native host parity

- Task/requirement/run/evidence IDs, schema migrations, document service, managed run service, OS host adapters และ safety boundaries
- Native packages ทั้งสอง OS, Unix PTY/ConPTY, project-root mapping, draft recovery และ split-tree UI
- เผื่อ modality-neutral input/output events, task-bound voice session IDs และ interrupt controls ตั้งแต่ foundation; native spike ตรวจ mic permissions/device lifecycle โดยไม่เปิด capture ใน release จนผู้ใช้เริ่ม voice mode
- Shared-data bridge contract และ local outbox; ถ้า Hermes API พร้อมทำ thin metadata round trip เพื่อพิสูจน์ dedup/conflict ตั้งแต่ช่วงนี้
- Gate: edit/save/reopen/crash recovery ไม่เสีย draft; human terminal busy/cwd เปลี่ยนไม่ทำ managed run ผิดที่; cancel ไม่เหลือ owned children; task files/evidence/credentials/retrieval ไม่ผูกข้าม project จาก stale selection/async callbacks โดยไม่มี explicit selection/authorized sharing (global project sidebar ยังแสดงทุก project ของผู้ใช้ได้)

### P2 — End-to-end coding/reproduce/review + measured context

- First supported language stack: LSP/DAP + run/test/format configs + Git diff + revision-bound findings
- Requirement→plan→reproduction→patch→regression→review→report ผ่าน task engine; checkpoint/resume และ effect reconciliation
- Context registry รวม96k, bounded retrieval, resource-group scheduler และ black-box qualification ของ gateway จริงที่เลือก; local allocation/performance เป็น milestone แยก ไม่อ้างว่าผ่านตาม remote
- Gate: seeded customer bug reproduce ก่อน/หายหลังแก้, debug breakpoint/stack ทำงานทั้งสอง OS, reviewer พบ seeded defect โดยไม่เพิ่ม false positives เกินเกณฑ์ที่ตกลง; near-capacity ไม่ทำ requirement หลุดในชุดทดสอบ

### P3 — Customer-flow tester + authorized security reporting

- Mixed browser/API scenarios, test accounts/fixtures/reset, business assertions, traces และ incident/defect triage
- Security scope object + selected checks/catalog + confirmed/suspected findings + remediation retest
- Gate: deliberately broken UI/API flow ล้มจริงและอธิบายขั้นที่ผิด; out-of-scope target ไม่รัน; report ไม่รั่ว secrets; test failure ไม่สับสนกับ harness failure

### P4 — Verified learning + cross-device handoff

- Evidence hydration, candidate validation/promotion, scoped retrieval และ utility measurements; เก็บ evidence ตั้งแต่ P1 ไม่รอเริ่ม logging ตอนนี้
- Hermes sync ตาม contract ที่ผ่าน audit, checkpoints/artifact availability, tombstones/backup restore, conflict UI และ offline behavior ที่เลือก
- Gate: งานคล้ายกันใหม่ retrieve lesson ที่เกี่ยวและลด failure/effort ตาม metric; ไม่รั่วข้ามลูกค้า; response-loss write ไม่ซ้ำ;สองเครื่องไม่ทำ same task effects พร้อมกัน; disconnect/restore ไม่ทำข้อมูลที่ลบกลับมา active

### P5 — Media/research/native control extensions

- Video adapter ที่เลือก, streaming assets/proxies, edit-plan preview, durable render/cancel/result lookup และ export validation
- Research provenance/retrieval; native-control adapter ที่มี explicit targets/permissions
- Voice track ตามลำดับ V0/V1/V2 ใน voice design: streaming speech chain → continuous interruption → optional native-audio provider; ใช้ task/policy/evidence เดิมและวัด latency บนทั้งสอง OS ไม่ถือว่า TTS อ่านคำตอบจบก้อนเท่ากับ realtime
- Gate: source clip จริง→ผล export ตาม brief; render interrupted แสดง state/reconcile ได้; app-control หยุดเมื่อ target เปลี่ยน; source evidence ใช้ตรวจข้อสรุปได้

### P6 — Release hardening และ migration

- Migration rehearsal ด้วย copy ข้อมูลจริงที่อนุญาต, export/restore checks, compatibility flags และ rollback ที่ชัดเจน
- Clean-machine native install/update/reopen/sleep tests, supported OS/toolchain matrix, resource soak tests และ local-model held-out tasks
- Gate: requirement-to-evidence release report ไม่มี failed mandatory checks หรือ skipped tests ที่ถูกนับเป็น pass; แสดงข้อจำกัดที่ยังเหลือและ rollback procedure

Migration strategy: คง API เก่าที่จำเป็นชั่วคราว, adapt legacy sessions เป็น archived task history โดยไม่แต่ง acceptance criteria ย้อนหลัง, ใช้ feature flags ทีละ workflow, backfill มี version/checksum และไม่ dual-write สองระบบโดยไม่มี transaction/outbox เปลี่ยนชื่อ/ย้าย package เมื่อ boundary ชัด ไม่เปิดโครงการ refactor ไม่สิ้นสุด

## 13. Evaluation matrix ที่ต้องใช้ตัดสินจริง

| ระดับ | พิสูจน์อะไร | ตัวอย่างหลักฐาน |
|---|---|---|
| Unit/property | state/budget/path/schema invariants | token boundary, revision conflict, scope filters, migration transformations |
| Fault integration | cancel/retry/recovery/transport | hung writes, lost responses, crash หลัง effect, giant frames, disk full, Pi disconnect |
| Native E2E | ผู้ใช้ทำงานผ่าน package จริงได้ | editor visible/edit/save, Thai IME, PTY resize, LSP/DAP, layout persistence บนทั้งสอง OS |
| Realtime voice E2E (future gate) | สนทนา/พูดแทรก/ผูก task และควบคุม mic ถูกต้อง | actual capture→provider→playback, code-switching, stale chunks, tool-in-flight interruption, reconnect/device switch, privacy และ p50/p95 latency |
| Real-model workflow | โมเดลใช้ harness ทำงานสำเร็จ | requirement coverage, bug reproduction/fix, tool-use validity, review precision/recall, browser/API outcome |
| Learning A/B | reuse ช่วยงานใหม่จริง | same model/config, held-out tasks, with/without lesson, recurrence/false positives/time/tokens |
| Soak/restore | ใช้ต่อเนื่องและกู้ข้อมูลได้ | multi-run queue, log/WAL growth, large assets, backup restore, offline sync conflicts |

Real-model matrix ต้องบันทึก model/runtime/quantization/template/context/KV settings, device hardware, provider revision, prompt/evidence hashes, random sampling configuration และ raw outcomes ที่ redacted วัดทั้ง short/medium/near96k, contradictory requirements, multi-step tools, large outputs และ resumed tasks ทั้งไทย/อังกฤษถ้าอยู่ใน scope

จำนวนเคส/จำนวน repeat, target latency, disk quotas, acceptable failure/false-positive rate และ recovery time ยังต้องตกลง; รายงานผลพร้อม denominator และ uncertainty ไม่ใช้คะแนน sentinel เดียวรับรอง coding ทั้งระบบ และไม่รับรอง Windows จากการทดสอบบน Mac

## 14. Decisions ที่ยังไม่ปิด

ความสำคัญสูงก่อน freeze: Pi OS/deployment access/backup/offline, optional Hermes adapter identity/API, native Mac/Windows minimum versions, first project/DB/language priority, customer-data policy, autonomy/learning authority, production targets/actions policy และ release priorities ส่วน local model/capacity ค่อยปิดก่อน local96k qualification ไม่ block remote-first development

สิ่งที่เสนอแล้วแต่ยังไม่อนุมัติ: Go modular core, TypeScript workbench, Wails candidate, local SQLite+outbox และ Pi metadata hub, initial local concurrency1, scoped lesson promotion, IDE language subset และ phase order ด้านบน

Voice capability อยู่ในภาพรวมแล้ว แต่ ASR/TTS/native-audio provider, mic mode, languages, audio egress/retention และ latency target รอ Q32–Q34 ก่อน freeze voice implementation; ไม่ block foundation/coding ที่ไม่ต้องใช้เสียง

ขั้นถัดไปคือใช้คำตอบและ project ตัวอย่างทำ **one-workflow specification** ที่มี screens/states/API/schema/tests ชัด แล้วให้ผู้ใช้ตรวจความเข้าใจก่อนลงมือ ไม่เริ่มจากแก้ทุกหน้าและเปิดทุก integration พร้อมกัน
