# Shell and Project — design

วันที่ตกลง: 2026-09-01
สถานะ: spec รอ implement
ขอบเขต: spec ที่ 1 จาก 4 · อีกสามอันคือ Code view, Work, Knowledge

## 1. ปัญหา

ทุกอย่างที่เพิ่มเข้ามาถูกต่อเติมลงในโครงที่ไม่ได้ออกแบบมารองรับ แต่ละคำขอได้คำตอบที่ดีเฉพาะจุด
แต่ภาพรวมไม่เคยถูกออกแบบ ผลคือแก้ทีละจุดแล้วยังรู้สึกว่า "ดีขึ้นแต่ยังยาก"

หลักฐานสามข้อ:

- **Code door ไม่ใช่ที่เขียนโค้ด** — `internal/web/ui/app.js` บรรทัดที่ door `code` ทำงาน
  มีคำสั่งเดียวคือ `switchWorkbench("files")` ซึ่งเปิด panel ไฟล์กว้าง 320-430px ใน inspector
  ขวา ไม่มี editor ไม่มี tab ไม่มี diff และพื้นที่หลักยังเป็นแชทอยู่
- **Project อยู่ผิดที่** — อยู่ใน Settings → System → Projects ทั้งที่ทุก session ผูกกับ project
  และเป็นสิ่งแรกที่ต้องเลือกก่อนทำงาน มันถูกจัดเป็น configuration ทั้งที่มันคือ workspace
- **พื้นที่หลักแสดงได้อย่างเดียว** — `#view-chat` เป็น view เดียวที่อยู่ใน `.reading-card`
  ที่เหลือทั้งหมดถูกย้ายไปอยู่ใน settings overlay หรือ inspector 320px ไม่มี "ที่ที่สาม"

## 2. สิ่งที่ตัดสินแล้ว

**Project คือขอบเขตงาน ไม่ใช่โฟลเดอร์โค้ด** โปรเจคโค้ดมี root โปรเจคชีวิตประจำวันไม่มี
โครงสร้างเดียวกัน ไม่มีข้อยกเว้น เพราะการวางแผนเที่ยวกับการวางแผน refactor มีรูปร่างเดียวกัน:
ขอบเขตหนึ่งที่มีงาน มีโน้ต มีบทสนทนา ต่างกันแค่มีไฟล์หรือไม่ นั่นคือ field ไม่ใช่ type

ทางเลือกที่ปฏิเสธ: **Workspace ครอบ Project** ทำให้ทุกการกระทำต้องตอบสองคำถามก่อนเริ่ม
("workspace ไหน แล้ว project ไหน") ซึ่งเป็นปัญหาเดียวกับเมนู 19 ช่องที่เพิ่งแก้ไป

**เปิดโปรแกรมมาต้องเห็น project picker** เจ้าของเลือกข้อนี้เอง shell จึงมี project เป็นราก

## 3. โครงจอ

### 3.1 สามเขต หน้าที่ตายตัว

```
┌─────────────────────────────────────────────────────────┐
│ [● project ▾]   Work Chat Code Knowledge      ⌕ ⇔ ⚙   │  header 46px
├────────┬──────────────────────────────┬─────────────────┤
│ rail   │           main               │      side       │
│ 220px  │        minmax(0,1fr)         │     320px       │
│ รายการ │        ตัวงานจริง             │   หลักฐาน       │
└────────┴──────────────────────────────┴─────────────────┘
```

หน้าที่ของแต่ละเขตไม่เปลี่ยนตามมุมมอง เนื้อหาเปลี่ยน ความหมายไม่เปลี่ยน:

| มุมมอง | rail = รายการ | main = ตัวงาน | side = หลักฐาน |
|---|---|---|---|
| Work | บอร์ดและฟิลเตอร์ | kanban | รายละเอียดงานและสิ่งที่ผูกอยู่ |
| Chat | แชทและงานที่ผูก | บทสนทนา | workbench rooms |
| Code | file tree และ diff | editor | เหตุผลที่แก้ ผลเทสต์ terminal |
| Knowledge | คลังและที่มา | ค้นหาและโน้ต | โน้ตที่เลือกและ backlink |

**ตัวสลับมุมมองมีอันเดียว อยู่บน header** rail จึงเป็นพื้นที่ของเนื้อหา ไม่ใช่เมนู
ปัญหาเดิมคือ rail แบกทั้งเมนู ทั้ง session dock ทั้ง settings จนไม่เหลือที่

**side พับได้ทุกมุมมอง** ปุ่ม `⇔` บน header จอแคบเหลือสองเขต จอกว้างสามเขต
ใช้ density token ที่มีอยู่แล้ว (`--rail-width`, `--workbench-*`) ไม่สร้างชุดที่สอง

