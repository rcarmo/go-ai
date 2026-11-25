// Execute the published host API, rather than manufacturing an abort in the
// raw message transport. Read the callback flag before closing the sandbox.
import { pathToFileURL } from 'node:url';

const pkg = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { CodemodeSandbox, RemoteCodemodeSandbox, serveCodemodeRemote, loadQuickJSWasm } = await import(new URL('index.js', pkg));
const [placement, source] = process.argv.slice(2);
let aborted = false;
let called = 0;
const options = {
  timeoutMs: 10000,
  memoryLimitBytes: 33554432,
  tools: [{ name: 'slow', execute: (_args, { signal }) => new Promise((_resolve, reject) => {
    called++;
    signal.addEventListener('abort', () => {
      aborted = true;
      reject(new Error('aborted'));
    }, { once: true });
  }) }],
};
const sandbox = placement === 'node-worker' ? new CodemodeSandbox(options) : new RemoteCodemodeSandbox({
  ...options,
  remote: () => ({ execute: (request, exchange) => serveCodemodeRemote(
    structuredClone(request), async messages => structuredClone(await exchange(structuredClone(messages))),
    { wasm: loadQuickJSWasm(), maxInterruptBudget: 100000 },
  ) }),
});
try {
  const result = await sandbox.execute(source);
  process.stdout.write(JSON.stringify({ ...result, aborted, called }));
} finally {
  await sandbox.close();
}
