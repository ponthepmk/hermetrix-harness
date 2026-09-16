# Hermetrix redesign — Requirement discovery

วันที่: 2026-09-13 · สถานะ: กำลังเก็บ requirement

เอกสารนี้แยกสิ่งที่ผู้ใช้ขอแล้วออกจากข้อเสนอและคำถามเปิด คำตอบที่ยังไม่ได้รับไม่ถือเป็นการอนุมัติ default แผนสถาปัตยกรรมฉบับร่างอยู่ใน [redesign blueprint](2026-09-13-blueprint.md) และข้อค้นพบจาก source อยู่ใน [baseline](2026-09-13-baseline.md)

## 1. สิ่งที่ยืนยันจากคำขอแล้ว

| ID | Requirement ที่ยืนยันแล้ว | ผลลัพธ์ที่ต้องออกแบบให้รองรับ |
|---|---|---|
| R01 | เป็น harness กลางสำหรับหลายประเภทงาน | จัดการ intent, แผน, execution, tools, ผลลัพธ์ และบทเรียนใน workflow เดียว |
| R02 | เปลี่ยนทั้งโครงสร้างและ UX ให้เรียบ ใช้ง่าย ขยายได้ | navigation คงที่, project scope ชัด, เปิดรายละเอียดเมื่อจำเป็น; ไม่เพิ่มปุ่มอย่างเดียว |
| R03 | ใช้ local LLM ที่ context 96k | budget, retrieval, durable plan/checkpoint และ qualification บนรุ่น/เครื่องจริง |
| R04 | วางแผนก่อนทำและทำเป็นขั้น | แผนที่อัปเดตได้, dependencies, success criteria, pause/resume, replan ที่ตรวจย้อนกลับได้ |
| R05 | เก็บความผิดพลาดและเรียนรู้ข้อเสีย | บันทึกเหตุการณ์ สาเหตุที่สงสัย/ยืนยัน วิธีแก้ และหลักฐานผลลัพธ์ |
| R06 | นำสิ่งที่เรียนรู้กลับมาใช้ให้ดีขึ้น | retrieval ในงานถัดไป พร้อมวัดผลว่าเพิ่มความสำเร็จ/ลดข้อผิดพลาดจริง |
| R07 | เขียนโค้ดเหมือน IDE | file/editor/project lifecycle ถูกต้อง; ชุดภาษาและระดับ feature ต้องเลือกเพิ่ม |
| R08 | review code ได้ | findings ผูกกับ diff/revision และมีหลักฐาน ไม่ใช่แค่ข้อความสรุป |
| R09 | ต่อ MCP ได้ | ความสามารถค้นหา/เชื่อมต่อ/เรียกใช้งานผ่านขอบเขตและสถานะที่ตรวจได้ |
| R10 | ทำงานตัดวิดีโอผ่านเครื่องมือได้ | ingest, plan/edit, preview, export, verify; แอปและระดับตัดต่อยังต้องระบุ |
| R11 | ควบคุมเครื่องได้ | แอปเป้าหมาย การ observe/action และสิทธิ์ต้องระบุ; ไม่เท่ากับ browser automation อย่างเดียว |
| R12 | จัดเก็บข้อมูลเพื่อใช้งานและเรียนรู้ | lifecycle, provenance, retention, lookup, export/restore และการลบที่ถูกต้อง |
| R13 | รองรับการ scale ในอนาคต | เลือกแนวขยายตามจำนวนงาน เครื่อง ผู้ใช้ และข้อมูลจริง |
| R14 | ออกแบบรอบคอบและทดสอบจริง | มี golden workflows, baseline, acceptance gates และหลักฐานก่อนประกาศพร้อม |
| R15 | มีแผนละเอียดทีละขั้นก่อนลงมือ redesign | เก็บ requirement → validate → architecture decisions → phased delivery |
| R16 | ใช้เอง สลับ Windows กับ MacBook และต้องการ native desktop ทั้งคู่ | shared core/UI + host adapters; ต้องทดสอบ native ทั้งสอง OS ไม่ใช่แค่ cross-compile |
| R17 | เลือก/custom provider ได้; ปัจจุบันใช้ Ollama | แยก provider protocol, endpoint, model, runtime host และ capability; เปลี่ยนได้อย่างเปิดเผย |
| R18 | มีโปรเจคใหม่และเดิมเข้ามาเรื่อย ๆ | onboarding ที่ไม่แก้ repo โดยพลการ, project presets, incremental indexing และข้อมูลแยกตามลูกค้า |
| R19 | AI มักทำไม่ตรงความต้องการ ต้อง review ก่อนทำต่อ | requirement IDs/revisions, coverage, unknowns, review findings และจุด replan |
| R20 | รับบั๊กลูกค้าแล้ว reproduce/debug/หาสาเหตุ/วางแผนแก้ | เก็บ original report, environment, reproduction, hypotheses, regression และรายงาน |
| R21 | ทดสอบความปลอดภัยระบบที่พัฒนาและออกรายงานแก้ไข | ขอบเขตเป้าหมายที่ได้รับอนุญาต, coverage matrix, evidence, remediation และ retest; ไม่รับประกันทดสอบได้ทุกความเป็นไปได้ |
| R22 | AI tester ออกแบบ customer flow แล้วเปิด browser/เรียก API จริง | executable scenarios, fixtures, business assertions, trace/report และโยงผลผิดพลาดเป็นงานแก้ไข |
| R23 | ค้นคว้าข้อมูลตามโจทย์ | หลักฐานแหล่งที่มา วันที่ ความขัดแย้ง และข้อสรุปที่ตรวจกลับได้ |
| R24 | ใช้ Raspberry Pi ที่มี Hermes เป็นตัวกลางข้อมูล เชื่อมผ่าน MCP | integration contract, project scope, versioned writes, offline queue, backup และการ sync; ยังไม่ทราบ Hermes ตัวใด/ความสามารถจริง |
| R25 | ออกแบบ MCP tools กลางให้ AI หลายตัวแลกเปลี่ยนข้อผิดพลาด ข้อดี และบทเรียน | Knowledge Hub ที่มี evidence/trust/scope, credentials แยก, candidate validation และ retrieval; ไม่แชร์ secrets ใน knowledge |
| R26 | รองรับ Go/Fiber, React/Next.js, Python และ PHP และเพิ่ม workflow ได้ | language/runtime adapters + versioned workflow definitions; ลำดับ language parity ยังต้องเลือก |
| R27 | Tester/security รองรับ local, staging และ production | environment profiles และ policy แยก; production ไม่ได้อนุญาต destructive tests ทุกแบบโดยอัตโนมัติ |
| R28 | เผื่อความสามารถพูดและโต้ตอบแบบ realtime ในอนาคต | voice session/streaming adapters แยกจาก task engine, interruption, mic/playback lifecycle, latency/resource budgets และ privacy; ยังไม่ใช่ฟีเจอร์ที่ implement แล้ว |
| R29 | Hermetrix วางแผน คุมงาน ลงมือ ทดสอบ ตรวจผล และปรับแผนได้เอง | runtime task/requirement/plan revisions, bounded delegation, checkpoints, evidence-backed validation และ stop/recovery policy; Codex/Astra เป็นผู้พัฒนาและตรวจคุณภาพระบบ ไม่ใช่ dependency ของทุก production run |

