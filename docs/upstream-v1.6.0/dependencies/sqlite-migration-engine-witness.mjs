import { appendFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";

const originalExec = DatabaseSync.prototype.exec;
const prepare = DatabaseSync.prototype.prepare;
DatabaseSync.prototype.exec = function (sql) {
	const result = originalExec.call(this, sql);
	if (sql.startsWith("CREATE TABLE tasks_v2 (")) {
		// Read from the connection that just executed the v2 migration DDL.
		const identity = prepare.call(this, "SELECT sqlite_version() AS version, sqlite_source_id() AS sourceId").get();
		const schema = prepare.call(this, "SELECT version FROM durable_schema WHERE singleton = 1").get();
		appendFileSync(process.env.SQLITE_ENGINE_TRACE, JSON.stringify({
			api: "node:sqlite DatabaseSync.exec",
			node: process.version,
			nodeSQLiteVersion: process.versions.sqlite,
			sqlite: identity,
			schemaVersionAtDDL: schema.version,
		}) + "\n");
	}
	return result;
};
