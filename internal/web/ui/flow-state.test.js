const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const {asList} = require("./runtime.js");
const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
function section(start, end) {
  const from = source.indexOf(start), to = source.indexOf(end, from);
  assert.ok(from >= 0 && to > from);
  return source.slice(from, to);
}
function deferred() {
  let resolve;
  const promise = new Promise(yes => { resolve = yes; });
  return {promise, resolve};
}
function harness(overrides = {}) {
  const projects = [{id:"a", root_path:"C:/a"}, {id:"b", root_path:"C:/b"}];
  const details = {
    a1:{session:{id:"a1", project_id:"a"}}, a2:{session:{id:"a2", project_id:"a"}},
    b1:{session:{id:"b1", project_id:"b"}},
  };
  let input = {value:"draft a1", selectionStart:3, focus(){}}, start = null;
  const calls = [], messages = [], renders = [], views = [];
  const state = {currentProject:projects[0], projects, selectedProject:"a", draftProjectID:"a", projectPath:"",
    sessionDetail:details.a1, selectedSession:"a1", draftMessage:input.value, composerAttachments:[], composerCaret:3,
    startDraft:"", taskDraftObjective:"", taskDraftDetails:{}, panes:[], view:"chat", activeTab:"chat",
    sessions:[], teams:[], terminals:[], browserTabs:[], qualifications:[], providers:[], skills:[], sending:false};
  const defaultAPI = async path => {
    if (path.endsWith("/open")) return projects.find(project => path.includes(`/projects/${project.id}/`));
    if (path === "/api/projects") return projects;
    if (path.startsWith("/api/sessions/")) return details[path.split("/").at(-1)];
    if (path === "/api/bootstrap") return {};
    return [];
  };
  const context = vm.createContext({
    state, asList, Map, Set, CONFIG_PAGE_IDS:[], console,
    $:selector=>selector === "#chatInput" ? input : selector === "#startPrompt" ? start : selector === "#zones" ? {classList:{contains:()=>true}} : null,
    document:{activeElement:null}, $$:()=>[],
    api:async (...args)=>{calls.push(args[0]); return overrides.api ? overrides.api(args[0], defaultAPI) : defaultAPI(...args);},
    fetch:overrides.fetch || (async()=>({ok:true})), consumeAgentStream:async()=>{},
    toast:message=>messages.push(message), captureCodeDraft:()=>{}, showShell:()=>{}, applyLayoutForView:()=>{},
    renderPanes:()=>{}, renderConfigPage:()=>{}, refreshPaneChat:()=>{}, pollElicitations:()=>{}, setTimeout:()=>{},
    profileAdmission:()=>({mode:"compatibility"}), profileLabel:()=>"Compact", currentActor:()=>"test-user",
    switchTab:tab=>{state.activeTab=tab;}, switchView:view=>{views.push(view);state.view=view;},
    openContentPane:id=>{if(!state.panes.includes(id))state.panes.push(id);},
  });
  context.renderChat = () => {
    context.captureComposer();
    if (state.sessionDetail) { input = {value:state.draftMessage, selectionStart:state.composerCaret, focus(){}}; start = null; }
    else { input = null; start = {value:state.startDraft}; }
    renders.push({session:state.sessionDetail?.session?.id, text:input?.value, attachments:state.composerAttachments.slice()});
  };
  context.renderAll = context.renderChat;
  vm.runInContext("let navigationGeneration = 0;", context);
  vm.runInContext(section("const SURFACE_DATA =", "function renderAll()"), context);
  vm.runInContext(section("let projectOpenGeneration =", "async function createProjectFromPicker("), context);
  vm.runInContext(section("function captureComposer()", "async function createAgentSession("), context);
  vm.runInContext(section("async function createAgentSession(", "// Deleting a session"), context);
  vm.runInContext(section("async function openTasks()", "function captureComposer()"), context);
  vm.runInContext(section("async function selectSession(", "async function decideToolApproval("), context);
  return {context, state, details, calls, messages, renders, views, input:()=>input};
}

test("each session restores its own unsent text and attachments", async () => {
  const {context, state, input} = harness();
  state.composerAttachments = [{local_id:"image-a", state:"ready", artifact_id:"artifact-a"}];
  await context.selectSession("a2");
  assert.equal(input().value, "");
  assert.equal(state.composerAttachments.length, 0);
  context.setComposerDraft("draft a2");
  await context.selectSession("a1");
  assert.equal(input().value, "draft a1");
  assert.equal(state.composerAttachments[0].artifact_id, "artifact-a");
  await context.selectSession("a2");
  assert.equal(input().value, "draft a2");
});

