# Raspberry Pi Knowledge Hub — Proposed contract & deployment plan

วันที่: 2026-09-13 · สถานะ: design draft เท่านั้น ยังไม่ได้สร้าง service, เชื่อมต่อ Pi, ติดตั้ง หรือเก็บ credentials

ยืนยันจากผู้ใช้: Pi5 RAM16GB + M.2, LAN/VPN, มี Hermes อยู่ และต้องการ MCP tools กลางให้ AI หลายตัวแลกเปลี่ยนความรู้/ข้อผิดพลาด/ข้อดี อ้างอิง [blueprint](2026-09-13-blueprint.md) และ [requirements](2026-09-13-requirements.md)

## 1. ขอบเขต service

เสนอหนึ่ง Go service บน Pi เป็นเจ้าของ local database และ application services มี MCP façade สำหรับ model clients, deterministic sync/worker/admin APIs และ streaming artifact API อยู่ใน process เดียวกันได้ ไม่สร้าง microservices หลายตัวตั้งแต่แรก

ชื่อ `Knowledge Hub` เป็นชื่อ component ที่เสนอ ไม่อ้างว่าเป็น feature ที่ Hermes เดิมมีแล้ว ติดตั้งแยก service account/config/data directory/listener จาก Hermes; เชื่อม optional adapter เมื่อรู้ repository/version/API และทดสอบ compatibility แล้ว

Pi จัดการ metadata, lexical retrieval, job coordination, evidence catalog และ storage ตาม quota งาน inference/embedding/vision/render หนักอยู่ worker/provider ที่เลือก Mac ไม่รับ inference jobs ใน baseline ปัจจุบัน และ worker รับงาน remote-provider ได้โดยเก็บ provider credential ที่ worker ไม่ปนกับ Hub access credential

Realtime voice อนาคตไม่ใช้ Hub/MCP เป็นทางผ่าน audio frames; capture/playback อยู่ desktop และ speech inference อยู่ provider/worker ที่เลือก Hub รับ finalized/redacted transcript หรือ decision/evidence refs เฉพาะตาม recording/sharing policy โดยไม่ตั้งต้นว่าเก็บเสียงสดหรือบทสนทนาทั้งหมดได้ ดู [voice design](2026-09-13-realtime-voice.md)

## 2. Data contracts

| Object | ข้อมูลสำคัญ |
|---|---|
| Identity/Scope | user/device/worker IDs, customer/project grants, schema revision |
| Experience | immutable success/failure/correction observation, expected/actual, task/run/model/tool/env refs, sensitivity, evidence refs |
| Incident/Defect | occurrence vs tracked problem, hypotheses, repro status, affected versions, resolution/reopen evidence |
| LessonVersion | candidate/approved/superseded/revoked, applicability, procedure/fact/counterexample, evidence lineage, evaluator provenance |
| UseFeedback | exact lesson version, later task/check, used/rejected/contradicted, outcome evidence; ไม่เปลี่ยน trust เอง |
| TaskCheckpoint | requirement/plan/source revisions, next actions, pending/uncertain effects, owner/device และ prerequisites |
| ArtifactManifest | hash/media type/size, owner/scope, source location, upload/availability/integrity state และ retention |
| ChangeRecord | ordered durable cursor, entity/version, operation/tombstone, scope; ไม่ใส่ raw secret |

Source code, video content และ transcript ยาวไม่อยู่ใน lesson body ใช้ refs + bounded sanitized facts Search index เป็น derived data; เก็บ provenance และ original evidence ที่ได้รับอนุญาตเพื่อให้ audit ได้

## 3. Model-facing MCP catalog

ชื่อด้านล่างเป็น proposed tool names ต้อง validate กับ protocol/client ที่เลือกก่อน freeze ทุก tool ใช้ typed schema, bounded input/output, pagination และ predictable error codes ไม่มี unrestricted SQL/shell/filesystem tools ใน Hub

