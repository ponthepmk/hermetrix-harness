# Handover — สถานะงาน ณ 2026-09-08

เอกสารนี้เป็น snapshot สำหรับคนที่เข้ามาทำ Hermetrix ต่อ ให้ยึด runtime และ tests เหนือข้อความในเอกสารเสมอ

แหล่งอ้างอิงหลัก:

| ต้องการรู้ | อ่านที่ |
|---|---|
| เปรียบเทียบ Aetox/Hermes และช่องว่าง | [`AETOX-HERMES-TRACEABILITY-AUDIT.md`](AETOX-HERMES-TRACEABILITY-AUDIT.md) |
| phase และ qualification gates | [`FUTURE-ARCHITECTURE-PLAN.md`](FUTURE-ARCHITECTURE-PLAN.md) |
| โครงสร้าง runtime ปัจจุบัน | [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| review ตามลำดับเวลา | [`REVIEW.md`](REVIEW.md) |

## 1. Product contract ปัจจุบัน

Hermetrix เป็น local-first Go harness ที่รวม authority-safe Skill lifecycle, typed context compiler, provider qualification, deferred tools/MCP และ Aetox-style product cockpit แบบ clean-room

สถานะ product shell คือ **`vertical_slice`** ไม่ใช่ native parity หรือ production:

- shell เปิดที่ project picker ก่อนเสมอ: project คือ root ของทุกอย่าง ไม่มีโฟลเดอร์โค้ดก็เปิดได้ กด project chip ที่หัวจอกลับไป picker ได้ทุกเมื่อ
- สามโซนปรับขนาดและพับได้อิสระ (rail ซ้าย, main กลาง, side ขวา) กับ view switch สี่มุมมอง Chat/Work/Code/Knowledge — แต่ละ project จำความกว้างโซนและสถานะพับต่อมุมมองของตัวเองไว้ใน localStorage
- Chat คือมุมมองเดียวที่สร้างเสร็จจริงแล้ว มี session dock อยู่ใน rail · Code มี pane-split ของ main สำเร็จแล้ว (files/terminal/browser/output สูงสุด 4 pane ที่จำการจัดวางไว้เช่นกัน) แต่ file tree/editor ของ Code เอง, Work และ Knowledge ทั้งมุมมอง **ยังเป็น spec ไม่ใช่ฟีเจอร์ที่สร้างแล้ว**
- command palette (`⌘K`/`Ctrl+K`), responsive density และ session-aware Skills & tools picker
- Review room แสดง Session Contract, exact revisions, Skill selection, direct tools และ pending approvals
- Review/Files/Office/Team workbench rooms อยู่ใน side pane ของ Chat; Terminal/Browser ย้ายไปเป็น pane content ของ Code view แล้ว เพราะทั้งคู่ต้องการพื้นที่ที่ side pane ความกว้างคงที่ให้ไม่ได้
- Skill Studio, provider/model/profile, MCP/tools, context และ system surfaces
- ใช้ Hermetrix logo/identity; ห้าม copy Aetox source, component หรือ asset เพราะ license boundary

ตัวเลขจาก `./scripts/doc-truth.sh` (2026-09-08, หลัง Review 19 landed):

```text
test functions        451        packages              27
direct primitives      13        HTTP routes           119
SQLite tables          47        schema-only tables      0
Go (non-test)      31,685        Go (test)           18,452
```

## 2. Workbench ที่ทำงานจริงแล้ว

### Files

- project-bound tree/read/write/diff
- กัน path escape, symlink และ non-regular file
- UTF-8 ไม่เกิน 2 MiB
- optimistic save ด้วย expected SHA, atomic replace และ immutable audit receipt

### Terminal

- interactive POSIX PTY: start/input/resize/output/close
- persisted bounded output tail 1 MiB โดย append เฉพาะ chunk ใหม่ (ไม่ rewrite ทั้งก้อนทุก 8 KiB) และ log/fail session เมื่อ persistence พัง
- restart mark session เป็น interrupted และไม่ replay command
- Windows build ผ่าน แต่ยัง fail-closed ว่า PTY unavailable จนมี ConPTY

### Managed Browser

- isolated headless Chrome/Chromium profile และ pure CDP
- open/navigate/back/read/click/type/capture/close
- DOM snapshot ถูกจำกัดขนาดและติดป้าย untrusted evidence
- local/private URL ต้อง explicit opt-in; `file:` จำกัดใน project root
- ทุก redirect/subresource/websocket ผ่าน CDP Fetch guard ก่อน network; Chrome E2E ยืนยัน request ไป loopback ถูกหยุดก่อน server เห็น hit
- screenshot เป็น immutable PNG artifact

ข้อจำกัด: ยังไม่มี embedded live WebView, download workspace และ proxy/DNS-pinned egress enforcement ที่กัน DNS rebinding ได้สมบูรณ์

### Office Deliverables

- สร้าง DOCX/XLSX/PPTX เป็น OOXML ZIP package จริงแบบ deterministic
- PDF package จริงสำหรับ Basic Latin
- ทุกไฟล์อยู่ใน CAS เป็น immutable Artifact พร้อม provenance
- PDF ที่มี Unicode นอก font capability ปัจจุบันถูกปฏิเสธอย่างตรงไปตรงมา

### Agent Team

- team definition persistent และแก้ด้วย optimistic revision
- 1–12 members, exactly one lead, run graph freeze ก่อนเริ่ม
- DAG validation, ไม่เกิน 100 tasks, concurrency cap 4
- child ทุกตัวได้ Agent Session/SessionContract แยก
- UI แก้ roster ด้วย optimistic revision และสร้าง custom task DAG ได้
- run snapshot team/member instructions; การแก้ roster กลาง run ไม่เปลี่ยน execution/provenance ย้อนหลัง
- user cancellation persist ก่อนแล้ว propagate ถึง child contexts; late completion เขียนทับ `cancelled` ไม่ได้
- child exact-effect approval ถูก persist พร้อม summary/preview/effect, pause ทั้ง run และอยู่รอดหลัง recovery; approve/deny resume turn เดิมโดยไม่ replay prompt/tool effect แล้วจึงเดิน DAG ที่เหลือ
- specialist fan-out และ lead synthesis; peer output ถูก label เป็น untrusted evidence
- token usage roll-up; restart จะคง approval ที่รอผู้ใช้ไว้ แต่ task ที่กำลัง sample/resolve effect จะเป็น interrupted โดยไม่ auto-retry

ข้อจำกัด: ยังไม่มี checkpoint/resume กลาง model sampling, artifact-only handoff และ per-task capability/model/budget editor

## 3. Skill authority ที่ต้องรักษา

> **เปลี่ยน default 2026-08-31 ตามคำสั่งเจ้าของ:** เดิม `manual` (agent promote เองไม่ได้) เปลี่ยนเป็น `gated_automation` + `auto_promote_agent_create/improve` เปิด
>
> agent เขียน Skill เองได้ผ่าน `skill_manage` และ policy promote ให้อัตโนมัติ สิ่งที่ทำให้ยังปลอดภัยคือ **ทุก promotion เป็น `AuthorityAction` ที่ย้อนได้** และแสดงใน Skill Studio ว่า "promoted by agent" พร้อมปุ่ม undo
>
> `auto_archive_agent_skills` ยังปิดอยู่ — การตัดสินว่า Skill ตายแล้วเป็นคนละเรื่องกับการเขียน Skill ใหม่

- agent เขียน candidate ได้ และ policy ปัจจุบัน promote ให้ (ย้อนได้เสมอ)
- `skill_manage` action `improve` ต้องส่ง exact `skill_id`+`version_id` ที่โหลดด้วย `skill_view` ใน session เดียวกัน = read-before-write guard
- curator ยัง report-only; auto-archive ยังปิด
- version immutable และตรวจ exact base revision
- protected/imported Skill ต้อง fork ก่อนแก้
- archive restore กลับมาเป็น candidate ไม่ active โดยอัตโนมัติ
- curator เป็น report-only: หา stale/duplicate/consolidation ได้แต่ห้าม mutate เอง
- MCP รองรับ **stdio** (`internal/mcp/stdio.go`) และ Streamable HTTP; stdio ใช้ launcher allowlist (npx/node/bun/deno/uv/uvx/python/python3/docker/go) รันตรงไม่ผ่าน shell และ child env มีแค่ PATH/HOME/locale + token ของตัวเอง
- discovery index ครบ 3 kind: tools, **resources**, **prompts** (`internal/mcp/catalogkinds.go`, `catalogstore.go`) เข้า deferred catalog เดียวกัน ชื่อ capability ขึ้นต้น `resource:` / `prompt:` และ waist ยังเท่าเดิม
- stdio process อยู่ใน pool (`internal/mcp/pool.go`) 1 process ต่อ server, idle 5 นาทีถูกเก็บ, แก้ config = process ใหม่; timeout/cancel จะ kill และ discard session เพื่อไม่ให้ blocking read ยึด pool ค้าง Catalog-list ที่เป็น read-only retry ได้ 1 ครั้ง แต่ `tools/call`, resource read และ prompt rendering ไม่ replay หลัง connection ตาย—request ถัดไปจึงค่อยเปิด process ใหม่
- **sampling + elicitation** ทำแล้ว (`internal/mcp/serverrequests.go`, `internal/agent/mcpbridge.go`): `stdioSession.call` เป็น bidirectional pump ตอบ server-to-client request บน pipe เดียวกัน
  - fail-closed: server ที่ `trust_annotations` ปิด ถูกปฏิเสธทั้งคู่ พร้อมบอกว่าต้องเปิดที่ไหน
  - sampling ใช้ provider ของ session, cap 1024 token และ 4 ครั้งต่อ tool call, ข้อความ server เข้าเป็น user content ใต้ system line ที่บอกว่ามาจาก server
  - elicitation รอ 3 นาที; decline ≠ cancel (คนปฏิเสธ กับ ไม่มีใครตอบ เป็นคนละเรื่อง) และ pending อยู่ใน memory เท่านั้น restart = หาย ซึ่งถูกแล้วเพราะ server ที่รออยู่ก็หายไปด้วย
  - `toolCallBudget("tool_call")` ขยายให้คลุมเวลารอ ส่วน local tool ยัง 10 วินาที
  - **HTTP transport ยังรับ server request ไม่ได้** (ต้องใช้ SSE + POST กลับ) — ประกาศ capability เฉพาะเมื่อมี handler จริง
- **ยังไม่มี:** MCP OAuth
- Skill Studio เป็นที่ให้ผู้ใช้สร้าง แก้ diff/replay/promote/reject/archive/restore และตรวจ provenance/usage

auto-promote เปิดตามคำสั่งเจ้าของแล้ว แต่ auto-delete ยังห้าม ทุก automation ต้องมี undo snapshot, bounded authority และ audit receipt เสมอ

## 4. Context และ provider

- profiles: 32k, 64k, 128k, 256k, 1M
- qualified capacity ใช้ evidence ที่ผูก exact provider/model/revision/profile ไม่เชื่อ declared context อย่างเดียว
- SessionContract/Skill catalog/cache epoch freeze ระหว่าง session
- typed fragments, causal-pair integrity, spill/recovery และ token ledger
- provider dispatch เป็น interface และรองรับ OpenAI-compatible, Anthropic native และ Gemini native; local runtimes ที่พูด OpenAI-compatible ใช้ adapter แรก
- session creation รองรับ explicit และ ordered failover; เลือกเฉพาะ profile ที่ credential/context/qualification ผ่านก่อน freeze contract และไม่ fallback หลัง sampling เริ่ม

credential เก็บได้ 2 ทาง: พิมพ์ลง UI แล้วเก็บที่ `internal/secrets` vault (`<data>/secrets.json` โหมด `0600`) หรือ environment variable — token ที่ save ไว้ชนะ env

ห้ามเขียน token ลง repo, SQLite, backup export, log, artifact หรือ UI response; API ตอบได้แค่ `credential_ready`/`credential_stored` และ credential ที่เคยปรากฏใน transcript ควร rotate

## 5. หลักฐานทดสอบรอบนี้

ผ่านใน sandbox นี้:

```bash
GOCACHE=/tmp/hermetrix-go-cache GOPROXY=off GOSUMDB=off \
  go test ./internal/product ./internal/store
GOCACHE=/tmp/hermetrix-go-cache GOPROXY=off GOSUMDB=off \
  go test -race ./internal/product ./internal/store ./internal/skills ./internal/context
GOCACHE=/tmp/hermetrix-go-cache GOPROXY=off GOSUMDB=off \
  go vet ./internal/product ./internal/store ./internal/web ./cmd/hermetrix
node --check internal/web/ui/app.js
./scripts/doc-truth.sh check
git diff --check
```

Linux/Windows cross-compile ของ product/web/cmd ใช้เป็น compile gate; runtime Windows Job Object และ ConPTY ยังต้องพิสูจน์บน Windows จริง Real Chrome integration test มีทั้ง UI hydration และ private-subresource 0-hit network guard และต้องรันบน host ที่มี Chrome

`GOOS=windows go build ./cmd/hermetrix` เคย build ไม่ผ่านเลย — `syscall.Kill`/`SysProcAttr.Setpgid` อยู่หลัง `if runtime.GOOS != "windows"` ซึ่งเป็น runtime check ไม่ใช่ compile-time guard, symbol ที่ไม่มีบน Windows ทำให้ทั้ง binary build ไม่ได้ ไม่มีอะไรบน Linux CI runner เคยเห็นเพราะไม่เคย cross-compile แก้แล้วด้วย build tag (`commands_unix.go`/`commands_windows.go`) และ CI เพิ่มขั้น cross-build+vet ทั้ง windows/linux/darwin ทุก commit

## 5.1 Quality gate ที่วัดสดรอบนี้ (2026-09-08)

**P10-A — untrusted metadata — ปิดแล้วสำหรับ model ที่ผ่าน** — corpus 24 fixture ใน `internal/hostile/`, รันด้วย `hermetrix hostile`

- **structural 12/12 เสมอ ไม่ขึ้นกับ model** — ยิงผ่าน MCP server จริง protocol จริง (description/title/tool-name/schema/annotation ที่ฝังคำสั่ง) `go test ./internal/hostile/` รันทุกครั้ง ไม่ต้องมี provider
- **behavioural วัดแล้วสองรุ่น**: `qwen3:4b` ในเครื่อง 8/12 (หลุดเฉพาะเคสที่สั่งให้ *พูด* ตาม ไม่ใช่ *ทำ*) · `qwen3.8-27b-fp8` ผ่าน gateway จริง **24/24 (100%)**
- ระหว่างวัดรอบ gateway เจอบั๊กจริง: `internal/hostile/behavioral.go` ส่ง system message สองก้อนแยกกัน ไม่ตรงกับที่ production ส่งจริง (`renderMessages` รวมเป็นก้อนเดียวเสมอ) — gateway backend บางตัวปฏิเสธตรง ๆ แก้แล้ว มี mutation test คุม
- คำตอบเต็มถูกเก็บใน report เสมอ `hermetrix hostile --rescore FILE` คำนวณคะแนนใหม่จากไฟล์ที่รันไปแล้วได้โดยไม่ต้องเรียก model ซ้ำ
- **ยังไม่ครอบ**: model family อื่นนอกจาก qwen, provider paid endpoint อื่น

**P9-B — task success delta** — corpus generator มีบั๊ก sizing: `DefaultNoiseFragments=18` เดิมอ้างว่าได้ ~50,000 token แต่วัดจริงด้วย estimator ได้สูงสุด 105,964 (เกิดจาก V-9 เพิ่ม superseded-fact fragment โดยไม่ recheck ตัวเลขนี้) ทำให้ task แรกที่รันชนเพดาน provider ทันที ("full context exceeds the provider window") แก้เป็น 6 (วัดจริงได้สูงสุด 47,908 เฉลี่ย 35,480) มี `TestFullContextStaysWithinASafeTokenBudget` คุมด้วย estimator จริงแทนคอมเมนต์เดา

หลังแก้ corpus แล้วรันจริงกับ gateway (`qwen3.8-27b-fp8`, compact-32k, `--retrieval --embed-url http://127.0.0.1:11434/v1`) — ดูผลจริงที่ `docs/HANDOVER.md` เวอร์ชันถัดไปหรือ log การรัน ยังไม่จบภายในเซสชันนี้

## 5.2 Branch ที่แยกไว้ต่างหาก

`wip/provider-routing` — provider ordered-failover rewrite จาก agent อีก session หนึ่ง ยังไม่ commit ตอนที่เจอ (ไม่ track ใน git เลย) commit ไว้แยก branch เพื่อไม่ให้หายตอนย้ายเครื่อง ไม่ merge เข้า `main` เพราะไม่ใช่คนออกแบบ ตรวจแค่ build/test ผ่าน ไม่ได้ vouch design — merge เป็นการตัดสินใจของเจ้าของหรือของผู้เขียน rewrite เอง

## 6. งานถัดไปตามลำดับ

1. **P11 native qualification:** เลือก Wails/Tauri จาก PTY/browser/a11y/signing spike แล้วทำ signed desktop package + reconnect E2E
2. **Workbench hardening:** Windows ConPTY, browser egress proxy, download/artifact policy, diagnostics/multi-file editor และ rich Office preview/Unicode PDF fonts
3. **Agent Team qualification:** checkpoint/restart กลาง sampling, artifact-only handoff และ per-task capability/model/budget
4. **Skill Learning OS:** controlled effectiveness eval, semantic duplicate clusters, user merge assistant และ opt-in reversible curator automation
5. **Provider/local-model matrix:** live 64k/128k canary, memory/TTFT/OOM telemetry และ paid-endpoint canary สำหรับ native adapters โดยไม่มี silent downgrade
6. **Release/security:** multi-user identity/RBAC, OS keychain, Linux sandbox packaging, Windows isolation profile, signed audit export, backup/migration/install/upgrade tests

Definition of done ยังไม่ใช่ “มีปุ่มแล้ว”: capability ต้องมี backend authority, persistence/recovery, negative test, user-visible failure state และ artifact/provenance เมื่อเกิด side effect
