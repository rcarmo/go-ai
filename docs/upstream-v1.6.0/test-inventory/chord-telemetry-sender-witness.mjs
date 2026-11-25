import { appendFileSync } from "node:fs";
import { createRequire, syncBuiltinESMExports } from "node:module";
import { fileURLToPath } from "node:url";

const root = `${process.env.CHORD_SENDER_PROBE_ROOT}/`;
const record = event => appendFileSync(process.env.CHORD_SENDER_PROBE_TRACE, JSON.stringify(event) + "\n");
const expected = new Map([
	["packages/chord/src/telemetry-ingest/sender.ts", "sendWorkerTelemetry"],
	["packages/chord/src/telemetry-ingest/recorder.ts", "TelemetryRecorder"],
	["packages/chord/src/telemetry-ingest/facet.ts", "telemetryIngestFacet"],
	["packages/telemetry/src/memory.ts", "telemetryNow"],
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
		if (typeof exports?.[binding] !== "function") throw Error(`Missing evaluated Telemetry export: ${binding}`);
		record({ kind: "source-evaluated", api: "node:vm runInThisContext", file, export: binding });
		if (binding !== "telemetryIngestFacet") return result;

		// Forward the actual target call and retain only stable fields. The
		// fixture's original target still receives the complete batch unchanged.
		const descriptor = Object.getOwnPropertyDescriptor(exports, binding);
		if (!descriptor?.configurable || !descriptor.get) throw Error(`Unexpected evaluator export: ${binding}`);
		const facet = new Proxy(exports[binding], {
			apply(fn, receiver, [options]) {
				const target = options.target;
				return Reflect.apply(fn, receiver, [{ ...options, target: { ...target, ingest(batch) {
					const accepted = Reflect.apply(target.ingest, target, [batch]);
					record({ kind: "ingest", spanNames: batch.spans.map(({ name }) => name),
						hasMetrics: batch.metrics.length > 0,
						fixtureMetrics: batch.metrics.filter(({ name }) => name === "runs" || name === "clicks")
							.map(({ name, sum }) => ({ name, sum })), accepted,
					});
					return accepted;
				} } }]);
			},
		});
		Object.defineProperty(exports, binding, { ...descriptor, get: () => facet });
		return result;
	};
};
syncBuiltinESMExports();
