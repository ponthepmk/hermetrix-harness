const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const runtime = require("./runtime.js");

function cockpit(overrides = {}) {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("const taskActions = new Map()");
  const end = source.indexOf("// Four is the ceiling", start);
  assert.ok(start > 0 && end > start);
  const timers = new Map();
  const context = vm.createContext({
    state:{durableTasks:[], taskExecutions:{}, providers:[], selectedDurableTask:"task-1"},
    URL, Date, Map, setInterval:callback => { const id = timers.size + 1; timers.set(id, callback); return id; },
    clearInterval:id => timers.delete(id), escapeHTML:runtime.escapeHTML,
    $:() => null, $$:() => [], currentActor:() => "test-user", toast:() => {}, asList:runtime.asList,
    pill:text => text, renderPanes:() => {}, askAction:async () => "Reviewed the proposed files",
    FormData:class { constructor(form) { this.form = form; } get(key) { return this.form[key]; } },
    ...overrides,
  });
  vm.runInContext(source.slice(start, end), context);
  return {context, timers};
}

const model = {id:"local", name:"Local", enabled:true, credential_ready:true, model:"worker", base_url:"http://127.0.0.1:8088/v1"};
const liveRun = () => ({id:"run-1", state:"running", plan_revision:1, lease_token:"private-lease", lease_expires_at:new Date(Date.now()+600000).toISOString()});
const baseTask = () => ({id:"task-1", title:"Fix sum", objective:"Addition returns sum", state:"ready", revision:2, active_plan_revision:1, egress_policy:"local_only", plan:{steps:[{key:"fix", title:"Fix", state:"pending"}]}});

test("task failures remain visible after refresh and clear only on the next action", async () => {
  const {context} = cockpit();
  context.refreshDurableTask = async () => {};
  await context.performTaskAction("task-1", {isConnected:false}, "Creating plan", async () => { throw new Error("Model context is too small"); });
  assert.equal(vm.runInContext('taskErrors.get("task-1")', context), "Model context is too small");
  assert.equal(vm.runInContext('taskActions.has("task-1")', context), false);
  await context.performTaskAction("task-1", {isConnected:false}, "Creating plan", async () => {});
  assert.equal(vm.runInContext('taskErrors.has("task-1")', context), false);
});

test("local task excludes remote, disabled and credential-missing providers; reviewer must differ in model or endpoint", () => {
  const providers = [model,
    {...model, id:"clone"},
    {...model, id:"reviewer", model:"review-model"},
    {...model, id:"remote", base_url:"https://api.example.com/v1"},
    {...model, id:"missing", credential_ready:false},
    {...model, id:"disabled", enabled:false},
  ];
  const {context} = cockpit({state:{providers}});
  assert.deepEqual(Array.from(context.taskEligibleProviders(baseTask()), item => item.id), ["local","clone","reviewer"]);
  assert.deepEqual(Array.from(context.taskEligibleProviders(baseTask(), "local"), item => item.id), ["reviewer"]);
  assert.deepEqual(Array.from(context.taskEligibleProviders({...baseTask(), egress_policy:"remote_allowed"}, "local"), item => item.id), ["reviewer","remote"]);
  assert.equal(context.taskProviderIsLocal({...model, base_url:"http://localhost.evil.test"}), false);
});

test("completed first step offers next step and preserves all authority fields on provider dispatch", async () => {
  const task = {...baseTask(), state:"running", revision:5};
  const run = liveRun();
  const calls = [];
  const packet = {task_id:task.id, task_revision:5, step:{key:"next", revision:1}, canonical_packet_hash:"sha256:packet"};
  let execution = {task, run, attempt:{id:"old", run_id:run.id, state:"completed"}, proposal:{state:"verified"}, effects:[]};
  const {context} = cockpit({state:{providers:[model], durableTasks:[task], selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}},
    api:async (path, options = {}) => {
      const body = options.body ? JSON.parse(options.body) : null;
      calls.push({path, body});
      if (path.endsWith("/lease")) { assert.equal(body.lease_token, run.lease_token); return run; }
      if (path.endsWith("/next-packet")) return packet;
      if (path.endsWith("/attempts")) { assert.equal(body.expected_task_revision, packet.task_revision); return {id:"attempt-2", packet}; }
      if (path.endsWith("/select-files") || path.endsWith("/proposals")) {
        assert.deepEqual(body.authority, {run_id:run.id, lease_token:run.lease_token});
        return {result:{files:["sum.go"]}};
      }
      if (path.endsWith("/execution")) return execution;
      throw new Error(`Unexpected API request ${path}`);
    },
  });
  assert.equal(context.taskCanStartStep(task, execution), true);
  assert.match(context.taskExecutionHTML(task, execution), /Start next step/);
  await context.startTaskProposal({preventDefault(){}, currentTarget:{provider_id:"local", files:""}}, {isConnected:false});
  assert.equal(calls.filter(call => call.path.endsWith("/runs")).length, 0);
  assert.equal(calls.filter(call => call.path.endsWith("/proposals")).length, 1);
  assert.equal(calls.filter(call => call.path.endsWith("/lease")).length, 2);
});

