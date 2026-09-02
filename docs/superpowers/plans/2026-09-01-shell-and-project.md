# Shell and Project Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ทำให้ project เป็นรากของทุกอย่าง และให้ทุกมุมมองใช้โครงสามเขตที่ผู้ใช้ปรับขนาดและแบ่ง pane ได้

**Architecture:** `projects` เปลี่ยนให้ `root_path` ว่างได้ (โปรเจคที่ไม่มีโค้ด) ผ่าน schemaV29 ที่ rebuild ตาราง
UI เปลี่ยนจาก "reading-card ที่มีแต่ Chat + settings overlay" เป็น "header คงที่ + rail/main/side ที่ลากปรับได้
และ main แบ่งเป็น pane ได้สูงสุด 2×2" Settings overlay และ backend ทั้งหมดไม่ถูกแตะ

**Tech Stack:** Go 1.x (stdlib + `modernc.org/sqlite`), vanilla JavaScript ไฟล์เดียว ไม่มี build step,
CSS custom properties, `localStorage` สำหรับ layout

**Spec:** [`docs/superpowers/specs/2026-09-01-shell-and-project-design.md`](../specs/2026-09-01-shell-and-project-design.md)

## Global Constraints

ทุก task อยู่ใต้ข้อบังคับเหล่านี้ ตรวจซ้ำก่อน commit ทุกครั้ง

- **ห้ามมี `.style.` ใน `internal/web/ui/app.js`** — server ส่ง `style-src 'self'` การเซ็ต inline style
  ถูกบล็อกทั้งหมด ขนาดและตำแหน่งต้องผ่าน CSS custom property หรือ class เท่านั้น
  `ui_contract_test.go` บังคับข้อนี้อยู่แล้ว
- **ระยะเวลาทุกค่าอ่านจากโทเคน `--dur-*`** ห้ามเขียนวินาทีเป็นตัวเลขใน `transition:` หรือ `animation:`
- **ทุก block ที่มีการเคลื่อนไหวต้องมีคู่ `prefers-reduced-motion`** และเมื่อปิดการเคลื่อนไหว
  ทุกสถานะยังต้องต่างกันด้วย**สีและคำ**
- **ทุก `id="..."` ใน `index.html` ต้องถูกอ้างถึงด้วย `#id` ใน `app.js`** — `TestEveryCockpitElementIDIsWiredToBehaviour` บังคับ
- **ห้ามมีคำว่า `Aetox` ใน `index.html` หรือ `style.css`** — license boundary
- **หมวดที่ว่างเปล่าห้ามวาด** ถ้ายังไม่มีระบบอยู่ข้างหลัง อย่าวาดช่องนั้น อย่าใส่เลข 0
- **ค่า boolean ที่เป็นจริงได้จากหลายเหตุผล เอามาเป็นป้ายเดียวไม่ได้** ต้องแตกเป็นหลายคำถามก่อน
- **Go:** `gofmt -w` ทุกไฟล์ที่แก้ · `go vet ./...` ต้องผ่าน · `GOOS=windows` และ `GOOS=linux go build ./...` ต้องผ่าน
- **ก่อน commit ทุกครั้ง:** `node --check internal/web/ui/app.js` และ `go test ./...` (23 packages)
- ใช้ `GOCACHE=/tmp/hermetrix-go-cache` เมื่อ sandbox ไม่ให้เขียน cache เริ่มต้น

---

## File Structure

| ไฟล์ | รับผิดชอบอะไร | สร้าง/แก้ |
|---|---|---|
| `internal/store/store.go` | schemaV29: rebuild `projects`, migrate session ที่ไม่มีโปรเจค | แก้ |
| `internal/store/store_test.go` | migration รักษาข้อมูลเดิมและ constraint ใหม่ | แก้ |
| `internal/product/models.go` | `Project` เพิ่ม `Pinned`, `LastOpenedAt`, `SessionCount` | แก้ |
| `internal/product/service.go` | `resolveProjectRoot` รับค่าว่าง · `PinProject` · `MarkProjectOpened` | แก้ |
| `internal/product/rootrequired.go` | จุดเดียวที่ตอบว่า "โปรเจคนี้ไม่มีโฟลเดอร์โค้ด" | **สร้าง** |
| `internal/product/service_test.go` | root ว่างผ่าน · เครื่องมือที่ต้องใช้ root ปฏิเสธด้วยเหตุผลจริง | แก้ |
| `internal/web/product.go` | handler `openProject`, `pinProject` | แก้ |
| `internal/web/server.go` | route ใหม่สองเส้น | แก้ |
| `internal/web/server_test.go` | API ใหม่เปลี่ยนสถานะจริง · listing ไม่ส่งตัวนับที่ไม่มีระบบ | แก้ |
| `internal/web/ui/style.css` | โทเคน `--dur-*` · โครงสามเขต · pane grid · picker · สถานะปุ่ม | แก้ |
| `internal/web/ui/index.html` | header, zones, picker, pane host | แก้ |
| `internal/web/ui/app.js` | `runAction` · `renderPicker` · `switchView` · pane · layout | แก้ |
| `internal/web/ui_contract_test.go` | บังคับกฎใหม่ทั้งหมด | แก้ |

`rootrequired.go` แยกออกมาเพราะคำตอบ "โปรเจคนี้ไม่มีโค้ด" ต้องเหมือนกันทุกที่ที่ถาม
(files, terminal, browser, command) การกระจายข้อความไปสี่ที่คือสี่คำตอบที่วันหนึ่งจะไม่ตรงกัน

---

### Task 1: schemaV29 — โปรเจคที่ไม่มีโค้ด

**Files:**
- Modify: `internal/store/store.go` (เพิ่ม `schemaV29`, `CurrentSchemaVersion` 28 → 29, migration step)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `migrate(ctx, db)` และรูปแบบ `if version < N { ... }` ที่มีอยู่แล้ว
- Produces: ตาราง `projects` ที่มีคอลัมน์ `pinned INTEGER`, `last_opened_at TEXT`,
  `root_path TEXT NOT NULL DEFAULT ''` และดัชนี `idx_projects_root` แบบ partial

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

เพิ่มท้าย `internal/store/store_test.go`:

