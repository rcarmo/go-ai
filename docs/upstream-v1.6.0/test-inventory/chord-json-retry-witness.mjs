import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_COPY_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_COPY_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/delta/tracker.ts", "track"],
	["packages/chord/src/json.ts", "copyJson"],
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
		if (binding === "copyJson") observeCopy(args[0]);
		return result;
	};
};
syncBuiltinESMExports();

// Forward the actual copy; record the scalar from the targeted draft insert.
function observeCopy(exports) {
	const descriptor = Object.getOwnPropertyDescriptor(exports, "copyJson");
	if (!descriptor?.configurable || !descriptor.get) throw Error("Unexpected evaluator copyJson export");
	const copy = new Proxy(exports.copyJson, {
		apply(fn, receiver, args) {
			const result = Reflect.apply(fn, receiver, args);
			if (args[0] === "a") record({ kind: "json-copy", input: args[0], output: result });
			return result;
		},
	});
	Object.defineProperty(exports, "copyJson", { ...descriptor, get: () => copy });
}
