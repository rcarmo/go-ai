import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_GETTER_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_GETTER_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/host/host.ts", "createHost"],
	["packages/chord/src/context/index.ts", "withCancel"],
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
		if (binding === "withCancel") observeContext(args[0]);
		return result;
	};
};
syncBuiltinESMExports();

function observeContext(exports) {
	const descriptor = Object.getOwnPropertyDescriptor(exports, "withCancel");
	if (!descriptor?.configurable || !descriptor.get || typeof exports.withAbortSignal !== "function") {
		throw Error("Unexpected Context exports");
	}
	const withCancel = new Proxy(exports.withCancel, {
		apply(fn, receiver, args) {
			const scope = Reflect.apply(fn, receiver, args);
			const parent = args[0];
			return { ...scope, cancel(reason) {
				const result = scope.cancel(reason);
				if (reason?.message === "cancelled") {
					const signal = scope.context.abortSignal;
					record({ kind: "context-cancelled", api: "withCancel -> withAbortSignal",
						supplied: { name: reason.name, message: reason.message }, childAborted: signal.aborted,
						parentAborted: parent.abortSignal?.aborted ?? false, sameReason: signal.reason === reason,
						observed: { name: signal.reason.name, message: signal.reason.message } });
				}
				return result;
			} };
		},
	});
	Object.defineProperty(exports, "withCancel", { ...descriptor, get: () => withCancel });
}
