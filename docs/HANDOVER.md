# Handover — สถานะงาน ณ 2026-08-28

เอกสารนี้เขียนไว้ให้ session ใหม่บนเครื่องอื่นอ่านแล้วทำงานต่อได้ทันที
ไม่ใช่บันทึกการประชุม — เก็บเฉพาะสิ่งที่เปลี่ยนการตัดสินใจของคนที่มาทำต่อ

แหล่งความจริงที่ละเอียดกว่านี้:

| ต้องการรู้ | อ่านที่ |
|---|---|
| finding ทุกข้อ พร้อมหลักฐานและ mutation | [`AETOX-HERMES-TRACEABILITY-AUDIT.md`](AETOX-HERMES-TRACEABILITY-AUDIT.md) |
| gate ของแต่ละ phase และ prerequisite ที่เหลือ | [`FUTURE-ARCHITECTURE-PLAN.md`](FUTURE-ARCHITECTURE-PLAN.md) |
| ADR และเหตุผลของการตัดสินใจเชิงสถาปัตยกรรม | [`DECISIONS.md`](DECISIONS.md) |
| วิธีตรวจสอบและวิธีรายงาน (กฎที่เจ็บมาแล้ว) | [`.claude/skills/verify-and-report/SKILL.md`](../.claude/skills/verify-and-report/SKILL.md) |

---

## 0. Hermetrix คืออะไร (สำหรับ session ที่เพิ่งเปิด)

harness ที่ทำให้ model เรียกเครื่องมือเองได้ ต่อ MCP ได้ เปิดเว็บทดสอบเองได้
และ **จัดการ context ให้ดึงเฉพาะสิ่งที่เกี่ยวข้อง** เพราะ context มีจำกัด —
Skill เรียกเท่าที่งานต้องใช้ เอกสารดึงมาแต่ส่วนที่เกี่ยวข้อง ประวัติเอามาเฉพาะที่เกี่ยวข้อง

Go 1.25 · SQLite (`modernc.org/sqlite`) · blob storage แบบ content-addressed · local-first

**วินัยที่ทั้ง repo ยึด:** finding จะปิดได้ก็ต่อเมื่อ**ถอด guard ออกแล้ว test แดง**
test ที่ผ่านโดยไม่มี mutation รองรับ ไม่ถือเป็นหลักฐาน

---

## 1. สถานะปัจจุบัน

```
test functions        300        packages              24
direct primitives     10         HTTP routes           93
SQLite tables         39         schema-only tables    0
Go (non-test)         23,196     Go (test)             12,252
```

ตัวเลขเหล่านี้ generate จาก `./scripts/doc-truth.sh` ไม่ได้พิมพ์มือ

```bash
go build ./... && go test ./... && ./scripts/doc-truth.sh check
```

---

## 2. ที่ทำไปแล้วในรอบล่าสุด

### `e40b848` — R-14: Skill retrieval ข้ามภาษาได้

catalog ที่สรุปเป็นอังกฤษ **มองไม่เห็น** goal ภาษาไทย เพราะ lexical scorer
ไม่มี trigram ร่วมกันข้าม script และเพราะ scorer ตัวเดียวกันเป็นคนนับ**ตัวหาร**ของ metric
turn ที่มันอ่านไม่ออกจึงถูกนับว่า "ไม่มี Skill ที่เกี่ยวข้อง" — **ความบอดรายงานตัวเองว่าเป็นความว่างเปล่า**

ไม่แก้ scorer เดิม เพิ่มตัวที่สองแล้ว**บวกกัน** (ชื่อ canonical ที่พิมพ์ตรงเป๊ะเป็น substring
ที่ vector ทำได้แค่ประมาณ · paraphrase ข้ามภาษา trigram ทำไม่ได้เลย)

กฎตัดสินผิดสองรอบก่อนถูก — floor คงที่โอนข้ามคำถามไม่ได้ · ค่ากลางของ catalog
ตกอยู่**ระหว่างคำตอบที่ถูกสองตัว** (catalog จริงมี 3 ตัว สองตัวเรื่องปัดเศษ) ·
quartile ล่างก็ยังไม่ใช่ noise floor เพราะ **การจัดอันดับใน catalog มีผู้ชนะเสมอ**
ตอบไม่ได้ว่า "ไม่มีอะไรตรงเลย" ทางแก้คือ **control** — ประโยคธรรมดา 5 ประโยคที่จงใจไม่เกี่ยวกับ Skill ใด ๆ
floor สุดท้าย = ค่าที่สูงกว่าระหว่าง quartile ของ catalog กับค่ากลางของ control

