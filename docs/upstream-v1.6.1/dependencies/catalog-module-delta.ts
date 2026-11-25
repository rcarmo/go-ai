import { pathToFileURL } from "node:url";

if (process.argv.length !== 4) throw Error("Usage: catalog-module-delta.ts <verified-v1.6.0-module> <verified-v1.6.1-module>");
const [previous, current] = await Promise.all(process.argv.slice(2).map(file => import(pathToFileURL(file).href)));

function canonical(value: unknown): unknown {
	if (Array.isArray(value)) return value.map(canonical);
	if (value !== null && typeof value === "object") {
		const record = value as Record<string, unknown>;
		return Object.fromEntries(Object.keys(record).sort().map(key => [key, canonical(record[key])]));
	}
	return value;
}
function flatten(root: Record<string, Record<string, unknown>>) {
	return new Map(Object.entries(root).flatMap(([provider, models]) => Object.entries(models).map(([id, value]) => [
		JSON.stringify([provider, id]), { provider, value: JSON.stringify(canonical(value)) },
	] as const)));
}
for (const kind of ["MODELS", "IMAGE_MODELS", "CLASSIFIER_MODELS"]) {
	const before = flatten(previous[kind]), after = flatten(current[kind]);
	const counts = { added: 0, removed: 0, changed: 0 };
	const providers = new Set<string>();
	for (const [id, entry] of after) {
		const old = before.get(id);
		if (old?.value === entry.value) continue;
		counts[old ? "changed" : "added"]++;
		providers.add(entry.provider);
	}
	for (const [id, entry] of before) {
		if (!after.has(id)) {
			counts.removed++;
			providers.add(entry.provider);
		}
	}
	console.log(JSON.stringify({ kind, previous: before.size, current: after.size, ...counts, changedProviders: [...providers].sort() }));
}