### 3.2 Project picker

จอแรกเมื่อยังไม่ได้เลือกโปรเจค และเป็นจอที่กลับมาได้จากการกดชื่อโปรเจคบน header

- ค้นหาอยู่บนสุด พิมพ์ชื่อที่ยังไม่มีแล้วสร้างได้จากช่องเดียวกัน
- **ปักหมุด** แล้ว **ล่าสุด** เป็นสองกลุ่ม กลุ่มที่ว่างไม่วาด
- การ์ดแต่ละใบบอก: ชื่อ, root (หรือ "ไม่มีโฟลเดอร์โค้ด") และตัวนับ**เฉพาะที่มีระบบอยู่จริง**
  ในเฟสนี้คือจำนวนแชทเท่านั้น งานค้างและโน้ตจะปรากฏเมื่อ spec 3 และ 4 ลงแล้ว
  (mockup วาดครบสามตัวเพื่อให้เห็นปลายทาง ไม่ใช่สิ่งที่เฟสนี้ส่งมอบ)
- เนื้อหาอยู่กลางจอ ระยะซ้ายเท่าระยะขวา

## 4. Data model

### 4.1 การเปลี่ยน schema

`projects.root_path` เป็น `TEXT NOT NULL UNIQUE` ซึ่งรับค่าว่างได้แค่แถวเดียว
โปรเจคที่ไม่มีโค้ดหลายอันจะชนกันทันที SQLite ลบ UNIQUE ออกจากคอลัมน์ไม่ได้
ต้อง **rebuild ตาราง** ใน `schemaV29`:

```sql
CREATE TABLE projects_new (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL DEFAULT '',   -- ว่าง = โปรเจคที่ไม่มีโค้ด
  state TEXT NOT NULL DEFAULT 'active',
  pinned INTEGER NOT NULL DEFAULT 0,
  last_opened_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO projects_new(id,name,root_path,state,created_at,updated_at)
  SELECT id,name,root_path,state,created_at,updated_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;
-- UNIQUE เฉพาะ root ที่มีจริง: โปรเจคไม่มีโค้ดกี่อันก็ได้
CREATE UNIQUE INDEX idx_projects_root ON projects(root_path) WHERE root_path <> '';
```

`last_opened_at` และ `pinned` มีไว้ให้ picker เรียงได้โดยไม่ต้องเดา

### 4.2 กฎของ root

`resolveProjectRoot` ปัจจุบันบังคับว่าต้องเป็นไดเรกทอรีที่มีอยู่จริง กฎใหม่:

- root ว่าง = โปรเจคไม่มีโค้ด ยอมรับ
- root ไม่ว่าง = ต้องเป็น absolute path, resolve symlink, ต้องเป็นไดเรกทอรีที่มีอยู่ (กฎเดิมทั้งหมด)

เครื่องมือที่ต้องใช้ root (files, terminal, browser, command) ต้อง **ปฏิเสธอย่างชัดเจน**
เมื่อโปรเจคไม่มี root ไม่ใช่ fallback ไปที่ working directory ของ server
ข้อความต้องบอกว่าโปรเจคนี้ไม่มีโฟลเดอร์โค้ด ไม่ใช่ "path ไม่ถูกต้อง"

### 4.3 API

| Method | Path | ทำอะไร |
|---|---|---|
| GET | `/api/projects` | เพิ่ม `pinned`, `last_opened_at`, และตัวนับ (sessions) ใน response |
| POST | `/api/projects/{id}/open` | บันทึก `last_opened_at` คืน project |
| PUT | `/api/projects/{id}/pin` | `{pinned: bool}` |

ตัวนับงานและโน้ตยังไม่มีในเฟสนี้ (spec 3 และ 4) จึงไม่ส่ง ไม่ใช่ส่งเลข 0
**หมวดที่ว่างเปล่าห้ามวาด** — การ์ดจะไม่แสดงบรรทัดที่ยังไม่มีระบบอยู่ข้างหลัง

## 5. สิ่งที่ย้ายและสิ่งที่อยู่ที่เดิม

**ย้าย:**
- Projects ออกจาก Settings → กลายเป็น picker และ header
- Chat จาก `.reading-card` → main ของมุมมอง Chat
- session dock จาก rail กลาง → rail ของมุมมอง Chat
- workbench rooms จาก inspector → side ของมุมมอง Chat
- doors Assistant/Code/Team → **ยกเลิก** ถูกแทนที่ด้วยมุมมอง Work/Chat/Code/Knowledge
  (door เดิมทำได้แค่สลับห้องใน inspector ไม่ใช่วิธีทำงานที่ต่างกันจริง)

