# Project Brain: ตรวจ Pi และความเป็นไปได้ — 2026-10-10

ตรวจประมาณ 16:06–16:10 น. Asia/Bangkok ผ่าน SSH `pizp2e0@PIZP2E0`, source ของ deployed revision และ MCP จริง Project Brain เป็นระบบอิสระ ใช้ร่วมกับ Hermetrix/Codex/Hermes ผ่าน MCP เท่านั้น ไม่รับผิดชอบ local execution, Desktop, Stop/Queue หรือ authority ของ Hermetrix

[แผน Hermetrix และผลแก้ระบบ](2026-10-10-hermetrix-plan-review.md) แยกจากรายงานนี้ รายงาน historical `pi-agent-platform-project-brain-reality-audit.md` ต้องอ่านคู่ rollout review ไม่ใช้ pre-rollout findings เป็นสถานะ live

## สถานะที่ตรวจได้

| เรื่อง | ผลจริง |
|---|---|
| Hardware | aarch64; RAM ประมาณ 15 GiB, available14 GiB; NVMe468 GiB ใช้80 GiB; swapไม่ได้ใช้ ณ snapshot |
| Services | kanban-board, kanban-mcp, second-brain-mcp, hermes-gateway active; listeners8901/8902/8765 เป็น loopback; cloudflared/SSH/Tailscale active |
| Source | Kanban`3fa0e0c` clean; Second Brain`2730d21` มี untracked credential backupสองไฟล์เดิม ไม่อ่าน/แก้ไฟล์เหล่านั้น |
| Database | integrity_check=ok; 5workspaces, 6platform projects, 0platform services, 3nodes, 3agents, 5credentials, 10grants, 6board projects, 76tasks |
| MCP board | list_platform_projects คืนหกโครงการที่ credentialได้รับ grant ไม่มี write probe |
| HTTP knowledge access | 6scoped clients; legacy override/public read/trusted stdio ปิดใน processนี้ |
| Isolation | credentialโปรเจกต์นี้ถูกปฏิเสธเมื่อ query foreign`cop-svt-svtap-api`; ผ่านหนึ่ง negative live case ไม่อ้างว่าครบทุกendpoint |
| Expiry | ห้าclients`2026-12-22T19:30:19Z` = 23ธ.ค.02:30:19ไทย; submit client`2026-12-23T04:14:13Z` = 23ธ.ค.11:14:13ไทย |
| Backup | timerรันรายชั่วโมง; SD mirrorมี HEAD`bc114bd` ตรง vault; service successไม่ได้พิสูจน์ state restore |
| Vault | visible retrieval Markdown99ไฟล์; corpus ส่วนใหญ่เป็น raw83; Gitมี search cache modified และ `.second-brain-state/` untracked |

พื้นฐาน per-project access, curated retrieval, candidate/admission มีแล้ว ใช้ระบบเดิมต่อได้ Pi เหมาะกับ metadata/SQLite/Markdown/MCP ไม่ต้องย้ายโมเดลหนักหรือ executorของ Hermetrixมาที่ Pi ยังไม่มี hardware benchmarkรองรับข้ออ้าง inference performance

## Retrieval recovery ที่ทำ

Query`planning tasks` สองครั้งตอบ`partial`, items=[] พร้อม incomplete-refresh/changed-source warnings Cache integrityยังok แต่ manifest99rowsมี stale signatures20ไฟล์ รวม referenceที่พร้อมใช้ ตรวจ codeพบ refreshใน requestใช้ประมาณครึ่งหนึ่งของdeadline2วินาที

ตามสิทธิ์ที่ผู้ใช้ให้ปรับบนPi สำรอง consistent SQLite cache ที่:

`/home/pizp2e0/backups/project-brain-index-audit-20261010-160855/search-before.sqlite3`

จากนั้นใช้ maintenance commandเดิม:

```text
.venv/bin/python -m second_brain.manage reindex --vault <existing vault>
exit=0; complete=true; changed=20; skipped=0; elapsed=1.21s
Markdown fingerprints unchanged=true (102 files including metadata)
```

