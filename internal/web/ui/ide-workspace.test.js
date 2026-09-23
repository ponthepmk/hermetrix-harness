const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
// Exercise the shipped controller. Test-only access to its private maps lets us
// model delayed HTTP responses without exporting implementation details to users.
const source = fs.readFileSync(`${__dirname}/ide-workspace.js`, "utf8").replace(
  /window\.HermetrixWorkspace\s*=\s*\{/,
  "window.HermetrixWorkspace={__test:{runs,debuggers,capabilities,pollDebug,pollOutput,selectFrame,breakpoint,goTo,command},"
);
function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return {promise, resolve};
}
function harness(api = async () => ({})) {
  const body = {innerHTML:"", isConnected:true, querySelector:()=>null, querySelectorAll:()=>[]};
  const shell = {hidden:false}, overlay = {hidden:true};
  const calls = [], navigations = [];
  const state = {currentProject:{id:"project-a", name:"Project A"},
    projectFile:{projectID:"project-a", path:"main.go", content:"package main", originalContent:"package main"},
    view:"code", paneLayout:"ide", panes:[]};
  const context = vm.createContext({window:{addEventListener:()=>{}}, state,
    document:{hidden:false, querySelector:selector=>selector === "#appShell" ? shell : selector === "#configOverlay" ? overlay : selector.startsWith(".pane") ? body : null,
      querySelectorAll:()=>[], addEventListener:()=>{}},
    api:async (path, options)=>{calls.push(path); return api(path, options);},
    captureCodeDraft:()=>{}, saveWorkbenchFile:async()=>{},
    activeCodeEditor:{setBreakpoints:()=>{}, setExecutionLine:()=>{}, goToLine:line=>navigations.push(line)},
    openWorkbenchFile:async()=>{}, workspacePaneVisible:()=>!shell.hidden && overlay.hidden,
    codeDraftKey:(id,path)=>JSON.stringify([id,path]), escapeHTML:String, stripANSI:String,
    toast:()=>{}, currentActor:()=>"user", renderPanes:()=>{}, saveLayout:()=>{}, collapseZone:()=>{},
    setTimeout:()=>1, clearTimeout:()=>{}, openContentPane:()=>{}, switchView:()=>{},
  });
  vm.runInContext(source, context);
  return {context, body, state, calls, navigations, shell, overlay,
    controller:context.window.HermetrixWorkspace, internals:context.window.HermetrixWorkspace.__test};
}

test("Run and Test share a project execution gate until the launch request completes", async () => {
  const pending = [], launches = [];
  const h = harness(async (path, options) => {
    if (path.endsWith("/ide")) { const result = deferred(); pending.push(result); return result.promise; }
    if (path.endsWith("/commands")) { launches.push(JSON.parse(options.body)); return {id:`job-${launches.length}`, state:"running"}; }
    return {};
  });
  const run = h.controller.action("run"), testRun = h.controller.action("test");
  for (const result of pending) result.resolve({tools:{go:true}});
  await Promise.all([run, testRun]);
  assert.equal(launches.length, 1, "a second launch would overwrite the only tracked job");
  assert.equal(h.internals.runs.get("project-a").job.id, "job-1");
});

test("Python commands use the executable actually available on Windows or Unix", () => {
  const h = harness();
  const doc = {path:"tools/check.py", content:"print('ok')"};
  assert.equal(h.internals.command("run", doc, {python:true}).executable, "python");
  assert.equal(h.internals.command("run", doc, {python:true, python3:true}).executable, "python3");
  assert.deepEqual(Array.from(h.internals.command("test", doc, {python:true}).arguments), ["-m", "unittest", "discover"]);
});

test("a late poll from the previous debugger cannot replace a restarted session", async () => {
  const response = deferred();
  const h = harness(() => response.promise);
  h.internals.capabilities.set("project-a", {runtimes:[]});
  const record = {session:{id:"debug-old", state:"running"}};
  h.internals.debuggers.set("project-a", record);
  const pending = h.internals.pollDebug("project-a");
  // Stop and a new Start have completed while the old GET was in flight.
  record.session = {id:"debug-new", state:"running"};
  response.resolve({id:"debug-old", state:"running"});
  await pending;
  assert.equal(record.session.id, "debug-new");
});