### เครื่องและสภาพใช้งานที่ยืนยันแล้ว

- Windows: Intel i7 Gen 12 รุ่น F, RTX 5070 Ti VRAM 16 GB, RAM 64 GB; ใช้ Ollama อยู่ และเปิดรับ runtime ทางเลือก
- MacBook: ผู้ใช้ระบุว่ารันโมเดลไม่ไหว จึงเป็น client/execution host ไม่ใช่ inference host เริ่มต้น; ยังไม่ทราบ chip/OS สำหรับ native packaging
- Provider เริ่มต้นตามผู้ใช้: `https://gateway.9arm.co`, model `qwen3.8-27b-fp8`; ยังไม่ได้ทำ authenticated call/ตรวจ path/capacity/latency ในรอบนี้ และไม่บันทึก API key ลงเอกสาร
- Raspberry Pi 5 RAM 16 GB, M.2 storage, เชื่อม LAN/VPN; ยังไม่ทราบ OS/ความจุ/backup และ offline behavior
- ผู้ใช้คนเดียว ไม่ได้ขอ multi-user SaaS ในรุ่นแรก
- ยืนยันชุดงาน coding/review, bug reproduction/debug, security assessment, browser/API tester, personal video และ research แล้ว แต่ยังไม่ได้จัดอันดับส่งมอบหรือเลือก golden project
- ผู้ใช้เสนอให้ใช้ Hermes บน Raspberry Pi เป็นศูนย์กลางข้อมูลผ่าน MCP แล้ว; ยังไม่เลือก consistency/offline policy และว่าจะรันโมเดล/เครื่องมือที่เครื่องใด
- ทราบ model identifier ของ remote provider เริ่มต้นแล้ว แต่ยังไม่ทราบ local Ollama model/runtime และความหมายเชิงตัวเลขของ 96k; remote model ไม่ใช่หลักฐานรับรอง local96k บน Windows

