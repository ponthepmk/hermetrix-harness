import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {launchMT5,decodeText} from './runner.mjs';

const editorPath = process.env.HERMETRIX_MT5_METAEDITOR;
test('native MetaEditor reports an actual compile error then produces a fresh EX5 after repair',
  {skip: !editorPath && process.env.HERMETRIX_REQUIRE_MT5_NATIVE !== '1'}, async t => {
    assert.equal(process.platform,'win32','native MetaEditor requires Windows');
    assert.ok(editorPath,'HERMETRIX_MT5_METAEDITOR must name the intended compiler');
    const editor = await fs.realpath(editorPath);
    const parent = await fs.realpath(os.tmpdir());
    const base = await fs.realpath(await fs.mkdtemp(path.join(parent,'hermetrix mt5 compile ')));
    assert.equal(path.dirname(base),parent);
    t.after(async()=>{
      let current;
      try { current=await fs.realpath(base); } catch(error) { if(error.code==='ENOENT')return;throw error; }
      assert.equal(current,base,'cleanup path changed');
      assert.equal(path.dirname(current),parent,'cleanup escaped the intended temp root');
      await fs.rm(current,{recursive:true,force:true});
    });
    const source = path.join(base,'EA compile probe.mq5'), binary = source.slice(0,-4)+'.ex5';
    const good = '#property strict\n#property version "1.00"\n#include <Trade/Trade.mqh>\nint OnInit(){CTrade helper;helper.SetExpertMagicNumber(20261023);return INIT_SUCCEEDED;}\nvoid OnTick(){}\n';
    const compile = async()=>{
      const exit = await launchMT5(editor,[`/compile:${source}`,`/include:${path.join(path.dirname(editor),'MQL5')}`,'/log'],{cwd:base,timeout:60000});
      const bytes = await fs.readFile(source.slice(0,-4)+'.log');
      const log = decodeText(bytes), summaries=[...log.matchAll(/(?:Result:\s*)?(\d+) errors?,\s*(\d+) warnings?/gi)];
      assert.equal(summaries.length,1,log.slice(-2000));
      return {exit,errors:Number(summaries[0][1]),warnings:Number(summaries[0][2]),log_sha256:createHash('sha256').update(bytes).digest('hex')};
    };
    await fs.writeFile(source,good.replace('return INIT_SUCCEEDED;','return INIT_SUCCEEDED'),{flag:'wx'});
    const failed = await compile();
    assert.ok(failed.errors>0,'the actual compiler must reject the missing semicolon');
    await assert.rejects(fs.stat(binary),error=>error.code==='ENOENT');
    await fs.writeFile(source,good);
    const passed = await compile();
    assert.equal(passed.errors,0);assert.equal(passed.warnings,0);
    const exe = await fs.readFile(binary);
    assert.ok(exe.length>0,'compile must produce new executable bytes');
    assert.notEqual(failed.log_sha256,passed.log_sha256);
    t.diagnostic(JSON.stringify({editor,failed,passed,binary_bytes:exe.length,binary_sha256:createHash('sha256').update(exe).digest('hex'),compile_only:true}));
  });
