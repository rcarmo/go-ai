import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";
import { isMainThread, parentPort } from "node:worker_threads";

const jsEntry = "node_modules/@silvia-odwyer/photon/photon_rs.js";
const target = pathToFileURL(`${process.env.PHOTON_PROBE_ROOT}/${jsEntry}`).href;
const thread = isMainThread ? "test-thread" : "image-worker";
function digest(bytes) {
	return { bytes: bytes.byteLength, sha256: createHash("sha256").update(bytes).digest("hex") };
}
function record(event) {
	appendFileSync(process.env.PHOTON_RUNTIME_TRACE, JSON.stringify({ thread, ...event }) + "\n");
}
function hasFrame(file) {
	return new Error().stack?.includes(`/${file}:`) ?? false;
}

registerHooks({
	load(url, context, nextLoad) {
		const loaded = nextLoad(url, context);
		if (url === target) {
			if (loaded.format !== "module" || loaded.source == null) throw Error("Photon ESM source unavailable");
			record({ kind: "load-js", api: "node:module load", entry: jsEntry, ...digest(Buffer.from(loaded.source)) });
		}
		return loaded;
	},
});

// The glue constructs its module and instance synchronously. Retain the
// compiler input digest only for the module that successfully instantiates.
const compiled = new WeakMap();
WebAssembly.Module = new Proxy(WebAssembly.Module, {
	construct(constructor, args, newTarget) {
		const input = hasFrame(jsEntry) ? digest(args[0]) : undefined;
		const module = Reflect.construct(constructor, args, newTarget);
		if (input !== undefined) compiled.set(module, input);
		return module;
	},
});
WebAssembly.Instance = new Proxy(WebAssembly.Instance, {
	construct(constructor, args, newTarget) {
		const instance = Reflect.construct(constructor, args, newTarget);
		const input = compiled.get(args[0]);
		if (input !== undefined) {
			const caller = isMainThread ? "packages/durable/test/image-fixtures.ts" : "packages/durable/src/images/node-worker.ts";
			if (!hasFrame(caller)) throw Error(`Missing Photon caller: ${caller}`);
			record({ kind: "instantiate-wasm", api: "WebAssembly.Module -> WebAssembly.Instance", caller, ...input });
		}
		return instance;
	},
});

if (parentPort !== null) {
	const postMessage = parentPort.postMessage;
	parentPort.postMessage = function (message, ...options) {
		const result = Reflect.apply(postMessage, this, [message, ...options]);
		if (hasFrame("packages/durable/src/images/node-worker.ts")) {
			record({ kind: "worker-reply", id: message.id, mimeType: message.result?.mimeType, resized: message.result?.resized });
		}
		return result;
	};
}
