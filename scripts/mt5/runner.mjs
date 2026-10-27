import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import * as fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const revision = 'mt5-runner-v3';
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const fail = message => { throw new Error(message); };
const inside = (root, target) => { const p = path.relative(root, target); return p !== '' && !p.startsWith(`..${path.sep}`) && p !== '..' && !path.isAbsolute(p); };

async function boundedRead(file, max = 2 * 1024 * 1024) {
  const st = await fs.stat(file);
  if (!st.isFile() || st.size > max) fail(`Missing or oversized artifact: ${path.basename(file)}`);
  return fs.readFile(file);
}

export function decodeText(bytes) {
  if (bytes[0] === 0xff && bytes[1] === 0xfe) return bytes.subarray(2).toString('utf16le');
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return new TextDecoder('utf-16be').decode(bytes.subarray(2));
  return bytes.toString('utf8').replace(/^\uFEFF/, '');
}

export function validateConfig(c) {
  const keys = ['terminal_root', 'dedicated_terminal', 'source', 'inputs', 'run_revision', 'tester', 'criteria', 'timeout_seconds'];
  if (!c || typeof c !== 'object' || Array.isArray(c) || Object.keys(c).some(k => !keys.includes(k))) fail('Unknown MT5 config field');
  if (c.dedicated_terminal !== true || !path.isAbsolute(c.terminal_root ?? '')) fail('An absolute, dedicated portable terminal_root is required');
  if (!/^[A-Za-z0-9_-]{1,80}$/.test(c.run_revision ?? '')) fail('Explicit run_revision is required (change it when history or environment changes)');
  if (typeof c.source !== 'string' || !c.source.endsWith('.mq5')) fail('source must be an MQ5 file');
  const inputs = [c.source, ...(c.inputs ?? [])];
  if (!Array.isArray(c.inputs ?? []) || inputs.length > 32 || new Set(inputs).size !== inputs.length) fail('Declare at most 32 distinct source/input files');
  for (const p of inputs) {
    if (typeof p !== 'string' || p.length > 240 || path.isAbsolute(p) || /[\\:\r\n\0]/.test(p) || p.split('/').some(x => x === '..' || !x) || p.startsWith('.mt5-runs/')) fail('Input paths must be project-relative paths without traversal');
  }
  const t = c.tester;
  if (!t || Object.keys(t).sort().join() !== ['symbol', 'period', 'from', 'to', 'deposit', 'currency', 'leverage', 'model'].sort().join()) fail('tester requires symbol, period, from, to, deposit, currency, leverage, model');
  if (!/^[A-Za-z0-9_.#-]{1,64}$/.test(t.symbol) || !/^(M(1|2|3|4|5|6|10|12|15|20|30)|H(1|2|3|4|6|8|12)|D1|W1|MN1)$/.test(t.period)) fail('Invalid tester symbol or period');
  const date = s => typeof s === 'string' && /^\d{4}\.\d{2}\.\d{2}$/.test(s) && new Date(s.replaceAll('.', '-') + 'T00:00:00Z').toISOString().slice(0, 10) === s.replaceAll('.', '-');
  if (!date(t.from) || !date(t.to) || t.from >= t.to) fail('Tester dates must be real dates, with from before to');
  if (!Number.isFinite(t.deposit) || t.deposit <= 0 || !/^[A-Z]{3}$/.test(t.currency) || !Number.isInteger(t.leverage) || t.leverage < 1 || t.leverage > 10000 || ![0, 1, 2, 4].includes(t.model)) fail('Invalid tester deposit, currency, leverage or tick model');
  const k = c.criteria;
  if (!k || Object.keys(k).sort().join() !== ['min_trades', 'min_profit_factor', 'max_equity_drawdown_pct', 'min_net_profit'].sort().join() || Object.values(k).some(v => !Number.isFinite(v))) fail('Explicit finite acceptance thresholds are required');
  if (!Number.isInteger(k.min_trades) || k.min_trades < 1 || k.min_profit_factor < 0 || k.max_equity_drawdown_pct < 0 || k.max_equity_drawdown_pct > 100) fail('Invalid acceptance thresholds');
  if (!Number.isInteger(c.timeout_seconds) || c.timeout_seconds < 1 || c.timeout_seconds > 86380) fail('timeout_seconds must be 1–86380; freeze a longer harness verification deadline with at least 20 seconds of headroom');
  return inputs;
}

function number(text) {
  // English reports only. Ambiguous decimal-comma or localized output fails.
  const value = text.trim().replace(/[ \u00a0\u202f]/g, '');
  if (!/^-?(?:\d+|\d{1,3}(?:,\d{3})+)(?:\.\d+)?$/.test(value)) fail('Report contains an unsupported numeric format');
  const n = Number(value.replaceAll(',', ''));
  if (!Number.isFinite(n)) fail('Non-finite report metric');
  return n;
}

function reportField(html) {
  if (typeof html !== 'string' || html.length > 8 * 1024 * 1024) fail('Invalid report size');
  const cells = [...html.matchAll(/<td\b[^>]*>([\s\S]*?)<\/td>/gi)].map(m => m[1].replace(/<[^>]*>/g, '').replace(/&nbsp;|&#160;/gi, ' ').replace(/&amp;/gi, '&').trim());
  return label => {
    const values = cells.flatMap((cell, i) => cell.replace(/:$/, '') === label ? [cells[i + 1]] : []);
    if (values.length !== 1 || !values[0]) fail(`Expected exactly one English report metric: ${label}`);
    return values[0];
  };
}

export function validateReportIdentity(html, c, stem) {
  const field = reportField(html);
  const expert = field('Expert').replaceAll('\\', '/').split('/').at(-1).replace(/\.ex5$/i, '');
  const period = /^(\w+)\s+\((\d{4}\.\d{2}\.\d{2})\s*-\s*(\d{4}\.\d{2}\.\d{2})\)$/.exec(field('Period'));
  if (expert !== stem || field('Symbol') !== c.tester.symbol || !period || period[1] !== c.tester.period || period[2] !== c.tester.from || period[3] !== c.tester.to) fail('Report expert, symbol, period or dates do not match the requested test');
}

export function parseReport(html) {
  const field = reportField(html);
  const drawdown = field('Equity Drawdown Maximal');
  const pct = /\(([\d.]+)%\)/.exec(drawdown);
  if (!pct) fail('Equity drawdown percentage is absent');
  const metrics = { net_profit: number(field('Total Net Profit')), profit_factor: number(field('Profit Factor')), trades: number(field('Total Trades')), equity_drawdown_pct: number(pct[1]) };
  if (!Number.isInteger(metrics.trades) || metrics.trades < 0 || metrics.profit_factor < 0 || metrics.equity_drawdown_pct < 0 || metrics.equity_drawdown_pct > 100) fail('Impossible report metrics');
  return metrics;
}

export function evaluate(metrics, criteria) {
  const failures = [];
  if (metrics.trades < criteria.min_trades) failures.push(`trades ${metrics.trades} < ${criteria.min_trades}`);
  if (metrics.profit_factor < criteria.min_profit_factor) failures.push(`profit_factor ${metrics.profit_factor} < ${criteria.min_profit_factor}`);
  if (metrics.equity_drawdown_pct > criteria.max_equity_drawdown_pct) failures.push(`equity_drawdown_pct ${metrics.equity_drawdown_pct} > ${criteria.max_equity_drawdown_pct}`);
  if (metrics.net_profit < criteria.min_net_profit) failures.push(`net_profit ${metrics.net_profit} < ${criteria.min_net_profit}`);
  return { passed: failures.length === 0, failures, metrics };
}

export function testerIni(c, id, stem) {
  const t = c.tester;
  return `[Experts]\r\nAllowLiveTrading=0\r\nAllowDllImport=0\r\nEnabled=1\r\n[StartUp]\r\nExpert=\r\nScript=\r\n[Tester]\r\nExpert=Hermetrix\\${id}\\${stem}.ex5\r\nSymbol=${t.symbol}\r\nPeriod=${t.period}\r\nFromDate=${t.from}\r\nToDate=${t.to}\r\nDeposit=${t.deposit}\r\nCurrency=${t.currency}\r\nLeverage=1:${t.leverage}\r\nModel=${t.model}\r\nOptimization=0\r\nForwardMode=0\r\nVisual=0\r\nUseLocal=1\r\nUseRemote=0\r\nUseCloud=0\r\nReport=Reports\\Hermetrix\\${id}\\report.htm\r\nReplaceReport=0\r\nShutdownTerminal=1\r\n`;
}

// MetaQuotes parses path switches from the raw Windows command line. Quoting
// the entire /key:value argument (Node's usual behavior) truncates spaced paths.
export function mt5ProcessSpec(executable, args, platform = process.platform) {
  if (platform !== 'win32') return { executable, args, options: {} };
  const literal = value => {
    if (typeof value !== 'string' || !path.win32.isAbsolute(value) || /["\r\n\0]/.test(value)) fail('MT5 native paths must be absolute without quotes or control characters');
    return `"${path.win32.normalize(value).replace(/\\+$/, '$&$&')}"`;
  };
  const argv0 = literal(executable), seen = new Set();
  let actions = 0;
  if (!Array.isArray(args) || !args.length) fail('An explicit MT5 compile or config action is required');
  const nativeArgs = args.map(arg => {
    if (arg === '/log' || arg === '/portable') return arg;
    const match = /^\/(compile|include|config):(.+)$/.exec(arg);
    if (!match || seen.has(match[1])) fail('Unsupported or duplicate MT5 native argument');
    seen.add(match[1]);
    if (match[1] !== 'include') actions++;
    return `/${match[1]}:${literal(match[2])}`;
  });
  if (actions !== 1) fail('Exactly one MT5 compile or config action is required');
  return { executable: path.win32.normalize(executable), args: nativeArgs, options: { windowsVerbatimArguments: true, argv0 } };
}

export function launch(executable, args, options) {
  return launchProcess(executable, args, options);
}

export async function launchMT5(executable, args, options) {
  const native = mt5ProcessSpec(executable, args);
  return launchProcess(native.executable, native.args, options, native.options);
}

function launchProcess(executable, args, { cwd, timeout, signal }, nativeOptions = {}) {
  if (signal?.aborted) return Promise.reject(new Error('MT5 process cancelled before dispatch'));
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, { cwd, shell: false, windowsHide: true, stdio: 'ignore', ...nativeOptions });
    let cancelled = false;
    const stop = () => {
      cancelled = true;
      if (child.exitCode === null && child.pid) {
        if (process.platform === 'win32') spawn('taskkill.exe', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore', shell: false }).on('error', () => child.kill());
        else child.kill('SIGKILL');
      }
    };
    const timer = setTimeout(stop, timeout);
    signal?.addEventListener('abort', stop, { once: true });
    if (signal?.aborted) stop();
    const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', stop); };
    child.once('error', error => { cleanup(); reject(error); });
    child.once('close', code => { cleanup(); cancelled ? reject(new Error('MT5 process interrupted; inspect retained evidence before retrying')) : resolve(code); });
  });
}

// run creates unique evidence folders; it never overwrites an existing EA or
// replays a dispatched operation without a terminal receipt. Tests may inject
// process transport, but the CLI always uses real native executables.
export async function run(configPath, { projectRoot = process.cwd(), signal, runProcess = launchMT5, platform = process.platform } = {}) {
  const root = await fs.realpath(projectRoot);
  const cfgPath = await fs.realpath(path.resolve(root, configPath));
  if (!inside(root, cfgPath)) fail('Config must be inside the project');
  const configBytes = await boundedRead(cfgPath, 64 * 1024);
  const c = JSON.parse(decodeText(configBytes));
  const inputs = validateConfig(c);
  if (platform !== 'win32') fail('Actual MT5 execution requires Windows');
  const terminalRoot = await fs.realpath(c.terminal_root);
  const editor = path.join(terminalRoot, 'metaeditor64.exe'), terminal = path.join(terminalRoot, 'terminal64.exe');
  for (const exe of [editor, terminal]) if (!(await fs.stat(exe)).isFile()) fail('Native MT5 executables are missing');
  const files = [];
  for (const name of inputs) {
    const p = await fs.realpath(path.resolve(root, name));
    if (!inside(root, p)) fail('Source/input escapes project through a link');
    const bytes = await boundedRead(p);
    files.push({ path: name, sha256: hash(bytes), bytes });
  }
  const manifest = { revision, adapter_sha256: hash(await boundedRead(fileURLToPath(import.meta.url))), config_sha256: hash(configBytes), inputs: files.map(({ path, sha256 }) => ({ path, sha256 })) };
  const id = hash(JSON.stringify(manifest)).slice(0, 32);
  const jobsRoot = path.join(root, '.mt5-runs');
  await fs.mkdir(jobsRoot, { recursive: true });
  if (!inside(root, await fs.realpath(jobsRoot))) fail('Evidence folder escapes project');
  const dir = path.join(jobsRoot, id), receiptPath = path.join(dir, 'result.json');
  try {
    await fs.mkdir(dir);
  } catch (error) {
    if (error.code !== 'EEXIST') throw error;
    if (!inside(root, await fs.realpath(dir))) fail('Saved evidence escapes project');
    // Do not dispatch twice after an interrupted run. A terminal receipt may
    // be read again, bound to exactly the same manifest and report bytes.
    const saved = JSON.parse(decodeText(await boundedRead(receiptPath)));
    if (saved.manifest_sha256 !== hash(JSON.stringify(manifest))) fail('Saved receipt binding changed');
    if (saved.report_sha256 && hash(await boundedRead(path.join(dir, 'report.htm'), 8 * 1024 * 1024)) !== saved.report_sha256) fail('Saved report changed');
    if (saved.phase === 'compile' && saved.passed === false) return { ...saved, reused: true };
    if (saved.phase !== 'backtest' || !saved.report_sha256 || !saved.binary_sha256 || saved.tester_exit !== 0) fail('Saved run is not a successful backtest receipt; inspect it before retrying');
    const savedReport = decodeText(await boundedRead(path.join(dir, 'report.htm'), 8 * 1024 * 1024));
    validateReportIdentity(savedReport, c, path.basename(c.source, '.mq5'));
    const checked = evaluate(parseReport(savedReport), c.criteria);
    if (hash(await boundedRead(path.join(dir, 'source', c.source.slice(0, -4) + '.ex5'), 32 * 1024 * 1024)) !== saved.binary_sha256) fail('Saved binary changed');
    return { ...saved, ...checked, reused: true };
  }
  await fs.writeFile(path.join(dir, 'manifest.json'), JSON.stringify(manifest, null, 2), { flag: 'wx' });
  const lockPath = path.join(terminalRoot, '.hermetrix-mt5.lock');
  const lock = await fs.open(lockPath, 'wx'); // a stale lock requires inspection
  let dispatched = false, terminalReceipt = false;
  const deadline = Date.now() + c.timeout_seconds * 1000;
  const options = () => ({ cwd: terminalRoot, timeout: Math.max(1, deadline - Date.now()), signal });
  const save = async result => {
    const receipt = { ...result, operation_id: id, manifest_sha256: hash(JSON.stringify(manifest)), manifest, evidence_dir: dir };
    await fs.writeFile(receiptPath, JSON.stringify(receipt, null, 2), { flag: 'wx' });
    terminalReceipt = true;
    return receipt;
  };
  try {
    await lock.writeFile(JSON.stringify({ operation_id: id, pid: process.pid }));
    const sourceRoot = path.join(dir, 'source');
    for (const file of files) { const target = path.join(sourceRoot, file.path); await fs.mkdir(path.dirname(target), { recursive: true }); await fs.writeFile(target, file.bytes, { flag: 'wx' }); }
    const source = path.join(sourceRoot, c.source), compiled = source.slice(0, -4) + '.ex5';
    dispatched = true;
    const compileExit = await runProcess(editor, [`/compile:${source}`, `/include:${path.join(terminalRoot, 'MQL5')}`, '/log'], options());
    const logBytes = await boundedRead(source.slice(0, -4) + '.log');
    const log = decodeText(logBytes), summary = /(?:Result:\s*)?(\d+) errors?,\s*(\d+) warnings?/gi;
    const results = [...log.matchAll(summary)];
    await fs.writeFile(path.join(dir, 'compile.log'), logBytes, { flag: 'wx' });
    if (results.length !== 1) fail('Compiler log must contain one supported English error/warning summary');
    if (Number(results[0][1]) !== 0 || Number(results[0][2]) !== 0) return await save({ passed: false, phase: 'compile', compile_exit: compileExit, failures: ['Compilation must have zero errors and warnings'], compile_log_tail: log.slice(-4000) });
    const binary = await boundedRead(compiled, 32 * 1024 * 1024);
    if (!binary.length) fail('Compiler produced no EX5 bytes');
    const stem = path.basename(c.source, '.mq5');
    const expertDir = path.join(terminalRoot, 'MQL5', 'Experts', 'Hermetrix', id);
    const reportDir = path.join(terminalRoot, 'Reports', 'Hermetrix', id);
    for (const p of [expertDir, reportDir]) { await fs.mkdir(p, { recursive: true }); if (!inside(terminalRoot, await fs.realpath(p))) fail('Terminal output escapes portable root'); }
    await fs.writeFile(path.join(expertDir, `${stem}.ex5`), binary, { flag: 'wx' });
    const ini = testerIni(c, id, stem), iniPath = path.join(dir, 'tester.ini');
    await fs.writeFile(iniPath, '\uFEFF' + ini, { encoding: 'utf16le', flag: 'wx' });
    if (Date.now() >= deadline || signal?.aborted) fail('Deadline or cancellation reached before tester dispatch');
    const testerExit = await runProcess(terminal, ['/portable', `/config:${iniPath}`], options());
    const reportBytes = await boundedRead(path.join(reportDir, 'report.htm'), 8 * 1024 * 1024);
    await fs.writeFile(path.join(dir, 'report.htm'), reportBytes, { flag: 'wx' });
    const reportText = decodeText(reportBytes);
    validateReportIdentity(reportText, c, stem);
    const result = evaluate(parseReport(reportText), c.criteria);
    if (testerExit !== 0) { result.passed = false; result.failures.push(`Tester process exit ${testerExit}`); }
    return await save({ ...result, phase: 'backtest', compile_exit: compileExit, tester_exit: testerExit, binary_sha256: hash(binary), report_sha256: hash(reportBytes), tester_ini_sha256: hash(Buffer.from('\uFEFF' + ini, 'utf16le')) });
  } finally {
    await lock.close();
    if (!dispatched || terminalReceipt) await fs.unlink(lockPath);
  }
}
