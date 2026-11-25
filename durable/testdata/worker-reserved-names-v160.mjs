import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, script, casesJSON] = process.argv.slice(2);
const options = { timeoutMs: 10000, memoryLimitBytes: 33554432 };
const rejected = {};
let callbacks = 0;
const execute = () => { callbacks++; return 'callback'; };
for (const [label, names] of Object.entries(JSON.parse(casesJSON))) {
  let sandbox;
  try {
    sandbox = publishedSandbox(placement, { ...options, globals: names.map(name => ({ name, execute })) });
    rejected[label] = null;
  } catch (error) {
    rejected[label] = error.message;
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
