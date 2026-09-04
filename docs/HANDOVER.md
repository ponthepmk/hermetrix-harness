# Handover — สถานะงาน ณ 2026-08-31

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

ตัวเลขจาก `./scripts/doc-truth.sh`:

```text
test functions        424        packages              26
direct primitives      13        HTTP routes           118
SQLite tables          47        schema-only tables      0
Go (non-test)      30,391        Go (test)           17,206
```

## 2. Workbench ที่ทำงานจริงแล้ว

### Files

- project-bound tree/read/write/diff
- กัน path escape, symlink และ non-regular file
- UTF-8 ไม่เกิน 2 MiB
- optimistic save ด้วย expected SHA, atomic replace และ immutable audit receipt

### Terminal

- interactive POSIX PTY: start/input/resize/output/close
- persisted bounded output tail 1 MiB
- restart mark session เป็น interrupted และไม่ replay command
- Windows build ผ่าน แต่ยัง fail-closed ว่า PTY unavailable จนมี ConPTY

### Managed Browser

- isolated headless Chrome/Chromium profile และ pure CDP
- open/navigate/back/read/click/type/capture/close
- DOM snapshot ถูกจำกัดขนาดและติดป้าย untrusted evidence
- local/private URL ต้อง explicit opt-in; `file:` จำกัดใน project root
- screenshot เป็น immutable PNG artifact

ข้อจำกัด: ยังไม่มี embedded live WebView, download workspace และ proxy-level DNS-rebinding/egress enforcement

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
- OpenAI-compatible provider ใช้งานได้; provider ecosystem แบบ native Anthropic/Gemini/local runtime adapters ยังเป็น phase ถัดไป

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

Linux/Windows cross-compile ของ product/web/cmd ผ่าน Real Chrome integration test มีและเปิด browser จริงเมื่อ host อนุญาต แต่ environment รอบนี้ทำให้ Chrome abort และห้าม TCP bind ดังนั้น full HTTP/visual browser E2E ถูก **blocked by environment** ไม่ถูกนับเป็น pass

## 6. งานถัดไปตามลำดับ

1. **P11 native qualification:** เลือก Wails/Tauri จาก PTY/browser/a11y/signing spike แล้วทำ signed desktop package + reconnect E2E
2. **Workbench hardening:** Windows ConPTY, browser egress proxy, download/artifact policy, diagnostics/multi-file editor และ rich Office preview/Unicode PDF fonts
3. **Agent Team qualification:** checkpoint/restart กลาง sampling, artifact-only handoff และ per-task capability/model/budget
4. **Skill Learning OS:** controlled effectiveness eval, semantic duplicate clusters, user merge assistant และ opt-in reversible curator automation
5. **Provider/local-model matrix:** live 64k/128k canary, memory/TTFT/OOM telemetry และ native adapters โดยไม่มี silent downgrade
6. **Release/security:** local identity, keychain, remote auth, OS sandbox, signed audit export, backup/migration/install/upgrade tests

Definition of done ยังไม่ใช่ “มีปุ่มแล้ว”: capability ต้องมี backend authority, persistence/recovery, negative test, user-visible failure state และ artifact/provenance เมื่อเกิด side effect
