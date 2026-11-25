import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_FINGERPRINT_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_FINGERPRINT_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/contracts/fingerprint.ts", "materialize"],
	["packages/chord/src/json.ts", "isJsonValue"],
]);
const vm = createRequire(import.meta.url)("node:vm");
const evaluate = vm.runInThisContext;
vm.runInThisContext = function (code, options) {
	const run = Reflect.apply(evaluate, this, [code, options]);
	if (typeof options?.filename !== "string") return run;
	const filename = options.filename.startsWith("file:") ? fileURLToPath(options.filename) : options.filename;
	if (!filename.startsWith(root)) return run;
	const file = filename.slice(root.length);
	const binding = expected.get(file);
	if (!binding) return run;
	if (typeof run !== "function" || !new Error().stack?.includes("/node_modules/vitest/dist/module-evaluator.js:")) {
		throw Error(`Unexpected evaluator for ${file}`);
	}
	return async function (...args) {
		const result = await Reflect.apply(run, this, args);
		const value = args[0]?.[binding];
		if (typeof value !== "function") throw Error(`Missing evaluated Chord export: ${binding}`);
		record({ kind: "source-evaluated", api: "node:vm runInThisContext", file, export: binding });
		if (process.env.CHORD_FINGERPRINT_PROBE_PHASE !== "original") observeBinding(args[0], binding);
		return result;
	};
};
syncBuiltinESMExports();

function observeBinding(exports, binding) {
	const descriptor = Object.getOwnPropertyDescriptor(exports, binding);
	if (!descriptor?.configurable || !descriptor.get) throw Error(`Unexpected evaluator export: ${binding}`);
	const forwarded = new Proxy(exports[binding], {
		apply(fn, receiver, args) {
			const result = Reflect.apply(fn, receiver, args);
			if (binding === "materialize") record({ kind: "fingerprint", value: result.fingerprint });
			if (binding === "isJsonValue" && args[0] === "T") {
				record({ kind: "json-guard", value: args[0], valid: result });
			}
			return result;
		},
	});
	Object.defineProperty(exports, binding, { ...descriptor, get: () => forwarded });
}
