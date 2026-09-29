# สรุปสเปก Hermetrix Harness ทั้งระบบ

อัปเดต: 2026-09-30 · ขอบเขต: สรุปข้อกำหนดผลิตภัณฑ์ สถาปัตยกรรม UX การเชื่อมต่อ Pi และเกณฑ์ตรวจรับจากเอกสารใน repository

> **วิธีอ่านสถานะ:** `มีแล้ว` หมายถึงเอกสารระบุว่ามี implementation หรือหลักฐานใช้งาน ณ วันที่ของเอกสารนั้น; `เป้าหมาย` คือ contract ที่ต้องพัฒนา/ตรวจรับ; `อนาคต` คือแนวทางที่ยังไม่เป็นความสามารถของ release ปัจจุบัน ไฟล์นี้เป็นสรุปสำหรับอ่านภาพรวม ไม่แทน requirement ID, API schema หรือเกณฑ์ตรวจรับฉบับเต็ม

## 1. เอกสารหลักและลำดับการอ้างอิง

| เอกสาร | บทบาท | สถานะที่ควรตีความ |
|---|---|---|
| [Master Project Specification](specs/2026-09-19-hermetrix-master-project-specification.md) | สัญญาหลักของผลิตภัณฑ์ ครอบคลุมระบบ, API, data, failure และ acceptance | เขียนจาก baseline commit `08fcac4`, schema v36; ป้าย `Current` ในไฟล์หมายถึง baseline นั้น |
| [Correctness, Durability and Performance Remediation](specs/2026-09-18-correctness-performance-remediation.md) | รายละเอียดแก้ findings R01–R16 และเกณฑ์ออก release | เอกสาร `Proposed` หลัง v36; ใช้เป็นข้อกำหนดเป้าหมาย ไม่ใช่หลักฐานว่าผ่านแล้ว |
| [Local Multimodal, Planner/Worker and Privacy](specs/2026-09-19-local-multimodal-privacy-implementation.md) | ขยายเรื่องเจ้าของข้อมูล, sharing, local runtime, preset, media | ทุกข้อ `OVL-*` เป็น `TARGET`; ส่วนที่ระบุชัดให้ใช้แทนกติกา Project/export ในสเปกแม่ |
| [Task-first UX](redesign/2026-09-20-task-first-ux.md) | การปรับหน้าเริ่มงาน, Plans, Workspace และผลทดลอง browser/Windows | มีทั้งการแก้ที่ตรวจแล้วและงานที่ยังรอ reviewer/model จริง |
| [Architecture](ARCHITECTURE.md), [README](../README.md), [Local Release](LOCAL_RELEASE.md) | ภาพระบบและสิ่งที่ build/ติดตั้งได้ | ใช้ตรวจความจริงของ implementation เทียบกับสเปกเก่า |
| [Pi rollout review](project-brain-rollout-review.md), [Pi operations](PROJECT_BRAIN_OPERATIONS.md) | หลักฐานและวิธีใช้ Pi Agent Platform/Project Brain | ยืนยัน scoped read/candidate flow ณ 2026-09-24; full target architecture ยังไม่ครบ |

ซอร์สใน checkout นี้กำหนด `CurrentSchemaVersion = 72` ที่ `internal/store/store.go` ส่วนเอกสารสเปกแม่อ้าง v36 และ overlay เสนอเลข migration v37–v46 จึง **ห้ามใช้เลข migration ในเอกสารเก่าเป็นแผนลงโค้ดโดยไม่ตรวจ HEAD ใหม่** สรุปนี้ไม่ได้รับรองว่าทุกฟีเจอร์ใหม่ผ่านเกณฑ์ release ครบแล้ว

## 2. เป้าหมายผลิตภัณฑ์และขอบเขต

Hermetrix คือ workbench ของ agent แบบ **local-first, single-host, single-principal** สำหรับงานหลายชนิด ใช้ model provider หลายแบบได้ โดยเก็บสัญญาการทำงาน บริบท เครื่องมือ ผลกระทบต่อระบบ หลักฐาน และบทเรียนให้ตรวจย้อนกลับได้ งานหลักมี 6 วงจร:

