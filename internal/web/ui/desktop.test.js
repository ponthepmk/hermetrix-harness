const test=require("node:test"),assert=require("node:assert/strict"),fs=require("node:fs"),vm=require("node:vm");
const source=fs.readFileSync(`${__dirname}/app.js`,"utf8");
function deferred(){let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};}
function harness(api,confirm=async()=>true){
 const input={value:"Reviewed text"};const stored=new Map();const observed={window:{handle:1,process_id:2,title:"Fixture"},elements:[{ref:1,name:"Fixture input"}],observation_hash:"before",screenshot_artifact_id:"before-image"};
 const context=vm.createContext({state:{desktopSelectedWindow:"2:1",desktopObservation:observed,durableTasks:[]},api,askAction:confirm,currentActor:()=>"fixture user",toast:()=>{},AbortController,
  $:()=>input,localStorage:{setItem:(k,v)=>stored.set(k,v),getItem:k=>stored.get(k)},refreshWorkbenchSurface:()=>{input.value="";},loadTaskExecution:async()=>{}});
 vm.runInContext(source.slice(source.indexOf("let desktopActionAbortController"),source.indexOf("function renderWorkbenchBrowser()")),context);
 return context;
}
test("Windows text is captured before the loading render clears the input",async()=>{
 const bodies=[];const c=harness(async(path,options)=>{bodies.push(JSON.parse(options.body));return path.endsWith("/confirm")?{id:"a",state:"completed",observation:{observation_hash:"after"}}:{id:"a",subject_hash:"review",state:"prepared"};});
 await c.performDesktopAction("set_value",1);
 assert.equal(bodies[0].text,"Reviewed text");assert.equal(bodies[1].text,"Reviewed text");assert.equal(c.state.desktopBusy,"");assert.equal(c.state.desktopLastAction.state,"completed");
});
test("double activation creates one pointer receipt and late result cannot replace another window",async()=>{
 const prepare=deferred(),confirm=deferred();let calls=0;
 const c=harness(async(path)=>{calls++;return path.endsWith("/confirm")?confirm.promise:prepare.promise;});
 const first=c.performDesktopAction("invoke",0,{x:30,y:40});await c.performDesktopAction("invoke",0,{x:30,y:40});assert.equal(calls,1);
 prepare.resolve({id:"a",state:"prepared",subject_hash:"review"});await new Promise(r=>setImmediate(r));
 c.state.desktopSelectedWindow="9:9";c.state.desktopObservation={observation_hash:"other-window"};
 confirm.resolve({id:"a",state:"completed",observation:{observation_hash:"late"}});await first;
 assert.equal(calls,2);assert.equal(c.state.desktopObservation.observation_hash,"other-window");assert.equal(c.state.desktopLastAction.state,"completed");
});
test("denying the exact point cancels its receipt without sending mouse input",async()=>{
 const paths=[];const c=harness(async path=>{paths.push(path);return {id:"a",subject_hash:"review",state:path.endsWith("cancel")?"cancelled":"prepared"};},async()=>false);
 await c.performDesktopAction("invoke",0,{x:0,y:0});assert.equal(paths.length,2);assert.ok(paths[1].endsWith("/cancel"));assert.equal(c.state.desktopLastAction.state,"cancelled");assert.equal(c.state.desktopBusy,"");
});
test("a stale image error remains visible and releases loading without input",async()=>{
 let calls=0;const c=harness(async()=>{calls++;throw new Error("desktop observation changed; inspect again");});
 await c.performDesktopAction("invoke",0,{x:4,y:5});assert.equal(calls,1);assert.match(c.state.desktopError,/inspect again/);assert.equal(c.state.desktopBusy,"");
});
test("window inventory requires an explicit choice and never captures the first window",async()=>{
 let calls=0;const c=harness(async()=>{calls++;return [{process_id:9,handle:9,title:"Other application"}];});
 await c.listDesktopWindows();assert.equal(c.state.desktopSelectedWindow,"");assert.equal(c.state.desktopObservation,null);
 await c.observeDesktopWindow();assert.equal(calls,1);
});
