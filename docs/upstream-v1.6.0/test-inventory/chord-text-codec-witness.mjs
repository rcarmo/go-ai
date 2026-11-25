import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_CODEC_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_CODEC_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/link/message.ts", "checkMessage"],
	["packages/schema/src/validator.ts", "compile"],
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
		if (binding === "compile" && process.env.CHORD_CODEC_PROBE_PHASE !== "original") observeCompiler(args[0]);
		return result;
	};
};
syncBuiltinESMExports();

// Forward the actual compiler and validator. Record only the pinned case's
// decoded value and exact decision, with no modification to either.
function observeCompiler(exports) {
	const descriptor = Object.getOwnPropertyDescriptor(exports, "compile");
	if (!descriptor?.configurable || !descriptor.get) throw Error("Unexpected evaluator compile export");
	const compile = new Proxy(exports.compile, {
		apply(fn, receiver, args) {
			const validator = Reflect.apply(fn, receiver, args);
			const check = validator.check;
			validator.check = function (value) {
				const valid = Reflect.apply(check, this, [value]);
				if (value?.kind === "call" && value.call === 0 && value.hold === 1 && value.method === 2 && value.old === 1) {
					record({ kind: "schema-decision", api: "pi-schema compile().check", decoded: value, valid });
				}
				return valid;
			};
			return validator;
		},
	});
	Object.defineProperty(exports, "compile", { ...descriptor, get: () => compile });
}
