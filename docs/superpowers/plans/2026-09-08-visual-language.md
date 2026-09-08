# Visual Language Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** แทนที่ผิวที่สะสมมาทีละสี ด้วยชุดโทเคนเดียวที่มี accent เดียว + สีสถานะสามสี และย้ายค่าสีที่เขียนตรง 194 จุดเข้าโทเคนทั้งหมด

**Architecture:** ไม่แตะโครงสร้างจอและไม่แตะ `app.js` logic — งานทั้งหมดอยู่ใน `internal/web/ui/style.css` กับ test ใน `internal/web/ui_contract_test.go` ลำดับคือ **วัดก่อน → ล็อกด้วย test → เปลี่ยนโทเคน → ไล่ย้ายทีละกลุ่มโดยเพดานลดลงทุกครั้ง**

**Tech Stack:** CSS custom properties, Go `testing` (`internal/web/ui_contract_test.go`), `//go:embed` ผ่าน `mustUIFile`

**Spec:** [docs/superpowers/specs/2026-09-08-visual-language-design.md](../specs/2026-09-08-visual-language-design.md)

## Global Constraints

- **ห้ามเขียน inline style** — server ส่ง `style-src 'self'` ขนาด/สีต้องผ่าน CSS custom property เท่านั้น (spec 1 §3.4)
- **ห้ามเพิ่ม `.style.` ใน `app.js`** — กฎเดิมของ `ui_contract_test`
- **ห้ามแตะโทเคนเวลา** `--dur-press` `--dur-arrive` `--dur-settle` `--dur-hold-done` `--dur-wait-pulse` (spec 1 §8.3)
- **ห้ามเขียนวินาทีเป็นตัวเลขตรง** ใน `transition:`/`animation:` — กฎเดิมที่ `literalDurationsIn` บังคับ
- ทุก block ที่เคลื่อนไหวต้องมีคู่ `prefers-reduced-motion` และเมื่อปิดการเคลื่อนไหว ทุกสถานะยังต้องต่างกันด้วย **สีและคำ**
- WCAG 2.2 AA: `--text` `--muted` `--accent` `--ok` `--warn` `--danger` ≥ **4.5:1** · `--faint` ≥ **3.0:1** วัดกับ `--bg` `--surface` `--surface-2` ทั้งสามพื้น
- font stack เดิมอยู่ที่เดิม ห้ามโหลดฟอนต์จากเน็ต
- ทุกคอมมิตต้องผ่าน: `go test ./internal/web/ -count=1` และ `node --check internal/web/ui/app.js`

---

### Task 1: ล็อกเพดานค่าสีที่เขียนตรง

วัดของจริงก่อนแตะสีใดๆ ถ้าไม่ล็อกก่อน การย้ายสีจะเพิ่มค่าที่เขียนตรงโดยไม่มีใครเห็น

**Files:**
- Modify: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `mustUIFile(t, "ui/style.css")` (มีอยู่แล้ว บรรทัด 108)
- Produces: `colourLiteralsIn(css string) []string` — คืนค่าสีทุกตัวที่อยู่นอกทุก `:root` block (hex และ `rgb()/rgba()`) · `colourLiteralCeiling` — ค่าคงที่ int

- [ ] **Step 1: เขียน test ที่ยังไม่ผ่าน**

