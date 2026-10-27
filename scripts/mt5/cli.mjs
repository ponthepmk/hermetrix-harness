import { pathToFileURL } from 'node:url';
import { run } from './runner.mjs';

export async function main(args) {
  if (args.length !== 2 || args[0] !== 'run') throw new Error('Usage: node scripts/mt5/cli.mjs run mt5.local.json');
  const controller = new AbortController();
  const stop = () => controller.abort();
  process.once('SIGINT', stop); process.once('SIGTERM', stop);
  try {
    const result = await run(args[1], { signal: controller.signal });
    console.log(JSON.stringify(result));
    return result.passed ? 0 : 1;
  } finally { process.removeListener('SIGINT', stop); process.removeListener('SIGTERM', stop); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { process.exitCode = await main(process.argv.slice(2)); }
  catch (error) { console.error(JSON.stringify({ passed: false, phase: 'requires_inspection', message: error.message })); process.exitCode = 1; }
}
