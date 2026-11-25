import { pathToFileURL } from 'node:url';

const pkg = pathToFileURL(process.env.GOAI_WORKER_PACKAGE_ROOT + '/node_modules/@earendil-works/pi-codemode/dist/');
const { CodemodeSandbox, InlineCodemodeSandbox, RemoteCodemodeSandbox, serveCodemodeRemote, loadQuickJSWasm } = await import(new URL('index.js', pkg));

export function publishedSandbox(placement, options) {
  const budget = 20000;
  if (placement === 'node-worker') return new CodemodeSandbox(options);
  if (placement === 'inline') return new InlineCodemodeSandbox({ ...options, wasm: loadQuickJSWasm(), interruptBudget: budget });
  if (placement === 'portable') return new RemoteCodemodeSandbox({
    ...options,
    remote: () => ({ execute: (request, exchange) => serveCodemodeRemote(
      structuredClone(request), async messages => structuredClone(await exchange(structuredClone(messages))),
      { wasm: loadQuickJSWasm(), maxInterruptBudget: budget },
    ) }),
  });
  throw new Error(`Unknown placement: ${placement}`);
}
