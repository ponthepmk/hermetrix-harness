const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync(`${__dirname}/app.js`, "utf8");
const start = source.indexOf("function renderRailNavigation(");
const end = source.indexOf("// renderChatRail/Main/Side", start);
assert.ok(start >= 0 && end > start);

function harness() {
  const projects = [{id:"a", name:"Project A"}, {id:"b", name:"Project B"}];
  const state = {view:"code", projects, currentProject:projects[0], draftProjectID:"a",
    providers:[{id:"local", name:"Local", model:"model", enabled:true, credential_ready:true}],
    profiles:[{name:"compact"}], sessions:[], elicitations:[], railProjectOpen:{}, railProjectsOpen:true,
    draftQualificationReason:"", sessionDetail:null, selectedSession:null, sending:false};
  const nodes = new Map();
  for (const id of ["sessionDock", "sessionSetupBody", "railNewSession", "sessionSetup", "chatProviderSelect", "chatQualificationReason", "openProvidersFromDock"]) {
    nodes.set(`#${id}`, {innerHTML:"", disabled:false, listeners:{}, focus(){},
      addEventListener(name, callback) { this.listeners[name] = callback; }});
  }
  const actions = [];
  const context = vm.createContext({state, $:selector=>nodes.get(selector), $$:()=>[],
    captureComposer:()=>{}, HermetrixRuntime:{preferredProvider:providers=>providers[0]},
    availableProfiles:()=>state.profiles, bestProfileFor:(_provider, profiles)=>profiles[0],
    profileAdmission:()=>({admitted:true, mode:"compatibility", budget:512}),
    suggestedOverrideReason:()=>"Reviewed reason", profileLabel:profile=>profile.name,
    escapeHTML:value=>String(value ?? ""), uiIcon:()=>"", openSessionSetup:()=>actions.push("setup"),
    closeSessionSetup:()=>actions.push("close"), switchTab:tab=>actions.push(tab), openTasks:()=>actions.push("plans"),
  });
  vm.runInContext(source.slice(start, end), context);
  return {state, nodes, context, actions, projects};
}

test("Workspace project switch updates global rail and model setup without a mounted Chat view", () => {
  const {state, nodes, context, projects, actions} = harness();
  assert.equal(nodes.has("#view-chat"), false);
  context.renderChat();
  assert.match(nodes.get("#sessionDock").innerHTML, /rail-project active[\s\S]*?data-rail-project="a"/);
  assert.match(nodes.get("#sessionSetupBody").innerHTML, /Project A/);
  state.currentProject = projects[1];
  state.draftProjectID = "b";
  context.renderChat();
  const rail = nodes.get("#sessionDock").innerHTML;
  assert.match(rail, /rail-project active[\s\S]*?data-rail-project="b"/);
  assert.doesNotMatch(rail, /rail-project active[\s\S]*?data-rail-project="a"/);
  assert.match(nodes.get("#sessionSetupBody").innerHTML, /Project B/);
  assert.doesNotMatch(nodes.get("#sessionSetupBody").innerHTML, /Project A/);
  nodes.get("#sessionSetup").listeners.click();
  assert.deepEqual(actions, ["setup"]);
  assert.equal(typeof nodes.get("#chatProviderSelect").listeners.change, "function");
});

test("a configured model missing credentials retains a direct recovery action in Workspace", () => {
  const {state, nodes, context, actions} = harness();
  state.providers[0].credential_ready = false;
  context.renderChat();
  assert.equal(state.sessionReady, false);
  const body = nodes.get("#sessionSetupBody").innerHTML;
  assert.match(body, /has no API key/);
  assert.match(body, /id="openProvidersFromDock">จัดการโมเดลและการเชื่อมต่อ/);
  nodes.get("#openProvidersFromDock").listeners.click();
  assert.deepEqual(actions, ["close", "providers"]);
});

test("editing the admission reason does not disable the global New task navigation", () => {
  const {state, nodes, context} = harness();
  context.renderChat();
  nodes.get("#chatQualificationReason").listeners.input({target:{value:""}});
  assert.equal(state.draftQualificationReason, "");
  assert.equal(nodes.get("#railNewSession").disabled, false);
  state.sending = true;
  context.renderChat();
  nodes.get("#chatQualificationReason").listeners.input({target:{value:"Long enough reason"}});
  assert.equal(nodes.get("#railNewSession").disabled, true);
});
