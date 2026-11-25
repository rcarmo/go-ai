import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { DatabaseSync } from "node:sqlite";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_READ_PLAN_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_READ_PLAN_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/sqlite/node.ts", "openNodeSqliteDatabase"],
	["packages/chord/src/store/read-plan.ts", "ReadPlan"],
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
		if (typeof args[0]?.[binding] !== "function") throw Error(`Missing evaluated Chord export: ${binding}`);
		record({ kind: "source-evaluated", api: "node:vm runInThisContext", file, export: binding });
		return result;
	};
};

// Forward the real statement and observe the one joined read-plan assertion.
const prepare = DatabaseSync.prototype.prepare;
DatabaseSync.prototype.prepare = function (sql) {
	const statement = Reflect.apply(prepare, this, [sql]);
	if (!sql.startsWith("EXPLAIN QUERY PLAN ") || !sql.includes(" CROSS JOIN ")) return statement;
	const db = this;
	const all = statement.all;
	Object.defineProperty(statement, "all", {
		value: function (...params) {
			const result = Reflect.apply(all, this, params);
			if (params.at(-1) === 51) {
				const sqlite = Reflect.apply(prepare, db, ["SELECT sqlite_version() AS version, sqlite_source_id() AS sourceId"]).get();
				record({ kind: "explain-plan", api: "node:sqlite StatementSync.all", sql, params,
					steps: result.map(({ detail }) => detail), engine: sqlite });
			}
			return result;
		},
	});
	return statement;
};
syncBuiltinESMExports();
