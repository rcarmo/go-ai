import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_WATCH_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_WATCH_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/state/derive.ts", "Derive"],
	["packages/chord/src/watch.ts", "Watch"],
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
		if (binding === "Watch" && process.env.CHORD_WATCH_PROBE_PHASE !== "original") observeWatch(value);
		return result;
	};
};
syncBuiltinESMExports();

// The targeted original starts one watched derive. Forward all calls, without
// holding source references or modifying delivery, including the busy promise.
function observeWatch(Watch) {
	const started = new WeakSet();
	const render = call => call.kind === "failed" ? { kind: "failed", error: call.error.message } : call;
	const start = Watch.prototype.start;
	Watch.prototype.start = function (listener) {
		started.add(this);
		return Reflect.apply(start, this, [function (call, ...rest) {
			record({ kind: "listener", call: render(call) });
			return Reflect.apply(listener, this, [call, ...rest]);
		}]);
	};
	const advance = Watch.prototype.advance;
	Watch.prototype.advance = function (call, ...rest) {
		if (started.has(this)) record({ kind: "source", call: render(call) });
		return Reflect.apply(advance, this, [call, ...rest]);
	};
	const fail = Watch.prototype.fail;
	Watch.prototype.fail = function (error, ...rest) {
		if (started.has(this)) record({ kind: "source", call: render({ kind: "failed", error }) });
		return Reflect.apply(fail, this, [error, ...rest]);
	};
}