จากบทสนทนาก่อนหน้า: ต้องการ sidebar ที่เลื่อนได้/พับได้, pane ลาก/ย่อขยายได้ รวมแบบบนสองล่างหนึ่ง, ไฟล์แยกตาม project, terminal เปิดได้ทันที และ theme เรียบหรูแบบการจัดวางในภาพอ้างอิง Codex สิ่งเหล่านี้เป็น input ด้าน interaction ไม่ใช่ข้อบังคับว่าต้องใช้เทคโนโลยีของ Codex

## 2. รอบแรก — คำถามที่กำหนดโครงสร้างหลัก

ส่ง Q01–Q06 ในแชทแล้ว; Q01–Q03 ได้คำตอบบางส่วนตามด้านบน ส่วน Q04–Q06 ยังรอคำตอบ

| ID | คำถาม | เหตุผลที่เปลี่ยนแบบระบบ |
|---|---|---|
| Q01 | ยืนยันคนเดียว Windows + MacBook; เหลือวิธีสลับ/ต่อเนื่องงานใน Q28 | identity, isolation, deployment, sync, conflict และการแบ่ง worker |
| Q02 | ทราบ initial remote model แล้ว; registry กำหนด 96k = 98,304 tokens แล้ว แต่ local model/quantization/runtime versions และผล qualification จริงยังเปิด ส่วน Mac ไม่รัน inference ในรุ่นเริ่มต้น | แยก remote black-box qualification กับ local allocation/performance qualification |
| Q03 | ทราบชุดงานแล้ว; เหลืออันดับ 3 งานแรกและตัวอย่าง project/ผลลัพธ์ที่ถือว่าสำเร็จ | กำหนด vertical slice แรกและ benchmark โดยไม่กระจายทุก feature พร้อมกัน |
| Q04 | ข้อมูลต้องอยู่ local ทั้งหมด หรืออนุญาต cloud/model/API แบบใดและเมื่อใด? | provider routing, media processing, embeddings, egress และ fallback |
| Q05 | ต้องอนุมัติทุก action หรืออนุมัติแผน/ขอบเขตแล้วให้ทำต่อ รวมงานเบื้องหลังหรือไม่? | policy ของ run, preview, permission reuse, stop/resume และ UX |
| Q06 | บทเรียนใช้เองได้ระดับใด มีข้อมูลใดห้ามเก็บ และต้องการ memory/skills หรือรวม fine-tune ด้วย? | learning authority, retention, dataset และ evaluation gates |

