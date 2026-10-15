const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const runtime = require("./runtime.js");

function cockpit(overrides = {}) {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("const taskActions = new Map()");
  const end = source.indexOf("// Tool surfaces have stable form IDs", start);
  assert.ok(start > 0 && end > start);
  const timers = new Map();
  const context = vm.createContext({
    state:{durableTasks:[], taskExecutions:{}, providers:[], selectedDurableTask:"task-1"},
    URL, Date, Map, setInterval:callback => { const id = timers.size + 1; timers.set(id, callback); return id; },
    clearInterval:id => timers.delete(id), escapeHTML:runtime.escapeHTML,
    $:() => null, $$:() => [], document:{querySelector:() => null}, currentActor:() => "test-user", toast:() => {}, asList:runtime.asList, formatDate:value => value,
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

test("boundary pause is offered only after active work has settled", () => {
  const {context}=cockpit({state:{durableTasks:[],taskExecutions:{},providers:[model],selectedDurableTask:"task-1"}});
  const task={...baseTask(),state:"running"};
  assert.match(context.taskExecutionHTML(task,{run:liveRun(),attempt:{state:"completed"},effects:[]}),/taskPauseRun/);
  assert.doesNotMatch(context.taskExecutionHTML(task,{run:liveRun(),attempt:{state:"running"},effects:[{state:"dispatched",action:"provider.propose"}]}),/taskPauseRun/);
  assert.equal(context.taskStage({...task,state:"paused"},{run:{state:"paused"},attempt:{state:"completed"},proposal:{state:"verified"}}).label,"พักงานแล้ว");
});

test("refresh fences stale completion evidence when another client advances the task", () => {
  const {context} = cockpit();
  context.state.taskExecutions["task-1"] = {task:{revision:1},completion:{awaiting_user_acceptance:true}};
  context.reconcileTaskCache([{id:"task-1",revision:3}]);
  assert.equal(context.state.taskExecutions["task-1"], undefined);
  context.state.taskExecutions["task-1"] = {task:{revision:3}};
  context.reconcileTaskCache([{id:"task-1",revision:2}]);
  assert.equal(context.state.taskExecutions["task-1"].task.revision, 3);
});

test("technically verified work presents the human acceptance action after restart", () => {
  const {context} = cockpit();
  const task = {...baseTask(), state:"verifying", plan:{steps:[{key:"fix", state:"completed"}]}};
  const execution = {run:{state:"paused", plan_revision:1}, attempt:{state:"completed"}, proposal:{state:"verified"}, effects:[], completion:{awaiting_user_acceptance:true}};
  assert.equal(context.taskCanStartStep(task, execution), false);
  assert.equal(context.taskStage(task, execution).label, "รอรับงาน");
  const html = context.taskExecutionHTML(task, execution);
  assert.match(html, /data-task-jump="taskAcceptResult"/);
  assert.doesNotMatch(html, /taskProposalForm/);
  assert.equal(context.taskCanStartStep({...task, state:"paused"}, execution), false);
});

test("explicit refresh replaces same-revision evidence and fences the old pending read", async () => {
  const task = baseTask(); let settleOld, executions = 0;
  const old = new Promise(resolve => { settleOld = resolve; });
  const {context} = cockpit({api:async path => {
    if (path.endsWith("/execution")) return ++executions === 1 ? old : {task, effects:[]};
    if (path.includes("/completion?")) return {task_revision:task.revision, gates:[{id:"criterion:AC-1",status:"fail",evidence_refs:["artifact:latest-failure"]}]};
    return [];
  }});
  context.state.durableTasks = [task];
  context.state.taskExecutions[task.id] = {task,completion:{gates:[{id:"criterion:AC-1",status:"pass"}]}};
  context.reconcileTaskCache([task]);
  assert.equal(context.state.taskExecutions[task.id].completion.gates[0].status, "pass", "ordinary list hydration retains current details");
  const pending = context.loadTaskExecution(task.id);
  context.reconcileTaskCache([task], true);
  assert.equal(context.state.taskExecutions[task.id], undefined);
  await context.loadTaskExecution(task.id);
  settleOld({task,effects:[{id:"obsolete"}]});
  await pending;
  assert.equal(context.state.taskExecutions[task.id].completion.gates[0].status, "fail");
  assert.deepEqual(context.state.taskExecutions[task.id].effects, []);
});

test("failed step names the recovery target until a fresh proposal reaches review", () => {
  const {context} = cockpit();
  const task = {...baseTask(), state:"paused", plan:{steps:[{key:"fix", title:"Fix sum", state:"failed"}]}};
  assert.match(context.taskStage(task, {effects:[]}).detail, /Fix sum/);
  assert.equal(context.taskStage(task, {run:{plan_revision:1}, proposal:{state:"pending_review"}}).label, "ตรวจการแก้ไข");
});

test("failed mandatory check keeps the plan and rollback guidance honest", () => {
  const {context} = cockpit();
  const task = {...baseTask(), state:"paused", plan:{steps:[{key:"fix", title:"Fix sum", state:"failed", last_error:"mandatory check node test.js failed"}]}};
  assert.match(context.taskPlanReferenceNote(task), /ตรวจไม่ผ่าน/);
  assert.doesNotMatch(context.taskPlanReferenceNote(task), /เสร็จแล้ว/);
  const execution = {run:{state:"paused"}, attempt:{state:"failed"}, proposal:{state:"verification_failed", artifact_id:"artifact-proposal"},
    effects:[{action:"workspace.run",state:"observed",receipt:{evidence_refs:["artifact:failed-check"]}}]};
  const html = context.taskExecutionHTML(task, execution);
  assert.match(html, /วิธีตรวจ node test.js ไม่ผ่าน/);
  assert.match(html, /พยายามคืนไฟล์/);
  assert.match(html, /\/api\/artifacts\/failed-check\/content/);
});

test("task errors tell a person what happened without hiding the recovery step", () => {
  const {context} = cockpit();
  assert.match(context.taskFriendlyError("invalid proposal JSON"), /โมเดลส่งข้อเสนอในรูปแบบที่ระบบอ่านไม่ได้/);
  assert.match(context.taskFriendlyError("completion blocked by User acceptance: User acceptance is required"), /รอคุณตรวจและรับรอง/);
  assert.match(context.taskFriendlyError("mandatory check node test.js failed"), /วิธีตรวจ node test.js ไม่ผ่าน/);
  assert.equal(context.taskFriendlyError("unknown fixture error"), "unknown fixture error");
});

test("chat failures explain the observed cause instead of claiming every error exhausted model steps", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("function turnFailureGuidance(");
  const end = source.indexOf("// An elicitation", start);
  assert.ok(start > 0 && end > start);
  const context = vm.createContext({escapeHTML:runtime.escapeHTML, pill:text => text, state:{sessionDetail:null}});
  vm.runInContext(source.slice(start, end), context);
  const failed = content => context.renderTimelineEvent({event_kind:"turn_failed", content});
  assert.match(failed("inference resource is quarantined: cancelled_runtime_idle_unconfirmed"), /โมเดลในเครื่องถูกพัก/);
  assert.doesNotMatch(failed("inference resource is quarantined"), /เกินจำนวนขั้นของโมเดล/);
  assert.match(failed("read provider stream: context canceled"), /คำตอบถูกขัดจังหวะก่อนจบ/);
  assert.match(failed("too many model steps"), /ครบจำนวนขั้นของงานนี้/);
  assert.match(failed("provider unavailable"), /ตรวจสถานะโมเดล/);
});

test("task failures remain visible after refresh and clear only on the next action", async () => {
  const {context} = cockpit();
  context.refreshDurableTask = async () => {};
  await context.performTaskAction("task-1", {isConnected:false}, "Creating plan", async () => { throw new Error("Model context is too small"); });
  assert.equal(vm.runInContext('taskErrors.get("task-1")', context), "Model context is too small");
  assert.equal(vm.runInContext('taskActions.has("task-1")', context), false);
  await context.performTaskAction("task-1", {isConnected:false}, "Creating plan", async () => {});
  assert.equal(vm.runInContext('taskErrors.has("task-1")', context), false);
});

test("uncertain Windows action shows fresh observation and reconciles once with an exact receipt", async () => {
  const task = baseTask();
  const action = {id:"native-1", handle:101, process_id:202, action:"invoke", state:"uncertain",
    target:{name:"Apply"}, created_at:"2026-09-26T10:00:00Z", subject_hash:"subject"};
  const observation = {window:{handle:101, process_id:202}, observation_hash:"fresh-hash", screenshot_artifact_id:"image-1"};
  const calls = [];
  const {context} = cockpit({state:{durableTasks:[task], selectedDurableTask:task.id,
    taskExecutions:{[task.id]:{desktopActions:[action]}}, desktopObservation:observation},
    api:async (path, options = {}) => { calls.push({path, body:JSON.parse(options.body)}); return {...action, reconciliation:{resolution:"observed_applied"}}; },
    askAction:async () => "The fixture button changed the window title",
  });
  context.refreshDurableTask = async () => {};
  const card = context.taskDesktopActionsHTML(task, context.state.taskExecutions[task.id]);
  assert.match(card, /ตรวจว่าเกิดผลแล้ว/);
  assert.match(card, /image-1/);
  await context.reconcileTaskDesktopAction(task.id, action.id, "observed_applied", {isConnected:false});
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/desktop/actions/native-1/reconcile");
  assert.equal(calls[0].body.observation_hash, "fresh-hash");
  assert.equal(calls[0].body.resolution, "observed_applied");
  assert.equal(context.state.desktopObservation, null);
});

test("uncertain browser action offers same-project tab recovery and exact screenshot evidence", () => {
  const task = {...baseTask(),project_id:"project-1"};
  const action = {id:"browser-1",tab_id:"old-tab",action:"click",state:"uncertain",target:{text:"Apply"},
    screenshot_before_id:"before-1",created_at:"2026-09-26T10:00:00Z"};
  const {context} = cockpit({state:{durableTasks:[task],selectedDurableTask:task.id,
    taskExecutions:{[task.id]:{browserActions:[action]}},browserTabs:[
      {id:"old-tab",state:"interrupted",project_id:"project-1",url:"https://example.com/page"},
      {id:"new-tab",state:"ready",project_id:"project-1",url:"https://example.com/page",observation_hash:"fresh",screenshot_artifact_id:"after-1"},
      {id:"other",state:"ready",project_id:"project-2",url:"https://example.com/page"},
    ]}});
  const html = context.taskBrowserActionsHTML(task, context.state.taskExecutions[task.id]);
  assert.match(html, /data-browser-verify-tab="browser-1"/);
  assert.match(html, /value="new-tab"/);
  assert.doesNotMatch(html, /value="other"/);
  assert.match(html, /\/api\/artifacts\/before-1\/content/);
  assert.match(html, /\/api\/artifacts\/after-1\/content/);
  assert.match(html, /ตรวจว่าเกิดผลแล้ว/);
});

test("TikTok export shows only current creator options with no default and explicit posting choices", () => {
  const {context} = cockpit();
  const direct = context.taskTikTokExportFieldsHTML({platform:"tiktok"}, {id:"tt-1", display_name:"@creator", capability:"video.publish_creator_info"},
    {privacy_level_options:["SELF_ONLY"], max_video_post_duration_sec:120, comment_disabled:true});
  assert.match(direct, /<option value="">เลือกการมองเห็น/);
  assert.match(direct, /<option value="SELF_ONLY">/);
  assert.doesNotMatch(direct, /PUBLIC_TO_EVERYONE/);
  assert.match(direct, /name="allow_comment" disabled/);
  assert.match(direct, /name="commercial_disclosure"/);
  assert.match(direct, /name="music_consent" required/);
  const draft = context.taskTikTokExportFieldsHTML({platform:"tiktok"}, {capability:"video.upload_identity"}, null);
  assert.doesNotMatch(draft, /name="privacy_level"/);
});

test("important article claim asks for a distinct second source and excerpt", () => {
  const {context} = cockpit();
  const html = context.taskClaimFormHTML([{id:"source-a",title:"Study A"},{id:"source-b",title:"Study B"}],
    {source_id:"source-a",consequential:true,corroborating_source_id:"source-b",corroborating_excerpt:"Second study evidence"},"task-1");
  assert.match(html, /name="consequential" checked/);
  assert.match(html, /name="corroborating_source_id" required/);
  assert.match(html, /Second study evidence/);
  assert.match(html, /เปิดต้นทางของทั้งสองบทความ/);
});

test("task completion shows current criterion and check progress with old evidence marked stale", () => {
  const {context} = cockpit();
  const html = context.taskCompletionHTML(baseTask(), {completion:{task_revision:2,can_complete:false,gates:[
    {id:"criterion:AC-1",label:"Page works",status:"pass",evidence_refs:["artifact:proof-1"]},
    {id:"criterion:AC-2",label:"Mobile works",status:"stale",reason:"Only a previous revision has validation; check the current goal and plan again"},
    {id:"check:step-1:build",label:"build",status:"not_run"},
  ]}});
  assert.match(html, /เกณฑ์สำเร็จ 1\/2/);
  assert.match(html, /วิธีตรวจ 0\/1/);
  assert.match(html, /หลักฐานเก่า/);
  assert.match(html, /ต้องตรวจ Goal และแผนฉบับปัจจุบันอีกครั้ง/);
  assert.match(html, /\/api\/artifacts\/proof-1\/content/);
});

test("task activity keeps old decisions visible without treating them as current", () => {
  const {context} = cockpit();
  const html = context.taskActivityHTML({activity:[
    {id:"new",kind:"validation",label:"AC-1",status:"pass",current:true,created_at:"2026-09-26T10:00:00Z",evidence_refs:["artifact:current"]},
    {id:"old",kind:"validation",label:"AC-1",status:"pass",current:false,created_at:"2026-09-25T10:00:00Z",evidence_refs:["artifact:old"]},
    {id:"failed",kind:"failure",label:"test",status:"confirmed",current:true,created_at:"2026-09-26T11:00:00Z",evidence_refs:["rollback_artifact:rollback-1"]},
  ]});
  assert.match(html, /ฉบับปัจจุบัน/);
  assert.match(html, /ฉบับก่อน/);
  assert.match(html, /\/api\/artifacts\/current\/content/);
  assert.match(html, /\/api\/artifacts\/old\/content/);
  assert.match(html, /ผลคืนไฟล์/);
  assert.match(html, /\/api\/artifacts\/rollback-1\/content/);
});

test("answering a Task unknown records an exact clarification and starts a new Goal revision", async () => {
  const task = {...baseTask(), requirement:{unknowns:["Which Page?", "Which audience?"],
    constraints:["review before posting"], criteria:[{id:"AC-1",description:"draft reviewed"}],
    contract:{require_plan_review:true,require_user_acceptance:true,clarifications:[]}}};
  const calls = [];
  const {context} = cockpit({state:{durableTasks:[task],selectedDurableTask:task.id,taskExecutions:{}},
    api:async (path, options) => { calls.push({path,body:JSON.parse(options.body)}); return {...task,revision:task.revision+1}; },
    askAction:async () => true});
  context.performTaskAction = async (_id,_body,_label,action) => action();
  await context.clarifyDurableTask({preventDefault(){},currentTarget:{question:"Which Page?",answer:"Company Page"}}, {});
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/tasks/task-1/requirements");
  assert.equal(calls[0].body.expected_task_revision, task.revision);
  assert.deepEqual(calls[0].body.unknowns, ["Which audience?"]);
  assert.equal(calls[0].body.contract.clarifications[0].question, "Which Page?");
  assert.equal(calls[0].body.contract.clarifications[0].answer, "Company Page");
  assert.equal(calls[0].body.contract.require_user_acceptance, true);
});

test("clarification draft survives reload in the same tab and expires with its Goal revision", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("const taskClarificationDrafts = new Map();");
  const end = source.indexOf("const taskPlanReviewFeedbacks = new Map();", start);
  const values = new Map();
  const sessionStorage = {getItem:key => values.get(key) || null,
    setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key)};
  const code = `${source.slice(start,end)};({taskClarificationDraft,persistTaskClarificationDraft,clearTaskClarificationDraft})`;
  const first = vm.runInNewContext(code, {Map,JSON,sessionStorage});
  const task = {id:"task-1",active_requirement_revision:3,requirement:{unknowns:["Which Page?"]}};
  first.persistTaskClarificationDraft(task, {question:"Which Page?",answer:"Company Page"});
  const afterReload = vm.runInNewContext(code, {Map,JSON,sessionStorage});
  assert.equal(afterReload.taskClarificationDraft(task).answer, "Company Page");
  assert.equal(afterReload.taskClarificationDraft({...task,active_requirement_revision:4}).answer, "");
  afterReload.clearTaskClarificationDraft(task.id);
  assert.equal(vm.runInNewContext(code, {Map,JSON,sessionStorage}).taskClarificationDraft(task).answer, "");
});

