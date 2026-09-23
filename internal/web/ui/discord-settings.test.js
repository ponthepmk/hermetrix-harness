const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const {escapeHTML} = require("./runtime.js");
const source = fs.readFileSync(`${__dirname}/discord-settings.js`,"utf8").replace("window.HermetrixDiscord = {render}",
  "window.HermetrixDiscord = {render,__test:{validation,configOf,fromConfig,invite,statusHTML,saveScope,saveToken,connection,getDraft:()=>draft,getSnapshot:()=>snapshot,getFeedback:()=>feedback}}");
const config = {application_id:"12345678901234567",guild_ids:["22345678901234567"],channel_ids:["32345678901234567"],user_ids:["42345678901234567"],
  project_id:"project",provider_id:"local",context_profile:"compact-32k",enabled:false};
function harness(handler) {
  let html="";
  const nodes = new Map(), fields = {};
  const node = () => ({value:"",innerHTML:"",disabled:false,hidden:false,classList:{toggle(){}},addEventListener(){},removeAttribute(){}});
  const root = {isConnected:true,getClientRects:()=>[{}],querySelector:selector=>{
    if (!nodes.has(selector)) nodes.set(selector,node());
    const item=nodes.get(selector);
    if (selector==='[data-discord-scope]') item.elements={namedItem:name=>fields[name] || null};
    return item;
  },querySelectorAll:()=>[]};
  Object.defineProperty(root,"innerHTML",{get:()=>html,set:value=>{html=value;nodes.clear();}});
  const calls=[],timers=[];
  const state={projects:[{id:"project",name:"Project"}],providers:[{id:"remote",name:"Remote",local:false},{id:"local",name:"Local",local:true}],profiles:[{name:"compact-32k"},{name:"extended-96k"}]};
  const context=vm.createContext({window:{},state,escapeHTML,Set,Map,JSON,encodeURIComponent,setTimeout:callback=>{timers.push(callback);return timers.length;},clearTimeout(){},
    taskEligibleProviders:()=>state.providers.filter(item=>item.local),availableProfiles:()=>state.profiles,
    profileAdmission:(_provider,profile)=>({admitted:profile.name==="compact-32k"}),profileLabel:profile=>profile.name,
    api:async(path,options)=>{calls.push({path,options});return handler ? handler(path,options) : {config,token_stored:true,state:"stopped"};}});
  vm.runInContext(source,context);
  return {api:context.window.HermetrixDiscord,internals:context.window.HermetrixDiscord.__test,root,nodes,fields,calls,state,timers};
}
test("only local models and admitted context profiles appear in Discord setup",async()=>{
  const h=harness();await h.api.render(h.root);
  assert.match(h.root.innerHTML,/value="local"/);assert.doesNotMatch(h.root.innerHTML,/value="remote"|extended-96k/);
  assert.match(h.root.innerHTML,/ข้อความ คำตอบ.*Discord/);assert.match(h.root.innerHTML,/ephemeral/);
});
test("missing IDs block scope submission and connection before any mutation",async()=>{
  const h=harness();await h.api.render(h.root);h.internals.getDraft().channel_ids="";
  await h.internals.saveScope();await h.internals.connection("start");
  assert.equal(h.calls.filter(call=>call.options).length,0);assert.match(h.internals.getFeedback(),/Channel ID/);
});
test("wildcards and malformed snowflakes are rejected; duplicate valid IDs normalize",async()=>{
  const h=harness();await h.api.render(h.root);const draft=h.internals.getDraft();
  draft.user_ids="*";assert.match(h.internals.validation(draft),/User ID/);
  draft.user_ids="42345678901234567, 42345678901234567";
  assert.equal(h.internals.validation(draft),"");assert.equal(h.internals.configOf(draft).user_ids.length,1);
});
test("remote providers and unqualified profiles cannot bypass form filtering",async()=>{
  const h=harness();await h.api.render(h.root);const draft=h.internals.getDraft();
  draft.provider_id="remote";assert.match(h.internals.validation(draft),/โมเดล/);
  draft.provider_id="local";draft.context_profile="extended-96k";assert.match(h.internals.validation(draft),/ขนาดบริบท/);
});
test("save scope sends only binding fields with enabled false and retains a failed draft",async()=>{
  const h=harness((_path,options)=>{if(options)throw Error("Network unavailable");return {config,token_stored:true,state:"ready"};});
  await h.api.render(h.root);h.internals.getDraft().guild_ids="52345678901234567";await h.internals.saveScope();
  const payload=JSON.parse(h.calls.find(call=>call.options).options.body);
  assert.equal(payload.enabled,false);assert.equal(payload.guild_ids[0],"52345678901234567");assert.equal(payload.owner_id,undefined);
  assert.equal(h.internals.getDraft().guild_ids,"52345678901234567");assert.equal(h.internals.getSnapshot().state,"unknown");
});
test("blank token preserves existing secret without making a token request",async()=>{
  const h=harness();await h.api.render(h.root);await h.internals.saveToken();
  assert.equal(h.calls.filter(call=>call.options).length,0);assert.match(h.internals.getFeedback(),/ช่องว่าง/);
});
test("explicit token save clears password before request and never renders token even on error",async()=>{
  let field;
  const h=harness((path,options)=>{if(path.endsWith("/token")){assert.equal(field.value,"");assert.equal(JSON.parse(options.body).token,"private-secret");throw Error("private-secret invalid");}return {config,token_stored:true,state:"stopped"};});
  await h.api.render(h.root);field=h.root.querySelector('[data-discord-token]');field.value="private-secret";
  await h.internals.saveToken();
  assert.equal(field.value,"");assert.doesNotMatch(h.root.innerHTML+h.internals.getFeedback(),/private-secret/);
});
test("connect requires saved settings; accepted start still displays connecting until ready",async()=>{
  let current="stopped";
  const h=harness((path,options)=>{if(path.endsWith("/start"))current="connecting";return {config,token_stored:true,state:current};});
  await h.api.render(h.root);h.internals.getDraft().user_ids="52345678901234567";await h.internals.connection("start");
  assert.equal(h.calls.filter(call=>call.options).length,0);
  h.internals.getDraft().user_ids=config.user_ids.join("\n");await h.internals.connection("start");
  assert.equal(h.internals.getSnapshot().state,"connecting");assert.doesNotMatch(h.internals.statusHTML(),/เชื่อมต่อ Discord แล้ว/);
});
test("error and stopped are never presented as connected",async()=>{
  for(const state of ["error","stopped","disabled","reconnecting"]) {
    const h=harness(()=>({config,token_stored:true,state,last_error:"Could not connect"}));await h.api.render(h.root);
    assert.doesNotMatch(h.internals.statusHTML(),/เชื่อมต่อ Discord แล้ว/);assert.match(h.internals.statusHTML(),/Could not connect/);
  }
});

