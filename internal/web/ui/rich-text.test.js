const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const rich = vm.runInNewContext(fs.readFileSync(`${__dirname}/vendor/rich-text.js`, 'utf8')+';HermetrixRichText;', {});

test('rich replies preserve nested steps, numbered starts, tables, quotes and emphasis', () => {
  const html = rich.render('## แผนงาน\n\n3. เริ่มงาน\n   - ตรวจ *ข้อมูล*\n   - ใช้ `MCP`\n\n| ส่วน | หน้าที่ |\n| --- | --- |\n| Brain | ความรู้ |\n\n> ข้อมูลที่ยืนยันแล้ว');
  assert.match(html, /<h2>แผนงาน<\/h2>/);
  assert.match(html, /<ol start="3">/);
  assert.match(html, /<ul>\s*<li>ตรวจ <em>ข้อมูล<\/em>/);
  assert.match(html, /<th>ส่วน<\/th>/);
  assert.match(html, /<td>Brain<\/td>/);
  assert.match(html, /<blockquote>/);
  const aligned=rich.render('| ค่า |\n| ---: |\n| 7 |');
  assert.match(aligned,/class="chat-align-right"/);
  assert.doesNotMatch(aligned,/style=/);
});
test('model HTML and unsafe links remain inert and images never fetch remotely', () => {
  const html = rich.render('<script>alert(1)</script>\n\n[bad](javascript:alert(1)) [source](https://example.com/)\n\n![image](https://tracking.invalid/pixel)');
  assert.doesNotMatch(html, /<script|href="javascript:|<img/);
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, /rel="noopener noreferrer"/);
});
test('mermaid source is escaped data and remains inspectable without executing markup', () => {
  const html = rich.render('```mermaid\nflowchart TD\nA["<img src=x onerror=alert(1)>"] --> B[ผลลัพธ์]\n```');
  assert.match(html, /class="chat-diagram"/);
  assert.match(html, /data-diagram-source="flowchart TD/);
  assert.match(html, /&lt;img/);
  assert.doesNotMatch(html, /<img/);
});
