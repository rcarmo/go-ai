import assert from "node:assert/strict";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [mode, extra] = Bun.argv.slice(2);
if ((mode !== "--write" && mode !== "--check") || extra) throw Error("Usage: bun run test-crosswalk.ts --write|--check");
const here = import.meta.dir;
const files = readFileSync(join(here, "test-inventory.tsv"), "utf8").trimEnd().split("\n").slice(1).map(line => line.split("\t"));
const knownPaths = new Set(files.map(([file]) => file));
const counts = new Map<string, number[]>();
const cases = new Map<string, { path: string; vector: string }>();
const seen = new Set<string>();
const index = ["mapped", "partial", "unmatched", "candidate"];
const ledgers = join(here, "assertions");
for (const name of readdirSync(ledgers).filter(file => file.endsWith(".jsonl")).sort()) {
	for (const line of readFileSync(join(ledgers, name), "utf8").trimEnd().split("\n")) {
		const row = JSON.parse(line);
		if (row.kind === "case" || row.kind === "fixture-case") {
			const path = row.kind === "case" ? row.file : row.runner;
			assert(knownPaths.has(path), `${row.id}: test file is outside the inventory`);
			assert(!cases.has(row.id), `${row.id}: duplicate case`);
			const tally = counts.get(path) ?? [0, 0, 0, 0, 0, 0];
			counts.set(path, tally);
			cases.set(row.id, { path, vector: row.kind === "case" ? path : `${row.file}[${row.group}][${row.vector}]` });
			tally[0]++;
			if (row.original !== null) tally[1]++;
		} else if (row.kind === "assertion") {
			const known = cases.get(row.case);
			assert(known, `${row.id}: missing case`);
			const site = `${known.vector}:${row.line}`;
			assert(!seen.has(site), `${row.id}: duplicate source site`);
			seen.add(site);
			const status = index.indexOf(row.status);
			assert(status >= 0, `${row.id}: unknown status`);
			counts.get(known.path)![2 + status]++;
		}
	}
}
const rows = [["upstream_test_file", "delta", "corpus_state", "scoped_cases", "original_cases_with_receipt", "mapped_sites", "partial_sites", "unmatched_sites", "candidate_sites"]];
for (const file of files) rows.push([...file, ...(counts.get(file[0]) ?? [0, 0, 0, 0, 0, 0]).map(String)]);
const output = rows.map(row => row.join("\t")).join("\n") + "\n";
const path = join(here, "test-crosswalk.tsv");
if (mode === "--write") writeFileSync(path, output);
else assert(readFileSync(path, "utf8") === output, "test-crosswalk.tsv is stale; regenerate with --write");
console.log(`${seen.size} scoped assertion sites across ${counts.size} of ${files.length} inventoried paths. No tests executed.`);
