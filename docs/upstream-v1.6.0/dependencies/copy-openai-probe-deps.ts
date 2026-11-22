import { cpSync, existsSync, lstatSync } from "node:fs";
import { join, relative, sep } from "node:path";

const [graphFile, lockSha, oracle, scratch, libc] = process.argv.slice(2);
if (process.argv.length !== 7) throw Error("Expected graph, lock digest, Oracle, scratch, and host libc");
const rows = (await Bun.file(graphFile).text()).trimEnd().split("\n").map(line => JSON.parse(line));
if (rows[0]?.pin !== "faac585884ae2c013a6b1400f3e9e77f10b34bac" || rows[0]?.inputs?.["package-lock.json"] !== lockSha) throw Error("Invalid pinned closure");
const roots = ["node_modules/vitest", "node_modules/openai", "node_modules/partial-json"].map(path => `npm:${path}`);
const placements = new Map(rows.filter(row => row.kind === "npm").map(row => [row.id, row]));
const edges = rows.filter(row => row.kind === "edge" && row.state === "resolved" && ["runtime", "optional", "peer"].includes(row.class));
const host = { os: process.platform, cpu: process.arch, libc };
function supports(values: string[] | undefined, value: string) {
	if (!values?.length) return true;
	const positive = values.filter(candidate => !candidate.startsWith("!"));
	return !values.includes(`!${value}`) && (!positive.length || positive.includes(value));
}
const visited = new Set<string>(roots);
const copied: string[] = [];
for (const id of visited) {
	const placement = placements.get(id);
	if (!placement || !/^(?:node_modules\/(?:@[\w.-]+\/)?[\w.-]+\/)*node_modules\/(?:@[\w.-]+\/)?[\w.-]+$/.test(placement.lockKey) || placement.lockKey.split("/").some((part: string) => part === "." || part === "..")) throw Error(`Unsupported placement: ${id}`);
	const path = join(oracle, placement.lockKey);
	if (!Object.entries(host).every(([key, value]) => supports(placement.platform[key], value)) || !existsSync(path)) continue;
	if (!lstatSync(path).isDirectory()) throw Error(`Unexpected linked npm placement: ${id}`);
	// Copy exactly this package, not any nested node_modules with unrelated lock slots.
	cpSync(path, join(scratch, placement.lockKey), {
		recursive: true,
		filter: source => !relative(path, source).split(sep).includes("node_modules"),
	});
	copied.push(placement.lockKey);
	for (const edge of edges) if (edge.from === id) visited.add(edge.to);
}
if (!roots.every(id => copied.includes(placements.get(id)?.lockKey))) throw Error("A selected root package is absent");
console.log(JSON.stringify({ roots, host, placements: copied.sort() }));
