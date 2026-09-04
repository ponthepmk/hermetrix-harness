const test = require("node:test");
const assert = require("node:assert/strict");
const runtime = require("./runtime.js");

test("escapeHTML encodes every HTML control character", () => {
  assert.equal(runtime.escapeHTML(`<script data-x="a'b">&`), "&lt;script data-x=&quot;a&#39;b&quot;&gt;&amp;");
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