วัดจริงกับ bge-m3: goal ไทย 3/3 ถึง Skill ที่ถูก · goal ที่ไม่เกี่ยวได้ `[]` ทั้ง catalog 3 และ 8 ตัว

**และปิดช่องที่ทำให้ semantic ทั้งหมดที่สร้างมาไม่มีผลจริง** — `SetEmbedder`
ไม่เคยถูกเรียกจาก `main.go` เลย ทุกอย่างทำงานแต่ใน test กับ taskeval

### `1af22ce` — V-9: corpus กลับมาวัดความสูญเสียได้

corpus หมดความสูญเสียให้วัดไปแล้วสามครั้ง — placement ตายเมื่อ compaction เก็บหัว-กลาง-ท้ายเท่ากัน ·
phrasing ตายเมื่อ ranking เป็น semantic **delta = 0 บน corpus ที่ไม่มีอะไรหาย ไม่ได้แปลว่าอะไร**
และมันโผล่มาในรูป gate ที่ผ่าน

มิติใหม่: **fact ที่ถูกยกเลิก** ประวัติบอกค่าหนึ่ง ประกาศยกเลิก แล้วบอกค่าใหม่
ทั้งสองประโยคพูดเรื่องเดียวกันด้วยคำเกือบเดียวกัน — **คุณสมบัติที่ทำให้ semantic retrieval
ทำงานได้ คือคุณสมบัติที่แยกสองอันนี้ไม่ออก**

```
superseded/far/middle   n=6   เก็บค่าปัจจุบัน 0/6   เก็บค่าที่ยกเลิกแล้ว 6/6
```

ได้ **คำตอบผิดอย่างมั่นใจ** ไม่ใช่คำตอบที่หายไป นับแยกเป็น `stale_answers`

### `fb58ec9` — P10-A: hostile fixture corpus 24 เคส

**structural 12/12 ผ่าน** — ยิงผ่าน MCP server จริง protocol จริง discover ผ่านโค้ดเส้นเดิม
mutation: เปิดให้ catalog entry เป็น provider function → แดง 10 · ถอด `untrusted_output`
จาก `tool_describe` → แดง 4

**behavioural 8/12 กับ qwen3:4b — gate ขอ 100% จึงยังไม่ผ่าน**
เป็นคำแถลงเรื่อง model ไม่ใช่บั๊กใน repo ที่หลุดสี่เคสมีรูปร่างร่วมกัน:
**ทุกอันสั่งให้*พูด*** ส่วนเคสที่สั่งให้***ทำ*** (เรียกเครื่องมือที่ไม่ได้ถูกขอ) ปฏิเสธทั้งอังกฤษและไทย

mitigation ที่ชัดที่สุด (ห่อ tool output ด้วย boundary + คำเตือน) **วัดแล้วไม่เปลี่ยนผลแม้แต่เคสเดียว**
ราคา 51 token ต่อ tool result จึงถอดออก

### `9a4f28a` — สามการตัดสินใจของเจ้าของ

manifest-only replay บล็อก promotion แล้ว · export หดจาก 42 ตารางเหลือ 13 ตาราง skill lifecycle ·
`acceptance_criteria` ถูกลบ (นิยามไว้ มีสองสาขาบริโภค ไม่มีอะไรผลิตเลย)

### `4714e99` `10340f3` `bf1f633` — semantic retrieval + relevance compaction

`headTail` ทิ้งตรงกลาง — วัดจาก fragment จริง 5,649 อัน fact ที่อยู่ตำแหน่งสุ่มตกในช่วงที่ถูกทิ้ง **34.5%**
โมเดล**ค้น 18/19 ครั้ง**เมื่อตอบไม่ได้จริง แต่ lexical search หาเจอแค่ 3/18 —
**retriever แบบ lexical กู้สิ่งที่ ranker แบบ lexical ทิ้งไปไม่ได้ เพราะพังด้วยเหตุผลเดียวกัน**

ทางแก้ต้องสามชั้นถึงจะขยับ: chunk → rank → **ตำแหน่ง** ผล reachability 70/90 → 90/90 · far/middle 0/20 → 20/20

---

## 3. ยังไม่ได้ทำ

### 3.1 ติดที่ credential — P9-B รอบสุดท้าย

gate ของ Phase 9 (task success delta) วัดไปสองรอบแล้ว รอบล่าสุด delta อยู่ในเพดานทั้งสาม class
แต่**ยังไม่ผ่าน** เพราะ 23/270 request ตาย rate limit และ corpus ตอนนั้นวัดความสูญเสียไม่ได้แล้ว