1. เปิด session ด้วย provider/model/context/Skill/tool/budget ที่ตรึงเป็น **Session Contract**
2. สนทนาและเรียก direct tools หรือ MCP ผ่านสิทธิ์และใบเสร็จของ effect
3. วาง requirements/plan แล้วทำ coding task ผ่าน proposal → อนุมัติ → apply → verification → independent review
4. สร้าง/ปรับปรุง Skill ด้วย candidate, replay/evaluation, promotion และ rollback ที่ตรวจสอบได้
5. คัดเลือก/บีบ context ระยะยาวโดยรักษาเจตนาที่ pin และเหตุ–ผลของ tool call/result
6. ควบคุม Project, ไฟล์, editor, terminal, browser, artifact, team และ maintenance จาก UI เดียว

รุ่นปัจจุบัน **ไม่ใช่** ระบบ multi-user/RBAC, distributed worker, unattended production coding, ตัวแทน VS Code ครบทุกภาษา, หรือ backup/restore ทั้งระบบผ่าน Skill portability export; การขยายเหล่านี้ต้องมี contract และหลักฐานแยก

## 3. โครงสร้างระบบและข้อมูล

- **Process:** `hermetrix serve` รวม Go services, HTTP API และ browser cockpit; `hermetrix-worker` รับ coding task แบบ bounded JSON แล้วส่ง *proposal* โดยไม่แก้ source เอง; คำสั่ง corpus/evaluation แยกจาก product workflow
- **Storage:** SQLite เก็บ metadata, ความสัมพันธ์, event, authority และ state; CAS แบบ SHA-256 เก็บ payload ไม่เปลี่ยน; credential vault แยกจาก DB/backup/API; migration ต้องเรียงลำดับและ startup ต้องล้มเหลวถ้า schema ไม่สมบูรณ์
- **Project:** เป็นขอบเขตของ session, task, file, job, artifact, browser และ team; Project ไม่มี code root ก็ถูกต้องถาวร ไม่ใช่ setup ที่ยังไม่เสร็จ; งานที่ต้องใช้ root ต้องแจ้งข้อจำกัดชัดเจน
- **หลักฐาน:** canonical events/receipts เป็น append-only หรือ immutable ตามชนิด; index, embedding และ checkpoint ที่เป็น derived data ต้องสร้างใหม่ได้จากข้อมูลหลัก
- **Versioning:** provider, MCP catalog, Skill, requirement, plan, proposal และ preset ที่มีผลต่อพฤติกรรมต้องผูก revision/hash ที่แน่นอน; การเปลี่ยนค่าใหม่ไม่ย้อนแก้ประวัติ

## 4. ความปลอดภัยและขอบเขตอำนาจ

- UI/API ผูก loopback เป็นค่าเริ่มต้น; non-loopback ต้องมี auth และ TLS; token ควบคุมอ่านจาก environment และแลกเป็น cookie ที่กำหนดอายุ/คุณสมบัติความปลอดภัย
- ทุก model output, project file, MCP response, browser DOM, import, Skill body และ media transcript เป็น **ข้อมูลที่ไม่เชื่อถือ**; เนื้อหาเหล่านี้เพิ่มสิทธิ์ให้ตัวเองไม่ได้
- Effect ที่เปลี่ยนไฟล์/รันคำสั่ง/เรียกบริการต้องมี intent, ขอบเขตอำนาจ, exact approval หรือ receipt ตาม workflow; approval ใช้ครั้งเดียว ผูก arguments hash, revision และ turn/attempt ที่ถูกต้อง
- ตรวจ path ภายใน Project, ปฏิเสธ traversal/symlink escape; รันคำสั่ง allowlist แบบ direct executable ไม่ผ่าน shell; จำกัดเวลา/output และติดตาม process tree. Windows มี Job Object/ConPTY แต่ยังไม่ใช่ filesystem/network sandbox เทียบ Linux/macOS
- Managed browser ใช้ Chromium profile แยกและ DevTools; จำกัด navigation/action/evidence; private/local network ต้อง opt-in. DOM และ screenshot เป็นหลักฐาน ไม่ใช่คำสั่ง
- Remediation กำหนดให้ตรวจ `Host`, `Origin`, Fetch Metadata, media type และ JSON หนึ่ง document ก่อน mutation; เปลี่ยน error เป็น code ที่เสถียร; ตรวจ contract ก่อน provider dispatch ทุกเส้นทาง
- ความไม่แน่นอนหลัง dispatch ต้องเป็น `uncertain` และ reconcile ด้วย operation ID เดิม **ไม่ replay อัตโนมัติ**; late receipt บันทึกข้อเท็จจริงได้แม้ lease เก่าหมดอายุ

