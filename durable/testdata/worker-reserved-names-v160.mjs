import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, script] = process.argv.slice(2);
const options = { timeoutMs: 10000, memoryLimitBytes: 33554432 };
const rejected = {};
let callbacks = 0;
const execute = () => { callbacks++; return 'callback'; };
for (const name of ['store', 'load', 'store.member', 'load.member']) {
  let sandbox;
  try {
    sandbox = publishedSandbox(placement, { ...options, globals: [{ name, execute }] });
    rejected[name] = null;
  } catch (error) {
    rejected[name] = error.message;
  } finally {
    await sandbox?.close();
  }
}
const sandbox = publishedSandbox(placement, { ...options, globals: [{ name: 'store1', execute }] });
try {
  const result = await sandbox.execute(script);
  process.stdout.write(JSON.stringify({ rejected, result, callbacks }));
} finally {
  await sandbox.close();
}
