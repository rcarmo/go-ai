// Exercise the published portable host's typed launch failure before a VM exists.
import { pathToFileURL } from 'node:url';
import { createInterface } from 'node:readline';
const packageURL = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { serveCodemodeRemote } = await import(new URL('portable.js', packageURL));
const lines = createInterface({ input: process.stdin });
let answer;
let running;
for await (const line of lines) {
  const frame = JSON.parse(line);
  if (frame.request && !running) {
    running = serveCodemodeRemote(frame.request, messages => new Promise(resolve => {
      if (answer) throw new Error('unexpected second launch-failure frame');
      answer = resolve;
      process.stdout.write(JSON.stringify({ id: 1, messages }) + '\n');
    }), { wasm: Promise.reject(new Error('no wasm')), maxInterruptBudget: 100000, maxMemoryLimitBytes: 33554432 });
    running.then(() => { process.stdout.write('{"settled":true}\n'); lines.close(); process.stdin.destroy(); });
  } else {
    if (!answer || frame.id !== 1) throw new Error('unexpected launch-failure reply');
    answer(frame.reply);
  }
}
await running;