ผล live หลังปรับ: queryเดิม`ok`, 2passages, Solutionมาก่อนProblem; exact-version readตรงข้อความหลังtrim newline; unrelated`quantum banana protocol`เป็นno_match; servicesเดิมทั้งสี่ยังactive

นี่คือ recovery/maintenanceของcache ไม่ใช่ root-cause fixถาวร ยังไม่พิสูจน์ว่า signatureเปลี่ยนจากอะไรหรือวัด cold-refresh worst-case ไม่มี source/service/credential/Markdown/task/admission change บนPi

## แผนของ Project Brain เอง

| ลำดับ | งาน | เกณฑ์ผ่าน |
|---|---|---|
| B0 | Index/readiness | scoped query+version-bound passageเป็น readiness; warm indexก่อนพร้อมใช้; cold/stale corpusทดสอบซ้ำ; partial rate/cache age/latencyเห็นจริง |
| B1 | State backup/restore | แยก Markdown/Git, authoritative session/candidate state และ disposablecache; consistent SQLite snapshotรวมWAL; restoreบนสำเนาเทียบhash/rows |
| B2 | Rotationและ grants | เตือนexpiry30/14/7วัน, rotate/revokeอย่างตรวจได้; isolation negativesทุกread/write/candidate endpointและmalformed/expiredcredential |
| B3 | Candidate admission | independent curator, immutable evidence/digest/revision, duplicate/conflict/revocation; idempotent inertsubmission; ไม่ให้senderpromoteเอง |
| B4 | Relevanceและ outcomes | ชุดคำถามไทย/อังกฤษ answerable/no-match/foreign/stale; top-3 applicability,citation consistency,p95latency,bytecost; เพิ่มreranker/embeddingเมื่อbaselineมีfailureวัดได้ |

Backupunitใช้`git push ...; touch marker` ซึ่งmarkerอาจใหม่แม้pushล้ม ควรผูกmarkerกับpush exit=0และทดสอบfailures; รอบนี้ไม่ได้แก้unit และไม่ใช้service successเป็นbackup guarantee การpushGitอย่างเดียวไม่ยืนยันว่า `.second-brain-state` สำรองครบ

ถ้าขยาย transport ให้ OAuth MCP หลาย client ควรยืนยัน resource/audience binding และ token isolation ตาม [MCP authorization](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/basic/authorization/index.mdx) แต่ไม่มี consumer requirement ที่ทำให้ต้องเปลี่ยน scoped token deployment นี้ทันที

ไม่ต้องสร้างRouter/HANDOFFเพื่อทำknowledge sharing เมื่อจะทำdistributedexecutionต้องแยกโครงการและพิสูจน์lease/generationfencing/quiescence/uncertain-effect reconciliationก่อน เงื่อนไขนี้ไม่โยนexecutionauthorityเข้ามาในProjectBrain

## Verification

```text
.venv/bin/python -m pytest -q -p no:cacheprovider
 tests/test_project_isolation.py tests/test_candidates.py
 tests/test_admission.py tests/test_retrieval.py
52 passed in 3.52s

Live: own query partial→maintenance→ok; exactversion passage matches aftertrim
Live: foreign scope denied; unrelated query no_match
Pi: Kanban integrity_check=ok; four application services active
```

Kanban integration testไม่รันผ่านในenvironmentนี้: systemPythonขาดmcp, servicevenvและBrainvenvขาดwerkzeug ไม่ติดตั้งdependencyใหม่หรือเปลี่ยนserviceenvironment; liveMCP discoveryผ่านแยกกัน

ไม่ได้รันfullsuite, write/curationprobe, restore drill, fullcorpusbenchmark หรือliveHermetrix/Bonsai reuseใหม่ จึงอ้างเพียง scopedretrievalที่ตรวจและ recovery outcome ไม่ใช่fulltargetarchitecture/automaticlearning และไม่ตั้งautomationหรือtimerใหม่
