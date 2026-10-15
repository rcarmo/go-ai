// Opt-in integration fixture: load the exact installed official 1.2.0 package.
import { pathToFileURL } from 'node:url';
import { createInterface } from 'node:readline';
const packageURL = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { serveCodemodeRemote } = await import(new URL('portable.js', packageURL));
const { loadQuickJSWasm } = await import(new URL('wasm.js', packageURL));
const wasm = await loadQuickJSWasm();
let next = 0;
const pending = new Map();
const lines = createInterface({ input: process.stdin });
let running;
for await (const line of lines) {
  const frame = JSON.parse(line);
  if (frame.request) {
    running = serveCodemodeRemote(frame.request, messages => new Promise(resolve => {
      const id = ++next;
      pending.set(id, resolve);
      process.stdout.write(JSON.stringify({ id, messages }) + '\n');
    }), { wasm, maxInterruptBudget: Number(process.env.GOAI_WORKER_MAX_BUDGET ?? 100000), maxMemoryLimitBytes: Number(process.env.GOAI_WORKER_MAX_MEMORY ?? 33554432) });
    running.then(() => { process.stdout.write('{"settled":true}\n'); lines.close(); process.stdin.destroy(); });
  } else {
    const resolve = pending.get(frame.id);
    if (!resolve) throw Error('unknown reply');
    pending.delete(frame.id);
    resolve(frame.reply);
  }
}
await running;