## 3. รอบสอง — รายละเอียดของ workflow

ตอบหลัง Q03 เลือกงานหลักแล้ว เพื่อลงลึกเฉพาะสิ่งที่มีผลต่อรุ่นแรก

| ID | คำถาม | ต้องใช้คำตอบก่อน |
|---|---|---|
| Q07 | ต้องการ IDE ใน Hermetrix เป็นตัวหลัก หรือยอมให้เชื่อม VS Code/Cursor/IDE เดิมร่วมกันได้? | ตัดสิน embedding editor, LSP/DAP และต้นทุนทำ IDE เอง |
| Q08 | ยืนยัน Go/Fiber, React/Next.js, Python, PHP; ยังต้องเลือก repo แรก, DB, เวอร์ชันและขนาด/monorepo | language servers, formatter, run/test profiles, indexing |
| Q09 | IDE รุ่นแรกต้องมีอะไรบ้าง: completion, definition/references, rename, diagnostics, Git diff, breakpoint, watch, call stack? | ขอบเขต IDE และ acceptance test ราย feature |
| Q10 | Code review ต้อง review working changes, commit, branch หรือ PR และจะเชื่อม Git host ใด? | baseline/revision contract, finding anchors, write/publish policy |
| Q11 | การตัดวิดีโอใช้แอปใดอยู่ เช่น DaVinci Resolve, Premiere, CapCut หรือรับ workflow ผ่าน FFmpeg ได้? มี MCP ที่ใช้อยู่แล้วหรือไม่? | adapter และ real compatibility spike; ไม่ถือว่า MCP ทุกตัวทำเหมือนกัน |
| Q12 | งานวิดีโอต้องทำอะไรบ้าง ขนาด/ความยาว/ความละเอียด/codec เท่าไร และต้อง preview/timeline ในแอปนี้หรือแอปตัดต่อ? | asset streaming, thumbnails/transcript, storage, render job และ UI |
| Q13 | ต้องควบคุมแอปใดบนเครื่อง ต้องใช้ browser อย่างเดียวหรือแอป native และยอมรับ Accessibility/Screen Recording permissions หรือไม่? | computer-control backend, target identity, observation และ packaging |
| Q14 | งานควบคุมเครื่องทำเฉพาะขณะคุณเฝ้า หรือขณะไม่ได้อยู่หน้าเครื่องด้วย? ถ้าสลับแอป/จอ/ล็อกเครื่องให้ทำอย่างไร? | exclusive control, pause, stale-observation guard และ recovery |
| Q15 | ต้องการโหมด Plan/Act/Review, draft ผลก่อนใช้จริง หรือปุ่มเริ่มแบบครั้งเดียว? และภาษา UI/คำตอบเป็นไทย อังกฤษ หรือทั้งคู่? | interaction flow, approval summary และ bilingual retrieval |
| Q16 | มีแอป/เครื่องมืออื่นใดที่ต้องเชื่อมตั้งแต่รุ่นแรก และ credential/สิทธิ์มีพร้อมหรือยัง? | dependency manifest, readiness และขอบเขต integration |

## 4. รอบสาม — Learning, operations และการส่งมอบ