| Tool | Input หลัก | ผลและข้อจำกัด |
|---|---|---|
| `hub_capabilities` | requested schema/features | supported versions, limits, readiness; ไม่เปิดเผย secrets/host internals ที่ไม่จำเป็น |
| `knowledge_search` | project scope, query, task context, limit/cursor | approved/applicable summaries + exact version/evidence IDs; candidate search ต้อง explicit และมี label |
| `knowledge_read` | entry ID + version | bounded body, applicability, trust, provenance; authorize ซ้ำก่อน hydrate |
| `experience_submit` | operation ID, expected/observed, env, outcome claim, evidence refs | immutable experience ID; success claim ไม่ถูกยกระดับเป็น verified fact อัตโนมัติ |
| `experience_read` | experience ID | observations/evidence metadata ที่มีสิทธิ์ ไม่ dereference external URL โดยไร้ policy |
| `incident_find` | project, symptoms, env/revision | related candidates พร้อมความต่าง; ไม่ merge defect เอง |
| `lesson_propose` | operation ID, evidence IDs, applicability, proposed lesson/base version | quarantined candidate; edit proposal เป็น version ใหม่ ไม่แก้ approved entry ตรง ๆ |
| `lesson_feedback` | operation ID, exact version, use/rejection/contradiction + evidence | append feedback แล้วคิว reevaluation ไม่ให้ feedback เพิ่ม trust เอง |
| `artifact_prepare` | operation ID, manifest, sensitivity | scoped transfer handle/limits; ไม่รับ JSON/base64 payload ก้อนใหญ่ |
| `artifact_status` | artifact/transfer ID | pending/available/missing/failed + verified byte/hash status |

ข้อความที่ส่งมาเป็น untrusted data ไม่เป็นคำสั่ง admin และ tool response ไม่ให้ client ตีความว่าได้สิทธิ์เพิ่มเติม Retrieval ไม่อาศัย user-supplied `scope` อย่างเดียว แต่ intersect กับ authenticated grants ฝั่ง server เสมอ

## 4. APIs ที่ไม่ควรให้โมเดลเรียกแบบอิสระ

- Sync API: versioned task/plan writes, change feed `changes.read`, idempotent outbox reconcile และ conflict responses
- Worker API: claim/renew/complete evaluation job, bounded evidence bundle, fencing token และ cancellation; worker identity ได้เฉพาะ job ที่ claim
- Admin/UI API: approve/reject/revoke lesson, resolve conflict, device enrollment/revoke, sharing/retention, export/forget และ backup operations
- Artifact byte API: streaming/chunk/range transfer, hashes, quotas, interrupted upload recovery, temporary blob GC และ download authorization

MCP client wrappers เรียก application services เดียวกันได้ แต่ lifecycle ที่จำเป็น เช่นบันทึก checkpoint/sync ต้องถูก client app ขับแน่นอน ไม่ขึ้นกับโมเดลตัดสินใจเรียก tool หรือไม่

## 5. Auth, secrets และ pairing

แยก identities: ผู้ใช้, native app device, Hermes bridge, evaluation worker และ other AI client ไม่ใช้ token เดียวร่วมทุกตัว `actor` ที่เชื่อถือได้มาจาก authentication ไม่ใช่ข้อความใน tool args

Permissions ที่เสนอ: `knowledge:read`, `experience:append`, `candidate:propose`, `feedback:append`, `artifact:read/upload`, `job:claim/result`, `knowledge:approve/revoke`, `policy:admin`, `data:export/forget` ทุก permission จำกัด customer/project และอาจจำกัด job/artifact เพิ่ม

Normal AI credential อ่าน/เสนอ/feedback ได้ แต่ approve/revoke/admin ไม่ได้โดยปริยาย Approval UI ใช้สิทธิ์ผู้ใช้แยกจาก model tools และทุกการตัดสินใจมี audit record

Pairing flow ที่เสนอ:

1. ผู้ใช้เข้าหน้า admin ผ่านช่องทางที่รับรองตัวตน สร้าง one-time enrollment ที่หมดอายุและจำกัด device/scope
2. เครื่องปลายทางแลก enrollment เป็น credential ของตัวเอง เก็บใน OS credential store; UI แสดง scopes/last use/revoke
3. Server เก็บ verifier/hash สำหรับ opaque tokens หรือ public identity material ตาม auth scheme ที่เลือก ไม่ log bearer values
4. Rotate/revoke แยก device ได้; lost laptop ไม่ต้องเปลี่ยน provider/API keys ของทุกระบบ หากสิทธิ์ไม่ได้เชื่อมกัน