## 5. Provider, session และงบการอนุมาน

- Adapter หลัก: OpenAI-compatible, Anthropic native, Gemini native; profile เก็บ endpoint/model/context/output/credential reference/revision โดย API ไม่คืน secret
- Routing เลือก provider ที่พร้อมและผ่าน qualification **ก่อน** สร้าง Session Contract; session ที่เริ่มแล้วไม่ fail over แบบเงียบ ๆ; ทุก step เก็บ StepBinding และ context snapshot
- แยก **context ที่ประกาศ**, **capacity ที่ probe ได้**, และ **คุณภาพที่ qualify แล้ว**; local runtime ต้องมีหลักฐานรุ่น model/runtime/hardware จริง. การเปลี่ยน runtime/template/context ทำให้ binding เดิมใช้ไม่ได้ตาม overlay
- Turn จำกัดจำนวน step, tool call, เวลาและ token; stream เป็น NDJSON; response ที่ EOF/partial โดยไม่มี terminal reason ถูกจัดเป็น incomplete และห้ามใช้ partial tool call
- Scheduler เป้าหมายต้องรับ provider dispatch ทุกชนิด รวม planner, worker, team, learning, reviewer และ nested MCP; บันทึก queue/usage/terminal reason และจัดการ reservation. Local GPU เริ่มที่หนึ่ง execution slot และต้อง quarantine หากยกเลิกแล้ว runtime ยังอาจทำงานอยู่
- Context Compiler ใช้ typed fragments, reserve output/uncertainty/tool burst, fail closed ถ้า pinned constraints ใส่ไม่ลง, เก็บ tool call/result เป็น causal unit; compaction มี verifier/fallback; lexical retrieval ทำงานได้แม้ไม่มี embedding ส่วน semantic เป็นตัวเลือก
- Overlay กำหนด immutable role presets และงบ input/generation แบบตรวจจริง: planner `40k/60k` input target/max กับ generation `5,120`; worker `16k/30k` กับ `1,500`; worker-debug `20k/30k` กับ `2,524` เป็น **ค่าเริ่มต้นเป้าหมาย** ที่ต้องเข้ากับ qualified capacity. การทดลอง Windows พบว่าต้องลดเพดาน planner สำหรับ context 61,440 เป็น input max 55,808 โดยคง output reserve

## 6. งานที่คงสถานะข้ามการขัดจังหวะและ coding workflow