| ID | คำถาม | ต้องใช้คำตอบก่อน |
|---|---|---|
| Q17 | บทเรียนใช้ข้าม project/ลูกค้า/ผู้ใช้ได้หรือไม่ อะไรต้องอยู่เฉพาะงานเดิม? | filtering ก่อน retrieval, sharing และการยกระดับ scope |
| Q18 | ให้เก็บ chat/log/diff/ภาพ/เสียง/วิดีโอนานเท่าไร ใช้พื้นที่สูงสุดเท่าไร และต้องมี private/incognito task หรือไม่? | retention quotas, indexes, CAS GC และ data deletion |
| Q19 | เมื่อกด “ลืม” ต้องลบใน index/บทเรียน/backup/export ด้วยหรือไม่ มีข้อกำหนดงานลูกค้าที่ต้องปฏิบัติตามไหม? | lineage invalidation, crypto/key management และ backup lifecycle |
| Q20 | ต้องการ feedback แบบใด: สำเร็จ/ไม่สำเร็จ, แก้คำตอบ, ให้คะแนน หรืออนุมัติบทเรียนเป็นรอบ? | ground truth, labels และภาระ human review |
| Q21 | ยอมให้งานเรียนรู้ใช้ GPU/CPU ช่วงใด มีเพดานเวลา/พลังงาน และยอมให้โหลดโมเดลเสริมสำหรับ embedding/vision/audio หรือไม่? | resource scheduler และ learning throughput |
| Q22 | เป้าหมายใน 6–12 เดือนคือกี่ผู้ใช้ กี่ project กี่งานพร้อมกัน กี่เครื่อง และข้อมูลกี่ GB/TB? | load model และจุดที่ควรแยก worker/เปลี่ยน storage |
| Q23 | ยอมรอคำตอบแรก/งานย่อยนานเท่าไร และงานต้องรันข้ามวันหรือ resume หลังเครื่อง sleep/restart ได้ระดับใด? | latency SLO, durable leases และ recovery guarantees |
| Q24 | ยืนยัน native Windows + macOS; เหลือขั้นต่ำ OS/CPU, การ signing/update และยังต้องใช้ browser UI แยกด้วยหรือไม่? | desktop shell, release pipeline, update/permissions และ testing matrix |
| Q25 | มีทีมกี่คน ภาษา/stack ที่ถนัด เวลาและงบโดยประมาณเท่าไร? | ลำดับงานและประมาณ effort; ยังไม่กำหนดวันเสร็จโดยเดา |
| Q26 | ข้อมูลเก่าอะไรต้องย้ายครบ และยอมมี maintenance window/ช่วงทดลองใช้คู่ขนานได้หรือไม่? | migration/rollback/compatibility และ rollout |
| Q27 | สำหรับรุ่นแรก อะไร “ต้องมี”, “เลื่อนได้” และอะไรที่ไม่ต้องการให้ระบบทำเลย? | release boundary และป้องกัน scope ขยายไม่มีจุดจบ |
| Q28 | สลับเครื่องคือใช้แยกกัน, รับช่วงงานเดิม หรือ remote กลับเครื่องเดิม? Mac ต้องทำงานได้เมื่อ Windows ปิดหรือไม่ และ source ใช้ Git อยู่หรือไม่? | checkpoint portability, host ownership, offline, sync และ conflict; ส่งถามแล้ว |
| Q29 | ยืนยันทุก environment รวม production; เหลือ targets/allowed actions, test accounts/fixtures/reset และ rate/maintenance policy | ความสามารถ production ไม่ใช่คำอนุญาตทดสอบระบบใดทันที |
| Q30 | ผู้ใช้ขอออกแบบ Hub/MCP tools เพิ่มได้; Hermes identity/API ยังไม่ทราบ จึงแยก Hub กับ optional Hermes adapter | ไม่ block design บนชื่อ Hermes แต่ต้องตรวจตัวจริงก่อนเชื่อม/ติดตั้งทับ service เดิม |
| Q31 | ยืนยัน Pi5/RAM16/M.2/LANหรือVPN; เหลือ OS/พื้นที่/backup/host access และ offline behavior | topology, reliability, deployment และ sync contract |
| Q32 | Voice ต้องใช้ไทย/อังกฤษสลับกันระดับใด และเริ่มด้วย continuous conversation, push-to-talk หรือทั้งคู่? | ASR/TTS quality, endpointing, interruption และ keyboard/accessibility fallback; ยังไม่ล็อก |
| Q33 | อนุญาตส่งเสียงไป cloud provider หรือให้ถอด/สร้างเสียง local เท่านั้น และอนุญาตเก็บเสียง/transcript/บทเรียนแบบใด? | audio egress และ retention แยกจากการเลือก text gateway; ยังไม่อนุมานการอนุญาต |
| Q34 | ยอมรับเวลาตอบเสียงและหยุดเมื่อพูดแทรกเท่าไร ต้องคุยต่อขณะ coding/tests ทำงานหรือไม่ และการสั่งงานด้วยเสียงอนุมัติ effects ได้ระดับใด? | latency SLO, scheduling, task control และ operation-bound confirmation; ยังไม่ล็อก |

