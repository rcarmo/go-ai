// Exercise the published standalone registry; the Go application registry
// is intentionally not used to emulate duplicate registration or removal.
import { pathToFileURL } from 'node:url';

const pkg = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { CodemodeSandbox, InlineCodemodeSandbox, RemoteCodemodeSandbox, serveCodemodeRemote, loadQuickJSWasm } = await import(new URL('index.js', pkg));
const [placement, firstSource, secondSource] = process.argv.slice(2);
const options = { timeoutMs: 10000, memoryLimitBytes: 33554432 };
const sandbox = placement === 'node-worker' ? new CodemodeSandbox(options)
  : placement === 'inline' ? new InlineCodemodeSandbox({ ...options, wasm: loadQuickJSWasm(), interruptBudget: 20000 })
  : new RemoteCodemodeSandbox({
    ...options,
    remote: () => ({ execute: (request, exchange) => serveCodemodeRemote(
      structuredClone(request), async messages => structuredClone(await exchange(structuredClone(messages))),
      { wasm: loadQuickJSWasm(), maxInterruptBudget: 20000 },
    ) }),
  });
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
