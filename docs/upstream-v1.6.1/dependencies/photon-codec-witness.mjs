// Preload in Vitest and the image worker. Record inputs only; leave the codec and message payloads untouched.
import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";
import { isMainThread, parentPort } from "node:worker_threads";

const root = process.env.PHOTON_ROOT;
const js = "node_modules/@silvia-odwyer/photon/photon_rs.js";
const glue = pathToFileURL(`${root}/${js}`).href;
const worker = "packages/durable/src/images/node-worker.ts";
const thread = isMainThread ? "vitest-fork" : "worker";
function bytes(value) {
	return { bytes: value.byteLength, sha256: createHash("sha256").update(value).digest("hex") };
}
function record(event) {
	appendFileSync(process.env.PHOTON_TRACE, JSON.stringify({ thread, ...event }) + "\n");
}
function from(file) {
	return new Error().stack?.includes(`/${file}:`) ?? false;
}

registerHooks({
	load(url, context, nextLoad) {
		const result = nextLoad(url, context);
		if (url === glue) {
			if (result.format !== "module" || result.source == null) throw Error("Missing Photon ESM source");
			record({ kind: "load-glue", format: result.format, ...bytes(Buffer.from(result.source)) });
		}
		return result;
	},
});

// The glue uses the synchronous constructors; report bytes only after successful instantiation.
const compiled = new WeakMap();
WebAssembly.Module = new Proxy(WebAssembly.Module, {
	construct(target, args, newTarget) {
		const input = from(js) ? bytes(args[0]) : undefined;
		const module = Reflect.construct(target, args, newTarget);
		if (input !== undefined) compiled.set(module, input);
		return module;
	},
});
WebAssembly.Instance = new Proxy(WebAssembly.Instance, {
	construct(target, args, newTarget) {
		const instance = Reflect.construct(target, args, newTarget);
		const input = compiled.get(args[0]);
		if (input !== undefined) {
			const caller = isMainThread ? "packages/durable/test/image-fixtures.ts" : worker;
			if (!from(caller)) throw Error(`Unexpected Photon instantiation: expected ${caller}`);
			record({ kind: "instantiate-wasm", caller, ...input });
		}
		return instance;
	},
});

if (parentPort !== null) {
	const on = parentPort.on;
	parentPort.on = function (event, listener) {
		if (event !== "message" || !from(worker)) return Reflect.apply(on, this, [event, listener]);
		return Reflect.apply(on, this, [event, function (request) {
			record({ kind: "request", id: request.id, mimeType: request.mimeType, ...bytes(request.bytes) });
			return Reflect.apply(listener, this, [request]);
		}]);
	};
	const post = parentPort.postMessage;
	parentPort.postMessage = function (message, ...args) {
		const status = Reflect.apply(post, this, [message, ...args]);
		if (from(worker)) {
			const image = message.result;
			const data = image ? Buffer.from(image.data, "base64") : Buffer.alloc(0);
			record({ kind: "reply", tag: message.kind, id: message.id, mimeType: image?.mimeType,
				convertedFrom: image?.convertedFrom, signature: data.subarray(0, 8).toString("hex"), ...bytes(data) });
		}
		return status;
	};
}
