import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_FRAMED_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_FRAMED_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/delta/index.ts", "applyImmutable"],
	["packages/chord/src/state/framed.ts", "FramedSource"],
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
		const exports = args[0];
		if (typeof exports?.[binding] !== "function") throw Error(`Missing evaluated Chord export: ${binding}`);
		record({ kind: "source-evaluated", api: "node:vm runInThisContext", file, export: binding });
		if (binding !== "applyImmutable") return result;

		// Forward the actual function and observe only the pinned case's input.
		const descriptor = Object.getOwnPropertyDescriptor(exports, binding);
		if (!descriptor?.configurable || !descriptor.get) throw Error(`Unexpected evaluator export: ${binding}`);
		const replay = new Proxy(exports[binding], {
			apply(fn, receiver, args) {
				const [before, ops] = args;
				const selected = JSON.stringify(before) === '{"a":1,"b":1,"c":1}' && ops?.length === 3;
				const input = selected ? structuredClone(before) : undefined;
				const output = Reflect.apply(fn, receiver, args);
				if (selected) record({ kind: "delta-replay", before: input, ops, after: output, inputUnchanged: JSON.stringify(before) === JSON.stringify(input) });
				return output;
			},
		});
		Object.defineProperty(exports, binding, { ...descriptor, get: () => replay });
		return result;
	};
};
syncBuiltinESMExports();
