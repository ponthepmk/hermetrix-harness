# IDE workspace — ใช้งานและขอบเขตที่ตรวจแล้ว

ปรับปรุงวันที่ 20 กันยายน 2026 สำหรับ Windows โดยเน้น Go และ JavaScript และเชื่อมกับโมเดลบนเครื่อง

## เริ่มใช้งาน

1. เปิดโปรเจกต์ที่มีโฟลเดอร์โค้ด แล้วเลือก **Workspace → AI workspace**
2. เลือกไฟล์จาก Files ใช้ Filter files เพื่อกรองรายการในโฟลเดอร์ปัจจุบัน
3. เขียนโค้ดในแผงกลาง มีสีไวยากรณ์ เลขบรรทัด แท็บไฟล์ ค้นหา/แทนที่ พับโค้ด และ Undo/Redo
4. เลือก **Format** เพื่อจัดรูปแบบใน editor โดยยังไม่เขียนทับดิสก์ กด Ctrl+Z ย้อนกลับได้ แล้ว **Save** หรือ Ctrl+S เมื่อพร้อม
5. เลือก **Run** หรือ **Test** ดูผลที่ Output กดข้อผิดพลาดแบบ `file:line` เพื่อกลับไปยังบรรทัดนั้น และกด Stop เพื่อยกเลิกงาน
6. เลือก **Ask AI** หรือใช้ Explain / Edit / Review / Plan ในแผง Local AI ไฟล์ที่เปิดหรือข้อความที่เลือกจะเป็นบริบท

AI workspace พับรายการเซสชันเพื่อให้โค้ดมีพื้นที่มากขึ้น เปิดกลับได้ด้วยปุ่มด้านบน จอขนาดไม่เกิน 900px ใช้แท็บ Files / Code / Chat / Console เพื่อเลือกแผงที่ต้องการ

## คีย์ลัดและการบันทึก

| คำสั่ง | วิธีใช้ |
|---|---|
| บันทึก | Ctrl+S |
| ค้นหา / แทนที่ | Ctrl+F / Ctrl+H |
| จัดรูปแบบ | Shift+Alt+F |
| Undo / Redo บน Windows | Ctrl+Z / Ctrl+Y |
| เปิด Debug | F5 |
| เติมคำในไฟล์ | Ctrl+Space |
| เพิ่ม / ลด indentation | Tab / Shift+Tab |

Format รองรับ Go ผ่าน `go/format` และ JS, TS, JSON, HTML, CSS, Markdown, YAML ผ่าน Prettier ที่บันเดิลไว้และโหลดเมื่อใช้ครั้งแรก ฟอร์แมตเตอร์เว็บใช้ค่าเริ่มต้น 2 spaces / LF ยังไม่อ่านการตั้งค่า Prettier ของโปรเจกต์

Save ตรวจ SHA ของไฟล์เดิมก่อนเขียน หากมีผู้แก้บนดิสก์ระหว่างนั้นจะรายงานข้อขัดแย้งแทนการเขียนทับ การพิมพ์ที่เกิดระหว่างรอ Save/Format จะยังอยู่ มีคำเตือนเมื่อจะออกจากหน้าที่มีการแก้ไขค้าง แต่ฉบับที่ยังไม่บันทึกไม่ได้สำรองข้ามการปิดเบราว์เซอร์

## Run, Test และ Terminal

| ไฟล์ | Run | Test |
|---|---|---|
| Go package main | `go run` ที่ package ของไฟล์ | `go test ./...` จากรากโปรเจกต์ |
| Go package อื่น | `go test` เฉพาะ package | `go test ./...` |
| go.mod | `go build ./...` | `go test ./...` |
| JS / CJS / MJS | `node` ไฟล์ที่เปิด | `node --test` |
| Python | `python3` หรือ `python` ที่ติดตั้ง | `-m unittest discover` ด้วย executable เดียวกัน |

Run/Test บันทึก **ไฟล์ปัจจุบัน** ก่อนเริ่ม ต้องบันทึกไฟล์อื่นที่แก้ค้างก่อนตรวจทั้งโปรเจกต์ งานใช้ arguments แยกจาก executable ไม่ประกอบ shell string จำกัดเวลาที่ 5 นาทีจากปุ่ม IDE และเก็บ output ไม่เกิน 2 MiB โดยแสดงส่วนท้ายไม่เกิน 128 KiB เพื่อไม่ทำให้หน้าจอหนัก

สำหรับ npm scripts, TypeScript toolchain, pytest, คำสั่งเฉพาะโปรเจกต์ หรือโปรแกรมที่รับ input ให้เปิด **Terminal** ซึ่งบน Windows ใช้ PowerShell ผ่าน ConPTY รองรับพิมพ์ วางข้อความ ปรับขนาด และ Ctrl+C ปิด terminal แล้วระบบปิด process tree ของ terminal นั้นด้วย

## Debug

