import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { createRequire, registerHooks, syncBuiltinESMExports } from "node:module";
import { pathToFileURL } from "node:url";
import { isMainThread, workerData } from "node:worker_threads";

const root = pathToFileURL(`${process.env.WORKER_QUICKJS_ROOT}/`).href;
const workerEntry = "packages/codemode/src/runtime/worker.ts";
const loader = "packages/codemode/src/wasm.ts";
const sdk = "node_modules/quickjs-wasi/dist/index.js";
const digest = bytes => ({ bytes: bytes.byteLength, sha256: createHash("sha256").update(bytes).digest("hex") });
const record = event => appendFileSync(process.env.WORKER_QUICKJS_TRACE, JSON.stringify(event) + "\n");
const isGuest = !isMainThread && workerData?.wasm instanceof WebAssembly.Module;
if (isGuest) {
	registerHooks({
		load(url, context, next) {
			const loaded = next(url, context);
			if (url === root + workerEntry || url === root + sdk) {
				if (loaded.source == null) throw Error(`Worker ESM source unavailable: ${url}`);
				record({ kind: "load-guest-js", entry: url.slice(root.length), format: loaded.format, ...digest(Buffer.from(loaded.source)) });
			}
			return loaded;
		},
	});
	const instantiate = WebAssembly.instantiate;
	WebAssembly.instantiate = async function (module, ...options) {
		const caller = new Error().stack?.includes(`${root}${sdk}:`);
		const result = await Reflect.apply(instantiate, this, [module, ...options]);
		if (caller) {
			if (module !== workerData.wasm || !(result instanceof WebAssembly.Instance)) throw Error("Unlinked worker QuickJS instantiation");
			record({ kind: "instantiate-worker-data", api: "WebAssembly.instantiate", caller: sdk });
		}
		return result;
	};
}

if (isMainThread) {
	const compiled = new WeakMap();
	const compile = WebAssembly.compile;
	WebAssembly.compile = async function (bytes, ...options) {
		const input = new Error().stack?.includes(`/${loader}:`) ? digest(bytes) : undefined;
		const module = await Reflect.apply(compile, this, [bytes, ...options]);
		if (input !== undefined) {
			compiled.set(module, input);
			record({ kind: "compile-wasm", api: "WebAssembly.compile", caller: loader, ...input });
		}
		return module;
	};
	const threads = createRequire(import.meta.url)("node:worker_threads");
	threads.Worker = new Proxy(threads.Worker, {
		construct(Worker, args, newTarget) {
			const matches = args[0] instanceof URL && args[0].href === root + workerEntry;
			const input = matches ? compiled.get(args[1]?.workerData?.wasm) : undefined;
			if (matches && input === undefined) throw Error("Worker did not receive the observed QuickJS module");
			const worker = Reflect.construct(Worker, args, newTarget);
			if (matches) {
				record({ kind: "worker-handoff", api: "node:worker_threads Worker", entry: workerEntry, ...input });
				worker.on("message", message => {
					if (message?.type === "done") record({ kind: "worker-result", ok: message.ok, value: message.value });
				});
			}
			return worker;
		},
	});
	syncBuiltinESMExports();
}
