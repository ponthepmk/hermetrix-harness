const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(`${__dirname}/app.js`, 'utf8');
const start = source.indexOf('function bestAuditProfileFor(');
const end = source.indexOf('// suggestedOverrideReason', start);
function setup(admission) {
  const scope = vm.createContext({profileAdmission: admission});
  vm.runInContext(source.slice(start, end), scope);
  return scope.bestAuditProfileFor;
}
const profiles = [
  {name:'compact-32k',total:32768,output_reserve:4096},
  {name:'certified-64k',total:65536,output_reserve:8192},
  {name:'extended-96k',total:98304,output_reserve:12288},
  {name:'extended-128k',total:131072,output_reserve:16384},
];
test('audit chooses sufficient answer reserve instead of the compact reasoning failure envelope', () => {
  const choose = setup(() => ({admitted:false,mode:'override_required'}));
  assert.equal(choose({max_output_tokens:16384}, profiles).name, 'extended-128k');
  assert.equal(choose({max_output_tokens:8192}, profiles).name, 'certified-64k');
});
test('audit does not invent capacity or admission and refuses blocked output budgets', () => {
  assert.equal(setup(() => ({admitted:true}))({max_output_tokens:4096}, profiles), null);
  assert.equal(setup(() => ({admitted:true}))({max_output_tokens:16384}, profiles.slice(0,3)), null);
  assert.equal(setup(() => ({blocking:true}))({max_output_tokens:16384}, profiles), null);
});