test("Task shortcut points to unresolved Windows and remote effects before completion", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const reviewStart = source.indexOf("function taskPlanAwaitsReview(");
  const reviewEnd = source.indexOf("function taskCanStartStep(", reviewStart);
  const start = source.indexOf("function taskJumpAction(");
  const end = source.indexOf("function taskPlanDiffHTML(", start);
  const jump = vm.runInNewContext(`${source.slice(reviewStart,reviewEnd)}${source.slice(start,end)};taskJumpAction`);
  const task = {state:"verifying",active_plan_revision:1,plan:{review_status:"approved"}};
  assert.equal(jump(task, {desktopActions:[{state:"uncertain"}],completion:{can_complete:true}}).target, "taskDesktopActionsHeading");
  assert.equal(jump(task, {socialPosts:[{state:"uncertain"}],completion:{can_complete:true}}).label, "ตรวจผลโพสต์");
  assert.equal(jump(task, {comfyJobs:[{state:"uncertain"}],completion:{can_complete:true}}).label, "ตรวจคิว Comfy");
  assert.equal(jump(task, {completion:{can_complete:true}}).target, "taskComplete");
  assert.notEqual(jump({...task,plan:{review_status:"pending"}}, {completion:{can_complete:true}}).target, "taskPlanReview");
  assert.equal(jump({...task,state:"waiting_for_input",requirement:{contract:{require_plan_review:true}},plan:{review_status:"pending"}}, {}).target, "taskPlanReview");
});

