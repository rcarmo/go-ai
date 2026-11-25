import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_PENDING_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_PENDING_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/remote/provider.ts", "ProviderSide"],
	["packages/chord/src/delta/pending.ts", "PendingOps"],
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
		if (binding === "PendingOps") observeQueue(value);
		return result;
	};
};
syncBuiltinESMExports();

// Forward both operations and keep only scalar replacement values. Match
// the queue for the pinned count fixture, not the notes array or other ops.
function observeQueue(PendingOps) {
	const pending = new WeakMap();
	const push = PendingOps.prototype.push;
	PendingOps.prototype.push = function (ops) {
		const result = Reflect.apply(push, this, [ops]);
		const op = ops[0];
		if (ops.length === 1 && op[0] === "r" && [2, 3, 4].includes(op[1])) {
			const values = pending.get(this) ?? [];
			values.push(op[1]);
			pending.set(this, values);
		}
		return result;
	};
	const take = PendingOps.prototype.take;
	PendingOps.prototype.take = function () {
		const result = Reflect.apply(take, this, []);
		const values = pending.get(this);
		pending.delete(this);
		if (JSON.stringify(values) === "[2,3,4]") record({ kind: "queue-take", pushed: values, result });
		return result;
	};
}