test("selecting another project's session changes Files and new-task project binding too", async () => {
  const {context, state, calls} = harness();
  await context.selectSession("b1");
  assert.equal(state.sessionDetail.session.id, "b1");
  assert.equal(state.currentProject.id, "b");
  assert.equal(state.selectedProject, "b");
  assert.equal(state.draftProjectID, "b");
  assert.ok(calls.includes("/api/projects/b/open"));
});

test("late session response cannot replace the more recent selection", async () => {
  const old = deferred();
  const {context, state, details} = harness({api:(path, fallback)=>path === "/api/sessions/a2" ? old.promise : fallback(path)});
  const loading = context.selectSession("a2");
  await context.selectSession("a1");
  old.resolve(details.a2);
  assert.equal(await loading, undefined);
  assert.equal(state.selectedSession, "a1");
  assert.equal(state.sessionDetail.session.id, "a1");
});

test("failed session selection keeps the visible session and its unsent draft", async () => {
  const {context, state, input} = harness({api:async(path, fallback)=>{
    if (path === "/api/sessions/a2") throw new Error("Session unavailable");
    return fallback(path);
  }});
  assert.equal(await context.selectSession("a2"), undefined);
  assert.equal(state.selectedSession, "a1");
  assert.equal(state.sessionDetail.session.id, "a1");
  assert.equal(input().value, "draft a1");
  assert.match(state.sessionError, /Session unavailable/);
});

test("project welcome and plan drafts survive navigation without leaking into the next project", async () => {
  const {context, state} = harness();
  state.startDraft = "Goal A";
  state.taskDraftObjective = "Plan A";
  state.taskDraftDetails = {criteria:"Check A"};
  await context.openProject("b");
  assert.equal(state.startDraft, "");
  assert.equal(state.taskDraftObjective, "");
  assert.equal(state.taskDraftDetails.criteria, undefined);
  state.startDraft = "Goal B";
  state.taskDraftObjective = "Plan B";
  state.taskDraftDetails = {criteria:"Check B"};
  await context.openProject("a");
  assert.equal(state.startDraft, "Goal A");
  assert.equal(state.taskDraftObjective, "Plan A");
  assert.equal(state.taskDraftDetails.criteria, "Check A");
});

test("New task starts empty while the previous session draft stays recoverable", async () => {
  const {context, state, input} = harness();
  state.composerAttachments = [{local_id:"image-a", state:"ready"}];
  assert.equal(context.beginNewTaskDraft(), true);
  context.renderChat();
  assert.equal(state.sessionDetail, null);
  assert.equal(state.startDraft, "");
  assert.equal(state.composerAttachments.length, 0);
  await context.selectSession("a1");
  assert.equal(input().value, "draft a1");
  assert.equal(state.composerAttachments[0].local_id, "image-a");
});

test("failed send restores the visible text and retains images across subsequent renders", async () => {
  const {context, state, input} = harness({fetch:async()=>({ok:false,json:async()=>({error:"Offline"})})});
  state.composerAttachments = [{local_id:"image-a", state:"ready", artifact_id:"artifact-a"}];
  await context.sendTurn({preventDefault(){}});
  assert.equal(input().value, "draft a1");
  context.renderChat();
  assert.equal(input().value, "draft a1");
  assert.equal(state.composerAttachments[0].artifact_id, "artifact-a");
});

test("successful send clears both attachment state and the rendered attachment chips", async () => {
  const {context, state, renders} = harness();
  state.composerAttachments = [{local_id:"image-a", state:"ready", artifact_id:"artifact-a"}];
  await context.sendTurn({preventDefault(){}});
  assert.equal(state.composerAttachments.length, 0);
  assert.equal(renders.at(-1).attachments.length, 0);
  assert.equal(state.draftMessage, "");
});

test("an active turn prevents cross-session and project navigation without starting requests", async () => {
  const {context, state, calls} = harness();
  state.sending = true;
  state.turnSessionID = "a1";
  await context.selectSession("b1");
  await context.openProject("b");
  assert.equal(calls.length, 0);
  assert.equal(state.currentProject.id, "a");
  assert.equal(state.sessionDetail.session.id, "a1");
});