## 5. แบบฟอร์มตัวอย่างงานจริง

ใช้หนึ่งใบต่อหนึ่ง golden workflow ไม่ต้องมีทุกสาขาตั้งแต่แรก

```text
ชื่อและลำดับความสำคัญ:
ผู้ใช้ / project / แอป:
สิ่งที่ให้มา (ไฟล์, repo, prompt, วิดีโอ):
สิ่งที่ต้องทำและเงื่อนไขที่ห้ามหลุด:
การกระทำที่ให้ทำเองได้ / ที่ต้องถาม:
ผลลัพธ์ที่ต้องส่งมอบ:
ผู้ตัดสินและวิธีตรวจว่าสำเร็จ:
ตัวอย่างความผิดพลาดที่เคยเจอ:
เวลา/ทรัพยากรที่ยอมรับ:
ข้อมูลที่เก็บเพื่อเรียนรู้ได้:
สิ่งที่อยากให้ครั้งถัดไปทำดีขึ้น:
```

ตัวอย่าง acceptance ที่เสนอให้เลือกสำหรับ workflow ที่ขอแล้ว ยังไม่ใช่ test case ที่ผู้ใช้ยืนยัน:

1. Coding: แก้บั๊กใน repo จริง → test ที่เคยล้มผ่าน → review diff ตรง revision → งานใกล้เคียงครั้งถัดไปไม่ทำพลาดเดิม
2. Media: รับ source clips → ร่างแผนตัด/คำบรรยาย → preview → export ตามสเปก → ตรวจไฟล์/เสียง/ช่วงเวลา
3. Computer: ใช้แอปที่ระบุสร้างผลลัพธ์ → หยุดเมื่อ target เปลี่ยน/ต้องใช้สิทธิ์ใหม่ → ยืนยันงานเสร็จจาก state จริง
4. Customer flow: เตรียมข้อมูลผ่าน API → ใช้งานหน้าเว็บด้วย role ที่กำหนด → ตรวจค่าที่บันทึกผ่าน API → เก็บ trace → หากผิด สร้าง reproduction และแผนแก้
5. Security: ระบุระบบ/รุ่นและขอบเขตที่อนุญาต → เลือก test matrix → เก็บ confirmed/suspected findings แยกกัน → report → retest patch
6. Research: คำถามพร้อมเงื่อนไข → ตรวจแหล่งข้อมูล → เก็บที่มา/วันที่ → สรุปพร้อมสิ่งที่ยังไม่ทราบ
7. Voice: เปิดโหมดคุยใน task → พูดคำสั่ง → เห็น transcript และได้เสียงตอบแบบ streaming → พูดแทรกแก้โจทย์ → ไม่พูดคำตอบเก่าต่อ/ไม่รัน action จาก transcript ที่ยังไม่จบ → ปิด mic แล้วหยุด capture จริง

## 6. สิ่งที่ยังไม่ล็อก

Framework UI, native shell, จำนวน services, storage สำหรับทีม, ภาษา IDE แรก, debugger adapters, media app, MCP servers, ค่า context budget, metric thresholds, retention, auto-learning และเวลา delivery ยังเป็น decision ที่รอหลักฐาน/คำตอบ