test("context meter distinguishes unknown usage from provider reported input and reserves", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("function contextSelectionHTML(");
  const end = source.indexOf("const CONFIG_PAGE_INTROS", start);
  assert.ok(start > 0 && end > start);
  const meter = vm.runInNewContext(`${source.slice(start,end)};sessionContextMeterHTML`, {Number,Math,escapeHTML:runtime.escapeHTML});
  const profile = {total:32768,output_reserve:4096,uncertainty_reserve:2048};
  assert.match(meter({id:"session-1"}, profile, []), /การใช้จริงยังไม่ทราบ/);
  assert.doesNotMatch(meter({id:"session-1"}, profile, []), /ใช้จริงล่าสุด 0/);
  const snapshot = {report:{predicted_prompt:11800,selected_ids:["goal:<unsafe>","project-brain:current"],
    dropped_ids:["old-tool","project-brain:omitted"],spilled:[]}};
  const measured = meter({id:"session-1"}, profile, [{metadata:{usage:{prompt_tokens:12000}}}], snapshot);
  assert.match(measured, /ใช้จริงล่าสุด 12,000/);
  assert.match(measured, /ประมาณก่อนส่งครั้งล่าสุด 11,800/);
  assert.match(measured, /งบ input 26,624/);
  assert.match(measured, /สำรองคำตอบ 4,096/);
  assert.match(measured, /goal:&lt;unsafe&gt;/);
  assert.match(measured, /old-tool/);
  assert.match(measured, /Project Brain ในคำขอนี้/);
  assert.match(measured, /อยู่ในบริบทที่ส่ง 1 · เสนอแต่ไม่ส่ง 1/);
  assert.doesNotMatch(measured, /goal:<unsafe>/);
});