test("disconnect stays available after a status read fails",async()=>{
  let fail=false;
  const h=harness(()=>{if(fail)throw Error("offline");return {config:{...config,enabled:true},token_stored:true,state:"ready"};});
  await h.api.render(h.root); fail=true; await h.api.render(h.root);
  assert.equal(h.internals.getSnapshot().state,"unknown");
  assert.equal(h.nodes.get('[data-discord-stop]').disabled,false);
});
test("invite link uses only validated application ID and zero bot permissions",()=>{
  const h=harness();assert.equal(h.internals.invite('1&permissions=8'),"");
  const link=h.internals.invite(config.application_id);assert.match(link,/scope=bot%20applications.commands/);assert.match(link,/permissions=0/);assert.doesNotMatch(link,/permissions=8/);
});
test("a failed status reload preserves scope inputs and does not claim old ready state",async()=>{
  let fail=false;
  const h=harness(()=>{if(fail)throw Error("offline");return {config,token_stored:true,state:"ready"};});
  await h.api.render(h.root);h.internals.getDraft().application_id="52345678901234567";fail=true;await h.api.render(h.root);
  assert.equal(h.internals.getDraft().application_id,"52345678901234567");assert.equal(h.internals.getSnapshot().state,"unknown");assert.doesNotMatch(h.internals.statusHTML(),/เชื่อมต่อ Discord แล้ว/);
});
test("a late ready polling response cannot overwrite a completed Stop",async()=>{
  let reads=0,resolve;
  const h=harness((_path,options)=>{
    if(options)return {};
    reads++;
    if(reads===2)return new Promise(done=>{resolve=done;});
    return {config,token_stored:true,state:reads===1?"ready":"stopped"};
  });
  await h.api.render(h.root);
  const poll=h.timers.at(-1)();
  await h.internals.connection("stop");
  resolve({config,token_stored:true,state:"ready"});await poll;
  assert.equal(h.internals.getSnapshot().state,"stopped");
});