Provider API keys, Hub credentials, SSH credentials และ video-app tokens เป็นคนละ secret references ห้ามเก็บใน knowledge/evidence/prompts/README/Git และไม่ให้ AI ส่งคีย์แลกกันผ่านบทเรียน การติดตั้งใช้ secure local entry/OS store/secret provisioning ผ่าน authenticated channel ไม่ขอ private key หรือ password มาแปะในแชท

LAN/VPN ไม่แทน application authentication; transport encryption/host verification ต้องชัดเจนตามการ deploy ใช้ read-only inspection ของ network ก่อนเลือก listen address/firewall/reverse proxy และไม่เปิด Pi service สู่ public internetโดยปริยาย

## 6. Consistency และ failure behavior

ทุก mutation มี stable operation ID, schema version และ expected revision หากแก้ mutable record Server bind idempotency key กับ authenticated actor/scope + payload hash; key เดิม payload ต่างต้อง reject ผลสำเร็จต้อง durable ก่อน ack และ dedup state อายุไม่น้อยกว่าช่วง replay/outbox ที่รองรับ มิฉะนั้น old replay ต้อง reject/reconcile ไม่ประมวลผลเหมือน request ใหม่

Response loss: client query/replay **metadata operation ที่มี dedup contract** ด้วย ID เดิมแล้วได้ original result ไม่มี exactly-once guarantee สำหรับ desktop/video/API effects ที่ไม่มี contract นี้

Concurrent edits: expected revision ไม่ตรงคืน typed conflict + current version; user/merge service resolve ห้าม blind overwrite Requirement เปลี่ยนทำให้ validation/plan ที่ผูก revision เก่าขึ้น stale

Lease/fencing: หนึ่ง active task/job owner ตาม coordinator; stale worker result รับได้เป็น historical evidence แต่ไม่เปลี่ยน current task state ถ้าต้องหยุด effect ที่กำลังวิ่งจริงให้ host runtime enforce cancellation/lease policy ด้วย server database check อย่างเดียวหยุด external process ไม่ได้

Offline: local drafts/checkpoints/outbox ปลอดภัยได้ แต่ autonomous shared-task continuation ยังรอ policy ผู้ใช้ หากประสาน ownership ไม่ได้ ใช้ local task fork หรือรอกลับ online ห้ามทั้งสองเครื่องถือว่ารับช่วงสำเร็จพร้อมกัน

Durable cursor feed เป็นแหล่งตาม sync; notifications/subscriptions ถ้ามีเป็น wake-up hint ไม่ใช่หลักฐานว่าไม่พลาด changes Client cursor เก่าเกิน retention ต้อง full scoped resync โดยไม่ resurrect tombstones

Artifacts มี states `local-only / upload-pending / available / missing / corrupt` Sync manifest สำเร็จไม่แปลว่า bytes พร้อม ต้อง verify hash/size และ authorized locations; SSRF/path traversal/symlink/archive extraction checks อยู่ที่ ingestion ไม่ให้ arbitrary URL/path ผ่านตรง

## 7. ป้องกันการแชร์ข้อสรุปผิดและ data poisoning

Flow: observation → candidate → relevant evaluation → user/policy promotion → scoped retrieval → measured reuse → revoke/revalidate

แยก observation/hypothesis/procedure; เก็บ contradictory evidence ไม่กลบด้วยเสียงข้างมากหลายโมเดล Model สองตัวพูดตรงกันไม่ได้พิสูจน์ว่า code/test ถูกต้อง การ validation ต้องผูก actual checks/user acceptance และ held-out cases ตามชนิดบทเรียน

การสรุปใช้ได้เฉพาะ source/evidence ที่มีสิทธิ์และ redacted แล้ว ข้อมูลจากลูกค้า A ไม่เข้า retrieval ของ B แม้ embedding ใกล้กัน การยกระดับเป็น general procedure ต้องไม่มี private facts และผ่าน sharing policy