test("a navigation started before a turn cannot commit while that other session is streaming", async () => {
  const pending = deferred();
  const {context, state, details} = harness({api:()=>pending.promise});
  const loading = context.selectSession("a2");
  state.sending = true;
  state.turnSessionID = "a1";
  pending.resolve(details.a2);
  await loading;
  assert.equal(state.sessionDetail.session.id, "a1");
});

test("an upload result remains attached to its original session draft", async () => {
  const {context, state} = harness();
  const pending = {local_id:"upload-a", state:"uploading"};
  state.composerAttachments = [pending];
  await context.selectSession("a2");
  pending.state = "ready";
  pending.artifact_id = "image-a";
  assert.equal(state.composerAttachments.length, 0);
  await context.selectSession("a1");
  assert.equal(state.composerAttachments[0].artifact_id, "image-a");
});

function configureCreation(state, projectID = "a") {
  state.draftProviderID = "local";
  state.draftProfileName = "compact";
  state.draftProjectID = projectID;
  state.providers = [{id:"local"}];
  state.profiles = [{name:"compact"}];
}

test("creation blocks user navigation until its internal session selection completes", async () => {
  const pending = deferred();
  const {context, state, calls} = harness({api:(path, fallback)=>path === "/api/sessions" ? pending.promise : fallback(path)});
  configureCreation(state);
  const creating = context.createAgentSession("New goal");
  assert.equal(state.sessionCreationPending, true);
  const requestCount = calls.length;
  await context.selectSession("b1");
  await context.openProject("b");
  assert.equal(calls.length, requestCount, "no navigation request starts during creation");
  pending.resolve({id:"a2"});
  assert.equal((await creating).id, "a2");
  assert.equal(state.selectedSession, "a2");
  assert.equal(state.sessionCreationPending, false);
  await context.selectSession("b1");
  assert.equal(state.currentProject.id, "b", "navigation resumes after creation");
});

test("internal selection can bind a newly created session to the explicitly chosen project", async () => {
  const {context, state} = harness({api:(path, fallback)=>path === "/api/sessions" ? {id:"b1"} : fallback(path)});
  configureCreation(state, "b");
  assert.equal((await context.createAgentSession("Goal B")).id, "b1");
  assert.equal(state.currentProject.id, "b");
  assert.equal(state.selectedSession, "b1");
  assert.equal(state.sessionCreationPending, false);
});

test("a pre-existing session lookup cannot commit after session creation starts", async () => {
  const selection = deferred(), creation = deferred();
  const {context, state, details} = harness({api:(path, fallback)=>{
    if (path === "/api/sessions/a2") return selection.promise;
    if (path === "/api/sessions") return creation.promise;
    return fallback(path);
  }});
  const selecting = context.selectSession("a2");
  configureCreation(state);
  const creating = context.createAgentSession("New goal");
  selection.resolve(details.a2);
  await selecting;
  assert.equal(state.selectedSession, "a1");
  creation.resolve({id:"a1"});
  await creating;
  assert.equal(state.sessionCreationPending, false);
});

test("failed creation releases navigation and preserves the goal", async () => {
  const {context, state} = harness({api:async(path, fallback)=>{
    if (path === "/api/sessions") throw new Error("Create failed");
    return fallback(path);
  }});
  configureCreation(state);
  state.startDraft = "Unsent goal";
  await context.createAgentSession("Unsent goal");
  assert.equal(state.sessionCreationPending, false);
  assert.equal(state.startDraft, "Unsent goal");
  await context.selectSession("a2");
  assert.equal(state.selectedSession, "a2");
});

test("a delayed Plans request cannot override a subsequent session navigation", async () => {
  const pending = deferred();
  const {context, views} = harness();
  context.hydrateSurface = () => pending.promise;
  const opening = context.openTasks();
  await context.selectSession("a2");
  pending.resolve(true);
  await opening;
  assert.equal(views.length, 0);
});

test("background transcript refresh does not cancel the user's pending Plans navigation", async () => {
  const pending = deferred();
  const {context, views} = harness();
  context.hydrateSurface = () => pending.promise;
  const opening = context.openTasks();
  await context.selectSession("a1", {refresh:true});
  pending.resolve(true);
  await opening;
  assert.deepEqual(views, ["code"]);
});