test("Skill picker labels actual last-request inclusion separately from suggestions", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("function skillPresenceBadge(");
  const end = source.indexOf("function renderCapabilityPicker()", start);
  const badge = vm.runInNewContext(`${source.slice(start,end)};skillPresenceBadge`, {pill:(label) => label, Set});
  const skill = {skill_id:"skill-1",version_id:"version-1",canonical_name:"review",pinned:false};
  assert.equal(badge(skill, new Set(["review"]), new Set()), "suggested");
  assert.equal(badge(skill, new Set(["review"]), new Set(["skill:skill-1:version-1"])), "in last request");
  assert.equal(badge({...skill,pinned:true}, new Set(), new Set()), "pinned");
  const subtitle = vm.runInNewContext(`${source.slice(start,end)};skillSelectionSubtitle`);
  assert.equal(subtitle({...skill,summary:"Reviews changes"}, [{skill_id:"skill-1",version_id:"version-1",selection_reason:"name matches the goal"}]),
    "Reviews changes · Suggested because: name matches the goal");
  assert.equal(subtitle({...skill,summary:"Reviews changes"}, []), "Reviews changes");
});

test("video evidence separates timed speech from sampled frames and escapes recognized text", () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("function videoFrameEvidenceHTML(");
  const end = source.indexOf("function renderArtifacts()", start);
  assert.ok(start > 0 && end > start);
  const body = new Map([["transcript-1", {language:"en", segments:[{start_ms:1200,end_ms:2400,text:"<unsafe> speech"}]}]]);
  const context = vm.createContext({state:{artifacts:[]},videoTranscriptBodies:body,escapeHTML:runtime.escapeHTML,Number,Math});
  vm.runInContext(source.slice(start,end), context);
  const html = context.videoFrameEvidenceHTML({id:"video-1"}, [{processor_kind:"audio_transcribe",state:"completed",result_artifact_ids:["transcript-1"]}]);
  assert.match(html, /1\.2 วินาที/);
  assert.match(html, /&lt;unsafe&gt; speech/);
  assert.match(html, /data-video-seek="video-1"/);
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<unsafe>/);
});