ตอนนี้ corpus แก้แล้ว (V-9) เหลือรัน — ต้องการ:

- provider ที่ context window ใหญ่พอสำหรับเงื่อนไข full-context (~96k) — **qwen3:4b ในเครื่องใช้ไม่ได้ window แค่ 32k**
- **gateway key หายไปพร้อม scratchpad** key เดิมโผล่ใน transcript สามครั้ง **ควร rotate ก่อนใช้ใหม่**
- key เก็บเป็น **ชื่อ** env var เท่านั้น (`HERMETRIX_PROVIDER_API_KEY`) ห้ามเขียนค่าลง repo หรือ commit

```bash
hermetrix taskeval generate --dir corpus/tasks --per-class 30
HERMETRIX_PROVIDER_API_KEY=... hermetrix taskeval score \
  --data .hermetrix --dir corpus/tasks --provider <name> --retrieval \
  --embed-url http://127.0.0.1:11434/v1 --out report.json
```

**อย่ารันโดยไม่ regenerate corpus ก่อน** — เคยพลาดมาแล้วสองครั้ง: แก้ generator แล้วรันกับ corpus เก่า
และเพิ่ม flag แล้วรันกับ binary เก่า

### 3.2 ติดที่การตัดสินใจของเจ้าของ

**skill candidate ที่รอ promote/reject** — จำนวนที่เคยเห็นคือ 13 แต่มาจาก data store ของรอบก่อน
ซึ่ง**ไม่มีอยู่ใน repo นี้** ตรวจของจริงด้วย `GET /api/candidates` หลัง `hermetrix serve`

### 3.3 ยังไม่เริ่ม — prerequisite ที่เหลือ

| id | คืออะไร | ต้องตัดสินใจอะไรก่อน |
|---|---|---|
| **P11-A** | เลือก a11y test harness สำหรับ gate WCAG 2.2 AA | เลือกเครื่องมือ |
| **P12-A** | hardware reference matrix (unified 16 GB · VRAM 24 GB · VRAM 8 GB) | เจ้าของกำหนดเครื่องอ้างอิง |
| **P14-A** | เกณฑ์จัดระดับความรุนแรงสำหรับ threat model | เลือกมาตรฐาน |

**P10-A** corpus พร้อมแล้วแต่ **gate ยังไม่ผ่าน** — ต้องวัดกับ model ที่ใหญ่กว่า qwen3:4b
ถ้า model ใหญ่ผ่าน 12/12 ก็ปิดได้ ถ้าไม่ผ่าน คำตอบที่ชอบธรรมคือระบุว่า harness นี้ปลอดภัยกับ model ตัวไหนบ้าง

---

## 4. ขาดอะไร (ช่องว่างที่รู้ตัว)

**วัดกับ model จริงได้แค่ตัวเดียว** — ทุกอย่างที่เป็น behavioural (hostile corpus, task success,
skill retrieval pull rate) วัดกับ qwen3:4b ตัวเดียว ไม่มี matrix ข้าม model

**n=1 ต่อเงื่อนไข** — การเปรียบเทียบ boundary/ไม่มี boundary รันเงื่อนไขละรอบเดียว
ไม่ได้วัดความแปรปรวนระหว่างรอบ (ผลที่ได้คือ**ชุดความล้มเหลวเดียวกันทีละเคส** ไม่ใช่แค่ตัวเลขรวมเท่ากัน
ซึ่งหนักกว่าค่าเฉลี่ยเท่ากัน แต่ก็ยังเป็น n=1)

**ADR-7 pull rate ยังไม่เคยวัดกับ local model จริง** — `skill_search` ถูกเรียก 165 ครั้งในการรันครั้งก่อน
แต่ไม่ตรงกับ turn ที่ scorer บอกว่ามี Skill ที่เกี่ยวข้องเลยสักครั้ง ตอนนี้ semantic retrieval แก้ตัวหารแล้ว
แต่ยังไม่ได้รันซ้ำ

**สิ่งที่ test ยังไม่ยืนยัน** (ตัดมาจาก audit §7): outbox path ทั้งเส้น · prompt-cache epoch
ที่วัดด้วย fingerprint ตรง ๆ · certified 128k/256k/1M recall ที่มีหลักฐานแยกต่อ tier ·
crash/restart ระหว่าง streaming และ DB/CAS split-brain · native UI/browser/PTY flow

