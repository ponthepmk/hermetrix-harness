import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { run, parseReport, evaluate, validateConfig, validateReportIdentity, testerIni, decodeText, launch, mt5ProcessSpec } from './runner.mjs';

const report = (profit = '200.00') => `<html><table><tr><td>Expert:</td><td>EA</td><td>Symbol:</td><td>EURUSD</td><td>Period:</td><td>H1 (2025.01.01 - 2026.01.01)</td></tr><tr><td>Total Net Profit:</td><td><b>${profit}</b></td><td>Profit Factor:</td><td>1.50</td></tr><tr><td>Total Trades:</td><td>100</td><td>Equity Drawdown Maximal:</td><td>100.00 (5.00%)</td></tr></table></html>`;
const config = terminal => ({ terminal_root: terminal, dedicated_terminal: true, source: 'EA.mq5', inputs: ['lib.mqh'], run_revision: 'history-2026-10-23', timeout_seconds: 90,
  tester: { symbol: 'EURUSD', period: 'H1', from: '2025.01.01', to: '2026.01.01', deposit: 10000, currency: 'USD', leverage: 100, model: 4 },
  criteria: { min_trades: 80, min_profit_factor: 1.3, max_equity_drawdown_pct: 10, min_net_profit: 0 } });

async function fixture(t) {
  const base = await fs.mkdtemp(path.join(os.tmpdir(), 'hermetrix-mt5-'));
  t.after(() => fs.rm(base, { recursive: true, force: true }));
  const root = path.join(base, 'project'), terminal = path.join(base, 'portable');
  await fs.mkdir(root); await fs.mkdir(terminal);
  await fs.writeFile(path.join(root, 'EA.mq5'), '#include "lib.mqh"\nvoid OnTick() {}\n');
  await fs.writeFile(path.join(root, 'lib.mqh'), '// dependency\n');
  for (const name of ['terminal64.exe', 'metaeditor64.exe']) await fs.writeFile(path.join(terminal, name), 'process transport fixture');
  const cfg = config(terminal);
  await fs.writeFile(path.join(root, 'mt5.local.json'), JSON.stringify(cfg));
  return { root, terminal, cfg };
}

test('English report metrics and all objective thresholds are enforced', () => {
  const m = parseReport(report());
  assert.deepEqual(m, { net_profit: 200, profit_factor: 1.5, trades: 100, equity_drawdown_pct: 5 });
  assert.equal(evaluate(m, config('C:/portable').criteria).passed, true);
  assert.equal(evaluate({ ...m, trades: 0 }, config('C:/portable').criteria).passed, false);
  assert.equal(evaluate(parseReport(report('-12.00')), config('C:/portable').criteria).passed, false);
  assert.throws(() => parseReport(report('1,25')), /numeric format/);
  assert.throws(() => parseReport(report() + '<td>Total Trades:</td><td>999</td>'), /exactly one/);
  assert.throws(() => parseReport('<html>finished successfully</html>'), /metric/);
  validateReportIdentity(report(), config('C:/portable'), 'EA');
  for (const body of [report().replace('EURUSD', 'GBPUSD'), report().replace('H1 (', 'M5 ('), report().replace('2025.01.01', '2024.01.01')]) assert.throws(() => validateReportIdentity(body, config('C:/portable'), 'EA'), /do not match/);
});

test('config rejects missing criteria, traversal, injection and real invalid dates', () => {
  const c = config(path.resolve('portable'));
  assert.deepEqual(validateConfig(c), ['EA.mq5', 'lib.mqh']);
  assert.deepEqual(validateConfig({ ...c, timeout_seconds: 3600 }), ['EA.mq5', 'lib.mqh']);
  for (const timeout_seconds of [0, -1, 86381, 1.5]) assert.throws(() => validateConfig({ ...c, timeout_seconds }), /timeout_seconds/);
  for (const change of [{ source: '../EA.mq5' }, { dedicated_terminal: false }, { password: 'secret' }, { criteria: {} }, { inputs: ['lib.mqh', 'lib.mqh'] }, { tester: { ...c.tester, symbol: 'EURUSD\nUseCloud=1' } }, { tester: { ...c.tester, from: '2025.02.30' } }]) assert.throws(() => validateConfig({ ...c, ...change }));
  const ini = testerIni(c, 'id', 'EA');
  for (const key of ['AllowLiveTrading=0', 'AllowDllImport=0', 'UseCloud=0', 'UseRemote=0', 'Optimization=0', 'ShutdownTerminal=1']) assert.ok(ini.includes(key));
  assert.equal(decodeText(Buffer.from('\uFEFF0 errors, 0 warnings', 'utf16le')), '0 errors, 0 warnings');
});

