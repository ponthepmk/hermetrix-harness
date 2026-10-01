const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function harness(saved = {}) {
  const values = new Map(Object.entries(saved)), events = [], selectors = [];
  const context = vm.createContext({window:{}, localStorage:{getItem:k=>values.get(k),setItem:(k,v)=>values.set(k,v)},
    document:{querySelectorAll:()=>selectors,dispatchEvent:event=>events.push(event)}, CustomEvent:class {constructor(type){this.type=type;}}});
  vm.runInContext(fs.readFileSync(`${__dirname}/conversation-controls.js`,'utf8'), context);
  return {api:context.window.HermetrixConversation,values,events,selectors};
}
test('approval mode defaults to Ask all and restores only an explicit valid Auto selection',()=>{
  assert.equal(harness().api.mode(),'ask_all');
  assert.equal(harness({'hermetrix.conversation.approvalMode':'invalid'}).api.mode(),'ask_all');
  assert.equal(harness({'hermetrix.conversation.approvalMode':'auto'}).api.mode(),'auto');
});
test('changing approval mode synchronizes mounted composers and persists the user choice',()=>{
  const h=harness();h.selectors.push({value:'ask_all'},{value:'ask_all'});h.api.setMode('auto');
  assert.ok(h.selectors.every(x=>x.value==='auto'));
  assert.equal(h.values.get('hermetrix.conversation.approvalMode'),'auto');
  assert.equal(h.events[0].type,'hermetrix:approval-mode');
});
test('slash completion recognizes commands without intercepting ordinary text or multiline prompts',()=>{
  const h=harness();assert.equal(h.api.matching('explain /files').length,0);
  assert.equal(h.api.matching('/files\nexplain').length,0);
  assert.equal(h.api.matching('/approve a').length,2);
  assert.equal(h.api.matching('/terminal')[0][0],'terminal');
});
test('automatic approval never retries the same receipt and stops when user switches to Ask all',async()=>{
  const source=fs.readFileSync(`${__dirname}/app.js`,'utf8');
  const frames=[],approved=[];
  let mode='auto';
  const state={sending:false,sessionDetail:{approvals:[{id:'one',state:'pending'}]}};
  const context=vm.createContext({state,Set,window:{HermetrixConversation:{mode:()=>mode}},
    document:{addEventListener(){}},setTimeout:fn=>frames.push(fn),decideToolApproval:async(id)=>approved.push(id)});
  vm.runInContext(source.slice(source.indexOf('const automaticApprovalAttempts ='),source.indexOf('function bindComposer()')),context);
  context.scheduleAutomaticApprovals();await frames.shift()();
  context.scheduleAutomaticApprovals();await frames.shift()();assert.deepEqual(approved,['one']);
  state.sessionDetail.approvals.push({id:'two',state:'pending'});mode='ask_all';
  context.scheduleAutomaticApprovals();await frames.shift()();assert.deepEqual(approved,['one']);
});