Realtime voice ยืนยันเป็นความสามารถอนาคตที่ต้องเผื่อโครงสร้างแล้ว; provider/transport/languages/latency/recording policy ยังไม่ล็อก รายละเอียดอยู่ใน [voice extension design](2026-09-13-realtime-voice.md) ต้องวาง task/session/event boundaries ตั้งแต่ foundation แต่ไม่บังคับเปิดไมค์หรือส่งมอบเสียงก่อน coding slice

เป้าหมายของ discovery คือมี product brief และ acceptance criteria ที่คุณยืนยันได้ก่อนทำ implementation backlog รายไฟล์ สถานะเอกสารจะเปลี่ยนเป็น requirements-baselined เมื่อ Q01–Q06 และคำถามที่เกี่ยวกับ workflow รุ่นแรกได้รับคำตอบ พร้อมตัดสิน Q22–Q31 เท่าที่มีผลต่อรุ่นนั้น ไม่จำเป็นต้องตอบคำถาม media ทั้งหมดก่อนเริ่มพิสูจน์ coding slice แต่ห้ามออกแบบ data/job interfaces โดยไม่เผื่อไฟล์ใหญ่และงานนาน

## 7. วิธีพัฒนาที่ผู้ใช้เลือก

**D01 — Astra-led delegation:** ผู้ใช้ต้องการ Astra เป็นผู้สั่งงาน/กำกับ และให้ sub-agents ทำงานย่อย โดยคงมาตรฐานคุณภาพของผลส่งมอบ เป้าหมายนี้หมายถึงวิธีพัฒนา Hermetrix ในงานปัจจุบัน ไม่ใช่เปลี่ยน LLM runtime ของ Hermetrix จาก gateway/local provider เป็น Astra โดยอัตโนมัติ

Astra รับผิดชอบ requirements/architecture, task briefs/acceptance criteria, integration, ตรวจ actual diff/test evidence และ final acceptance งานที่ delegate ต้องมี scope/ownership/interfaces และผลตรวจที่ทำซ้ำได้ ไม่รับงานจากคำรายงานว่าเสร็จเพียงอย่างเดียว

Sub-agent เป็นบทบาทการแบ่งงาน ไม่จำเป็นต้องเป็นโมเดลที่เล็กกว่า ผู้ใช้ยังไม่ได้เลือกโมเดลรอง จึงไม่เปลี่ยนไปโมเดลรองหรือ reasoning ต่ำกว่าอย่างเงียบ ๆ ให้คง/inherit การตั้งค่าที่ใช้อยู่และตรวจชื่อโมเดลจาก execution configuration ก่อนอ้างว่าเป็น Astra เกณฑ์และรายละเอียดอยู่ในหัวข้อ Astra-led delivery ของ [blueprint](2026-09-13-blueprint.md)

“คุณภาพเหมือน Astra ทำ” เป็นเป้าหมายที่ต้องวัดด้วย acceptance/workflow tests และการตรวจรับ ไม่ใช่การรับประกันความเท่ากันทุกงานจากชื่อโมเดล จำนวน agents หรือการผ่าน review หลายรอบ งานที่ยังไม่ถูกตรวจจริงต้องแสดงสถานะนั้น ไม่ลดเกณฑ์เมื่อหมด quota

**D02 — Hermetrix runtime ownership:** ผู้ใช้ยืนยันภายหลังว่าเป้าหมายผลิตภัณฑ์คือ Hermetrix ต้องเป็นผู้วางแผนและควบคุมงานเอง จัด context ใช้ MCP/Skills/plugins/browser ลงมือและทดสอบผลจริง พร้อมเก็บบทเรียนที่ตรวจสอบได้ บทบาท Astra-led ใน D01 จึงเป็นวิธีพัฒนาและตรวจรับ Hermetrix ระหว่างสร้างระบบ ส่วน runtime ที่ส่งมอบต้องดำเนิน golden workflow ได้โดยไม่ต้องมี Codex/Astra คอยแตก task ทุกครั้ง
