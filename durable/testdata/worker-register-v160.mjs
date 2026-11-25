// Exercise the published standalone registry; the Go application registry
// is intentionally not used to emulate duplicate registration or removal.
import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, firstSource, secondSource] = process.argv.slice(2);
const sandbox = publishedSandbox(placement, { timeoutMs: 10000, memoryLimitBytes: 33554432 });
try {
  let callbacks = 0;
  const echo = { name: 'echo', execute: args => { callbacks++; return args; } };
  sandbox.registerTool(echo);
  let duplicate = '';
  try {
    sandbox.registerTool(echo);
  } catch (error) {
    duplicate = error.message;
  }
  const names = sandbox.tools.map(tool => tool.name);
  const first = await sandbox.execute(firstSource);
  const removed = sandbox.unregisterTool('echo');
  const second = await sandbox.execute(secondSource);
  process.stdout.write(JSON.stringify({ duplicate, names, first, removed, second, callbacks }));
} finally {
  await sandbox.close();
}
