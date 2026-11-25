import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, script] = process.argv.slice(2);
const seen = [];
const sandbox = publishedSandbox(placement, {
  timeoutMs: 10000,
  memoryLimitBytes: 33554432,
  tools: [{ name: 'echo', execute: args => args }],
  globals: [{ name: 'attach', execute: args => void seen.push(args) }],
});
try {
  const result = await sandbox.execute(script);
  process.stdout.write(JSON.stringify({ ok: result.ok, value: result.value, calls: result.calls.map(call => call.name), seen }));
} finally {
  await sandbox.close();
}