Lesson ไม่เปลี่ยน tool permissions/ติดตั้ง MCP/อนุมัติ production effects อัตโนมัติ สามารถสร้าง proposal ขอเปลี่ยน policy ได้แต่ต้องเข้าช่องทางผู้ใช้ต่างหาก

กด revoke ต้องยกเลิก reuse/index versions ที่เกี่ยวและ propagate change; task ที่เคยใช้ lesson เก็บประวัติเดิมพร้อม revoked annotation ไม่ rewrite evidence ย้อนหลัง คำสั่ง forget มี lineage/deletion status ชัด รวม behavior ของ offline clients และ backups

Voice-derived knowledge ต้องอ้าง utterance/transcript revision และแยก ASR error, user correction, task failure และ response ถูกพูดแทรก ไม่ใช้เสียงสะท้อน/partial transcript/ผู้ใช้เงียบเป็น feedback ยืนยันความถูกต้อง การสรุปหรือ embedding จากเสียงอยู่ภายใต้ consent/scope/retention เดียวกับต้นฉบับ การ revoke การเก็บต้องหยุด new ingestion และจัดการ derived data ตาม lineage

## 8. Installation plan — ยังไม่ลงเครื่องจริง

### สิ่งที่ต้องทราบก่อน deploy

Pi hostname/IP หรือ existing SSH alias, OS/version/architecture, service manager, authorized access method, M.2 free space/filesystem, existing Hermes service/ports/data paths, backup destination และว่าจะใช้ LAN หรือ VPN เป็นช่องหลัก ไม่ต้องส่ง password/private key/API key ในแชท

### Rollout ทีละขั้น

1. Inspect read-only: OS/storage/listeners/Hermes config shape และ resource usage โดยไม่อ่าน token values; บันทึกสิ่งที่ต้องรักษา
2. เตรียม pinned build/package ตาม architecture พร้อม checksum, service config/data layout และ upgrade/rollback runbook; service account สิทธิ์เท่าที่ต้องใช้
3. ตั้ง quota/log rotation/health/restart behavior และ auth/transport; ไม่แก้ Hermes เดิมหรือ firewall กว้าง ๆ โดยไม่มี scope ชัด
4. Synthetic-only trial: discovery → authorized read → deduplicated write → restart/readback → scope denial → conflict → forget/restore
5. Pair Mac/Windows ด้วยคนละ credential; ส่ง sanitized experience หนึ่งเคส, propose lesson, validate/promote, retrieve กลับอีกเครื่อง
6. ทดสอบ actual lesson reuse ใน task ใหม่ด้วย selected gateway; provider credential อยู่ฝั่ง worker ที่รัน model ไม่ใช่ lesson store
7. เพิ่ม evidence streaming + coordinator workers + optional Hermes adapter หลัง contract tests ผ่าน
8. Enable real-project scopes ทีละ project และ backup/restore drill ก่อนถือว่า Hub เป็นศูนย์กลางที่เชื่อถือได้

### Mandatory acceptance

- Unauthorized/cross-project reads และ model self-approval ถูกปฏิเสธ
- Duplicate writes หลัง response loss ไม่สร้าง experience/job ซ้ำ; stale revision ไม่ overwrite
- Pi restart/disk-full และ interrupted transfer ไม่ ack ข้อมูลที่ไม่ durable หรือแสดงไฟล์ไม่ครบว่า available
- Client offline/reconnect และ task handoff ไม่ทำ effect ซ้ำ; stale owner ไม่ commit state ใหม่
- Secret scans ของ logs/export/test artifacts ไม่พบ seeded canary credentials
- Approved lesson ใช้ข้ามเครื่องได้เฉพาะ scope ที่ให้; revoked/deleted lesson ไม่กลับมา active หลัง sync/restore
- Backup กู้ไป isolated destination ได้โดยไม่อาศัย M.2 ต้นฉบับ และ restore ไม่กระทบ Hermes เดิม

เป้าหมาย latency/จำนวน jobs/ความจุ/ระยะเก็บข้อมูลยังต้องตกลงและวัด ไม่รับรองว่า Pi เก็บวิดีโอทั้งหมดหรือประมวลผลโมเดลได้เพราะ RAM16GB อย่างเดียว
