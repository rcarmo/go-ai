// Runs only in the verifier's private, offline Node staging tree.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { registerHooks } from "node:module";
import { fileURLToPath } from "node:url";
import { loadPhoton } from "./packages/coding-agent/src/utils/photon.ts";

const main = "node_modules/@silvia-odwyer/photon-node/photon_rs.js";
const mainURL = new URL(main, import.meta.url).href;
const digest = data => ({ bytes: data.byteLength, sha256: createHash("sha256").update(data).digest("hex") });
const trace = [];
const hooks = registerHooks({
	load(url, context, nextLoad) {
		const result = nextLoad(url, context);
		if (url === mainURL) {
			assert.equal(result.format, "commonjs");
			trace.push({ kind: "load-commonjs", member: "photon_rs.js", ...digest(Buffer.from(result.source)) });
		}
		return result;
	},
});

const compiled = new WeakMap();
WebAssembly.Module = new Proxy(WebAssembly.Module, {
	construct(target, args, newTarget) {
		const module = Reflect.construct(target, args, newTarget);
		if (new Error().stack?.includes(`${fileURLToPath(mainURL)}:`)) compiled.set(module, digest(args[0]));
		return module;
	},
});
WebAssembly.Instance = new Proxy(WebAssembly.Instance, {
	construct(target, args, newTarget) {
		const instance = Reflect.construct(target, args, newTarget);
		const bytes = compiled.get(args[0]);
		if (bytes) trace.push({ kind: "instantiate-wasm", member: "photon_rs_bg.wasm", ...bytes });
		return instance;
	},
});

const photon = await loadPhoton();
hooks.deregister();
assert(photon?.PhotonImage);
assert.deepEqual(trace.map(event => event.kind), ["load-commonjs", "instantiate-wasm"]);
const pixels = new Uint8Array([255, 0, 0, 255, 0, 255, 0, 255]);
const image = new photon.PhotonImage(pixels, 2, 1);
try {
	const png = image.get_bytes();
	assert.equal(Buffer.from(png.subarray(0, 8)).toString("hex"), "89504e470d0a1a0a");
	const decoded = photon.PhotonImage.new_from_byteslice(png);
	try {
		assert.equal(decoded.get_width(), 2);
		assert.equal(decoded.get_height(), 1);
		assert.deepEqual(decoded.get_raw_pixels(), pixels);
		console.log(JSON.stringify({ node: process.version, trace, png: { width: 2, height: 1, ...digest(png) } }));
	} finally {
		decoded.free();
	}
} finally {
	image.free();
}