**อยู่ที่เดิม ไม่แตะ:**
- Settings overlay ทั้งหมด (`CONFIG_SECTIONS`, nav, search) — เพิ่งออกแบบใหม่และใช้งานได้
- command palette `⌘K` — เพิ่ม "ไปที่โปรเจค…" และมุมมองทั้งสี่เข้าไป
- capability picker, density toggle, composer keys
- API และ backend ทั้งหมดของ chat, files, terminal, browser, MCP, skills

**ยังไม่ทำในเฟสนี้:** มุมมอง Work, Knowledge และ Code แสดงเป็นสถานะ "ยังไม่ได้สร้าง"
พร้อมบอกว่าอยู่ใน spec ไหน ไม่ใช่แท็บว่างที่กดแล้วไม่มีอะไร

### 4.4 สองกรณีที่ต้องมีคำตอบชัด

**session เดิมที่ไม่มีโปรเจค** ทุกวันนี้ `project_id` เป็นค่าว่างได้ ("chat only") เมื่อ project
กลายเป็นราก session เหล่านั้นจะไม่มีที่ยืน คำตอบ: ตอน migrate ให้ผูกกับโปรเจคชื่อ
`Inbox` ที่สร้างอัตโนมัติและไม่มี root ไม่ลบและไม่ซ่อน session ใดทั้งสิ้น
`Inbox` เป็นโปรเจคปกติทุกประการ ลบเองได้ถ้าว่าง

**โปรเจคที่ลงทะเบียนตอน start** `EnsureWorkspaceProject` ยังทำงานเหมือนเดิม
`--workspace` ยังลงทะเบียน root นั้นเป็นโปรเจคและ pin ให้อัตโนมัติ เพื่อให้เปิดมาแล้ว
เจอโปรเจคที่ตั้งใจทำงานด้วยอยู่บนสุดของ picker

## 6. Error handling

- โปรเจคถูกลบหรือ root หายไประหว่างใช้งาน: header แสดงสถานะ ไม่ crash และ picker
  ยังเปิดได้เสมอ
- ไม่มีโปรเจคเลย: picker แสดงเฉพาะช่องค้นหาและปุ่มสร้าง ไม่วาดหมวดที่ว่าง
- เปิดมุมมองที่ยังไม่ได้สร้าง: บอกตรงๆ ว่ายังไม่มี ไม่ใช่หน้าว่าง

## 7. Testing

- `store`: migration v29 รักษาข้อมูลเดิม, โปรเจคไม่มี root หลายอันอยู่ร่วมกันได้,
  โปรเจคที่มี root ซ้ำกันยังถูกปฏิเสธ
- `product`: `resolveProjectRoot` รับค่าว่าง, ปฏิเสธ path ที่ไม่มีจริง,
  เครื่องมือที่ต้องใช้ root ปฏิเสธพร้อมข้อความที่บอกสาเหตุจริง
- `web`: `/open` และ `/pin` เปลี่ยนสถานะจริง, listing ไม่ส่งตัวนับที่ยังไม่มีระบบ
- `ui_contract_test`: header มีตัวสลับมุมมองอันเดียว, picker เป็น element จริง,
  ทุก id ที่เพิ่มถูก bind (กฎเดิมของไฟล์นี้)
- ตรวจในเบราว์เซอร์จริงที่ 1280×800 และ 1600×1000: ไม่มี horizontal overflow ทุกมุมมอง

## 8. นอกขอบเขต spec นี้

| อยู่ใน spec ไหน | เรื่อง |
|---|---|
| 2 | editor จริง, syntax highlight, diff renderer, tab ไฟล์ |
| 3 | schema งาน, kanban, การผูกงานกับแชท |
| 4 | schema โน้ต, ค้นหาเชิงความหมาย, tool ให้ agent เขียน/ค้นโน้ต |

**ราคาที่รู้ล่วงหน้า:** spec 4 จะทำให้ direct tool waist โตจาก 11 เป็น 13 ถ้าเพิ่ม
`note_write` และ `note_search` เข้า waist ทางเลือกคือยัดเข้า deferred catalog แบบเดียวกับ
MCP resources ซึ่ง waist ไม่โต ตัดสินตอนถึง spec นั้น

**dependency ก้อนใหญ่ที่ต้องตัดสินใน spec 2:** UI ตอนนี้เป็น vanilla JS ไฟล์เดียว ไม่มี build step
การใส่ CodeMirror หรือ Monaco เปลี่ยนข้อนี้ ต้องชั่งกับการเขียน highlighter เองแบบจำกัดภาษา
