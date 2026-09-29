const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const source = fs.readFileSync(`${__dirname}/project-brain-candidate.js`, "utf8").replace(
  "window.HermetrixProjectBrain = {render}",
  "window.HermetrixProjectBrain = {render,__test:{configure,recordFor,readyForPreview,policyHTML,formHTML,deliveryHTML,selection,preview,submit,refreshStatus,allowSharing}}"
);

function harness(handler = () => ({}), confirm = true) {
  const calls = [];
  const saved = new Map();
  const context = vm.createContext({window:{},Map,Set,JSON,Number,Array,String,encodeURIComponent,
    localStorage:{getItem:key=>saved.get(key) || null,setItem:(key,value)=>saved.set(key,value)},
    escapeHTML:value=>String(value ?? "").replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll('"',"&quot;"),
    formatDate:value=>value, currentActor:()=>"owner", askAction:async()=>confirm, load:async()=>{},
    api:async(path,options)=>{calls.push({path,options});return handler(path,options);}});
  vm.runInContext(source,context);
  context.window.HermetrixProjectBrain.__test.configure({
    api:context.api,askAction:context.askAction,load:context.load,currentActor:context.currentActor,
    escapeHTML:context.escapeHTML,formatDate:context.formatDate
  });
  return {api:context.window.HermetrixProjectBrain,internals:context.window.HermetrixProjectBrain.__test,calls,saved};
}

function selection(h) {
  const record = h.internals.recordFor({id:"task-1",state:"completed",title:"Solved"});
  record.root = {isConnected:false};
  record.eligibility = {eligible:true,validations:[{id:"validation-1",requirement_id:"AC-1",check_id:"go-test",
    subject_revision:"requirement:1",observed_at:"2026-09-24T00:00:00Z",artifacts:[
      {id:"artifact-1",name:"test output",content_digest:"sha256:abc",eligible:true},
      {id:"artifact-private",name:"private",content_digest:"sha256:def",eligible:false}]}]};
  record.validationID = "validation-1";
  record.solution = "Run the checked fix";
  record.artifactIDs.add("artifact-1");
  return record;
}

test("completed task does not submit or widen sharing merely by rendering", () => {
  const h = harness();
  const root = {isConnected:false};
  h.api.render(root,{id:"task-1",state:"completed"},{id:"project-1",sharing_revision:1});
  h.api.render(root,{id:"task-2",state:"running"},{id:"project-1",sharing_revision:1});
  assert.equal(h.calls.length,0);
  const record = h.internals.recordFor({id:"task-1",state:"completed"});
  record.eligibility = {eligible:false,reason:"Project export is disabled"};
  assert.match(h.internals.policyHTML(record),/อนุญาตแชร์โปรเจกต์นี้/);
});

test("only current eligible evidence can enter a preview selection", () => {
  const h = harness();
  const record = selection(h);
  assert.equal(h.internals.readyForPreview(record),true);
  record.artifactIDs.delete("artifact-1");
  record.artifactIDs.add("artifact-private");
  assert.equal(h.internals.readyForPreview(record),false);
  record.artifactIDs.add("artifact-1");
  const payload = h.internals.selection(record);
  assert.deepEqual([...payload.selected_artifact_ids],["artifact-1"]);
  assert.doesNotMatch(h.internals.formHTML(record),/name="artifact_ids" value="artifact-private" checked/);
});

test("preview is read-only and a second explicit action queues exact approval", async () => {
  const approval = {actor:"owner",content_digest:"sha256:abc"};
  const h = harness((path,options) => path.endsWith("/preview")
    ? {candidate:{candidate_id:"candidate-1",title:"Solved",problem:"Broken",solution:"Run the checked fix",
        verification:{kind:"test",scope:"requirement:1",evidence_refs:[{evidence_id:"artifact-1"}]}},approval,selected_artifact_ids:["artifact-1"]}
    : {candidate_id:"candidate-1",state:"pending",attempts:0});
  const record = selection(h);
  await h.internals.preview(record);
  assert.equal(h.calls.length,1);
  assert.equal(h.calls[0].path,"/api/project-brain/candidates/preview");
  assert.deepEqual(JSON.parse(h.calls[0].options.body).selected_artifact_ids,["artifact-1"]);
  assert.equal(record.delivery,null);
  await h.internals.submit(record);
  assert.equal(h.calls.length,2);
  assert.equal(h.calls[1].path,"/api/project-brain/candidates");
  assert.deepEqual(JSON.parse(h.calls[1].options.body).approval,approval);
  assert.equal(record.delivery.state,"pending");
  assert.match(h.internals.deliveryHTML(record),/รอส่งไป Pi/);
  assert.doesNotMatch(h.internals.deliveryHTML(record),/รับรองแล้ว/);
  assert.equal(h.saved.get("hermetrix-project-brain-candidate:task-1"),"candidate-1");
});

test("ambiguous submit failure probes deterministic candidate before retry", async () => {
  const h = harness((path,options) => {
    if (options?.method === "POST") throw Error("response lost");
    if (path.endsWith("/candidate-1")) return {candidate_id:"candidate-1",state:"submitted",remote_commit:"abc"};
    throw Error("unexpected request");
  });
  const record = selection(h);
  record.preview = {candidate:{candidate_id:"candidate-1"},approval:{actor:"owner"}};
  await h.internals.submit(record);
  assert.equal(h.calls.filter(call => call.options?.method === "POST").length,1);
  assert.equal(record.delivery.state,"submitted");
  assert.match(h.internals.deliveryHTML(record),/รอผู้ดูแล Pi ตรวจ/);
});

test("outbox retry and conflict receipts use their actual persisted states", () => {
  const h = harness();
  const record = selection(h);
  record.candidateID = "candidate-1";
  record.delivery = {candidate_id:"candidate-1",state:"pending",attempts:1,last_error:"submit_unavailable"};
  assert.match(h.internals.deliveryHTML(record),/รอส่งอีกครั้ง/);
  record.delivery = {candidate_id:"candidate-1",state:"conflict",attempts:1,last_error:"candidate_conflict"};
  assert.match(h.internals.deliveryHTML(record),/ข้อมูลที่ส่งขัดแย้ง/);
});

test("sharing policy changes require a separate owner confirmation and exact revision", async () => {
  const eligibility = {task_id:"task-1",eligible:true,validations:[]};
  const declined = harness(()=>eligibility,false);
  const privateTask = selection(declined);
  privateTask.project = {id:"project-1",sharing_revision:3};
  await declined.internals.allowSharing(privateTask,"project");
  assert.equal(declined.calls.length,0);

  const approved = harness((path,options)=>options ? {revision:4} : eligibility,true);
  const record = selection(approved);
  record.project = {id:"project-1",sharing_revision:3};
  await approved.internals.allowSharing(record,"project");
  const change = approved.calls.find(call => call.options?.method === "POST");
  assert.equal(change.path,"/api/sharing/visibility");
  assert.deepEqual(JSON.parse(change.options.body),{
    visibility:"project_shared",export_policy:"explicit_selection",expected_revision:3,
    actor:"owner",reason:"owner enabled explicit Project Brain candidate sharing",
    object_kind:"project",object_id:"project-1"
  });
});
