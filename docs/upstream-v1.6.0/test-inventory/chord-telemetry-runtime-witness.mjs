import { appendFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { fileURLToPath } from "node:url";
import { createRequire, syncBuiltinESMExports } from "node:module";

const exec = DatabaseSync.prototype.exec;
const prepare = DatabaseSync.prototype.prepare;
const migrated = new WeakSet();
DatabaseSync.prototype.exec = function (sql) {
	const result = exec.call(this, sql);
	if (sql.startsWith("CREATE TABLE telemetry_spans (")) {
		if (migrated.has(this)) throw Error("Telemetry schema unexpectedly created twice on one connection");
		migrated.add(this);
		const sqlite = prepare.call(this, "SELECT sqlite_version() AS version, sqlite_source_id() AS sourceId").get();
		appendFileSync(process.env.CHORD_TELEMETRY_TRACE, JSON.stringify({
			api: "node:sqlite DatabaseSync.exec", sqlite, nodeReportedVersion: process.versions.sqlite,
		}) + "\n");
	}
	return result;
};

// Vitest evaluates a transformed module by calling the function returned
// from vm.runInThisContext. Observe the actual evaluation and export.
const vm = createRequire(import.meta.url)("node:vm");
const evaluate = vm.runInThisContext;
const exportsByFile = new Map([
	["packages/telemetry/src/memory.ts", "InMemoryTelemetryContext"],
	["packages/telemetry/src/render.ts", "renderTrace"],
]);
vm.runInThisContext = function (code, options) {
	const run = Reflect.apply(evaluate, this, [code, options]);
	if (typeof options?.filename !== "string") return run;
	const filename = options.filename.startsWith("file:") ? fileURLToPath(options.filename) : options.filename;
	const root = `${process.env.CHORD_PROBE_ROOT}/`;
	if (!filename.startsWith(root)) return run;
	const file = filename.slice(root.length);
	if (!exportsByFile.has(file)) return run;
	if (typeof run !== "function" || !new Error().stack?.includes("/node_modules/vitest/dist/module-evaluator.js:")) {
		throw Error(`Unexpected evaluator for ${file}`);
	}
	const binding = exportsByFile.get(file);
	return async function (...args) {
		const result = await Reflect.apply(run, this, args);
		if (typeof args[0]?.[binding] !== "function") throw Error(`Missing evaluated Telemetry export: ${binding}`);
		appendFileSync(process.env.CHORD_MODULE_TRACE, JSON.stringify({
			api: "node:vm runInThisContext module evaluation", file, export: binding,
		}) + "\n");
		return result;
	};
};
syncBuiltinESMExports();