test("a poll started before a completed step cannot rewind the same debugger session", async () => {
  const response = deferred();
  const h = harness(() => response.promise);
  h.internals.capabilities.set("project-a", {runtimes:[]});
  const record = {session:{id:"debug-a", state:"paused", updated_at:"2026-09-20T00:00:01Z", stack:[]}};
  h.internals.debuggers.set("project-a", record);
  const pending = h.internals.pollDebug("project-a");
  record.session = {id:"debug-a", state:"paused", updated_at:"2026-09-20T00:00:02Z", stack:[]};
  response.resolve({id:"debug-a", state:"paused", updated_at:"2026-09-20T00:00:01Z", stack:[]});
  await pending;
  assert.equal(record.session.updated_at, "2026-09-20T00:00:02Z");
});

test("an old breakpoint update cannot replace a new debugger session", async () => {
  const response = deferred();
  const h = harness(() => response.promise);
  h.internals.capabilities.set("project-a", {runtimes:[]});
  const record = {session:{id:"debug-old", state:"running"}};
  h.internals.debuggers.set("project-a", record);
  const pending = h.internals.breakpoint(h.state.projectFile, 3, true);
  record.session = {id:"debug-new", state:"running"};
  response.resolve({id:"debug-old", state:"running", breakpoints:[]});
  await pending;
  assert.equal(record.session.id, "debug-new");
});

test("delayed locals from an earlier pause do not overwrite the current pause", async () => {
  const response = deferred();
  const h = harness(() => response.promise);
  h.internals.capabilities.set("project-a", {runtimes:[]});
  const frame = {id:"frame-0", path:"main.go", line:3};
  const record = {session:{id:"debug-a", state:"paused", updated_at:"2026-09-20T00:00:01Z", stack:[frame]}, variables:[{name:"value",value:"new"}]};
  h.internals.debuggers.set("project-a", record);
  const pending = h.internals.selectFrame("project-a", frame, false);
  record.session = {...record.session, updated_at:"2026-09-20T00:00:02Z", stack:[{...frame,line:4}]};
  record.frame = "frame-0"; // Debuggers may reuse frame IDs after stepping.
  response.resolve([{name:"value",value:"old"}]);
  await pending;
  assert.equal(record.variables[0].value, "new");
});

test("delayed source navigation does not move the cursor in another project's editor", async () => {
  const response = deferred();
  const h = harness();
  h.context.openWorkbenchFile = () => response.promise;
  const pending = h.internals.goTo("main.go", 17);
  h.state.currentProject = {id:"project-b"};
  h.state.projectFile = {projectID:"project-b",path:"other.go"};
  response.resolve();
  await pending;
  assert.deepEqual(h.navigations, []);
});

test("output and debugger polling pause behind settings and the project picker", async () => {
  const h = harness();
  h.internals.runs.set("project-a", {job:{id:"job-a",state:"running"}});
  h.internals.debuggers.set("project-a", {session:{id:"debug-a",state:"running"}});
  h.overlay.hidden = false;
  await h.internals.pollOutput("project-a");
  await h.internals.pollDebug("project-a");
  h.overlay.hidden = true;
  h.shell.hidden = true;
  await h.internals.pollOutput("project-a");
  await h.internals.pollDebug("project-a");
  assert.deepEqual(h.calls, []);
});

test("format response cannot replace text typed while formatting", async () => {
  const response = deferred(), updates = [];
  const h = harness(() => response.promise);
  let buffer = "package main\nconst value=1\n";
  h.context.activeCodeEditor = {documentKey:JSON.stringify(["project-a","main.go"]), getValue:()=>buffer, setValue:value=>updates.push(value)};
  const pending = h.controller.action("format");
  buffer = "package main\nconst value=2\n";
  response.resolve({content:"package main\n\nconst value = 1\n"});
  await pending;
  assert.deepEqual(updates, []);
  assert.match(buffer, /value=2/);
});

