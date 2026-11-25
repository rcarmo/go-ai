import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";
import { DatabaseSync } from "node:sqlite";
const root = `${process.env.CHORD_SERIES_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_SERIES_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/store/store.ts", "ChordStore"],
	["packages/chord/src/sqlite/node.ts", "NodeSqliteDatabase"],
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
		return result;
	};
};
syncBuiltinESMExports();

// Probe only the targeted original. Forward its real SQLite calls unchanged,
// and count rows on the same connection at the failure and transaction end.
if (process.env.CHORD_SERIES_PROBE_PHASE !== "original") {
	const exec = DatabaseSync.prototype.exec;
	const prepare = DatabaseSync.prototype.prepare;
	const begun = new WeakMap();
	const failed = new WeakMap();
	const count = db => Reflect.apply(prepare, db, ["SELECT count(*) AS n FROM chord_documents"]).get().n;
	DatabaseSync.prototype.prepare = function (sql) {
		try {
			return Reflect.apply(prepare, this, [sql]);
		} catch (error) {
			if (sql === "INSERT INTO no_such_table VALUES (1)") {
				failed.set(this, { sql, error: error.message, documentsBeforeEnd: count(this) });
			}
			throw error;
		}
	};
	DatabaseSync.prototype.exec = function (sql) {
		const result = Reflect.apply(exec, this, [sql]);
		if (sql === "BEGIN IMMEDIATE" || sql === "BEGIN") begun.set(this, sql);
		if (sql === "ROLLBACK" || sql === "COMMIT") {
			const failure = failed.get(this);
			if (failure) {
				const engine = Reflect.apply(prepare, this, ["SELECT sqlite_version() AS version, sqlite_source_id() AS sourceId"]).get();
				record({ kind: "failed-transaction", api: "node:sqlite DatabaseSync", begin: begun.get(this), ...failure,
					end: sql, documentsAfterEnd: count(this), engine });
				failed.delete(this);
			}
			begun.delete(this);
		}
		return result;
	};
}