test("browser click prepares an exact one-use receipt before dispatch", async () => {
  const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
  const start = source.indexOf("async function openWorkbenchBrowser(");
  const end = source.indexOf("// Files, terminal and browser", start);
  assert.ok(start > 0 && end > start);
  const tab = {id:"tab-1",project_id:"project-1",url:"https://example.com",updated_at:"now",observation_hash:"view-1",elements:[{ref:2,text:"Apply"}]};
  const state = {browserTabs:[tab],selectedBrowserTab:tab.id,durableTasks:[{id:"task-1",project_id:"project-1"}],selectedDurableTask:"task-1"};
  const calls = [];
  const context = vm.createContext({state,URL,FormData:class {},encodeURIComponent,currentActor:()=>"owner",toast:()=>{},
    $:()=>null,askAction:async ()=>true,refreshWorkbenchSurface:()=>{},loadTaskExecution:async ()=>{},
    api:async (path, options) => { calls.push({path,body:JSON.parse(options.body)}); return path.endsWith("/prepare") ?
      {id:"receipt-1",subject_hash:"exact-1",target:{text:"Apply"},screenshot_before_id:"image-1"} : {...tab,updated_at:"after"}; },
  });
  vm.runInContext(source.slice(start,end), context);
  await context.browserWorkbenchAction("click",2);
  assert.equal(calls[0].path,"/api/browser/tabs/tab-1/actions/prepare");
  assert.equal(calls[0].body.task_id,"task-1");
  assert.equal(calls[1].body.receipt_id,"receipt-1");
  assert.equal(calls[1].body.subject_hash,"exact-1");
});

