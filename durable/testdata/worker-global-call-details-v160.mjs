import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, script, mode] = process.argv.slice(2);
const seen = [];
const remember = args => void seen.push(args);
const echo = args => args;
if (mode !== 'calls' && mode !== 'spread' && mode !== 'error') throw new Error(`Unknown global fixture: ${mode}`);
const sandbox = publishedSandbox(placement, {
  timeoutMs: 10000,
  memoryLimitBytes: 33554432,
  tools: mode === 'calls' ? [{ name: 'echo', execute: echo }] : [],
  globals: mode === 'calls'
    ? [{ name: 'attach', execute: remember }]
    : mode === 'spread'
      ? [{ name: 'models.list', spread: true, execute: remember }, { name: 'models.first', execute: echo }]
      : [],
});
try {
  const result = await sandbox.execute(script);
  process.stdout.write(JSON.stringify({ ok: result.ok, value: result.value, error: result.error, calls: result.calls.map(call => call.name), seen }));
} finally {
  await sandbox.close();
}
