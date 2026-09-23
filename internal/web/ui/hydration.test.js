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
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return {promise, resolve, reject};
}
function harness(api) {
  const project = {id:"a", root_path:"C:/project-a"};
  const renders = {chat:0, all:0, panes:0, config:0}, messages = [];
  const context = vm.createContext({
    state:{currentProject:project, projects:[project], selectedProject:"a", projectPath:"", projectFiles:[], workspaceFiles:[],
      teams:[], terminals:[], browserTabs:[], sessions:[], qualifications:[], providers:[], skills:[],
      activeTab:"chat", view:"chat", startDraft:"Keep this unsent goal", panes:["files","editor"]},
    api, asList, Map, Set, console, CONFIG_PAGE_IDS:["maintenance","projects","office","insights"],
    $:()=>({classList:{contains:()=>true}}),
    renderAll:()=>{renders.all++;}, renderChat:()=>{renders.chat++;},
    renderConfigPage:()=>{renders.config++;}, renderPanes:()=>{renders.panes++;},
    captureCodeDraft:()=>{}, captureWorkspaceDrafts:()=>{}, restoreWorkspaceDrafts:()=>{},
    showShell:()=>{}, applyLayoutForView:()=>{}, selectSession:async()=>{},
    toast:message=>messages.push(message),
  });
  vm.runInContext("let navigationGeneration = 0;", context);
  vm.runInContext(section("const SURFACE_DATA =", "function renderAll()"), context);
  vm.runInContext(section("let projectOpenGeneration =", "async function createProjectFromPicker("), context);
  return {context, renders, messages};
}

test("late file responses cannot overwrite another project's files or ready cache", async () => {
  const old = deferred(); let requests = 0;
  const {context} = harness(async path => {
    requests++;
    return path.includes("/a/") ? old.promise : [{name:"b.go"}];
  });
  const first = context.hydrateSurface("files");
  context.state.currentProject = {id:"b", root_path:"C:/project-b"};
  context.invalidateSurfaces();
  assert.equal(await context.hydrateSurface("files"), true);
  old.resolve([{name:"a.go"}]);
  assert.equal(await first, false);
  assert.equal(context.state.workspaceFiles[0].name, "b.go");
  await context.hydrateSurface("files");
  assert.equal(requests, 2, "the newer completed cache survives the older response");
});

test("a superseded failure does not evict a newer hydration request", async () => {
  const old = deferred(); let requests = 0;
  const {context} = harness(async () => ++requests === 1 ? old.promise : [{id:"new-task"}]);
  const first = context.hydrateSurface("tasks");
  assert.equal(await context.hydrateSurface("tasks", true), true);
  old.reject(new Error("old request failed"));
  assert.equal(await first, false);
  await context.hydrateSurface("tasks");
  assert.equal(requests, 2);
  assert.equal(context.state.durableTasks[0].id, "new-task");
});

test("surface data commits together and failed hydration can be retried explicitly", async () => {
  const runs = deferred(); let retry = false;
  const {context} = harness(async path => path === "/api/teams" ? [{id:"new-team"}] : retry ? [] : runs.promise);
  context.state.teams = [{id:"existing-team"}];
  const pending = context.hydrateSurface("team");
  await Promise.resolve();
  assert.equal(context.state.teams[0].id, "existing-team");
  runs.reject(new Error("runs unavailable"));
  await assert.rejects(pending, /runs unavailable/);
  assert.equal(context.state.teams[0].id, "existing-team");
  retry = true;
  await context.hydrateSurface("team");
  assert.equal(context.state.teams[0].id, "new-team");
});

test("Office, Insights and Projects fetch the data displayed by their own pages", async () => {
  const calls = [];
  const {context} = harness(async path => { calls.push(path); return []; });
  for (const surface of ["office", "insights", "projects"]) await context.hydrateSurface(surface);
  for (const path of ["/api/jobs", "/api/curator/findings", "/api/projects/a/files?path=", "/api/artifacts", "/api/memories", "/api/tasks?limit=100"]) {
    assert.ok(calls.includes(path), `missing ${path}`);
  }
});

test("optional startup failure does not blank Chat and null collections are normalized", async () => {
  const {context, renders, messages} = harness(async path => {
    if (path === "/api/bootstrap") return {providers:null, sessions:null, skills:null};
    if (path === "/api/qualifications") throw new Error("qualification unavailable");
    if (path.startsWith("/api/capabilities")) return null;
    return [];
  });
  assert.equal(await context.load(), true);
  assert.equal(renders.all, 1);
  assert.equal(context.state.providers.length, 0);
  assert.equal(context.state.sessions.length, 0);
  assert.equal(context.state.runtimeCapabilities.interactive_terminal, undefined);
  assert.deepEqual(messages, ["qualification unavailable"]);
});

test("bootstrap failure leaves an actionable error and preserves the unsent goal", async () => {
  const {context, renders} = harness(async path => {
    if (path === "/api/bootstrap") throw new Error("Workspace offline; refresh to retry");
    return [];
  });
  assert.equal(await context.load(), false);
  assert.match(context.state.sessionError, /Workspace offline/);
  assert.equal(context.state.startDraft, "Keep this unsent goal");
  assert.equal(renders.chat, 1);
});

test("a settings refresh does not rebuild editor panes behind the overlay", async () => {
  const {context, renders} = harness(async path => path === "/api/bootstrap" ? {} : []);
  context.state.view = "code";
  context.state.activeTab = "maintenance";
  await context.load();
  assert.equal(renders.config, 1);
  assert.equal(renders.panes, 0);
});

test("late bootstrap for a previous project cannot change the current workspace", async () => {
  const old = deferred();
  const {context, renders} = harness(async path => path === "/api/bootstrap" ? old.promise : []);
  const loading = context.load();
  context.state.currentProject = {id:"b"};
  old.resolve({providers:[{id:"old-provider"}]});
  assert.equal(await loading, false);
  assert.equal(context.state.providers.length, 0);
  assert.equal(renders.all, 0);
});

test("opening two projects rapidly leaves the most recent selection active", async () => {
  const first = deferred();
  const {context} = harness(async path => {
    if (path === "/api/projects/a/open") return first.promise;
    if (path === "/api/projects/b/open") return {id:"b", root_path:"C:/project-b"};
    if (path === "/api/bootstrap") return {};
    return [];
  });
  const openingA = context.openProject("a");
  await context.openProject("b");
  first.resolve({id:"a", root_path:"C:/project-a"});
  await openingA;
  assert.equal(context.state.currentProject.id, "b");
  assert.equal(context.state.selectedProject, "b");
});
