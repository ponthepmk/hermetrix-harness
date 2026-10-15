const test = require("node:test");
const assert = require("node:assert/strict");
const {createTracker, requestLabel} = require("./interaction.js");
const fs = require("node:fs");
const vm = require("node:vm");

test("overlapping requests keep loading until every actual request settles", () => {
  let now = 0;
  const tracker = createTracker({now:() => now});
  const control = {};
  const a = tracker.begin({control, label:"Plans"});
  const b = tracker.begin({control, label:"Evidence"});
  assert.equal(tracker.pending(control), true);
  now = 9000;
  assert.equal(tracker.snapshot().elapsed, 9);
  a();
  assert.equal(tracker.snapshot().state, "loading");
  assert.equal(tracker.snapshot().count, 1);
  b();
  assert.equal(tracker.snapshot().state, "success");
  assert.equal(tracker.pending(control), false);
});

test("a failed request cannot be hidden by a later successful overlapping request", () => {
  const tracker = createTracker();
  const a = tracker.begin({label:"A"});
  const b = tracker.begin({label:"B"});
  a(new Error("network unavailable"));
  assert.equal(tracker.snapshot().state, "loading");
  b();
  assert.equal(tracker.snapshot().state, "error");
  tracker.dismiss();
  assert.equal(tracker.snapshot().state, "idle");
});

test("finishing twice cannot prematurely finish another control's work", () => {
  const tracker = createTracker();
  const a = {}, b = {};
  const first = tracker.begin({control:a, label:"A"});
  const second = tracker.begin({control:b, label:"B"});
  first(); first();
  assert.equal(tracker.pending(a), false);
  assert.equal(tracker.pending(b), true);
  assert.equal(tracker.snapshot().count, 1);
  tracker.dismiss();
  assert.equal(tracker.snapshot().state, "loading");
  second();
});

test("an explicit new attempt can recover from a previous failed operation", () => {
  const tracker = createTracker();
  tracker.begin({label:"Load"})(new Error("offline"));
  const retry = tracker.begin({label:"Load again"});
  retry();
  assert.equal(tracker.snapshot().state, "success");
});

test("read and write feedback describes the actual requested operation", () => {
  assert.match(requestLabel("/api/tasks?limit=100"), /โหลดแผน/);
  assert.match(requestLabel("/api/tasks", "POST"), /บันทึกงาน/);
  assert.match(requestLabel("/api/providers/local/test", "POST"), /ทดสอบการเชื่อมต่อ/);
  assert.match(requestLabel("/api/task-code-proposals/x/post-review", "POST"), /ตรวจทาน/);
});

function apiHarness(fetch, {noIntent = false} = {}) {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const receipts = [], timers = new Map();
  const context = vm.createContext({
    state:{}, FormData, AbortController, fetch,
    HermetrixInteraction:{beginRequest:() => noIntent ? null : error => receipts.push(error)},
    setTimeout:callback => { timers.set(1, callback); return 1; },
    clearTimeout:id => timers.delete(id),
  });
  vm.runInContext(source.slice(source.indexOf("async function api("), source.indexOf("function currentActor(")), context);
  return {context, timers, receipts};
}

test("a foreground read that never responds times out and releases its loading state", async () => {
  let signal;
  const {context, timers, receipts} = apiHarness((_path, options) => new Promise((_resolve, reject) => {
    signal = options.signal;
    signal.addEventListener("abort", () => reject(new Error("aborted")));
  }));
  const pending = context.api("/api/tasks");
  assert.equal(receipts.length, 0);
  timers.get(1)();
  await assert.rejects(pending, /โหลดข้อมูลนานเกินไป/);
  assert.equal(signal.aborted, true);
  assert.equal(receipts.length, 1);
  assert.equal(timers.size, 0);
});

test("a write is never timed out or retried by the feedback layer", async () => {
  let requests = 0, finish;
  const {context, timers, receipts} = apiHarness((_path, options) => {
    requests++;
    assert.equal(options.signal, undefined);
    return new Promise(resolve => { finish = () => resolve({ok:true, headers:{get:() => null}, json:async () => ({saved:true})}); });
  });
  const pending = context.api("/api/tasks", {method:"POST", body:"{}"});
  assert.equal(timers.size, 0);
  assert.equal(receipts.length, 0);
  finish();
  const body = await pending;
  assert.equal(body.saved, true);
  assert.equal(requests, 1);
  assert.deepEqual(receipts, [undefined]);
});

test("a read timing out during body transfer reports failure instead of empty success", async () => {
  let rejectBody;
  const {context, timers, receipts} = apiHarness(async () => ({ok:true, headers:{get:() => null}, json:() => new Promise((_resolve, reject) => { rejectBody = reject; })}));
  const pending = context.api("/api/tasks");
  await new Promise(resolve => setImmediate(resolve));
  timers.get(1)();
  rejectBody(new Error("aborted body"));
  await assert.rejects(pending, /โหลดข้อมูลนานเกินไป/);
  assert.equal(receipts.length, 1);
});

test("bootstrap reads are bounded even before the first click", async () => {
  const {context, timers} = apiHarness((_path, options) => new Promise((_resolve, reject) => {
    options.signal.addEventListener("abort", () => reject(new Error("aborted")));
  }), {noIntent:true});
  const pending = context.api("/api/bootstrap");
  timers.get(1)();
  await assert.rejects(pending, /โหลดข้อมูลนานเกินไป/);
  assert.equal(timers.size, 0);
});

test("background polling keeps its own lifecycle without a foreground timeout", async () => {
  const {context, timers, receipts} = apiHarness(async (_path, options) => {
    assert.equal(options.signal, undefined);
    return {ok:true, headers:{get:() => null}, json:async () => []};
  }, {noIntent:true});
  await context.api("/api/terminal/output", {uiSilent:true});
  assert.equal(timers.size, 0);
  assert.equal(receipts.length, 0);
});