test('real process launcher returns actual nonzero exit status', async () => {
  assert.equal(await launch(process.execPath, ['-e', 'process.exit(3)'], { cwd: process.cwd(), timeout: 2000 }), 3);
});

test('MetaQuotes Windows switches preserve full spaced paths without shell parsing', () => {
  const editor = 'C:/Program Files/MT5/metaeditor64.exe';
  const args = ['/compile:D:/EA project/EA file.mq5', '/include:C:/Program Files/MT5/MQL5', '/log'];
  const native = mt5ProcessSpec(editor, args, 'win32');
  assert.equal(native.executable, 'C:\\Program Files\\MT5\\metaeditor64.exe');
  assert.deepEqual(native.args, ['/compile:"D:\\EA project\\EA file.mq5"', '/include:"C:\\Program Files\\MT5\\MQL5"', '/log']);
  assert.deepEqual(native.options, { windowsVerbatimArguments: true, argv0: '"C:\\Program Files\\MT5\\metaeditor64.exe"' });
  assert.deepEqual(mt5ProcessSpec('C:/MT5/terminal64.exe', ['/portable', '/config:D:/EA project/tester.ini'], 'win32').args,
    ['/portable', '/config:"D:\\EA project\\tester.ini"']);
  assert.deepEqual(mt5ProcessSpec(editor, args, 'linux').args, args);
  for (const bad of [[], ['/log'], ['/config:relative.ini'], ['/config:C:\\ok.ini" /compile:C:\\other.mq5'],
    ['/config:C:\\bad\n.ini'], ['/config:C:\\a.ini', '/config:C:\\b.ini'], ['/compile:C:\\a.mq5', '/config:C:\\b.ini']]) {
    assert.throws(() => mt5ProcessSpec(editor, bad, 'win32'));
  }
});

test('compile/backtest transport contract retains evidence and never dispatches twice', async t => {
  const f = await fixture(t); let calls = 0;
  const runProcess = async (exe, args) => {
    calls++;
    if (exe.endsWith('metaeditor64.exe')) {
      assert.equal(args[2], '/log');
      const source = args[0].slice('/compile:'.length);
      assert.equal(await fs.readFile(path.join(path.dirname(source), 'lib.mqh'), 'utf8'), '// dependency\n');
      await fs.writeFile(source.slice(0, -4) + '.log', Buffer.from('\uFEFFResult: 0 errors, 0 warnings, 100 msec', 'utf16le'));
      await fs.writeFile(source.slice(0, -4) + '.ex5', 'new executable bytes');
      return 1; // MetaEditor exit code alone is not a compile verdict.
    }
    assert.equal(args[0], '/portable');
    const ini = decodeText(await fs.readFile(args[1].slice('/config:'.length)));
    const rel = /Report=(.+)\r/.exec(ini)[1];
    await fs.writeFile(path.join(f.terminal, ...rel.split('\\')), report()); return 0;
  };
  const options = { projectRoot: f.root, platform: 'win32', runProcess };
  const first = await run('mt5.local.json', options);
  assert.equal(first.passed, true); assert.equal(calls, 2);
  assert.ok(first.binary_sha256 && first.report_sha256 && first.manifest.inputs.length === 2);
  const again = await run('mt5.local.json', options);
  assert.equal(again.reused, true); assert.equal(calls, 2);
  await fs.writeFile(path.join(first.evidence_dir, 'report.htm'), report('-100'));
  await assert.rejects(run('mt5.local.json', options), /Saved report changed/);
  assert.equal(calls, 2);
});

test('interrupted compile retains lock and cannot replay the operation', async t => {
  const f = await fixture(t); let calls = 0;
  const options = { projectRoot: f.root, platform: 'win32', runProcess: async () => { calls++; throw new Error('interrupted'); } };
  await assert.rejects(run('mt5.local.json', options), /interrupted/);
  await assert.rejects(run('mt5.local.json', options));
  assert.equal(calls, 1);
  assert.ok(await fs.stat(path.join(f.terminal, '.hermetrix-mt5.lock')));
});

test('compiler errors fail without launching the tester', async t => {
  const f = await fixture(t); let calls = 0;
  const result = await run('mt5.local.json', { projectRoot: f.root, platform: 'win32', runProcess: async (_exe, args) => {
    calls++; const source = args[0].slice('/compile:'.length);
    await fs.writeFile(source.slice(0, -4) + '.log', 'Result: 1 errors, 0 warnings'); return 0;
  } });
  assert.equal(result.passed, false); assert.equal(result.phase, 'compile'); assert.equal(calls, 1);
});