```go
// TestSchemaV29AllowsProjectsWithoutCode pins the change that makes a project a
// bounded scope rather than a code folder: several projects may have no root at
// all, while two projects still cannot claim the same one.
func TestSchemaV29AllowsProjectsWithoutCode(t *testing.T) {
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	insert := func(id, name, root string) error {
		_, err := store.DB.Exec(`INSERT INTO projects(id,name,root_path,created_at,updated_at)
      VALUES(?,?,?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, id, name, root)
		return err
	}
	if err := insert("p1", "life", ""); err != nil {
		t.Fatalf("a project with no code root was rejected: %v", err)
	}
	if err := insert("p2", "notes", ""); err != nil {
		t.Fatalf("a second project with no code root was rejected: %v", err)
	}
	if err := insert("p3", "code", "/tmp/one"); err != nil {
		t.Fatal(err)
	}
	if err := insert("p4", "same", "/tmp/one"); err == nil {
		t.Error("two projects claimed the same root")
	}
	var pinned int
	var opened sql.NullString
	if err := store.DB.QueryRow(`SELECT pinned,last_opened_at FROM projects WHERE id='p1'`).
		Scan(&pinned, &opened); err != nil {
		t.Fatalf("picker columns are missing: %v", err)
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/store -run SchemaV29 -v
```

คาดว่า: FAIL — `NOT NULL constraint failed` หรือ `no such column: pinned`

- [ ] **Step 3: เขียน migration**

ใน `internal/store/store.go` ต่อจาก `schemaV28`:

```go
// schemaV29 makes a project a bounded scope rather than a code folder. A
// project without code is ordinary -- planning a trip and planning a refactor
// have the same shape -- so root_path becomes optional. SQLite cannot drop a
// column's UNIQUE constraint, so the table is rebuilt and the uniqueness moves
// to a partial index that only covers roots that actually exist.
const schemaV29 = `
CREATE TABLE projects_v29 (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'active',
  pinned INTEGER NOT NULL DEFAULT 0,
  last_opened_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
INSERT INTO projects_v29(id,name,root_path,state,created_at,updated_at)
  SELECT id,name,root_path,state,created_at,updated_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_v29 RENAME TO projects;
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_root ON projects(root_path) WHERE root_path <> '';
`
```

แล้วเพิ่ม step ต่อจาก `if version < 28 { ... }`:

```go
	if version < 29 {
		if _, err := tx.ExecContext(ctx, schemaV29); err != nil {
			return fmt.Errorf("apply schema v29: %w", err)
		}
	}
```

และเปลี่ยน `const CurrentSchemaVersion = 28` เป็น `29`

- [ ] **Step 4: รันให้ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/store -run SchemaV29 -v && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/store
```

คาดว่า: PASS ทั้งสองคำสั่ง (เทสต์เดิมของ store ต้องไม่พัง — migration ต้องรักษาข้อมูล)

- [ ] **Step 5: commit**

```bash
gofmt -w internal/store/store.go internal/store/store_test.go
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): a project no longer has to be a code folder"
```

---

### Task 2: session ที่ไม่มีโปรเจคได้บ้าน

**Files:**
- Modify: `internal/store/store.go` (ต่อท้าย `schemaV29`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `schemaV29` จาก Task 1
- Produces: โปรเจคชื่อ `Inbox` (`id = 'project_inbox'`, `root_path = ''`) และทุก
  `agent_sessions.project_id` ที่ว่างหรือ NULL ชี้มาที่มัน

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestOrphanSessionsLandInInbox covers the migration's other half. Sessions may
// have had no project at all ("chat only"); once a project is the root of
// everything they would have nowhere to live. None of them may be hidden or
// dropped, so they move to an ordinary project the user can rename or delete.
func TestOrphanSessionsLandInInbox(t *testing.T) {
	root := t.TempDir()
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO provider_profiles
    (id,name,adapter_kind,base_url,model,api_key_env,context_window,context_evidence,
     max_output_tokens,enabled,created_at,updated_at)
    VALUES('pr','P','openai-compatible','https://h.example/v1','m','',131072,'declared',4096,1,
    '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO agent_sessions
    (id,title,provider_id,context_profile,state,project_id,created_at,updated_at)
    VALUES('s1','Orphan','pr','compact-32k','idle',NULL,
    '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Reopening runs the migration against a database that already has rows.
	reopened, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var project, name, rootPath string
	if err := reopened.DB.QueryRow(`SELECT s.project_id,p.name,p.root_path
    FROM agent_sessions s JOIN projects p ON p.id=s.project_id WHERE s.id='s1'`).
		Scan(&project, &name, &rootPath); err != nil {
		t.Fatalf("the orphan session has no project: %v", err)
	}
	if name != "Inbox" || rootPath != "" {
		t.Errorf("orphan landed in %q (root %q), want the rootless Inbox", name, rootPath)
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/store -run OrphanSessions -v
```

คาดว่า: FAIL — `sql: no rows in result set` เพราะ `project_id` ยังเป็น NULL

- [ ] **Step 3: ต่อท้าย `schemaV29`**

เพิ่มก่อน backtick ปิดของ `schemaV29`:

```sql
-- Sessions could have no project at all. They must not be hidden or dropped, so
-- they move into an ordinary project: Inbox is a normal row with no root, which
-- the user can rename, pin or delete once it is empty.
INSERT OR IGNORE INTO projects(id,name,root_path,state,created_at,updated_at)
  SELECT 'project_inbox','Inbox','','active',
         strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
  WHERE EXISTS(SELECT 1 FROM agent_sessions WHERE project_id IS NULL OR project_id='');
UPDATE agent_sessions SET project_id='project_inbox'
  WHERE project_id IS NULL OR project_id='';
```

`WHERE EXISTS` ทำให้ Inbox ไม่ถูกสร้างในฐานข้อมูลที่ไม่มี session กำพร้า —
หมวดที่ว่างเปล่าห้ามวาด รวมถึงห้ามสร้างด้วย

- [ ] **Step 4: pin โปรเจคของ `--workspace`**

สเปค §4.4 บอกว่าโปรเจคที่ลงทะเบียนตอน start ต้องถูก pin ให้อัตโนมัติ เพื่อให้เปิดมาแล้ว
เจอโปรเจคที่ตั้งใจทำงานด้วยอยู่บนสุดของ picker

ใน `cmd/hermetrix/main.go` ต่อจาก `EnsureWorkspaceProject` ที่มีอยู่:

```go
	// The project the process was started for belongs at the top of the picker.
	// Pinning is a convenience, so a failure here is reported and stepped over:
	// a server that refuses to start because a preference did not save would be
	// trading the whole product for the ordering of one list.
	if _, pinErr := productService.PinProject(ctx, workspaceProject.ID, true); pinErr != nil {
		logger.Warn("pin workspace project", "error", pinErr, "project", workspaceProject.Name)
	}
```

> ต้องทำ **หลัง Task 4** เพราะ `PinProject` ยังไม่มี — ถ้าทำ task ตามลำดับ ให้ข้าม step นี้
> ไว้ก่อนแล้วกลับมาทำตอนจบ Task 4 และ commit รวมกับ Task 4

- [ ] **Step 5: รันให้ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/store -v 2>&1 | tail -20
```

คาดว่า: PASS ทุกเทสต์ใน package

- [ ] **Step 6: commit**

```bash
gofmt -w internal/store/store_test.go
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): sessions without a project move to Inbox rather than disappearing"
```

---

### Task 3: root เป็น optional และการปฏิเสธที่บอกความจริง

**Files:**
- Create: `internal/product/rootrequired.go`
- Modify: `internal/product/service.go` (`resolveProjectRoot`), `internal/product/models.go`
- Test: `internal/product/service_test.go`

**Interfaces:**
- Consumes: `Project` จาก `models.go`, `resolveProjectRoot(raw string) (string, error)`
- Produces:
  - `func (s *Service) RequireRoot(ctx context.Context, projectID string) (string, error)` — คืน root
    หรือ error ที่บอกว่าโปรเจคนี้ไม่มีโฟลเดอร์โค้ด
  - `var ErrProjectHasNoCode = errors.New("this project has no code folder")`
  - `Project.Pinned bool` และ `Project.LastOpenedAt *time.Time`

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

ต่อท้าย `internal/product/service_test.go`:

```go
// TestProjectWithoutCodeIsOrdinaryButHonest covers both halves of the rule: a
// project may have no code, and every tool that needs code must say that is why
// it refused rather than reporting a bad path.
func TestProjectWithoutCodeIsOrdinaryButHonest(t *testing.T) {
	ctx := context.Background()
	service, _ := newProductFixture(t)

	life, err := service.SaveProject(ctx, ProjectInput{Name: "Daily life"})
	if err != nil {
		t.Fatalf("a project with no code folder was refused: %v", err)
	}
	if life.RootPath != "" {
		t.Errorf("root = %q, want empty", life.RootPath)
	}
	if _, err := service.RequireRoot(ctx, life.ID); !errors.Is(err, ErrProjectHasNoCode) {
		t.Errorf("RequireRoot said %v, want ErrProjectHasNoCode", err)
	}

	code, err := service.SaveProject(ctx, ProjectInput{Name: "Code", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequireRoot(ctx, code.ID); err != nil {
		t.Errorf("RequireRoot refused a project that has a root: %v", err)
	}

	// A path that was typed and is wrong is still an error, and a different one.
	if _, err := service.SaveProject(ctx, ProjectInput{Name: "Missing", RootPath: "/no/such/place"}); err == nil {
		t.Error("a root that does not exist was accepted")
	} else if errors.Is(err, ErrProjectHasNoCode) {
		t.Error("a wrong path was reported as a project with no code folder")
	}
}
```

`newProductFixture` มีอยู่แล้วในไฟล์นี้หรือไม่ ให้ตรวจก่อน ถ้าไม่มีให้ใช้ helper ที่เทสต์อื่น
ในไฟล์เดียวกันใช้อยู่ (`grep -n "func new.*[Ff]ixture\|store.Open" internal/product/service_test.go | head`)
แล้วเรียกตัวนั้นแทน อย่าสร้าง helper ใหม่ที่ทำงานซ้ำกับของเดิม

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/product -run ProjectWithoutCode -v
```

คาดว่า: FAIL — `undefined: ErrProjectHasNoCode` และ `service.RequireRoot`

- [ ] **Step 3: เขียน implementation**

สร้าง `internal/product/rootrequired.go`:

```go
package product

import (
	"context"
	"errors"
	"fmt"
)

// A project is a bounded scope, and code is optional inside it: planning a trip
// and planning a refactor have the same shape, and only one of them has files.
//
// Every tool that does need files asks here, so the answer is one sentence in
// one place. Spreading it across the file, terminal, browser and command paths
// would be four answers to one question, and one day they would not match.

// ErrProjectHasNoCode reports a project that has no code folder. It is distinct
// from a path that is wrong: "you have not given this project a folder" and
// "that folder is not there" are different problems with different fixes.
var ErrProjectHasNoCode = errors.New("this project has no code folder")

// RequireRoot returns the project's code root, or explains that there is none.
func (s *Service) RequireRoot(ctx context.Context, projectID string) (string, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project.RootPath == "" {
		return "", fmt.Errorf("%q: %w. Add one in the project's settings, or use a project that has code",
			project.Name, ErrProjectHasNoCode)
	}
	return project.RootPath, nil
}
```

ใน `internal/product/service.go` แก้ `resolveProjectRoot`:

```go
func resolveProjectRoot(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	// Empty is a project with no code, which is an ordinary kind of project.
	if trimmed == "" {
		return "", nil
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project root must be an existing directory")
	}
	return real, nil
}
```

ใน `internal/product/models.go` เพิ่มสองฟิลด์ใน `Project`:

```go
	// Pinned and LastOpenedAt exist so the picker can order itself from data
	// rather than guessing which project someone wants.
	Pinned       bool       `json:"pinned"`
	LastOpenedAt *time.Time `json:"last_opened_at,omitempty"`
```

แล้วแก้ทุก `SELECT`/`Scan` ของ project ใน `service.go` ให้อ่านสองคอลัมน์นี้ด้วย
(`grep -n "FROM projects" internal/product/service.go` เพื่อหาให้ครบ)

- [ ] **Step 4: รันให้ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/product 2>&1 | tail -5
```

คาดว่า: PASS ทั้ง package

- [ ] **Step 5: commit**

```bash
gofmt -w internal/product/
git add internal/product/
git commit -m "feat(product): a project may have no code, and tools that need code say so"
```

---

### Task 4: pin, open และ listing ที่ไม่โกหก

**Files:**
- Modify: `internal/product/service.go` (`PinProject`, `MarkProjectOpened`, `ListProjects`)
- Modify: `internal/web/product.go` (handler), `internal/web/server.go` (route)
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: `Project` ที่มี `Pinned`/`LastOpenedAt` จาก Task 3
- Produces:
  - `func (s *Service) PinProject(ctx context.Context, id string, pinned bool) (Project, error)`
  - `func (s *Service) MarkProjectOpened(ctx context.Context, id string) (Project, error)`
  - `Project.SessionCount int` (`json:"session_count"`)
  - `PUT /api/projects/{id}/pin` รับ `{"pinned": bool}`
  - `POST /api/projects/{id}/open`

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

ต่อท้าย `internal/web/server_test.go`:

```go
// TestPickerDataChangesAndTellsTheTruth covers what the picker orders itself by,
// and the rule that it never shows a count for a subsystem that does not exist
// yet: a zero next to "tasks" would be a claim that there are no tasks, when the
// truth is that there is no task system.
func TestPickerDataChangesAndTellsTheTruth(t *testing.T) {
	server := testHTTPServer(t)
	created := requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Daily life"}, http.StatusCreated)
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created, &project); err != nil {
		t.Fatal(err)
	}

	pinned := requestJSON(t, server.URL+"/api/projects/"+project.ID+"/pin", http.MethodPut,
		map[string]any{"pinned": true}, http.StatusOK)
	if !bytes.Contains(pinned, []byte(`"pinned":true`)) {
		t.Fatalf("pin did not take: %s", pinned)
	}
	opened := requestJSON(t, server.URL+"/api/projects/"+project.ID+"/open", http.MethodPost,
		map[string]any{}, http.StatusOK)
	if !bytes.Contains(opened, []byte(`"last_opened_at"`)) {
		t.Fatalf("open did not record a time: %s", opened)
	}

	listing := requestJSON(t, server.URL+"/api/projects", http.MethodGet, nil, http.StatusOK)
	if !bytes.Contains(listing, []byte(`"session_count"`)) {
		t.Error("listing does not carry the one count that has a system behind it")
	}
	for _, absent := range []string{"task_count", "note_count"} {
		if bytes.Contains(listing, []byte(absent)) {
			t.Errorf("listing reports %s although no such system exists yet", absent)
		}
	}
}
```

ถ้า `requestJSON` ยังไม่รองรับ `nil` body สำหรับ GET ให้ตรวจ signature เดิมก่อน
(`grep -n "func requestJSON" -A 6 internal/web/server_test.go`) และเรียกให้ตรงกับของเดิม

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run PickerData -v
```

คาดว่า: FAIL — 404 เพราะยังไม่มี route

- [ ] **Step 3: เขียน implementation**

ใน `internal/product/service.go`:

```go
// PinProject keeps a project at the top of the picker. It is a per-machine
// preference stored with the project because the picker is the same on every
// screen this database serves.
func (s *Service) PinProject(ctx context.Context, id string, pinned bool) (Project, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE projects SET pinned=?,updated_at=? WHERE id=?`,
		boolInt(pinned), formatTime(time.Now().UTC()), id)
	if err != nil {
		return Project{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Project{}, sql.ErrNoRows
	}
	return s.GetProject(ctx, id)
}

// MarkProjectOpened records that someone worked here, which is what "recent"
// in the picker is ordered by. Opening is not editing, so updated_at is left
// alone: otherwise every project would look freshly changed.
func (s *Service) MarkProjectOpened(ctx context.Context, id string) (Project, error) {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE projects SET last_opened_at=? WHERE id=?`,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return Project{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Project{}, sql.ErrNoRows
	}
	return s.GetProject(ctx, id)
}
```

`ListProjects` ต้องนับ session ด้วย และเรียงแบบที่ picker ใช้:

```go
	rows, err := s.store.DB.QueryContext(ctx, `SELECT p.id,p.name,p.root_path,p.state,p.pinned,
    p.last_opened_at,p.created_at,p.updated_at,
    (SELECT COUNT(*) FROM agent_sessions s WHERE s.project_id=p.id)
    FROM projects p ORDER BY p.pinned DESC, COALESCE(p.last_opened_at,p.created_at) DESC`)
```

ใน `internal/web/product.go`:

```go
// pinProject and openProject are the two things the picker writes. Nothing else
// about a project changes from that screen.
func (s *Server) pinProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Pinned bool `json:"pinned"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.PinProject(r.Context(), r.PathValue("id"), input.Pinned)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) openProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.MarkProjectOpened(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
```

ใน `internal/web/server.go` ต่อจาก route project เดิม:

```go
	mux.HandleFunc("PUT /api/projects/{id}/pin", s.pinProject)
	mux.HandleFunc("POST /api/projects/{id}/open", s.openProject)
```

- [ ] **Step 4: รันให้ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web ./internal/product 2>&1 | tail -5
```

คาดว่า: PASS ทั้งสอง package

- [ ] **Step 5: commit**

```bash
gofmt -w internal/product/ internal/web/
git add internal/product/ internal/web/
git commit -m "feat(product): the picker orders itself from pinned and last opened"
```

---

### Task 5: โทเคนเวลาและปุ่มที่บอกว่ากำลังทำงาน

ทำก่อน UI ที่เหลือเพราะทุก task หลังจากนี้ใช้ `runAction` กับโทเคนชุดนี้

**Files:**
- Modify: `internal/web/ui/style.css`, `internal/web/ui/app.js`, `internal/web/ui_contract_test.go`
- Test: `internal/web/ui_contract_test.go`

**Interfaces:**
- Produces:
  - CSS: `--dur-press`, `--dur-arrive`, `--dur-settle`, `--dur-hold-done` บน `:root`
  - JS: `async function runAction(button, {working, done, run})` — จัดการสามสถานะบนปุ่มนั้นเอง
    - `working` และ `done` เป็นข้อความที่ระบุงาน ไม่ใช่ "Loading…"
    - `run` เป็น `async () => any`; error ถูกแสดงติดกับปุ่ม ไม่ใช่ toast

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

ใน `internal/web/ui_contract_test.go` เพิ่มฟังก์ชันใหม่:

```go
// TestMotionIsTokenisedAndReadableWithoutIt locks two rules that fail silently.
// Durations written as literals are several answers to one question, and an
// animation that carries state on its own is invisible to anyone who turns
// motion off.
func TestMotionIsTokenisedAndReadableWithoutIt(t *testing.T) {
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, token := range []string{"--dur-press:", "--dur-arrive:", "--dur-settle:", "--dur-hold-done:"} {
		if !strings.Contains(stylesheet, token) {
			t.Errorf("motion token %s is missing", token)
		}
	}
	// A literal second inside transition/animation is a second place answering
	// the same question as the token set.
	literal := regexp.MustCompile(`(?:transition|animation)[^;{}]*?\b\d*\.?\d+m?s\b`)
	for _, found := range literal.FindAllString(stylesheet, -1) {
		if !strings.Contains(found, "var(--dur-") {
			t.Errorf("duration written as a literal instead of a token: %q", found)
		}
	}
	if strings.Count(stylesheet, "prefers-reduced-motion") < 3 {
		t.Error("motion is used in more places than it is guarded for reduced-motion")
	}
}

// TestActionsReportOnThemselves keeps feedback on the control that was pressed.
// A toast belongs to something that happened elsewhere.
func TestActionsReportOnThemselves(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function runAction", "aria-busy", "data-action-state"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("action feedback is missing %s", marker)
		}
	}
}
```

เพิ่ม `"regexp"` เข้า import ของไฟล์นี้ถ้ายังไม่มี

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run "MotionIs|ActionsReport" -v
```

คาดว่า: FAIL — โทเคนไม่มี และมี literal `.15s`/`.2s` อยู่แปดที่

- [ ] **Step 3: เขียน implementation**

ใน `internal/web/ui/style.css` เพิ่มบน `:root` ที่มีอยู่:

```css
  /* One place answers "how long". A number written into a rule is a second
     place answering the same question, and one day they will disagree. */
  --dur-press: .09s;
  --dur-arrive: .16s;
  --dur-settle: .22s;
  --dur-hold-done: 1.1s;
```

แล้วแทน literal ทุกตัวใน `transition:`/`animation:` ด้วยโทเคน เช่น
`transition: border-color .15s, transform .15s;` เป็น
`transition: border-color var(--dur-press), transform var(--dur-press);`
(หาให้ครบด้วย `grep -n "transition:\|animation:" internal/web/ui/style.css`)

เพิ่มสถานะปุ่ม:

```css
/* Three states, each different in colour and in words. Motion is the fourth
   signal, never the only one: with prefers-reduced-motion everything below
   still reads exactly the same. */
[data-action-state="working"] { border-color: rgba(69,232,232,.4); color: var(--accent); cursor: progress; }
[data-action-state="working"]::after {
  content: "";
  display: inline-block;
  width: 7px; height: 7px;
  margin-left: 7px;
  border-radius: 50%;
  background: currentColor;
  animation: action-pulse var(--dur-settle) infinite alternate;
}
[data-action-state="done"] { border-color: rgba(156,255,56,.42); color: var(--accent-lime); }
[data-action-state="failed"] { border-color: rgba(255,123,123,.45); color: var(--red); }
.action-error { margin: 6px 0 0; color: var(--red); font-size: 10px; line-height: 1.5; }
@keyframes action-pulse { from { opacity: .35; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) {
  [data-action-state="working"]::after { animation: none; opacity: 1; }
}
```

ใน `internal/web/ui/app.js` เพิ่มก่อน `bindComposer`:

```js
// Every action answers two questions and no others: did the press register,
// and what is it doing now. The answer belongs on the control that was pressed
// -- a toast is for something that happened somewhere else, and it is gone in
// under three seconds, which is the wrong place for a failure.
//
// The label while working names the work. "Discovering this server's tools" is
// an answer; "Loading" is a word that fills the same space and says nothing.
async function runAction(button, { working, done, run }) {
  if (!button || button.dataset.actionState === "working") return;
  const idle = button.textContent;
  const previousError = button.parentElement?.querySelector(".action-error");
  previousError?.remove();
  button.dataset.actionState = "working";
  button.setAttribute("aria-busy", "true");
  button.disabled = true;
  button.textContent = working;
  try {
    const result = await run();
    button.dataset.actionState = "done";
    button.textContent = done;
    // Work that finished says so and holds long enough to read, rather than
    // snapping back as though nothing happened.
    setTimeout(() => {
      button.removeAttribute("data-action-state");
      button.removeAttribute("aria-busy");
      button.disabled = false;
      button.textContent = idle;
    }, motionMS("--dur-hold-done"));
    return result;
  } catch (error) {
    button.dataset.actionState = "failed";
    button.removeAttribute("aria-busy");
    button.disabled = false;
    button.textContent = idle;
    // The failure stays next to the button until the next attempt.
    const note = document.createElement("p");
    note.className = "action-error";
    note.textContent = error.message;
    button.parentElement?.appendChild(note);
    return undefined;
  }
}

// motionMS reads a duration token so JavaScript timing and CSS timing cannot
// drift apart. A number typed here would be the second answer this file spent
// a whole rule avoiding.
function motionMS(token) {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  const value = parseFloat(raw) || 0;
  return raw.endsWith("ms") ? value : value * 1000;
}
```

- [ ] **Step 4: รันให้ผ่าน**

```bash
node --check internal/web/ui/app.js && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run "MotionIs|ActionsReport" -v
```

คาดว่า: PASS ทั้งสองเทสต์

- [ ] **Step 5: commit**

```bash
gofmt -w internal/web/ui_contract_test.go
git add internal/web/
git commit -m "feat(ui): actions report on themselves, and durations have one home"
```

---

### Task 6: โครงสามเขตที่ลากปรับได้

**Files:**
- Modify: `internal/web/ui/index.html`, `internal/web/ui/style.css`, `internal/web/ui/app.js`
- Test: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `motionMS` จาก Task 5
- Produces:
  - HTML: `#appHeader`, `#projectChip`, `#viewSwitch`, `#zoneRail`, `#zoneMain`, `#zoneSide`,
    `#toggleRail`, `#toggleSide`, ที่จับลาก `.zone-handle[data-handle="rail"|"side"]`
  - JS: `function setZoneWidth(zone, px)` เขียนผ่าน `--rail-width` / `--workbench-max`
  - CSS: `.app-shell` เป็น grid สามคอลัมน์ที่อ่านค่าจาก custom property

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestShellHasOneViewSwitchAndDraggableZones pins the shape the redesign exists
// for. Two switchers is the mistake the mockup made; a zone that cannot be
// resized is the mistake the first draft made when it put a terminal in 320px.
func TestShellHasOneViewSwitchAndDraggableZones(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, marker := range []string{
		`id="appHeader"`, `id="projectChip"`, `id="viewSwitch"`,
		`id="zoneRail"`, `id="zoneMain"`, `id="zoneSide"`,
		`data-handle="rail"`, `data-handle="side"`,
	} {
		if !strings.Contains(index, marker) {
			t.Errorf("shell HTML is missing %s", marker)
		}
	}
	if strings.Count(index, `id="viewSwitch"`) != 1 {
		t.Error("there must be exactly one view switch")
	}
	for _, marker := range []string{"setZoneWidth", "data-view", "startZoneDrag"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("shell JavaScript is missing %s", marker)
		}
	}
	if !strings.Contains(stylesheet, ".app-shell") || !strings.Contains(stylesheet, ".zone-handle") {
		t.Error("stylesheet does not define the resizable three-zone shell")
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run ShellHasOneViewSwitch -v
```

คาดว่า: FAIL — id ทั้งหมดยังไม่มี

- [ ] **Step 3: เขียน implementation**

`index.html` แทนที่ `<div class="shell">…</div>` เดิม (เก็บ dialog ทุกตัวและ settings overlay ไว้เหมือนเดิม):

```html
  <div class="app-shell" id="appShell" hidden>
    <header class="app-header" id="appHeader">
      <button class="project-chip" id="projectChip"><span class="project-dot"></span><strong id="projectName">—</strong><span>▾</span></button>
      <nav class="view-switch" id="viewSwitch" aria-label="Views">
        <button data-view="work">Work</button>
        <button data-view="chat" class="on">Chat</button>
        <button data-view="code">Code</button>
        <button data-view="knowledge">Knowledge</button>
      </nav>
      <div class="header-right">
        <button class="command-trigger" id="commandButton" aria-label="Open command palette"><span>⌕</span> Go to or run… <kbd>⌘K</kbd></button>
        <button class="ghost compact" id="toggleRail" aria-label="Toggle the list pane">☰</button>
        <button class="ghost compact" id="toggleSide" aria-label="Toggle the evidence pane">⇔</button>
        <button class="ghost compact" id="densityToggle" aria-pressed="false" aria-label="Toggle interface density">Comfortable</button>
        <button class="ghost compact" id="refreshButton">Refresh</button>
        <button class="ghost compact" id="openConfig">⚙<b id="proposalBadge" hidden>0</b></button>
      </div>
    </header>
    <div class="zones" id="zones">
      <div class="zone rail" id="zoneRail"></div>
      <div class="zone-handle" data-handle="rail" role="separator" aria-orientation="vertical" tabindex="0"></div>
      <div class="zone main" id="zoneMain"></div>
      <div class="zone-handle" data-handle="side" role="separator" aria-orientation="vertical" tabindex="0"></div>
      <div class="zone side" id="zoneSide"></div>
    </div>
  </div>
```

`style.css`:

```css
/* The shell is one grid whose columns are tokens, so a drag writes a number in
   exactly one place and the whole layout follows. */
.app-shell { display: grid; grid-template-rows: auto minmax(0,1fr); height: 100vh; overflow: hidden; }
.app-shell[hidden] { display: none; }
.app-header { display: flex; align-items: center; gap: 14px; padding: 0 12px; height: 46px;
  border-bottom: 1px solid var(--line); background: rgba(15,19,27,.92); }
.header-right { margin-left: auto; display: flex; align-items: center; gap: 6px; }
.view-switch { display: flex; gap: 3px; padding: 3px; border: 1px solid var(--line); border-radius: 10px; background: var(--panel-2, #0b0f15); }
.view-switch button { border: 0; border-radius: 7px; padding: 6px 14px; background: transparent;
  color: var(--muted); font: inherit; font-size: 11px; cursor: pointer; }
.view-switch button.on { background: linear-gradient(135deg,var(--accent),#72f0cc); color: #061113; font-weight: 700; }
.project-chip { display: flex; align-items: center; gap: 8px; border: 1px solid var(--line);
  border-radius: 9px; padding: 6px 10px; background: #0b0f15; color: var(--text); cursor: pointer; font-size: 12px; }
.project-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--accent-lime); }
.zones { display: grid; grid-template-columns: var(--rail-width) 5px minmax(0,1fr) 5px var(--workbench-max); min-height: 0; }
.zones.rail-hidden { grid-template-columns: 0 0 minmax(0,1fr) 5px var(--workbench-max); }
.zones.side-hidden { grid-template-columns: var(--rail-width) 5px minmax(0,1fr) 0 0; }
.zones.rail-hidden.side-hidden { grid-template-columns: 0 0 minmax(0,1fr) 0 0; }
.zone { min-width: 0; overflow: auto; }
.zone.rail { border-right: 1px solid var(--line); background: #0b0f15; padding: 10px 9px; }
.zone.main { padding: 14px 16px; }
.zone.side { border-left: 1px solid var(--line); background: #0b0f15; padding: 10px 11px; }
.zones.rail-hidden .zone.rail, .zones.side-hidden .zone.side { display: none; }
.zone-handle { cursor: col-resize; background: transparent; transition: background var(--dur-press); }
.zone-handle:hover, .zone-handle:focus-visible { background: rgba(69,232,232,.3); }
.zones.rail-hidden > .zone-handle[data-handle="rail"],
.zones.side-hidden > .zone-handle[data-handle="side"] { display: none; }
@media (prefers-reduced-motion: reduce) { .zone-handle { transition: none; } }
```

`app.js`:

```js
// A zone's width lives in a custom property because the server sends
// style-src 'self': JavaScript cannot set an inline width at all. Writing the
// token also means the drag, the density preset and the stylesheet are all
// reading one number rather than three.
const ZONE_LIMITS = { rail: [150, 420], side: [220, 640] };

function setZoneWidth(zone, px) {
  const [min, max] = ZONE_LIMITS[zone];
  const clamped = Math.min(max, Math.max(min, Math.round(px)));
  const token = zone === "rail" ? "--rail-width" : "--workbench-max";
  document.documentElement.style.setProperty(token, `${clamped}px`);
  state.zoneWidths[zone] = clamped;
  return clamped;
}

function startZoneDrag(handle, event) {
  const zone = handle.dataset.handle;
  const zones = $("#zones").getBoundingClientRect();
  const move = pointer => {
    const px = zone === "rail" ? pointer.clientX - zones.left : zones.right - pointer.clientX;
    setZoneWidth(zone, px);
  };
  move(event);
  const stop = () => {
    document.removeEventListener("pointermove", move);
    document.removeEventListener("pointerup", stop);
    saveLayout();
  };
  document.addEventListener("pointermove", move);
  document.addEventListener("pointerup", stop);
}
```

> `document.documentElement.style.setProperty` เป็นการเขียน custom property ไม่ใช่ inline style
> ของ element ที่แสดงผล จึงไม่ชนกับ CSP และไม่ใช่ `.style.` แบบที่เทสต์ห้าม —
> **ตรวจให้แน่ว่า `ui_contract_test` ยังผ่าน** ถ้ากฎเดิมจับ substring `.style.` ต้องแก้กฎนั้น
> ให้จับเฉพาะ `.style.` ที่ตามด้วยชื่อ property (เช่น `.style.height`) และอนุญาต `setProperty`
> พร้อมคอมเมนต์อธิบายเหตุผล

- [ ] **Step 4: รันให้ผ่าน**

```bash
node --check internal/web/ui/app.js && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web 2>&1 | tail -5
```

คาดว่า: PASS ทั้ง package

- [ ] **Step 5: commit**

```bash
git add internal/web/
git commit -m "feat(ui): three zones the user can resize instead of three widths we chose"
```

---

### Task 7: Project picker

**Files:**
- Modify: `internal/web/ui/index.html`, `internal/web/ui/style.css`, `internal/web/ui/app.js`
- Test: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `runAction` (Task 5), `GET /api/projects` ที่มี `pinned`/`last_opened_at`/`session_count`
  และ `POST /api/projects/{id}/open` (Task 4)
- Produces:
  - HTML: `#projectPicker`, `#pickerSearch`, `#pickerPinned`, `#pickerRecent`, `#pickerCreate`
  - JS: `function renderPicker()`, `async function openProject(id)`, `async function createProjectFromPicker()`
  - `state.currentProject` — object หรือ `null`

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestPickerIsTheFirstScreen covers the decision that a project is the root of
// everything: the app opens on the choice, and the picker never draws a group
// or a count that has nothing behind it.
func TestPickerIsTheFirstScreen(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{`id="projectPicker"`, `id="pickerSearch"`, `id="pickerPinned"`, `id="pickerRecent"`} {
		if !strings.Contains(index, marker) {
			t.Errorf("picker HTML is missing %s", marker)
		}
	}
	for _, marker := range []string{"function renderPicker", "/open", "session_count", "currentProject"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("picker JavaScript is missing %s", marker)
		}
	}
	// A count for a subsystem that does not exist is a claim about data that
	// has no store behind it.
	for _, absent := range []string{"task_count", "note_count"} {
		if strings.Contains(javascript, absent) {
			t.Errorf("picker renders %s although no such system exists yet", absent)
		}
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run PickerIsTheFirstScreen -v
```

คาดว่า: FAIL — id และฟังก์ชันยังไม่มี

- [ ] **Step 3: เขียน implementation**

`index.html` เพิ่มก่อน `.app-shell`:

```html
  <div class="picker" id="projectPicker">
    <div class="picker-inner">
      <h2>เลือกโปรเจค</h2>
      <p>ทุกอย่างเกาะกับโปรเจค — งาน โน้ต แชท โค้ด · โปรเจคไม่จำเป็นต้องมีโฟลเดอร์โค้ด</p>
      <label class="picker-search"><span>⌕</span><input id="pickerSearch" type="search" autocomplete="off" placeholder="ค้นหาโปรเจค หรือพิมพ์ชื่อใหม่เพื่อสร้าง…"></label>
      <div id="pickerPinned"></div>
      <div id="pickerRecent"></div>
      <button class="primary" id="pickerCreate" hidden>สร้างโปรเจคใหม่</button>
    </div>
  </div>
```

`app.js`:

```js
// The picker is the first screen because a project is the root of everything:
// work, notes, chat and code all hang off one. It draws only the groups that
// have something in them -- an empty heading reads as a place you can go.
function renderPicker() {
  const query = ($("#pickerSearch")?.value || "").trim().toLowerCase();
  const matches = state.projects.filter(item =>
    !query || `${item.name} ${item.root_path}`.toLowerCase().includes(query));
  const card = item => {
    const where = item.root_path
      ? `<div class="picker-path">${escapeHTML(item.root_path)}</div>`
      : `<div class="picker-path none">ไม่มีโฟลเดอร์โค้ด</div>`;
    // Only the count that has a system behind it. Tasks and notes arrive with
    // their own specs; a zero here would be a claim, not a fact.
    return `<button class="picker-card" data-open-project="${escapeHTML(item.id)}">
      <strong>${escapeHTML(item.name)}</strong>${where}
      <span class="picker-stat">${Number(item.session_count || 0).toLocaleString()} แชท</span></button>`;
  };
  const group = (title, items) => items.length
    ? `<p class="picker-group">${title}</p><div class="picker-grid">${items.map(card).join("")}</div>` : "";
  $("#pickerPinned").innerHTML = group("ปักหมุด", matches.filter(item => item.pinned));
  $("#pickerRecent").innerHTML = group("ล่าสุด", matches.filter(item => !item.pinned));
  const exact = state.projects.some(item => item.name.toLowerCase() === query);
  const create = $("#pickerCreate");
  create.hidden = !query || exact;
  create.textContent = `สร้างโปรเจค “${query}”`;
  $$("[data-open-project]").forEach(button =>
    button.addEventListener("click", () => openProject(button.dataset.openProject)));
}

// Opening records that someone worked here, which is what "recent" is ordered
// by, and then hands the screen to the shell.
async function openProject(id) {
  try {
    state.currentProject = await api(`/api/projects/${encodeURIComponent(id)}/open`, { method:"POST", body:"{}" });
    state.projects = await api("/api/projects");
    state.selectedProject = id;
    showShell();
    await load();
  } catch (error) { toast(error.message, true); }
}

async function createProjectFromPicker() {
  const name = ($("#pickerSearch")?.value || "").trim();
  if (!name) return;
  await runAction($("#pickerCreate"), {
    working: `กำลังสร้าง “${name}”`,
    done: "สร้างแล้ว",
    run: async () => {
      // A project created from the picker has no code folder yet. Adding one is
      // a separate, deliberate step: guessing a path here would bind a scope to
      // a directory the user never named.
      const created = await api("/api/projects", { method:"POST", body:JSON.stringify({ name, root_path:"" }) });
      state.projects = await api("/api/projects");
      $("#pickerSearch").value = "";
      renderPicker();
      await openProject(created.id);
    }
  });
}

// showShell and showPicker are the only two things that decide which of the two
// top-level screens is on.
function showShell() {
  $("#projectPicker").hidden = true;
  $("#appShell").hidden = false;
  $("#projectName").textContent = state.currentProject?.name || "—";
}

function showPicker() {
  $("#appShell").hidden = true;
  $("#projectPicker").hidden = false;
  renderPicker();
  $("#pickerSearch")?.focus();
}
```

`style.css`:

```css
.picker { position: fixed; inset: 0; z-index: 10; display: grid; place-items: center; overflow: auto;
  padding: 24px; background: var(--bg); animation: config-arrive var(--dur-arrive) ease-out; }
.picker[hidden] { display: none; }
.picker-inner { width: min(760px, 100%); }
.picker-inner h2 { margin: 0 0 5px; font-size: 22px; text-align: center; }
.picker-inner > p { margin: 0 0 20px; text-align: center; color: var(--muted); font-size: 11px; }
.picker-search { display: grid; grid-template-columns: auto minmax(0,1fr); align-items: center; gap: 9px;
  border: 1px solid var(--line); border-radius: 11px; padding: 11px 14px; background: #0b0f15; margin-bottom: 20px; }
.picker-search input { width: 100%; border: 0; padding: 0; background: transparent; box-shadow: none; font-size: 12px; }
.picker-group { margin: 0 0 8px; color: #667180; font-size: 9px; letter-spacing: .13em; text-transform: uppercase; }
.picker-grid { display: grid; grid-template-columns: repeat(auto-fill,minmax(228px,1fr)); gap: 10px; margin-bottom: 18px; }
.picker-card { border: 1px solid var(--line); border-radius: 12px; background: var(--panel); padding: 13px;
  color: var(--text); text-align: left; cursor: pointer; transition: border-color var(--dur-press); }
.picker-card:hover { border-color: rgba(69,232,232,.35); }
.picker-card strong { display: block; font-size: 12px; margin-bottom: 6px; }
.picker-path { color: var(--dim, #5b6673); font: 9px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.picker-path.none { font-family: inherit; font-style: italic; }
.picker-stat { display: block; margin-top: 9px; color: var(--muted); font-size: 9px; }
@media (prefers-reduced-motion: reduce) { .picker { animation: none; } .picker-card { transition: none; } }
```

ผูก event ใน `DOMContentLoaded`:

```js
  $("#pickerSearch").addEventListener("input", renderPicker);
  $("#pickerCreate").addEventListener("click", createProjectFromPicker);
  $("#projectChip").addEventListener("click", showPicker);
```

- [ ] **Step 4: รันให้ผ่าน**

```bash
node --check internal/web/ui/app.js && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web 2>&1 | tail -5
```

คาดว่า: PASS

- [ ] **Step 5: commit**

```bash
git add internal/web/
git commit -m "feat(ui): the app opens on the project, not on a blank chat"
```

---

### Task 8: สลับมุมมอง และย้าย Chat เข้าโครงใหม่

**Files:**
- Modify: `internal/web/ui/app.js`, `internal/web/ui/index.html`, `internal/web/ui/style.css`
- Test: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: zones จาก Task 6, picker จาก Task 7
- Produces:
  - JS: `function switchView(name)` — name เป็นหนึ่งใน `work|chat|code|knowledge`
  - `state.view` และ `VIEWS` registry ที่แต่ละตัวมี `{ rail(), main(), side() }`
  - มุมมองที่ยังไม่สร้างคืน markup ที่บอกว่าอยู่ใน spec ไหน

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestUnbuiltViewsSaySoRatherThanShowingNothing keeps the shell honest while
// three of its four views are still specs. A tab that opens onto a blank panel
// is a dead end wearing the clothes of a feature.
func TestUnbuiltViewsSaySoRatherThanShowingNothing(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function switchView", "const VIEWS", "state.view"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("view switching is missing %s", marker)
		}
	}
	for _, spec := range []string{"spec 2", "spec 3", "spec 4"} {
		if !strings.Contains(javascript, spec) {
			t.Errorf("an unbuilt view does not name the spec it is waiting on (%s)", spec)
		}
	}
	// The doors were three ways of switching one inspector room, not three ways
	// of working. They are gone.
	if strings.Contains(mustUIFile(t, "ui/index.html"), `data-door=`) {
		t.Error("the old door switch survived the redesign")
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run UnbuiltViews -v
```

คาดว่า: FAIL — `switchView` ยังไม่มี และ `data-door=` ยังอยู่

- [ ] **Step 3: เขียน implementation**

```js
// Each view fills the same three zones. What changes is the content; what each
// zone means does not, which is what makes moving between them predictable.
//
// Three of the four are specs, not code, yet. They say so and name the spec:
// a view that opens onto nothing is worse than one that explains itself.
const VIEWS = {
  chat: {
    label: "Chat",
    rail: () => renderChatRail(),
    main: () => renderChatMain(),
    side: () => renderChatSide()
  },
  work: {
    label: "Work",
    rail: () => unbuilt("งานและบอร์ด", "spec 3"),
    main: () => unbuilt("Work — kanban, งานค้าง, ผูกงานกับแชท", "spec 3"),
    side: () => ""
  },
  code: {
    label: "Code",
    rail: () => unbuilt("ไฟล์และ diff", "spec 2"),
    main: () => unbuilt("Code — editor, syntax highlight, diff", "spec 2"),
    side: () => ""
  },
  knowledge: {
    label: "Knowledge",
    rail: () => unbuilt("คลังและที่มา", "spec 4"),
    main: () => unbuilt("Knowledge — โน้ต, ค้นหาเชิงความหมาย", "spec 4"),
    side: () => ""
  }
};

function unbuilt(what, spec) {
  return `<div class="unbuilt"><h3>${escapeHTML(what)}</h3>
    <p>ยังไม่ได้สร้าง อยู่ใน <strong>${escapeHTML(spec)}</strong> ของแผน redesign</p></div>`;
}

function switchView(name) {
  const view = VIEWS[name] ? name : "chat";
  state.view = view;
  $$("#viewSwitch button").forEach(button => button.classList.toggle("on", button.dataset.view === view));
  $("#zoneRail").innerHTML = VIEWS[view].rail();
  $("#zoneMain").innerHTML = VIEWS[view].main();
  const side = VIEWS[view].side();
  $("#zoneSide").innerHTML = side;
  // A zone with nothing to put in it is hidden, not drawn empty.
  $("#zones").classList.toggle("side-hidden", !side);
  applyLayout();
}
```

แยก `renderChat` เดิมออกเป็นสามฟังก์ชันที่คืน string: `renderChatRail` (session dock เดิม),
`renderChatMain` (transcript + composer เดิม), `renderChatSide` (workbench rooms ที่เหลือ:
Review, Files, Office, Team) แล้วให้ `renderChat()` เดิมเรียก `switchView("chat")` แทน
เพื่อไม่ต้องแก้ทุกจุดที่เรียกมัน

`style.css`:

```css
.unbuilt { border: 1px dashed var(--line); border-radius: 12px; padding: 22px; text-align: center; }
.unbuilt h3 { margin: 0 0 6px; font-size: 13px; }
.unbuilt p { margin: 0; color: var(--muted); font-size: 10px; line-height: 1.6; }
```

ลบ `.door-switch` และ `.door` ออกจาก `index.html` และ CSS

- [ ] **Step 4: รันให้ผ่าน**

```bash
node --check internal/web/ui/app.js && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web 2>&1 | tail -5
```

คาดว่า: PASS ทั้ง package รวมถึงเทสต์ workbench เดิมที่ยังอ้าง `renderWorkbench*`

- [ ] **Step 5: commit**

```bash
git add internal/web/
git commit -m "feat(ui): four views over the same three zones, and the unbuilt ones say so"
```

---

### Task 9: แบ่ง pane และย้าย Terminal/Browser ออกจาก 320px

**Files:**
- Modify: `internal/web/ui/index.html`, `internal/web/ui/style.css`, `internal/web/ui/app.js`
- Test: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `switchView` (Task 8), `runAction` (Task 5)
- Produces:
  - JS: `const PANE_CONTENT` — `{ id, label, render() }` สำหรับ terminal, browser, files, output
  - `function splitPane()`, `function closePane(index)`, `function setPaneContent(index, id)`,
    `function maximisePane(index)`
  - `state.panes` — array ยาว 1–4 ของ content id
  - CSS: `.pane-grid` ที่อ่าน `--pane-columns` / `--pane-rows`

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestPanesGiveTerminalAndBrowserRoom is the correction the spec opens with: a
// terminal in a 320px rail is the same mistake as a file tree in one. Content
// that needs room must be able to take it, up to a ceiling that stays testable.
func TestPanesGiveTerminalAndBrowserRoom(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, marker := range []string{
		"const PANE_CONTENT", "function splitPane", "function setPaneContent",
		"function maximisePane", "MAX_PANES",
	} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("pane support is missing %s", marker)
		}
	}
	// Terminal and browser are pane content now, not rooms in a fixed strip.
	for _, id := range []string{`id: "terminal"`, `id: "browser"`} {
		if !strings.Contains(javascript, id) {
			t.Errorf("pane content is missing %s", id)
		}
	}
	if !strings.Contains(stylesheet, ".pane-grid") || !strings.Contains(stylesheet, "--pane-columns") {
		t.Error("stylesheet does not define the pane grid")
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run PanesGiveTerminal -v
```

คาดว่า: FAIL — ยังไม่มี pane ทั้งชุด

- [ ] **Step 3: เขียน implementation**

```js
// Four is the ceiling because a fifth pane on one screen is smaller than the
// thing inside it, and because a bounded number is a number that can be tested.
// This is a split, not a tiling manager.
const MAX_PANES = 4;

// Content is not tied to a position: any pane may show any of these. Terminal
// and browser are here rather than in the side strip because both need room,
// which is the whole reason the panes exist.
const PANE_CONTENT = [
  { id: "files", label: "Files", render: () => renderWorkbenchFilesHTML() },
  { id: "terminal", label: "Terminal", render: () => renderWorkbenchTerminalHTML() },
  { id: "browser", label: "Browser", render: () => renderWorkbenchBrowserHTML() },
  { id: "output", label: "Output", render: () => renderPaneOutputHTML() }
];

function paneContent(id) {
  return PANE_CONTENT.find(item => item.id === id) || PANE_CONTENT[0];
}

function renderPanes() {
  const host = $("#zoneMain");
  if (!state.panes.length) state.panes = ["files"];
  const columns = state.panes.length > 1 ? 2 : 1;
  const rows = state.panes.length > 2 ? 2 : 1;
  document.documentElement.style.setProperty("--pane-columns", String(columns));
  document.documentElement.style.setProperty("--pane-rows", String(rows));
  host.innerHTML = `<div class="pane-grid ${state.maximisedPane === null ? "" : "one-up"}">${
    state.panes.map((id, index) => {
      const hidden = state.maximisedPane !== null && state.maximisedPane !== index;
      const item = paneContent(id);
      return `<section class="pane ${hidden ? "pane-hidden" : ""}" data-pane="${index}">
        <header class="pane-head">
          <select data-pane-content="${index}" aria-label="Pane content">${
            PANE_CONTENT.map(option =>
              `<option value="${option.id}" ${option.id === id ? "selected" : ""}>${escapeHTML(option.label)}</option>`).join("")
          }</select>
          <button class="ghost compact" data-pane-max="${index}" aria-label="Maximise this pane">${
            state.maximisedPane === index ? "▪" : "▫"}</button>
          ${state.panes.length > 1
            ? `<button class="ghost compact" data-pane-close="${index}" aria-label="Close this pane">×</button>` : ""}
        </header>
        <div class="pane-body">${item.render()}</div>
      </section>`;
    }).join("")
  }</div>
  ${state.panes.length < MAX_PANES ? `<button class="ghost compact pane-add" id="paneAdd">＋ แบ่งช่อง</button>` : ""}`;
  bindPaneControls();
}

function splitPane() {
  if (state.panes.length >= MAX_PANES) return;
  // A new pane starts on something other than what is already open, because
  // splitting to see the same thing twice is not why anyone splits.
  const unused = PANE_CONTENT.find(item => !state.panes.includes(item.id));
  state.panes.push((unused || PANE_CONTENT[0]).id);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function closePane(index) {
  if (state.panes.length <= 1) return;
  state.panes.splice(index, 1);
  state.maximisedPane = null;
  renderPanes();
  saveLayout();
}

function setPaneContent(index, id) {
  state.panes[index] = paneContent(id).id;
  renderPanes();
  saveLayout();
}

function maximisePane(index) {
  state.maximisedPane = state.maximisedPane === index ? null : index;
  renderPanes();
  saveLayout();
}

function bindPaneControls() {
  $("#paneAdd")?.addEventListener("click", splitPane);
  $$("[data-pane-content]").forEach(select =>
    select.addEventListener("change", event => setPaneContent(Number(select.dataset.paneContent), event.target.value)));
  $$("[data-pane-max]").forEach(button =>
    button.addEventListener("click", () => maximisePane(Number(button.dataset.paneMax))));
  $$("[data-pane-close]").forEach(button =>
    button.addEventListener("click", () => closePane(Number(button.dataset.paneClose))));
}
```

แยก `renderWorkbenchFiles`, `renderWorkbenchTerminal`, `renderWorkbenchBrowser` ที่มีอยู่
ให้มีคู่ `*HTML()` ที่ **คืน string** ส่วนตัวเดิมที่เขียนลง `#workbenchContent` ให้เรียกคู่นั้นแทน
เพื่อไม่ทำลาย workbench เดิมที่ settings/Chat side ยังใช้ (`renderPaneOutputHTML` เป็นตัวใหม่:
แสดง background jobs ล่าสุดของโปรเจคนี้จาก `state.jobs`)

`style.css`:

```css
.pane-grid { display: grid; gap: 8px; height: 100%;
  grid-template-columns: repeat(var(--pane-columns,1),minmax(0,1fr));
  grid-template-rows: repeat(var(--pane-rows,1),minmax(0,1fr)); }
.pane-grid.one-up { grid-template-columns: minmax(0,1fr); grid-template-rows: minmax(0,1fr); }
.pane { display: grid; grid-template-rows: auto minmax(0,1fr); min-width: 0; min-height: 0;
  border: 1px solid var(--line); border-radius: 10px; background: var(--panel); overflow: hidden; }
.pane-hidden { display: none; }
.pane-head { display: flex; align-items: center; gap: 6px; padding: 6px 8px; border-bottom: 1px solid var(--line); }
.pane-head select { flex: 1; min-width: 0; padding: 4px 6px; font-size: 10px; }
.pane-body { overflow: auto; padding: 9px; min-height: 0; }
.pane-add { margin-top: 8px; }
```

- [ ] **Step 4: รันให้ผ่าน**

```bash
node --check internal/web/ui/app.js && GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web 2>&1 | tail -5
```

คาดว่า: PASS ทั้ง package

- [ ] **Step 5: commit**

```bash
git add internal/web/
git commit -m "feat(ui): terminal and browser become pane content instead of a 320px strip"
```

---

### Task 10: จำ layout และตรวจทั้งระบบ

**Files:**
- Modify: `internal/web/ui/app.js`, `internal/web/ui_contract_test.go`
- Modify: `README.md`, `docs/HANDOVER.md`

**Interfaces:**
- Consumes: `state.zoneWidths` (Task 6), `state.panes`/`state.maximisedPane` (Task 9)
- Produces: `function saveLayout()`, `function applyLayout()` — key
  `hermetrix.layout.<projectID>.<view>`

- [ ] **Step 1: เขียนเทสต์ที่ยังไม่ผ่าน**

```go
// TestLayoutIsRememberedPerProjectAndView pins where the layout lives and why.
// It is a preference of this screen, not data about the project, so it must not
// reach SQLite and must not travel in a backup.
func TestLayoutIsRememberedPerProjectAndView(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function saveLayout", "function applyLayout", "hermetrix.layout."} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("layout persistence is missing %s", marker)
		}
	}
	// Reading storage must never be the thing that blanks the screen.
	if !strings.Contains(javascript, "catch") {
		t.Error("layout code does not guard against unreadable storage")
	}
	for _, forbidden := range []string{"/api/layout", "layout_json"} {
		if strings.Contains(javascript, forbidden) {
			t.Errorf("layout is being sent to the server (%s); it is a per-screen preference", forbidden)
		}
	}
}
```

- [ ] **Step 2: รันให้เห็นว่าไม่ผ่าน**

```bash
GOCACHE=/tmp/hermetrix-go-cache go test ./internal/web -run LayoutIsRemembered -v
```

คาดว่า: FAIL — ยังไม่มี `saveLayout`

- [ ] **Step 3: เขียน implementation**

```js
// Layout is a preference of this screen, not data about the project: two people
// opening the same project want their own arrangement, and a backup should not
// carry anyone's pane sizes. That is why this is localStorage and not SQLite.
function layoutKey() {
  return `hermetrix.layout.${state.currentProject?.id || "none"}.${state.view || "chat"}`;
}

function saveLayout() {
  try {
    localStorage.setItem(layoutKey(), JSON.stringify({
      zones: state.zoneWidths,
      panes: state.panes,
      maximised: state.maximisedPane,
      railHidden: $("#zones")?.classList.contains("rail-hidden") || false,
      sideHidden: $("#zones")?.classList.contains("side-hidden") || false
    }));
  } catch { /* private window, cleared storage: the app still works without it */ }
}

// applyLayout restores what it can and silently falls back for the rest. A
// stored value that no longer parses must not be the thing that blanks a screen.
function applyLayout() {
  let saved = null;
  try { saved = JSON.parse(localStorage.getItem(layoutKey()) || "null"); } catch { saved = null; }
  const zones = saved?.zones || {};
  setZoneWidth("rail", zones.rail || 220);
  setZoneWidth("side", zones.side || 320);
  state.panes = Array.isArray(saved?.panes) && saved.panes.length ? saved.panes.slice(0, MAX_PANES) : ["files"];
  state.maximisedPane = Number.isInteger(saved?.maximised) && saved.maximised < state.panes.length
    ? saved.maximised : null;
  $("#zones")?.classList.toggle("rail-hidden", Boolean(saved?.railHidden));
  if (state.view === "code") renderPanes();
}
```

ผูก toggle สองปุ่มบน header:

```js
  $("#toggleRail").addEventListener("click", () => {
    $("#zones").classList.toggle("rail-hidden");
    saveLayout();
  });
  $("#toggleSide").addEventListener("click", () => {
    $("#zones").classList.toggle("side-hidden");
    saveLayout();
  });
  $$(".zone-handle").forEach(handle =>
    handle.addEventListener("pointerdown", event => startZoneDrag(handle, event)));
```

- [ ] **Step 4: ตรวจทั้งระบบ**

```bash
node --check internal/web/ui/app.js
GOCACHE=/tmp/hermetrix-go-cache go vet ./...
GOCACHE=/tmp/hermetrix-go-cache go test ./...
GOOS=windows GOARCH=amd64 GOCACHE=/tmp/hermetrix-go-cache go build ./...
GOOS=linux GOARCH=amd64 GOCACHE=/tmp/hermetrix-go-cache go build ./...
./scripts/doc-truth.sh check
git diff --check
```

คาดว่า: 23 packages ผ่าน · vet เงียบ · cross-compile ผ่าน · doc-truth ผ่าน

จากนั้นเปิดของจริงและวัด ไม่ใช่ดูแล้วบอกว่าดีขึ้น:

```bash
go run ./cmd/hermetrix serve --data ./.hermetrix --listen 127.0.0.1:7331 --desktop
```

ตรวจที่ 1280×800 และ 1600×1000 ทุกข้อ:
- เปิดมาเจอ picker · เลือกโปรเจคแล้วเข้า shell · กดชื่อโปรเจคกลับไป picker ได้
- ลาก handle ทั้งสองแล้วความกว้างเปลี่ยน · refresh แล้วยังเท่าเดิม
- แบ่ง pane ถึง 4 ช่อง แล้ว `document.documentElement.scrollWidth - clientWidth === 0`
- Terminal ใน pane ใช้งานได้จริง (พิมพ์คำสั่งแล้วเห็นผล)
- กดปุ่มที่ยิง API แล้วปุ่มนั้นเปลี่ยนสถานะเอง ไม่ใช่แค่ toast
- เปิด DevTools → Rendering → Emulate `prefers-reduced-motion` แล้วทุกสถานะยังอ่านออก
- console ไม่มี error

- [ ] **Step 5: อัปเดตเอกสารและ commit**

ใน `README.md` เพิ่มหัวข้อ "Projects and views" อธิบายว่า project เป็นราก โปรเจคไม่มีโค้ดได้
และ pane แบ่งได้ 4 ช่อง · ใน `docs/HANDOVER.md` แทนที่ย่อหน้าที่บอกว่า cockpit เป็น
"สาม pane: rail ซ้าย, workspace กลาง, workbench inspector ขวา" ด้วยโครงใหม่ และระบุว่า
Work/Code/Knowledge ยังเป็น spec

```bash
gofmt -w internal/web/ui_contract_test.go
git add -A
git commit -m "feat(ui): remember each project's layout, and verify the whole shell"
```

---

## Self-Review

ตรวจแผนนี้กับสเปคด้วยตาใหม่

**ครอบคลุมสเปคไหม**

| สเปค | task |
|---|---|
| §2 project เป็นขอบเขต root optional | 1, 3 |
| §3.1 สามเขตปรับขนาดได้ | 6 |
| §3.2 ทำไมไม่ใช่ panel ตายตัว | 9 (แก้จริง) |
| §3.3 แบ่ง pane สูงสุด 2×2 + เลือกเนื้อหา + ขยายเต็ม | 9 |
| §3.4 จำ layout ต่อโปรเจคต่อมุมมอง | 10 |
| §3.5 project picker | 7 |
| §4.1 schemaV29 | 1 |
| §4.2 กฎของ root + การปฏิเสธที่บอกความจริง | 3 |
| §4.3 API `/open` `/pin` | 4 |
| §4.4 Inbox + EnsureWorkspaceProject | 2 |
| §5 สิ่งที่ย้าย/อยู่ที่เดิม | 8, 9 |
| §6 error handling | 3 (root), 7 (ไม่มีโปรเจค), 8 (มุมมองที่ยังไม่สร้าง), 10 (storage เสีย) |
| §7 testing | ทุก task |
| §8 ปุ่มบอกสถานะ + โทเคน + reduced-motion | 5 |

**ช่องว่างที่พบและปิดแล้ว:** §4.4 ระบุว่าโปรเจคของ `--workspace` ต้องถูก pin อัตโนมัติ
ร่างแรกของแผนไม่มี task ไหนทำ — เพิ่มเป็น Task 2 Step 4 พร้อมหมายเหตุว่าต้องรอ
`PinProject` จาก Task 4 และให้ commit รวมกับ Task 4

**ชื่อและ type ตรงกันไหม:** `setZoneWidth(zone, px)` ใช้ใน Task 6 และ 10 ตรงกัน ·
`state.panes` เป็น array ของ string id ทั้งใน Task 9 และ 10 · `runAction(button, opts)`
เรียกใน Task 7 ตรงกับที่ประกาศใน Task 5 · `MAX_PANES` ประกาศใน Task 9 ใช้ใน Task 10

**ที่ยังไม่มี code จริงและตั้งใจ:** `renderChatRail/Main/Side` และ `render*HTML()` เป็นการ
แยกฟังก์ชันเดิมที่มีอยู่แล้ว ไม่ใช่โค้ดใหม่ — ผู้ทำต้องอ่านของเดิมแล้วแยก ไม่ใช่เขียนใหม่
