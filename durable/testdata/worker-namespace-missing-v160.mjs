import { publishedSandbox } from './published-sandbox-v160.mjs';

const [placement, missing, existing] = process.argv.slice(2);
let callbacks = 0;
const execute = () => { callbacks++; return 'callback'; };
const sandbox = publishedSandbox(placement, {
  timeoutMs: 10000,
  memoryLimitBytes: 33554432,
  globals: ['models.classify', 'models.generateImages'].map(name => ({ name, execute })),
});
try {
  const miss = await sandbox.execute(missing);
  const afterMiss = callbacks;
  const hit = await sandbox.execute(existing);
  process.stdout.write(JSON.stringify({ miss, hit, afterMiss, callbacks }));
} finally {
  await sandbox.close();
}
