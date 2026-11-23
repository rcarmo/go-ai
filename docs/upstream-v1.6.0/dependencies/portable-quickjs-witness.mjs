import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";

const digest = bytes => ({ bytes: bytes.byteLength, sha256: createHash("sha256").update(bytes).digest("hex") });
const write = event => appendFileSync(process.env.PORTABLE_QUICKJS_TRACE, JSON.stringify(event) + "\n");
const root = pathToFileURL(`${process.env.GOAI_WORKER_PACKAGE_ROOT}/`).href;
const codemode = "node_modules/@earendil-works/pi-codemode/dist/";
const loader = `${codemode}wasm.js`;
const quickjs = "node_modules/quickjs-wasi/dist/index.js";
const loadedEntries = new Set([`${codemode}portable.js`, loader, quickjs]);
registerHooks({
	load(url, context, next) {
		const loaded = next(url, context);
		const entry = url.slice(root.length);
		if (url.startsWith(root) && loadedEntries.has(entry)) {
			if (loaded.format !== "module" || loaded.source == null) throw Error(`ESM source unavailable: ${entry}`);
			write({ kind: "load-js", entry, ...digest(Buffer.from(loaded.source)) });
		}
		return loaded;
	},
});

// Track the actual compiled module through instantiation. The published
// QuickJS wrapper cannot satisfy this probe with an unrelated module.
const compilation = new WeakMap();
const compile = WebAssembly.compile;
WebAssembly.compile = async function (input, ...options) {
	const observed = new Error().stack?.includes(`${root}${loader}:`);
	const bytes = observed ? digest(input) : undefined;
	const module = await Reflect.apply(compile, this, [input, ...options]);
	if (bytes !== undefined) {
		compilation.set(module, bytes);
		write({ kind: "compile-wasm", api: "WebAssembly.compile", caller: loader, ...bytes });
	}
	return module;
};
const instantiate = WebAssembly.instantiate;
WebAssembly.instantiate = async function (input, ...options) {
	const bytes = compilation.get(input);
	const observed = new Error().stack?.includes(`${root}${quickjs}:`);
	const result = await Reflect.apply(instantiate, this, [input, ...options]);
	if (bytes !== undefined) {
		if (!observed || !(result instanceof WebAssembly.Instance)) throw Error("Missing portable QuickJS instance");
		write({ kind: "instantiate-wasm", api: "WebAssembly.instantiate", caller: quickjs, ...bytes });
	}
	return result;
};