test("initial run reads packet after run revision and sends lease to attempt", async () => {
  const task = baseTask(), run = liveRun(), calls = [];
  let execution = {task, effects:[]};
  const packet = {task_revision:3, step:{key:"fix", revision:1}, canonical_packet_hash:"packet"};
  const {context} = cockpit({state:{providers:[model], durableTasks:[task], selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}},
    api:async (path, options = {}) => {
      const body = options.body ? JSON.parse(options.body) : null;
      calls.push(path);
      if (path.endsWith("/runs")) return {run, task:{...task, state:"running", revision:3}};
      if (path.endsWith("/lease")) return run;
      if (path.endsWith("/next-packet")) return packet;
      if (path.endsWith("/attempts")) { assert.equal(body.lease_token, run.lease_token); assert.equal(body.expected_task_revision, 3); return {id:"attempt-1", packet}; }
      if (path.endsWith("/proposals")) { assert.equal(body.authority.lease_token, run.lease_token); return {}; }
      if (path.endsWith("/execution")) return execution;
      throw new Error(`Unexpected API request ${path}`);
    },
  });
  await context.startTaskProposal({preventDefault(){}, currentTarget:{provider_id:"local", files:"sum.go"}}, {isConnected:false});
  assert.ok(calls.indexOf("/api/tasks/task-1/runs") < calls.indexOf("/api/tasks/task-1/next-packet"));
  assert.equal(calls.some(path => path.endsWith("/select-files")), false);
});

for (const [handler, action] of [["applyTaskProposal", "apply"], ["verifyTaskProposal", "verify-frozen"], ["postReviewTaskProposal", "post-review"]]) {
  test(`${action} renews and passes run authority instead of relying on implicit ownership`, async () => {
    const task = {...baseTask(), state:"running"}, run = liveRun();
    const execution = {task, run, proposal:{id:"proposal-1", provider_id:"local"}, effects:[]};
    let dispatched = 0;
    const {context} = cockpit({state:{providers:[model,{...model, id:"reviewer", model:"other"}], durableTasks:[task], selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}},
      $:selector => selector === "#taskReviewerProvider" ? {value:"reviewer"} : null,
      api:async (path, options = {}) => {
        if (path.endsWith("/lease")) return run;
        if (path.endsWith(`/${action}`)) {
          assert.deepEqual(JSON.parse(options.body).authority, {run_id:run.id, lease_token:run.lease_token}); dispatched++; return {};
        }
        if (path.endsWith("/execution")) return execution;
        throw new Error(`Unexpected API request ${path}`);
      },
    });
    await context[handler]({isConnected:false});
    assert.equal(dispatched, 1);
  });
}

test("expired lease and uncertain effects cannot start work", async () => {
  const task = {...baseTask(), state:"running"};
  const run = {...liveRun(), lease_expires_at:"2020-01-01T00:00:00Z"};
  let requests = 0;
  const {context} = cockpit({state:{providers:[], taskExecutions:{"task-1":{run}}}, api:async () => { requests++; }});
  assert.equal(context.taskCanStartStep(task, {run, effects:[]}), false);
  assert.equal(context.taskCanStartStep(baseTask(), {effects:[{state:"uncertain"}]}), false);
  await assert.rejects(context.renewTaskAuthority("task-1"), /lease has expired/);
  assert.equal(requests, 0);
  assert.match(context.taskExecutionHTML(task, {run, effects:[]}), /Recover saved work/);
});

test("expired run recovery is revision-bound and exposed as an explicit action", async () => {
  const task = {...baseTask(), state:"running", revision:7};
  const run = {...liveRun(), lease_expires_at:"2020-01-01T00:00:00Z"};
  const calls = [];
  const recovered = {...task, state:"paused", revision:8, pause_reason:"run lease expired"};
  const {context} = cockpit({state:{durableTasks:[task], selectedDurableTask:task.id, taskExecutions:{[task.id]:{task, run, effects:[]}}},
    api:async (path, options = {}) => {
      calls.push({path, body:options.body ? JSON.parse(options.body) : null});
      if (path.endsWith("/recover-expired-run")) return recovered;
      throw new Error(`Unexpected API request ${path}`);
    },
  });
  context.refreshDurableTask = async () => {};
  await context.recoverExpiredTaskRun({isConnected:false});
  assert.deepEqual(calls[0].body, {expected_task_revision:7, actor:"test-user"});
  assert.equal(context.state.durableTasks[0].state, "paused");
});