รองรับ Go ผ่าน Delve และ JavaScript ผ่าน Node Inspector ที่เชื่อมบน loopback เท่านั้น คลิก gutter ข้างเลขบรรทัดเพื่อตั้ง breakpoint แล้วเลือก Debug → Start debugging ใช้ Continue, Pause, Step over/in/out และ Stop พร้อมดู Call stack และ Variables

เครื่องนี้ติดตั้ง Delve ไว้ที่ `.hermetrix-tools/dlv.exe` รายละเอียดการติดตั้งและข้อจำกัดอยู่ใน [DEBUGGER.md](DEBUGGER.md)

Debug ใช้โค้ดที่บันทึกแล้ว และโปรแกรมมีสิทธิ์ของบัญชีที่รัน Hermetrix ไม่ใช่ OS sandbox ที่จำกัดทุกการเขียนไฟล์/เครือข่าย

## AI local และแผนงาน

- Local AI เลือกเฉพาะ provider ที่เปิดใช้งาน พร้อมใช้งาน และชี้ localhost/loopback ไม่ส่งต่อไป remote provider อัตโนมัติ
- แนบไฟล์หรือ selection ไม่เกิน 24,000 ตัวอักษร ระบุชัดหากตัดข้อความหรือเป็นฉบับที่ยังไม่บันทึก
- Explain / Review อ่านและอธิบาย; Edit เตรียมช่องให้ระบุสิ่งที่ต้องการแก้; Plan เปิดงานแบบมีเป้าหมาย เกณฑ์สำเร็จ ขั้นตอน และรายการตรวจ
- การเขียนไฟล์ผ่าน AI ใช้ approval และ SHA เดิม ไฟล์ที่เปิดจะอัปเดตเมื่อไม่มีฉบับแก้ค้าง ถ้ามีฉบับแก้ค้างจะเก็บไว้และแจ้งให้ตรวจความต่าง
- งานแบบ durable แยกขั้นเสนอการแก้ไข อนุมัติ ใช้การแก้ไข รัน checks และตรวจงาน โดยการตรวจสุดท้ายแบบอิสระต้องมีอีกโมเดลหรือ endpoint หนึ่ง
- Planner รับเฉพาะ manifest ชื่อไฟล์/ขนาดที่กรอง secret และรายการ executable เดียวกับ command runner โดยไม่อ่านข้อความไฟล์ในขั้นวางแผน
- หาก run lease หมดอายุ ปุ่ม Recover saved work จะเก็บ proposal/evidence เดิม abandon intent ที่ยังไม่ dispatch และหมุน authority เพื่อทำ attempt เดิมต่อเมื่อทุก action มี receipt; action ที่ dispatch แล้วแต่ไม่มี receipt จะบังคับ pause/reconcile โดยไม่ replay

## หลักฐานที่ตรวจจริง

- Browser: คลิกพิมพ์หลายบรรทัด เลือกข้อความ Tab/Shift+Tab completion Undo/Redo Ctrl+S และเปิดใหม่ตรงกับไฟล์บนดิสก์
- Browser: Format ไม่เขียนดิสก์และ Undo ได้ → Run บันทึกไฟล์แล้วได้ผล 10 → Node Test ผ่าน → Debug หยุด breakpoint อ่าน `total=5` และ `doubled=10` → Stop
- Go Debug บน Windows: breakpoint, locals, step over/in/out, pause, continue, stop, compiler failure และ exit code ที่ล้มเหลว
- Windows Terminal: cmd/PowerShell, รับ input, cursor output, resize, Ctrl+C และปิด process ลูก
- ตรวจการกด Run/Test ซ้อน การเปลี่ยนไฟล์หรือโปรเจกต์ระหว่างรอผล และผล debugger เก่าที่กลับมาช้ากว่า
- ทดสอบหน้าจอ 1280×720, 800×900, 390×844 และ flow IDE ที่ 1440×900
- โมเดล Bonsai จริงอ่านไฟล์ตัวอย่าง เสนอแก้ `a - b` เป็น `a + b` ผ่าน exact-write approval แล้ว Node tests เปลี่ยนจาก exit 1 เป็นผ่าน 3 tests / exit 0 โมเดลอ่านไฟล์ซ้ำเพื่อตรวจผล หลักฐานอยู่ใน `.hermetrix-ide/ai-proof-evidence.json` เป็นการตรวจโดยโมเดลเดิม ไม่ใช่ independent review

## ขอบเขตที่ยังไม่เทียบเท่า VS Code

ยังไม่มี extension marketplace, language server แบบเต็ม, semantic rename/find references, IntelliSense ข้ามโปรเจกต์, debugger expression evaluation, TypeScript source maps, browser debugger และตัวเลือก goroutine ของ Go การเติมคำใน editor ปัจจุบันเป็นคำจากเอกสาร ไม่ใช่ semantic completion จาก language server

ตัว IDE และระบบงานใช้งานได้ตามรายการที่ทดสอบข้างต้น แต่ความถูกต้องของโค้ดที่ AI สร้างต้องพิสูจน์ด้วย checks ของแต่ละโปรเจกต์ ไม่ได้อนุมานจากการตอบกลับของโมเดล
