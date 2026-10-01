const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const source = fs.readFileSync(__dirname + '/auto-retry.js','utf8');
function harness(overrides={}) {
 let now=299999;
 const sent=[];
 const detail={session:{id:'s',state:'active'},events:[{id:'previous',event_kind:'message',role:'assistant',content:'Previous answer'}]};
 const state={sessionDetail:detail,chatTurnError:'gateway offline',draftMessage:'retry this',failedRequest:{sessionID:'s',beforeEventID:'previous',content:'retry this',draftText:'retry this',attachments:[{kind:'text',text:'saved attachment'}],retryAt:300000},...overrides};
 class Clock extends Date{static now(){return now}}
 const context=vm.createContext({state,Date:Clock,window:{},document:{addEventListener(){}},setInterval(){},escapeHTML:String,
 api:async()=>state.sessionDetail,captureComposer(){},renderChat(){},refreshPaneChat(){},scheduleAutomaticApprovals(){},workspacePaneVisible:()=>false,
 submitChatText:async(text,attachments)=>{sent.push({text,attachments});state.chatTurnError='';return true},setComposerDraft:text=>state.draftMessage=text});
 vm.runInContext(source,context);
 return {state,sent,context,time:value=>now=value};
}
test('uncommitted gateway failure retries after five minutes with original attachments',async()=>{
 const h=harness();await h.context.refreshAutomaticTurns();assert.equal(h.sent.length,0);
 h.time(300000);await h.context.refreshAutomaticTurns();assert.equal(h.sent.length,1);
 assert.equal(h.sent[0].text,'retry this');assert.equal(h.sent[0].attachments[0].text,'saved attachment');assert.equal(h.state.draftMessage,'');
 await h.context.refreshAutomaticTurns();assert.equal(h.sent.length,1);
});
test('automatic retry preserves a newer draft typed while waiting',async()=>{
 const h=harness({draftMessage:'new question'});h.time(300000);await h.context.refreshAutomaticTurns();assert.equal(h.sent.length,1);assert.equal(h.state.draftMessage,'new question');
});
test('committed response and paused sessions are never resent by the client',async()=>{
 const h=harness();h.state.sessionDetail.events.push({id:'new-answer',event_kind:'message',role:'assistant'});h.time(300000);await h.context.refreshAutomaticTurns();assert.equal(h.sent.length,0);
 const p=harness();p.state.sessionDetail.auto_retry={state:'paused'};p.time(300000);await p.context.refreshAutomaticTurns();assert.equal(p.sent.length,0);
});
test('opening workspace without a session or failed request never throws',()=>{
 const h=harness();h.state.failedRequest=undefined;h.state.sessionDetail=null;
 assert.equal(h.context.autoRetryNoticeHTML(null),'');
 assert.equal(h.context.autoRetryNoticeHTML({}),'');
});