```go
// ค่าสีที่เขียนตรงคือที่ที่สองที่ตอบว่า "สีนี้คืออะไร" เพดานนี้ลดได้อย่างเดียว
// ไม่ใช่เพราะ 194 เป็นตัวเลขที่ยอมรับได้ แต่เพราะมันคือจุดที่เริ่มนับ
const colourLiteralCeiling = 194

func TestColourLiteralsOnlyLiveInTokens(t *testing.T) {
	found := colourLiteralsIn(mustUIFile(t, "ui/style.css"))
	if len(found) > colourLiteralCeiling {
		t.Errorf("ค่าสีที่เขียนตรงนอก :root มี %d ค่า เพดานคือ %d — เพดานนี้ลดได้อย่างเดียว",
			len(found), colourLiteralCeiling)
	}
}

// เช็คเกอร์ต้องพิสูจน์ว่าจับได้จริง ไม่ใช่คืนศูนย์แล้วผ่านทุกครั้ง
func TestColourLiteralCheckerSeesLiteralsAndIgnoresTokens(t *testing.T) {
	css := `:root { --bg: #0c0e12; }
.a { color: #ff0000; }
.b { background: rgba(1,2,3,.4); }
.c { color: var(--bg); }`
	found := colourLiteralsIn(css)
	if len(found) != 2 {
		t.Fatalf("อยากได้ 2 ค่า (#ff0000 กับ rgba(...)) ได้ %v", found)
	}
}
```

- [ ] **Step 2: รัน แล้วต้องพัง**

Run: `go test ./internal/web/ -run TestColourLiteral -count=1`
Expected: FAIL — `undefined: colourLiteralsIn`

- [ ] **Step 3: เขียน helper ให้น้อยที่สุดที่ทำให้ผ่าน**

```go
// colourLiteralsIn คืนค่าสีที่เขียนตรงทุกตัวที่อยู่นอก :root block
//
// ตัด :root ออกก่อนเสมอ เพราะที่นั่นคือที่ที่ค่าสีควรอยู่ ไฟล์นี้มี :root สี่ block
// (ธีมหลัก, media query, และ density) ทุกอันต้องถูกตัด ไม่ใช่แค่อันแรก
func colourLiteralsIn(css string) []string {
	stripped := rootBlockPattern.ReplaceAllString(css, "")
	found := hexPattern.FindAllString(stripped, -1)
	return append(found, rgbPattern.FindAllString(stripped, -1)...)
}

var (
	rootBlockPattern = regexp.MustCompile(`(?s):root(\[[^\]]*\])?\s*\{[^}]*\}`)
	hexPattern       = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	rgbPattern       = regexp.MustCompile(`\brgba?\(`)
)
```

- [ ] **Step 4: รัน ต้องผ่าน**

Run: `go test ./internal/web/ -run TestColourLiteral -count=1`
Expected: PASS ทั้งสอง test

- [ ] **Step 5: พิสูจน์ว่าเพดานจับจริง (mutation)**

เพิ่มบรรทัด `.mutation-probe { color: #123456; }` ต่อท้าย `style.css` ชั่วคราว รัน test อีกครั้ง
Expected: FAIL ที่ 195 > 194 · แล้วลบบรรทัดนั้นทิ้ง รันใหม่ต้องกลับมา PASS

- [ ] **Step 6: Commit**

```bash
git add internal/web/ui_contract_test.go
git commit -m "test: lock the colour-literal ceiling before changing any colour"
```

---

### Task 2: ตรวจ contrast ด้วยโค้ด ไม่ใช่สายตา

พาเลตปัจจุบัน**ผ่าน AA อยู่แล้ว** (ต่ำสุด `--muted` บน `--panel-2` = 6.15) test นี้จึงไม่ได้จับบั๊กวันนี้ — มันมีไว้ให้ Task 3 ที่เปลี่ยนทุกสีพร้อมกันมีตาข่ายรอง

**Files:**
- Modify: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `mustUIFile`
- Produces: `tokenValue(css, name string) string` · `contrastRatio(a, b string) float64`

- [ ] **Step 1: เขียน test ที่ยังไม่ผ่าน**

```go
// contrast วัดด้วยเลข ไม่ใช่สายตา เพราะ Task ถัดไปเปลี่ยนทุกสีพร้อมกัน
// และ "ดูโอเคนะ" ไม่ใช่หลักฐาน
func TestPaletteMeetsWCAGAA(t *testing.T) {
	css := mustUIFile(t, "ui/style.css")
	backgrounds := []string{"--bg", "--panel", "--panel-2"}
	for _, item := range []struct {
		token string
		min   float64
	}{
		{"--text", 4.5},
		{"--muted", 4.5},
	} {
		for _, bg := range backgrounds {
			got := contrastRatio(tokenValue(css, item.token), tokenValue(css, bg))
			if got < item.min {
				t.Errorf("%s บน %s = %.2f ต้อง ≥ %.1f", item.token, bg, got, item.min)
			}
		}
	}
}

// เช็คเกอร์ต้องรู้จักคู่ที่ตกจริง ไม่ใช่คืนเลขสวยเสมอ
func TestContrastCheckerRejectsALowPair(t *testing.T) {
	if got := contrastRatio("#777777", "#6f6f6f"); got >= 4.5 {
		t.Fatalf("เทาบนเทาได้ %.2f ซึ่งไม่ควรผ่าน 4.5", got)
	}
	if got := contrastRatio("#ffffff", "#000000"); got < 20 {
		t.Fatalf("ขาวบนดำได้ %.2f ควรใกล้ 21", got)
	}
}
```

- [ ] **Step 2: รัน แล้วต้องพัง**

Run: `go test ./internal/web/ -run 'TestPaletteMeetsWCAGAA|TestContrastChecker' -count=1`
Expected: FAIL — `undefined: contrastRatio`

- [ ] **Step 3: เขียน helper**

`ui_contract_test.go` มี `regexp` กับ `strings` อยู่แล้ว **ต้องเพิ่ม `math` กับ `strconv`** ในบล็อก import
ไม่งั้นคอมไพล์ไม่ผ่าน

```go
// tokenValue อ่านค่าโทเคนจาก :root block แรก ถ้าไม่เจอคืนค่าว่าง
// ซึ่ง contrastRatio จะคืน 0 และ test จะฟ้องว่าโทเคนหาย ไม่ใช่ผ่านเงียบ
func tokenValue(css, name string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(name) + `:\s*(#[0-9a-fA-F]{3,8})`)
	match := pattern.FindStringSubmatch(css)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

// contrastRatio คำนวณตาม WCAG 2.x relative luminance
func contrastRatio(foreground, background string) float64 {
	first, second := relativeLuminance(foreground), relativeLuminance(background)
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0
	}
	channel := func(offset int) float64 {
		value, err := strconv.ParseInt(hex[offset:offset+2], 16, 0)
		if err != nil {
			return 0
		}
		scaled := float64(value) / 255
		if scaled <= 0.03928 {
			return scaled / 12.92
		}
		return math.Pow((scaled+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(0) + 0.7152*channel(2) + 0.0722*channel(4)
}
```

- [ ] **Step 4: รัน ต้องผ่าน**

Run: `go test ./internal/web/ -run 'TestPaletteMeetsWCAGAA|TestContrastChecker' -count=1`
Expected: PASS — พาเลตปัจจุบันผ่านอยู่แล้ว (`--muted` บน `--panel-2` = 6.15)

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui_contract_test.go
git commit -m "test: check contrast in code, before the palette changes under it"
```

---

### Task 3: พาเลตใหม่ พร้อม alias ชั่วคราว

**Files:**
- Modify: `internal/web/ui/style.css:1-27` (`:root` block แรก)
- Modify: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: `tokenValue`, `contrastRatio` จาก Task 2
- Produces: โทเคน `--surface` `--surface-2` `--faint` `--ok` `--warn` `--danger` `--radius-sm` `--radius-lg` `--space-1`..`--space-6` · alias ชั่วคราว `--panel` `--panel-2` `--amber` `--red` `--blue` `--accent-lime` `--accent-violet`

- [ ] **Step 1: ขยาย test contrast ให้ครอบโทเคนใหม่ (ยังไม่ผ่าน)**

```go
// แทนที่ตารางใน TestPaletteMeetsWCAGAA ด้วยชุดเต็ม
	backgrounds := []string{"--bg", "--surface", "--surface-2"}
	for _, item := range []struct {
		token string
		min   float64
	}{
		{"--text", 4.5},
		{"--muted", 4.5},
		{"--faint", 3.0},
		{"--accent", 4.5},
		{"--ok", 4.5},
		{"--warn", 4.5},
		{"--danger", 4.5},
	} {
```

- [ ] **Step 2: รัน แล้วต้องพัง**

Run: `go test ./internal/web/ -run TestPaletteMeetsWCAGAA -count=1`
Expected: FAIL — `--surface` และ `--faint` ยังไม่มี จึงได้ ratio 0

- [ ] **Step 3: เขียน `:root` ชุดใหม่**

แทนที่บรรทัด 1-14 ของ `internal/web/ui/style.css` (ตั้งแต่ `--bg` ถึง `--radius`) ด้วย:

```css
:root {
  /* ชั้นพื้นหลังสามระดับ แทนการขีดเส้นแบ่งทุกกล่อง */
  --bg: #0e1013;
  --surface: #14171b;
  --surface-2: #1b1f24;
  --line: #262b31;

  --text: #e8eaed;
  --muted: #a1a8b0;
  --faint: #767d86;

  /* สีเดียวของแอป ความหรูมาจากความยับยั้ง ไม่ใช่จำนวนสี */
  --accent: #57c9c2;
  --accent-soft: rgba(87,201,194,.12);

  /* สีบอกสถานะ ไม่บอกหมวด ชื่อบอกหน้าที่เพื่อให้เปลี่ยนสีได้โดยไม่ต้องเปลี่ยนชื่อ */
  --ok: #6bbf87;
  --warn: #d9a441;
  --danger: #e0656a;

  /* alias ชั่วคราวระหว่างย้าย — Task 7 ลบทิ้ง ห้ามเหลือค้าง */
  --panel: var(--surface);
  --panel-2: var(--surface-2);
  --amber: var(--warn);
  --red: var(--danger);
  --blue: var(--accent);
  --accent-lime: var(--accent);
  --accent-violet: var(--accent);

  --radius-sm: 8px;
  --radius: 12px;
  --radius-lg: 16px;

  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 24px;
  --space-6: 32px;

  /* ขนาดตัวอักษรห้าระดับ คู่กับ line-height ที่เหมาะกับระดับนั้น
     เขียนคู่กันเพราะขนาดที่ไม่มี line-height คือครึ่งเดียวของคำตอบ */
  --text-xs: 12px;   --leading-xs: 1.4;
  --text-sm: 13px;   --leading-sm: 1.5;
  --text-md: 14px;   --leading-md: 1.6;
  --text-lg: 16px;   --leading-lg: 1.5;
  --text-xl: 20px;   --leading-xl: 1.35;
```

โทเคนเวลาและ `font-family` ที่อยู่ต่อจากนี้ **คงไว้ทุกบรรทัด ห้ามแตะ**

- [ ] **Step 4: รัน ต้องผ่าน**

Run: `go test ./internal/web/ -count=1`
Expected: PASS ทั้ง package — contrast ทุกคู่ผ่าน (ต่ำสุดคือ `--danger` บน `--surface-2` = 4.92)

- [ ] **Step 5: ดูด้วยตาว่าไม่พัง**

Run: `go run ./cmd/hermetrix serve --data .hermetrix` แล้วเปิด `http://127.0.0.1:7331` ที่ 1280×800
Expected: จออ่านได้ ไม่มีข้อความหาย ไม่มี horizontal overflow (สียังปนของเก่าอยู่ — นั่นคือสิ่งที่ Task 4-6 แก้)

- [ ] **Step 6: Commit**

```bash
git add internal/web/ui/style.css internal/web/ui_contract_test.go
git commit -m "feat(ui): one accent and three state colours, with contrast proved in code"
```

---

### Task 4: ย้ายค่าสีของ chrome (header, rail, shell)

**Files:**
- Modify: `internal/web/ui/style.css`
- Modify: `internal/web/ui_contract_test.go` (ลดเพดาน)

**Interfaces:**
- Consumes: โทเคนจาก Task 3
- Produces: ไม่มี symbol ใหม่ — ลด `colourLiteralCeiling`

- [ ] **Step 1: ไล่ค่าสีในกฎของ `.app-shell` `.app-header` `.header-right` `.rail` และลูกของมัน**

แต่ละค่าตอบว่ามันคือโทเคนไหน:
- พื้นของกล่องที่ลอยขึ้นมา → `var(--surface)` หรือ `var(--surface-2)`
- เส้นที่เป็นขอบเขตจริง → `var(--line)` · เส้นที่แค่แบ่งสายตา → ลบทิ้ง ใช้ระดับพื้นแทน
- ข้อความรอง → `var(--muted)` · ป้ายกำกับ/หน่วย → `var(--faint)`

ถ้าค่าไหนไม่ตรงโทเคนใดเลย แปลว่าเจอความหมายที่ยังไม่มีชื่อ — **ตั้งชื่อโทเคนเพิ่ม ไม่ใช่ปล่อย hex ไว้**

- [ ] **Step 2: นับใหม่**

Run: `go test ./internal/web/ -run TestColourLiterals -count=1 -v`
บันทึกจำนวนที่เหลือจากข้อความ error หรือรัน:
```bash
python3 -c "
import re
s=open('internal/web/ui/style.css').read()
s=re.sub(r':root(\[[^\]]*\])?\s*\{[^}]*\}','',s,flags=re.S)
print(len(re.findall(r'#[0-9a-fA-F]{3,8}\b',s))+len(re.findall(r'\brgba?\(',s)))"
```

- [ ] **Step 3: ลดเพดานให้เท่าจำนวนที่เหลือจริง**

แก้ `colourLiteralCeiling` เป็นตัวเลขที่นับได้ใน Step 2

- [ ] **Step 4: รันทั้ง package**

Run: `go test ./internal/web/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui/style.css internal/web/ui_contract_test.go
git commit -m "refactor(ui): shell chrome reads its colours from tokens"
```

---

### Task 5: ย้ายค่าสีของ main และ chat

**Files:**
- Modify: `internal/web/ui/style.css`
- Modify: `internal/web/ui_contract_test.go` (ลดเพดาน)

**Interfaces:**
- Consumes: โทเคนจาก Task 3
- Produces: ไม่มี symbol ใหม่ — ลด `colourLiteralCeiling` อีกครั้ง

- [ ] **Step 1: ไล่ค่าสีในกฎของ `.reading-card` `.message` `.composer` `.stats` `.panel` `.empty` และลูกของมัน**

กฎเดียวกับ Task 4 ทุกข้อ · จุดที่เคยใช้ `--accent-lime`/`--accent-violet`/`--blue` ให้ถามว่า
มันสื่อ **สถานะ** อะไร แล้วใช้ `--ok`/`--warn`/`--danger` ถ้าใช่ ถ้าไม่ใช่สถานะ ให้ใช้ `var(--accent)`
หรือไม่ใส่สีเลย

- [ ] **Step 2: นับใหม่ด้วยคำสั่งเดียวกับ Task 4 Step 2**

- [ ] **Step 3: ลดเพดานให้เท่าจำนวนที่เหลือจริง**

- [ ] **Step 4: รันทั้ง package**

Run: `go test ./internal/web/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui/style.css internal/web/ui_contract_test.go
git commit -m "refactor(ui): chat surfaces read their colours from tokens"
```

---

### Task 6: ย้ายค่าสีที่เหลือทั้งหมด จนเพดานเป็นศูนย์

**Files:**
- Modify: `internal/web/ui/style.css`
- Modify: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: โทเคนจาก Task 3
- Produces: `colourLiteralCeiling = 0`

- [ ] **Step 1: ไล่ที่เหลือทั้งหมด** (settings overlay, workbench, picker, mcp, skill studio, density block)

- [ ] **Step 2: ตั้งเพดานเป็น 0**

```go
const colourLiteralCeiling = 0
```

- [ ] **Step 3: รัน ต้องผ่านที่ศูนย์**

Run: `go test ./internal/web/ -count=1`
Expected: PASS — ถ้ายังไม่ถึงศูนย์ test จะบอกว่าเหลือกี่ค่า ให้ไล่ต่อจนหมด

- [ ] **Step 4: Commit**

```bash
git add internal/web/ui/style.css internal/web/ui_contract_test.go
git commit -m "refactor(ui): every colour in the stylesheet now has a name"
```

---

### Task 7: ลบ alias และห้ามชื่อที่ยกเลิกกลับมา

alias ที่ค้างคือหนี้ — มันทำให้ชื่อที่ตายแล้วยังเขียนได้

**Files:**
- Modify: `internal/web/ui/style.css` (`:root`)
- Modify: `internal/web/ui_contract_test.go`

**Interfaces:**
- Consumes: ไม่มี
- Produces: `TestRetiredTokensStayRetired`

- [ ] **Step 1: เขียน test ที่ยังไม่ผ่าน**

```go
// ชื่อที่ยกเลิกแล้วต้องหายจริง ไม่ใช่ยังเขียนได้เพราะมี alias ค้าง
func TestRetiredTokensStayRetired(t *testing.T) {
	css := mustUIFile(t, "ui/style.css")
	for _, retired := range []string{"--accent-lime", "--accent-violet", "--blue", "--amber", "--red", "--panel-2", "--panel"} {
		if strings.Contains(css, retired) {
			t.Errorf("โทเคนที่ยกเลิกแล้วยังอยู่: %s", retired)
		}
	}
}
```

- [ ] **Step 2: รัน แล้วต้องพัง**

Run: `go test ./internal/web/ -run TestRetiredTokensStayRetired -count=1`
Expected: FAIL — alias ทั้งเจ็ดยังอยู่ใน `:root`

- [ ] **Step 3: ลบ alias ทั้งบล็อกออกจาก `:root`**

ลบเจ็ดบรรทัดที่ Task 3 ใส่ไว้ใต้คอมเมนต์ "alias ชั่วคราวระหว่างย้าย"

- [ ] **Step 4: รันทั้ง package**

Run: `go test ./internal/web/ -count=1`
Expected: PASS — ถ้าพังแปลว่ายังมีที่ใช้ alias เหลือ ให้ย้ายที่นั้นก่อน

- [ ] **Step 5: Commit**

```bash
git add internal/web/ui/style.css internal/web/ui_contract_test.go
git commit -m "refactor(ui): retire the six-hue palette for good"
```

---

### Task 8: ระยะห่าง ความมน และขนาดตัวอักษร

**Files:**
- Modify: `internal/web/ui/style.css`

**Interfaces:**
- Consumes: `--space-1`..`--space-6`, `--radius-sm`, `--radius`, `--radius-lg`, `--text-xs`..`--text-xl`, `--leading-xs`..`--leading-xl` จาก Task 3
- Produces: ไม่มี symbol ใหม่

- [ ] **Step 1: ไล่ `border-radius` ทุกที่ให้เลือกระดับตามขนาดของสิ่งนั้น**

ปุ่ม/ป้าย/ช่องกรอก → `var(--radius-sm)` · การ์ด/panel → `var(--radius)` · overlay/dialog → `var(--radius-lg)`

- [ ] **Step 2: ไล่ `padding`/`gap`/`margin` ที่เขียน px ตรงให้ใช้ `--space-*`**

ค่าที่ไม่ตรงสเกลใดเลย ให้ปัดเข้าสเกลที่ใกล้ที่สุด ไม่ใช่เพิ่มโทเคนใหม่ — สเกลที่มีข้อยกเว้นทุกกรณีไม่ใช่สเกล

- [ ] **Step 3: ไล่ `font-size` ที่เขียน px ตรงให้ใช้ระดับที่ตรงหน้าที่**

ป้ายกำกับ/หน่วย/timestamp → `var(--text-xs)` · UI ทั่วไป → `var(--text-sm)` ·
เนื้อหาที่ต้องอ่าน → `var(--text-md)` · หัวข้อ panel → `var(--text-lg)` · หัวข้อจอ → `var(--text-xl)`

ทุกที่ที่ตั้ง `font-size` ต้องตั้ง `line-height` คู่กันด้วยโทเคน `--leading-*` ที่ตรงระดับ —
ขนาดที่ไม่มี line-height คือครึ่งเดียวของคำตอบ และจะไปยืมค่าจากที่อื่นโดยบังเอิญ

- [ ] **Step 4: รันทั้ง package**

Run: `go test ./internal/web/ -count=1 && node --check internal/web/ui/app.js`
Expected: PASS

- [ ] **Step 5: ดูด้วยตาที่สองความกว้าง**

Run: `go run ./cmd/hermetrix serve --data .hermetrix`
เปิด `http://127.0.0.1:7331` ที่ **1280×800** และ **1600×1000**
Expected: ไม่มี horizontal overflow ทุกมุมมอง · ปุ่มไม่บวม · การ์ดไม่แข็ง · ไม่มีข้อความไหนเล็กกว่า 12px

- [ ] **Step 6: Commit**

```bash
git add internal/web/ui/style.css
git commit -m "refactor(ui): three radii, one spacing scale, five type sizes"
```

---

### Task 9: ตรวจว่าปิดการเคลื่อนไหวแล้วยังใช้งานได้

**Files:**
- Modify: `internal/web/ui/style.css` (ถ้าพบสถานะที่แยกด้วยการเคลื่อนไหวอย่างเดียว)

**Interfaces:**
- Consumes: ไม่มี
- Produces: ไม่มี

- [ ] **Step 1: รัน test ที่มีอยู่แล้ว**

Run: `go test ./internal/web/ -run TestMotionIsTokenised -count=1`
Expected: PASS (กฎเดิม ไม่ได้แก้ในแผนนี้ แต่ต้องไม่พังจากการเปลี่ยนสี)

- [ ] **Step 2: เปิดเบราว์เซอร์ในโหมด reduced motion**

macOS: System Settings → Accessibility → Display → Reduce motion
Expected: hover/active/disabled/loading ยังแยกออกจากกันได้ด้วย **สีและคำ** ไม่ใช่การเคลื่อนไหว

- [ ] **Step 3: ถ้าเจอสถานะที่แยกด้วยการเคลื่อนไหวอย่างเดียว ให้เพิ่มความต่างของสี**

ใช้ `var(--surface-2)` สำหรับสิ่งที่ hover/เลือกอยู่ และ `var(--accent)` สำหรับ focus ring

- [ ] **Step 4: Commit**

```bash
git add internal/web/ui/style.css
git commit -m "fix(ui): every state stays distinguishable with motion off"
```

---

### Task 10: ปิดงาน — doc-truth และการตรวจครั้งสุดท้าย

**Files:**
- Modify: `scripts/doc-truth.sh`
- Modify: `docs/superpowers/specs/2026-09-08-visual-language-design.md` (สถานะ)

**Interfaces:**
- Consumes: ไม่มี
- Produces: claim `colour-literals-live-in-tokens`

- [ ] **Step 1: เพิ่ม claim ลง registry**

เพิ่มบรรทัดนี้ในบล็อก `claims=$(cat <<'CLAIMS'` ของ `scripts/doc-truth.sh`:

```
colour-literals-live-in-tokens|colourLiteralCeiling = 0|internal/web/ui_contract_test.go
palette-contrast-is-measured|func contrastRatio|internal/web/ui_contract_test.go
```

- [ ] **Step 2: รัน doc-truth**

Run: `./scripts/doc-truth.sh check`
Expected: exit 0 — ถ้าพัง แปลว่า anchor ไม่ตรงกับโค้ดจริง ให้แก้ anchor ไม่ใช่แก้ test

- [ ] **Step 3: เปลี่ยนสถานะสเปค**

แก้บรรทัด `สถานะ: spec รอ implement` เป็น `สถานะ: implement แล้ว 2026-XX-XX`

- [ ] **Step 4: รันทุกอย่าง**

Run:
```bash
go build ./... && go vet ./... && go test ./... -count=1
node --check internal/web/ui/app.js && ./scripts/doc-truth.sh check
```
Expected: PASS ทั้งหมด

- [ ] **Step 5: Commit**

```bash
git add scripts/doc-truth.sh docs/superpowers/specs/2026-09-08-visual-language-design.md
git commit -m "docs: the palette has one accent, and a test says so"
```
