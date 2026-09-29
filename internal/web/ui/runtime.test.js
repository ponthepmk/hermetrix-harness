const test = require("node:test");
const assert = require("node:assert/strict");
const runtime = require("./runtime.js");

test("default model is ready while an explicit model choice remains respected", () => {
  const missing = {id:"remote", enabled:true, credential_ready:false};
  const ready = {id:"local", enabled:true, credential_ready:true};
  const disabled = {id:"disabled", enabled:false, credential_ready:true};
  assert.equal(runtime.preferredProvider([disabled, missing, ready]), ready);
  assert.equal(runtime.preferredProvider([missing, ready], "remote"), missing);
  assert.equal(runtime.preferredProvider([disabled]), null);
});

test("a qualified 64k main profile is preferred for a new chat without overriding a choice", () => {
  const legacy = {id:"legacy", enabled:true, credential_ready:true, context_window:65536, context_evidence:"declared", max_output_tokens:2048};
  const main = {id:"main", enabled:true, credential_ready:true, context_window:65536, context_evidence:"runtime_probe", max_output_tokens:8192};
  assert.equal(runtime.preferredProvider([legacy, main]), main);
  assert.equal(runtime.preferredProvider([legacy, main], "legacy"), legacy);
});

test("escapeHTML encodes every HTML control character", () => {
  assert.equal(runtime.escapeHTML(`<script data-x="a'b">&`), "&lt;script data-x=&quot;a&#39;b&quot;&gt;&amp;");
});

test("chat Markdown stays readable without allowing authored HTML", () => {
  const html = runtime.renderChatMarkdown("## Summary\n\n**Two IDs** are different.\n- Board ID\n- Platform ID\n\n```js\n<script>alert(1)</script>\n```");
  assert.match(html, /<h4>Summary<\/h4>/);
  assert.match(html, /<strong>Two IDs<\/strong>/);
  assert.match(html, /<ul><li>Board ID<\/li><li>Platform ID<\/li><\/ul>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
});

test("workspace write previews never expose the file body", () => {
  const body = "secret-value".repeat(100);
  const preview = runtime.toolArgumentsPreview({
    metadata: { tool_name:"workspace.write_file" },
    content: JSON.stringify({ path:"notes.txt", content:body, expected_sha256:"abc" }),
  });
  assert.doesNotMatch(preview, /secret-value/);
  assert.match(preview, /1200 characters/);
  assert.match(preview, /notes\.txt/);
});

test("timeline pairs receipts by call id without dropping unpaired events", () => {
  const call = { event_kind:"tool_call", metadata:{ tool_call_id:"call-1" } };
  const message = { event_kind:"message", content:"between" };
  const receipt = { event_kind:"tool_result", metadata:{ tool_call_id:"call-1" } };
  const orphan = { event_kind:"tool_result", metadata:{ tool_call_id:"call-2" } };
  assert.deepEqual(runtime.groupTimeline([call, message, receipt, orphan]), [
    { kind:"tool_step", call, result:receipt },
    { kind:"event", event:message },
    { kind:"event", event:orphan },
  ]);
});

test("unexpected collection payloads fail closed to an empty list", () => {
  assert.deepEqual(runtime.asList(null), []);
  assert.deepEqual(runtime.asList({ length:1 }), []);
  assert.deepEqual(runtime.asList(["ok"]), ["ok"]);
});
