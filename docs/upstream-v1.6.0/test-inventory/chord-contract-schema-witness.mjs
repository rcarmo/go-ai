import { createHash } from "node:crypto";
import { appendFileSync, readFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const root = process.env.CHORD_SCHEMA_PROBE_ROOT;
const trace = process.env.CHORD_SCHEMA_PROBE_TRACE;
const launcher = `${root}/node_modules/typescript/bin/tsc`;
const native = "node_modules/@typescript/typescript-linux-arm64/lib/tsc";
const record = event => appendFileSync(trace, JSON.stringify(event) + "\n");
const digest = file => {
	const bytes = readFileSync(file);
	return { bytes: bytes.length, sha256: createHash("sha256").update(bytes).digest("hex") };
};

const processModule = require("node:child_process");
if (process.argv[1] === launcher) {
	// The archived launcher tries execve, then execFileSync if execve is not
	// available. Record only the path that starts the actual compiler.
	const start = file => {
		if (file !== `${root}/${native}`) throw Error(`Unexpected TypeScript executable: ${file}`);
		return { kind: "compiler-executable", file: native, ...digest(file) };
	};
	if (typeof process.execve === "function") {
		const execve = process.execve;
		process.execve = function (...args) {
			const event = start(args[0]);
			record({ ...event, api: "process.execve" });
			try {
				return Reflect.apply(execve, this, args);
			} catch (error) {
				record({ kind: "execve-refused" });
				throw error;
			}
		};
	}
	const execFileSync = processModule.execFileSync;
	processModule.execFileSync = function (...args) {
		const event = start(args[0]);
		const result = Reflect.apply(execFileSync, this, args);
		record({ ...event, api: "child_process.execFileSync" });
		return result;
	};
} else {
	const spawnSync = processModule.spawnSync;
	processModule.spawnSync = function (file, args, ...options) {
		const result = Reflect.apply(spawnSync, this, [file, args, ...options]);
		if (file !== process.execPath || args?.[0] !== launcher || args[1] !== "-p") return result;
		const directory = args[2];
		if (!directory.startsWith(`${root}/tmp/chord-declarations-`)) throw Error("Unexpected declaration fixture path");
		if (result.status === 0 && result.stdout === "") {
			const config = JSON.parse(readFileSync(join(directory, "tsconfig.json"), "utf8"));
			const declaration = join(directory, "out/contract.d.ts");
			const text = readFileSync(declaration, "utf8");
			record({ kind: "declaration", api: "child_process.spawnSync", status: result.status, config,
				fixture: digest(join(directory, "contract.ts")), declaration: digest(declaration),
				mentionsMethod: text.includes("Method<"), leaksNodeModules: /import\("[^"]*node_modules[^"]*"[^)]*\)\.\w+/.test(text),
			});
		}
		return result;
	};

	const vm = require("node:vm");
	const evaluate = vm.runInThisContext;
	const expected = new Map([
		["packages/schema/src/validator.ts", "compile"],
		["packages/schema/src/checks.ts", "bytesCheck"],
	]);
	vm.runInThisContext = function (code, options) {
		const run = Reflect.apply(evaluate, this, [code, options]);
		if (typeof options?.filename !== "string") return run;
		const filename = options.filename.startsWith("file:") ? fileURLToPath(options.filename) : options.filename;
		if (!filename.startsWith(`${root}/`)) return run;
		const file = filename.slice(root.length + 1);
		const binding = expected.get(file);
		if (!binding) return run;
		if (typeof run !== "function" || !new Error().stack?.includes("/node_modules/vitest/dist/module-evaluator.js:")) {
			throw Error(`Unexpected evaluator for ${file}`);
		}
		return async function (...args) {
			const result = await Reflect.apply(run, this, args);
			if (typeof args[0]?.[binding] !== "function") throw Error(`Missing evaluated Schema export: ${binding}`);
			record({ kind: "source-evaluated", api: "node:vm runInThisContext", file, export: binding });
			return result;
		};
	};
}
syncBuiltinESMExports();