const appSource = fs.readFileSync(`${__dirname}/app.js`, "utf8");
function appSection(start, end) {
  const from = appSource.indexOf(start), to = appSource.indexOf(end, from);
  assert.ok(from >= 0 && to > from);
  return appSource.slice(from, to);
}
function saveHarness() {
  const response = deferred(), requests = [], dirty = [];
  const state = {currentProject:{id:"project-a"}, projectFile:{projectID:"project-a",path:"main.go",content:"edit-one",originalContent:"saved-zero",sha256:"sha-zero"}};
  let buffer = "edit-one";
  const drafts = new Map();
  const context = vm.createContext({state, codeDrafts:drafts, $:()=>null, ideFormatOnSaveEnabled:()=>false,
    codeDraftKey:(id,path)=>JSON.stringify([id,path]), activeCodeEditor:{documentKey:JSON.stringify(["project-a","main.go"]),changed:true,getValue:()=>buffer},
    api:async (path, options)=>{requests.push({path,body:JSON.parse(options.body)});return response.promise;},
    currentActor:()=>"user", toast:()=>{}, window:{HermetrixWorkspace:{dirty:()=>dirty.push(state.projectFile.projectID),feedback:()=>{}}},
  });
  vm.runInContext(appSection("function captureCodeDraft()", "function disposeWorkspaceWidgets()"), context);
  vm.runInContext(appSection("async function saveWorkbenchFile(", "function stripANSI("), context);
  return {context,state,drafts,requests,dirty,response,setBuffer:value=>{buffer=value;}};
}

test("unsaved editor drafts survive a browser reload within the local tab session", () => {
  const values = new Map();
  const sessionStorage = {getItem:key=>values.get(key) ?? null, setItem:(key,value)=>values.set(key,value), removeItem:key=>values.delete(key)};
  const context = vm.createContext({Map, JSON, sessionStorage, window:{addEventListener:()=>{}}, setTimeout:callback=>{callback(); return 1;}, clearTimeout:()=>{}});
  vm.runInContext(appSection("const codeDrafts = new Map();", "function projectCodeTabs("), context);
  const key = context.codeDraftKey("project-a", "main.go");
  const draft = {projectID:"project-a", path:"main.go", content:"edited", originalContent:"saved", sha256:"sha-saved"};
  context.persistCodeDraft(key, draft);
  assert.equal(context.persistedCodeDraft("project-a", "main.go").content, "edited");
  context.persistCodeDraft(key, {...draft, content:"saved"});
  assert.equal(context.persistedCodeDraft("project-a", "main.go"), null);
});

test("save completion updates the disk baseline without replacing newer typing", async () => {
  const h = saveHarness();
  const pending = h.context.saveWorkbenchFile();
  h.setBuffer("edit-two");
  h.response.resolve({document:{path:"main.go",content:"edit-one",sha256:"sha-one"},diff:"saved"});
  await pending;
  assert.equal(h.requests[0].body.content, "edit-one");
  assert.equal(h.state.projectFile.content, "edit-two");
  assert.equal(h.state.projectFile.originalContent, "edit-one");
  assert.equal(h.state.projectFile.sha256, "sha-one");
  assert.equal(h.context.activeCodeEditor.getValue(), "edit-two");
});

test("save completion for a previous project cannot replace the active project's buffer", async () => {
  const h = saveHarness();
  const pending = h.context.saveWorkbenchFile();
  h.state.currentProject = {id:"project-b"};
  h.state.projectFile = {projectID:"project-b",path:"other.go",content:"B-draft",originalContent:"B-saved"};
  h.context.activeCodeEditor = {documentKey:JSON.stringify(["project-b","other.go"]),changed:true,getValue:()=>"B-draft"};
  h.response.resolve({document:{path:"main.go",content:"edit-one",sha256:"sha-one"},diff:"saved"});
  await pending;
  assert.equal(h.state.projectFile.projectID, "project-b");
  assert.equal(h.state.projectFile.content, "B-draft");
  assert.equal(h.drafts.get(JSON.stringify(["project-a","main.go"])).sha256,"sha-one");
  assert.deepEqual(h.dirty, []);
});
