import { createHash } from "node:crypto";
import { appendFileSync } from "node:fs";

const source = "packages/codemode/src/wasm.ts";
const compile = WebAssembly.compile;
WebAssembly.compile = function (input, ...options) {
	if (!new Error().stack?.includes(`/${source}:`)) return Reflect.apply(compile, this, [input, ...options]);
	const bytes = Buffer.from(input);
	const sha256 = createHash("sha256").update(bytes).digest("hex");
	return Reflect.apply(compile, this, [input, ...options]).then(module => {
		appendFileSync(process.env.WASM_LOAD_TRACE, JSON.stringify({
			source, api: "WebAssembly.compile", bytes: bytes.length, sha256,
		}) + "\n");
		return module;
	});
};