1. **Task/requirements:** Task ผูก Project, objective, state และ revision; criteria มี ID คงที่. เปลี่ยน requirements สร้าง revision ใหม่และทำให้ evidence เก่าที่ไม่ตรง subject ใช้ปิดงานไม่ได้
2. **Plan:** ขั้นมี dependency, criterion mapping, checks และ effect scope; ปฏิเสธ cycle/ID ซ้ำ/coverage ไม่ครบ. Next-step packet มี revision, criteria, evidence, checkpoint และ unresolved effects พร้อม canonical hash; ห้ามตัด authoritative constraints เงียบ ๆ
3. **Run/attempt/effect:** มี run ที่ `running` ได้ไม่เกินหนึ่งต่อ Task; lease token/generation และ authority ต้องตรวจ transactionally ก่อน plan/dispatch; effect intent แยก planned/dispatched/observed/uncertain; uncertain effect บล็อกการไปต่อจน reconcile
4. **Code proposal:** เลือกไฟล์ bounded/กรอง secret; model เสนอ structured change โดยยังไม่แก้ source; คนตรวจและอนุมัติ proposal ที่แน่นอน; apply ตรวจ preimage ทุกไฟล์ก่อนเริ่ม; ล้มเหลวหลายไฟล์ต้อง rollback พร้อมผลรายไฟล์
5. **Verification/review:** รัน checks ที่ตรึงไว้ผ่าน managed jobs และเก็บ evidence; mandatory check ไม่ผ่านต้อง rollback/fail; reviewer ใช้ provider profile แยกและตรวจ exact applied source/evidence ก่อน promote validations และปิดงาน. สเปกแม่เดิมกำหนดให้ model หรือ endpoint ต่างกันด้วย แต่ README/implementation ปัจจุบันยอมให้ profile แยกใช้ model/endpoint เดียวกันได้ จึงเป็น review อีก pass ไม่ใช่หลักฐานความเป็นอิสระของโมเดล
6. **Recovery:** restart ไม่ส่ง provider/command/browser/MCP effect ซ้ำ; ambiguity หลัง apply หรือ receipt ล้มเหลวต้องมี hold/recovery state ที่ผู้ใช้เห็น; งานที่เสร็จต้องมีผลตรวจทุก criterion กับ revision ปัจจุบัน

Task UI ฉบับปรับล่าสุดให้เริ่มจากเลือก Project → ใส่เป้าหมาย → Chat หรือ Plans → ข้อเสนอทีละขั้น → ตรวจผลก่อนปิดงาน. หน้า Plans ต้องส่ง run ID/lease token ใน protected actions, ต่ออายุ lease, ป้องกันกดซ้ำ และบอกตั้งแต่ต้นเมื่อยังไม่มี reviewer อิสระพร้อมใช้

## 7. เครื่องมือ, MCP และ agent teams

- Direct tool definitions/version ถูกตรึงใน Session Contract; file read/search มีเพดานและ root guard; write ต้อง exact approval/preimage; runtime tools มี `workspace.run` และ managed `browser`
- MCP ใช้ deferred catalog ผ่าน `tool_search`, `tool_describe`, `tool_call` เพื่อไม่ใส่ remote schemas ทั้งหมดใน prompt; validate JSON กับ schema/revision ที่พบจริง; รองรับ stdio และ Streamable HTTP ตามรุ่น protocol ที่ implementation ระบุ
- MCP annotations เป็นข้อมูลไม่เชื่อถือโดยค่าเริ่มต้น; unknown/mutating tool ต้อง approval เว้นแต่ server นั้นตั้ง trust อย่างชัด; เมื่อผล dispatch ไม่แน่นอนห้าม retry tool/resource/prompt call อัตโนมัติ
- Agent Team มี roster ที่ผูก Project, lead หนึ่งคน, task DAG, child sessions แยก, concurrency สูงสุดสี่, approval ที่ pause เฉพาะ child, cancellation propagation และ usage/provenance ราย child
- [Agent Platform contract reviews](agent-platform-contract-v1-final-harness-review.md) และ [Managed Execution review](managed-execution-v1-final-harness-review.md) แยก **สัญญา platform/managed execution ในอนาคต** จาก Pi read-only integration ที่ใช้งานจริง; ห้ามตีความ review หรือ H2A probe ว่าเปิด distributed write authority แล้ว

## 8. Skill, ความจำ และการเรียนรู้