test("local task excludes remote, disabled and credential-missing providers; second-pass reviewer uses a distinct profile", () => {
  const providers = [model,
    {...model, id:"clone"},
    {...model, id:"reviewer", model:"review-model"},
    {...model, id:"remote", base_url:"https://api.example.com/v1"},
    {...model, id:"missing", credential_ready:false},
    {...model, id:"disabled", enabled:false},
  ];
  const {context} = cockpit({state:{providers}});
  assert.deepEqual(Array.from(context.taskEligibleProviders(baseTask()), item => item.id), ["local","clone","reviewer"]);
  assert.deepEqual(Array.from(context.taskEligibleProviders(baseTask(), "local"), item => item.id), ["clone","reviewer"]);
  assert.deepEqual(Array.from(context.taskEligibleProviders({...baseTask(), egress_policy:"remote_allowed"}, "local"), item => item.id), ["clone","reviewer","remote"]);
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
  assert.match(context.taskExecutionHTML(task, execution), /เริ่มขั้นถัดไป/);
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
  assert.match(context.taskExecutionHTML(task, {run, effects:[]}), /กู้งานที่บันทึกไว้/);
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
  assert.match(context.taskExecutionHTML(task, execution), /สร้างข้อเสนอให้ตรวจ/);
  assert.doesNotMatch(context.taskExecutionHTML(task, execution), /run's lease expired/);
});

test("failed post-test review offers a fresh attempt on the same approved plan", () => {
  const task = {...baseTask(), state:"paused", active_plan_revision:1};
  const execution = {run:{...liveRun(), state:"failed"}, attempt:{state:"failed"}, proposal:{state:"post_review_rejected"}, effects:[{state:"observed"}]};
  const {context} = cockpit({state:{providers:[model]}});
  assert.equal(context.taskCanStartStep(task, execution), true);
  assert.match(context.taskExecutionHTML(task, execution), /แก้แล้วลองขั้นนี้ใหม่/);
  assert.match(context.taskExecutionHTML(task, execution), /taskProposalForm/);
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
  const {context} = cockpit({state:{providers:[model], durableTasks:[task], taskExecutions:{}}, api:async path => {
    if (path.endsWith("/execution")) return {task, proposal, effects:[]};
    if (path.includes("/completion?")) return {task_revision:task.revision, gates:[], can_complete:false};
    if (path.includes("/next-action?")) return {decision:{action_id:"inspect_proposal"}};
    return {summary:"Change addition", changes:[{path:"sum.go", content:"<script>alert(1)</script>"}]};
  }});
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
  assert.match(html, /แนะนำงานอ่านได้/);
  assert.match(html, /ถูก 6\/6 กรณี/);
  assert.match(html, /inspect_file/);
  assert.match(html, /เหลือลองได้/);
  assert.match(html, /Use &lt;safe&gt; retry/);
  assert.match(html, /ไม่สามารถเรียกเครื่องมือหรือแก้ไฟล์/);
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
