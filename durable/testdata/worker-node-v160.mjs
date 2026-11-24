// Test transport only: run the published entry in a real Node Worker thread
// and forward its messages unchanged to the Go worker adapter over stdio.
import { createInterface } from 'node:readline';
import { pathToFileURL } from 'node:url';
import { Worker } from 'node:worker_threads';

const pkg = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { loadQuickJSWasm } = await import(new URL('wasm.js', pkg));
const lines = createInterface({ input: process.stdin });
const pending = new Set();
const interrupt = new SharedArrayBuffer(4);
let worker, next = 0, terminal, stopping = false;
const send = frame => process.stdout.write(JSON.stringify(frame) + '\n');

for await (const line of lines) {
  const { request, id, reply } = JSON.parse(line);
  if (request) {
    if (worker || request.interruptBudget !== 100000) throw Error('unexpected worker configuration');
    const data = request.data;
    worker = new Worker(new URL('runtime/worker.js', pkg), {
      workerData: { ...data, tools: data.tools ?? [], wasm: await loadQuickJSWasm(), interrupt },
    });
    worker.on('message', message => {
      const id = ++next;
      if (message.type === 'done' || message.type === 'crash') terminal = id;
      pending.add(id);
      send({ id, messages: [message] });
    });
    worker.on('exit', code => {
      if (!stopping) throw Error(`worker exited before acknowledgement (code ${code})`);
    });
  } else {
    if (!pending.delete(id)) throw Error('unexpected host reply');
    if (id === terminal) {
      stopping = true;
      Atomics.store(new Int32Array(interrupt), 0, 1);
      await worker.terminate();
      send({ settled: true });
      lines.close();
      process.stdin.destroy();
      break;
    }
    if (reply?.type === 'result') worker.postMessage(reply);
    else if (reply && reply.type !== 'stop') throw Error(`unexpected host reply type ${reply.type}`);
  }
}
if (!stopping) throw Error('host closed before acknowledging the worker result');