- Skill มีชื่อ/scope/origin/owner/version; active version เปลี่ยนในที่เดิมไม่ได้. Create/improve/fork/restore ออก candidate; promotion ต้องผ่าน lint/check, optimistic revision และ replay/capability/behavioral evidence ตาม policy; archive/restore เก็บ audit
- Session ตรึง metadata และ exact Skill versions; body โหลดเท่าที่จำเป็น. Skill ที่ promote ภายหลังใช้ใน session เก่าไม่ได้หาก version ไม่อยู่ในสัญญา
- Learning trigger ถูกเขียนร่วม transaction กับ completed turn; background reviewer persisted/idempotent และเสนอ candidate เท่านั้น; auto-promotion มี policy/receipt/rollback; curator รายงาน stale/duplicate โดยไม่แก้เอง
- Memory/Knowledge view ใช้โน้ต `memories` ที่คนสร้างเอง; เปลี่ยนโน้ตด้วย archive + ฉบับใหม่เพื่อรักษาประวัติ; retrieval ควรผสม exact lexical กับ optional semantic โดยกรอง scope/owner ก่อนจัดอันดับ
- Pi Project Brain เป็นคลังความรู้แยกที่รับ **inert candidate** พร้อม evidence/version แล้วให้ curator อีกสิทธิ์ตรวจรับก่อนเข้า retrieval; passage ที่ดึงมาเป็น external evidence มี project/lifecycle/source/line/hash ไม่ใช่ authority ของ Hermetrix

## 9. Workbench, UI และ deliverables

- Project picker เป็นการเลือกขอบเขตแรก; UI ปัจจุบันเน้น Chat, Plans และ Workspace; เอกสาร shell/design เก่ากำหนด Work, Code, Knowledge เป็นมุมมองและแนว UX แยก จึงต้องเทียบกับ runtime ก่อนอ้างว่าทุกมุมมองครบ
- พื้นที่ rail/main/side และ pane ปรับขนาด/ซ่อนได้; narrow screen ใช้ tabs; layout จำแยกต่อ Project/view. Composer รักษา draft/focus ระหว่าง stream และสลับงาน; approval แสดง effect ที่แน่นอน
- Code/IDE: file tree, editor, compare-and-write ด้วย SHA, diff/review, Run/Test, terminal และ Go/JavaScript debugger ตามขอบเขต [IDE guide](IDE-WORKSPACE.md); Code view spec ยังวางเรื่อง source สองชั้น, tokenizer, diff และไฟล์ใหญ่ไว้ละเอียด
- Work view spec แสดง state จาก team runs/jobs/approvals ไม่สร้าง to-do อีกชุดและไม่ให้ลากเปลี่ยนสถานะเครื่อง; Knowledge view spec จัดโน้ต, Skill, Artifact และ history พร้อมแหล่งที่มา
- Browser tab มี snapshot/action/screenshot evidence; artifact สร้าง DOCX/XLSX/PPTX/PDF แบบ bounded; PDF native ปัจจุบันจำกัดตัวอักษร Basic Latin ถ้าไม่มีฟอนต์รองรับ Unicode
- UI ต้องแสดง capability จริง, empty/error/loading ที่ไม่ลบงานของผู้ใช้, ไม่ให้ response ของ Project/session เก่าทับหน้าปัจจุบัน; optional panels โหลดเมื่อเปิด. Browser E2E ที่ release ต้องผ่านจริง ไม่ถือการ skip ว่าผ่าน

## 10. Privacy, sharing และ multimodal overlay

ส่วนนี้เป็น **เป้าหมาย OVL** เว้นแต่มีหลักฐานล่าสุดระบุว่าทำแล้ว:

- เพิ่ม principal ID ภายใน installation และ owner binding ให้ session/task/artifact/memory/Skill; ค่าเริ่มต้นของข้อมูลเก่า/ใหม่คือ `private` และ `export_policy=deny`; Project association ไม่ทำให้แชร์อัตโนมัติ
- `project_shared` หมายถึงมีสิทธิ์เข้า *การเลือกส่งออกโดยชัดเจน* ไม่ใช่ public. Derived content สืบทอดนโยบายที่เข้มที่สุด; declassification ต้องมี sanitized derivative และ audit
- Project share เลือก object/revision/source files, preview dependency graph และ metadata ที่เหลือ; export เฉพาะ field allowlist, ไม่รวม transcript, prompt, secrets, runtime state; manifest/hash/expiry ถูกตรึงและ recheck ก่อนสร้าง package
- Import ต้อง stage → validate → preview → apply, ตรวจ archive/path/hash/size, สร้าง IDs ใหม่และ Project ใหม่แบบไม่มี root โดยค่าเริ่มต้น; Skill กลับเป็น candidate, Task เป็น draft; ห้ามคืน approval/effect/lease เป็น live หรือ overwrite source อัตโนมัติ
- `local-only` ครอบ inference, embedding, STT, image analysis และ reviewer; remote egress ต้องได้อนุญาตและ binding ใหม่ ไม่ fail over เงียบ ๆ
- Multipart events/media เป็น typed ordered parts อ้าง Artifact แทนฝัง base64 ใน event; vision ต้อง qualify จริง; audio/video ใช้ durable media jobs พร้อม decoder limit, cancellation, recovery และ source/time anchors. Summary ภาพ/วิดีโอเป็นหลักฐานที่อาจไม่ครบ ไม่แทน original
- แยก package **Project sharing**, **same-user workspace migration**, **full disaster recovery**, และ **Skill portability** เพราะความหมาย/อำนาจไม่เหมือนกัน. Overlay ระบุ migration groups และ acceptance A01–A14; O0–O5 เป็น release slice แรก ส่วน O6–O8 ยังรอตามสเปกเดิม

## 11. การเชื่อมต่อ Raspberry Pi ที่ตรวจแล้ว

ณ [rollout review 2026-09-24](project-brain-rollout-review.md), Pi มีบริการแยกสองชุด: Agent Platform/board สำหรับ identity และงาน กับ Project Brain สำหรับ curated knowledge. ใช้ MCP credentials แยกต่อ client/project/scope; Hermetrix และ Hermes มีสิทธิ์อ่าน Project Brain ส่วน Codex มีสิทธิ์งานที่ grant และส่ง knowledge candidate ได้. Pi เก็บ project/work identity, grants และ knowledge lifecycle; Hermetrix ยังถือ local paths, Context Compiler, DecisionEngine, leases, effect intents และกฎ no-replay

เส้นทางจริงที่ตรวจ: Project Brain passage ที่ curator รับแล้วถูกดึงเข้ามาเป็น context fragment แบบ version-bound ใน local Bonsai session และโมเดลตอบคำถามจากเนื้อหานั้นได้. **ยังไม่มี** managed remote execution/write handoff, autonomous coding, automatic knowledge extraction ทุก session, shared/global knowledge policy หรือ measured reuse rate. ข้อมูลบน Pi ต้องอ่านเป็นหลักฐานภายนอกที่ตรวจ applicability ก่อนใช้

## 12. Reliability, performance และเกณฑ์ออก release

- Remediation เน้นแก้ request/authority, filesystem/OS, provider scheduler/budget, context/retrieval, UI และ recovery ตามลำดับ; ห้ามเพิ่ม concurrency ก่อน authority/file invariants ผ่าน
- เป้าหมาย performance ต้องวัดบน host/workload อ้างอิงก่อนเป็น SLO: คิวและเวลาตอบ, context ประวัติยาว, DB wait, snapshot/CAS, UI long timeline และ cost ของ retrieval; optional retrieval timeout ต้องไม่บล็อกคำตอบเกิน budget
- เก็บ usage แบบ `actual`/`estimated`/`unknown`; ไม่แสดง missing usage เป็นศูนย์. Metrics/logs ห้ามมี prompt, raw arguments, content หรือ credentials; ทุก error ควรโยง request ID
- Gate หลัก: build, vet, Go tests, race บน amd64 ที่รองรับ, lint, frontend syntax/tests, native Windows/Linux/macOS workflows, required Chrome E2E, aged-DB migration, security/concurrency/failure tests และ recovery drill. รายงาน passed/failed/skipped/not-run แยกกัน
- **Baseline buildable** ≠ **technically verified** ≠ **qualified for wider use**; ระดับท้ายต้องปิด remediation invariants และ P1 findings, ผ่าน scheduler/native/browser/recovery/long-history gates และทำ docs ให้ตรงความจริง. [Local release](LOCAL_RELEASE.md) ระบุว่า Windows pilot ทำงานได้บางวงจร แต่ `go test -race` ยังไม่ยืนยัน และ local model ตัวเดียวปิด independent review ไม่ได้