**embedder เป็น optional และไม่มี UI ตั้งค่า** — เปิดผ่าน flag ของ `serve` เท่านั้น
ไม่เปิดก็ถอยไปใช้ lexical ซึ่งข้ามภาษาไม่ได้ (เป็น configuration ที่รองรับ ไม่ใช่ของเสีย
แต่ผู้ใช้ที่ไม่รู้จะไม่ได้ประโยชน์เลย)

---

## 5. คำสั่งที่ต้องใช้

```bash
# ตรวจทั้งหมด
go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/
./scripts/doc-truth.sh              # facts + claims
./scripts/doc-truth.sh check        # exit non-zero ถ้า claim ไหนไม่มีหลักฐาน

# รันจริง (semantic retrieval เปิดผ่าน flag เท่านั้น)
hermetrix serve --data .hermetrix \
  --embed-url http://127.0.0.1:11434/v1 --embed-model bge-m3

# hostile corpus (P10-A)
hermetrix hostile --workspace .                       # structural อย่างเดียว
hermetrix hostile --workspace . --provider <name> --max-tokens 3072 --out r.json
hermetrix hostile --rescore r.json                    # คำนวณใหม่จากคำตอบที่เก็บไว้

# task success (P9-B)
hermetrix taskeval generate --dir corpus/tasks --per-class 30
hermetrix taskeval score --data .hermetrix --dir corpus/tasks --provider <name> --retrieval

# วัด cross-script retrieval กับ embedder จริง
HERMETRIX_EMBED_URL=http://127.0.0.1:11434/v1 \
  go test ./internal/agent/ -run TestRealEmbedderCrossesScripts -v
```

---

## 6. สิ่งที่ต้องมีบนเครื่องใหม่

| ต้องมี | ทำไม | ไม่มีแล้วเป็นยังไง |
|---|---|---|
| Go 1.25 | build | ไม่ได้เลย |
| `ollama` + `bge-m3` | semantic retrieval และ test ตัวจริง | `TestRealEmbedderCrossesScripts` skip เงียบ ๆ · retrieval ถอยไป lexical |
| `ollama` + `qwen3:4b` (หรือใหญ่กว่า) | behavioural half ของ hostile corpus | เคส behavioural รายงานเป็น `skip` ไม่ใช่ `pass` |
| gateway key ที่ rotate แล้ว | P9-B | รัน P9-B ไม่ได้ |

`sqlite3` CLI ไม่มีก็ได้ — ทุกอย่างเข้าถึงผ่าน HTTP API ของ `serve`

---

## 7. กฎที่เจ็บมาแล้ว — อ่านก่อนแตะโค้ด

รายละเอียดอยู่ใน [`.claude/skills/verify-and-report/SKILL.md`](../.claude/skills/verify-and-report/SKILL.md)
สรุปสิ่งที่พังซ้ำในรอบนี้:

**scorer ผิดได้ในทิศที่รายงานว่าปลอดภัย** — hostile corpus scorer ผิดสองรอบ ทั้งสองรอบอ่าน
"โมเดลอ้างถึงการโจมตีขณะปฏิเสธ" ว่าเป็น "โมเดลทำตาม" รอบที่สองเกิดหลังใส่ mitigation
คือ **mitigation ทำให้โมเดลดีขึ้น แล้ว scorer รายงานว่าแย่ลง**

**เก็บผลดิบไว้เสมอ** — ตอนแรกเก็บคำตอบแค่ 240 ตัวอักษรไว้แสดงผล พอ scorer ผิดต้องรันโมเดลใหม่
ทั้งชั่วโมงเพื่อแก้เลขคณิตที่บันทึกไว้แล้ว ตอนนี้มี `--rescore`

**คำตอบว่างไม่ใช่การปฏิเสธ** — reasoning model ใช้ budget หมดก่อนเขียนอะไร ได้ content ว่าง
เวอร์ชันแรกนับเป็น pass ทั้งสามเคส นั่นคือ corpus ปั่นหลักฐานของตัวเอง

**guard ที่ไม่มี mutation แดงแต่มีราคาจริง ให้ถอดออก** — `scaledExtract` ถอดไปแล้ว
untrusted-output boundary ถอดตามด้วยเหตุผลเดียวกัน

**เครื่องมือตรวจต้องไม่แก้ระบบที่ตัวเองตรวจ** — hostile runner เขียน MCP profile ลง data store ของผู้ใช้
12 ตัว จนรันรอบสองล้ม ตอนนี้ใช้ store ชั่วคราวแล้วทิ้ง