test("a newly reviewed plan can start without inheriting the previous plan's rejected proposal or expired lease", () => {
  const task = {...baseTask(), active_plan_revision:2};
  const execution = {run:{...liveRun(), plan_revision:1, lease_expires_at:"2020-01-01T00:00:00Z"}, proposal:{state:"rejected"}, effects:[]};
  const {context} = cockpit({state:{providers:[model]}});
  assert.equal(context.taskCanStartStep(task, execution), true);
  assert.match(context.taskExecutionHTML(task, execution), /Generate proposed changes/);
  assert.doesNotMatch(context.taskExecutionHTML(task, execution), /run's lease expired/);
});

test("lease heartbeat stops when the task surface is closed", async () => {
  const task = {...baseTask(), state:"running"}, run = liveRun();
  const execution = {task, run, effects:[]};
  const body = {isConnected:true};
  const {context, timers} = cockpit({state:{selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}}});
  context.maintainTaskLease(task, execution, body);
  assert.equal(timers.size, 1);
  body.isConnected = false;
  await [...timers.values()][0]();
  assert.equal(timers.size, 0);
});

test("replacing the task surface immediately moves its heartbeat to the new body", async () => {
  const task = {...baseTask(), state:"running"}, run = liveRun();
  const execution = {task, run, effects:[]};
  const oldBody = {isConnected:true}, newBody = {isConnected:true};
  let renewals = 0;
  const {context, timers} = cockpit({state:{selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}}, api:async () => { renewals++; return run; }});
  context.maintainTaskLease(task, execution, oldBody);
  oldBody.isConnected = false;
  context.maintainTaskLease(task, execution, newBody);
  assert.equal(timers.size, 1);
  await [...timers.values()][0]();
  assert.equal(renewals, 1);
  assert.equal(timers.size, 1);
});

test("proposal contents load from immutable artifact and are escaped before approval", async () => {
  const task = baseTask();
  const proposal = {id:"p", artifact_id:"artifact-1", state:"pending_review"};
  const {context} = cockpit({state:{providers:[model], taskExecutions:{}}, api:async path => path.endsWith("/execution") ? {task, proposal, effects:[]} : {summary:"Change addition", changes:[{path:"sum.go", content:"<script>alert(1)</script>"}]}});
  await context.loadTaskExecution(task.id);
  const html = context.taskExecutionHTML(task, context.state.taskExecutions[task.id]);
  assert.match(html, /sum\.go/);
  assert.match(html, /&lt;script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /data-task-proposal-decision="approved" >/);
});

test("decision lab shows immutable evidence and makes the read-only boundary explicit", () => {
  const task = baseTask();
  const labs = {[task.id]:{loaded:true, loading:false,
    admission:{policy:{enabled:true, provider_id:"local"}},
    benchmarks:[{provider_id:"local", correct_cases:6, total_cases:6, invalid_rate:0, average_latency_ms:321, passed:true}],
    shadows:[{agreement:true, valid:true, baseline:{action_id:"inspect_file"}, model:{action_id:"inspect_file"}, latency_ms:95}],
    metrics:{total_runs:1, agreement_rate:1, average_latency_ms:95}, knowledge:[{memory_kind:"debug", snippet:"Use <safe> retry", score:2, matched_terms:["retry"]}],
    progress:{attempts:2, remaining_attempts:18, remaining_planner_escalations:2, no_progress:false, budget_exhausted:false, blocked_reasons:[]},
    recommendation:{decision:{action_id:"inspect_file", reason:"inspect source"}},
  }};
  const {context} = cockpit({state:{providers:[model], taskDecisionLabs:labs}});
  const html = context.taskDecisionLabHTML(task);
  assert.match(html, /read-only enabled/);
  assert.match(html, /6\/6 correct/);
  assert.match(html, /inspect_file/);
  assert.match(html, /Attempts left/);
  assert.match(html, /Use &lt;safe&gt; retry/);
  assert.match(html, /cannot execute tools or edit files/);
});

test("read-only recommendation is revision-bound and never dispatches an execution endpoint", async () => {
  const task = baseTask(), calls = [];
  const labs = {[task.id]:{loaded:true, loading:false, admission:{policy:{enabled:true, provider_id:"local"}}, benchmarks:[], shadows:[]}};
  const execution = {task, effects:[]};
  const {context} = cockpit({state:{providers:[model], durableTasks:[task], selectedDurableTask:task.id, taskExecutions:{[task.id]:execution}, taskDecisionLabs:labs},
    api:async (path, options = {}) => {
      calls.push({path, body:options.body ? JSON.parse(options.body) : null});
      if (path.endsWith("/decision-read-only")) return {decision:{action_id:"inspect_file", reason:"inspect source"}};
      if (path.endsWith("/execution")) return execution;
      throw new Error(`Unexpected API request ${path}`);
    },
  });
  await context.recommendTaskDecision({isConnected:false});
  assert.deepEqual(calls[0].body, {expected_task_revision:task.revision});
  assert.equal(calls.some(call => /attempts|proposals|apply|verify/.test(call.path)), false);
  assert.equal(context.taskDecisionLab(task.id).recommendation.decision.action_id, "inspect_file");
});