## 13. งานอนาคตและคำถามเปิด

- Full IDE language services/LSP/DAP และ parity ภาษา Go/Fiber, React/Next.js, Python, PHP; customer-flow tester, authorized security testing/reporting, research workflow, video editing และ native computer control
- Realtime voice มี [design แยก](redesign/2026-09-13-realtime-voice.md) เรื่อง turn/interruption, audio lifecycle, latency, privacy และ handoff; ยังไม่ใช่ความสามารถปัจจุบัน
- Cross-device/distributed handoff, Platform Router, generalized browser/MCP task orchestration และ managed execution ต้องมี identity/lease/fencing/replay contract ก่อนเปิดใช้
- Full recovery/portable encrypted workspace, production packaging/signing/updater, support policy และการทดสอบ native หลาย OS ยังเป็น gate แยก
- ข้อเสนอ [redesign blueprint](redesign/2026-09-13-blueprint.md) และ [requirement discovery](redesign/2026-09-13-requirements.md) ครอบคลุมเป้าหมายกว้างกว่าสเปก release; สิ่งที่ยังไม่ล็อกในไฟล์เหล่านั้นไม่ควรถูกตีความว่า implement หรือรับรองแล้ว

## 14. แผนที่เอกสารสเปกย่อย

| เรื่อง | เอกสารรายละเอียด |
|---|---|
| UX shell/Project, runtime tools, Code, Work, Knowledge, visual language | [Shell](superpowers/specs/2026-09-01-shell-and-project-design.md), [Tools](superpowers/specs/2026-09-02-agent-runtime-tools-design.md), [Code](superpowers/specs/2026-09-02-code-view-design.md), [Work](superpowers/specs/2026-09-08-work-view-design.md), [Knowledge](superpowers/specs/2026-09-08-knowledge-view-design.md), [Visual](superpowers/specs/2026-09-08-visual-language-design.md) |
| Redesign และ deployment extensions | [Requirements](redesign/2026-09-13-requirements.md), [Baseline](redesign/2026-09-13-baseline.md), [Blueprint](redesign/2026-09-13-blueprint.md), [Knowledge Hub](redesign/2026-09-13-knowledge-hub.md), [Voice](redesign/2026-09-13-realtime-voice.md), [Worker connector](redesign/2026-09-13-worker-connector.md) |
| Agent Platform และ Pi | [Contract review](agent-platform-contract-v1-harness-review.md), [Final contract review](agent-platform-contract-v1-final-harness-review.md), [Managed execution](managed-execution-v1-final-harness-review.md), [H1 proposal](h1-agent-platform-readonly-integration-proposal.md), [H2A acceptance](h2a-acceptance-status.md), [Project Brain rollout](project-brain-rollout-review.md) |
| สถานะ การตัดสินใจ และหลักฐาน | [Roadmap](ROADMAP.md), [ADR ledger](DECISIONS.md), [Project review](reviews/2026-09-18-project-review.md), [Review log](REVIEW.md), [Local release](LOCAL_RELEASE.md) |

สำหรับ field/API/limit, security invariant, migration หรือ acceptance ID ที่ต้องใช้ลงมือ implement ให้เปิดเอกสารต้นฉบับที่ลิงก์ไว้และตรวจซอร์สกับ runtime ปัจจุบันอีกครั้ง
