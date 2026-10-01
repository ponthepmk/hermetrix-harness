const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const source = fs.readFileSync(`${__dirname}/app.js`, 'utf8');
function policy(qualifications=[]) {
  const profiles = [16384,32768,65536,131072].map(total=>({name:total<=32768?`compact-${total/1024}k`:`extended-${total/1024}k`,total,output_reserve:total/8}));
  const state = {profiles,qualifications};
  const context = vm.createContext({state});
  vm.runInContext(source.slice(source.indexOf('function availableProfiles('),source.indexOf('// suggestedOverrideReason')),context);
  return context;
}
test('auto capacity uses the largest model envelope, retaining qualification admission',()=>{
  const c=policy();const provider={id:'p',revision:'r',model:'m',context_window:131072};
  const selected=c.bestProfileFor(provider,c.availableProfiles(provider));
  assert.equal(selected.total,131072);
  assert.equal(c.profileAdmission(provider,selected).mode,'override_required');
});
test('verified loaded allocation caps model capacity; a small recall test does not',()=>{
  const run={provider_id:'p',provider_revision:'r',state:'completed',allocated_context:32768,results:{runtime_allocation:false}};
  const provider={id:'p',revision:'r',context_window:131072};
  let c=policy([run]);assert.equal(c.bestProfileFor(provider,c.availableProfiles(provider)).total,131072);
  c=policy([{...run,results:{runtime_allocation:true}}]);assert.equal(c.bestProfileFor(provider,c.availableProfiles(provider)).total,32768);
  c=policy([{...run,provider_revision:'old',results:{runtime_allocation:true}}]);assert.equal(c.bestProfileFor(provider,c.availableProfiles(provider)).total,131072);
});
