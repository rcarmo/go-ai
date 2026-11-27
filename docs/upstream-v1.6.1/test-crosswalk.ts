import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const base = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const pin = "ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe";
const areas = ["ai", "codemode", "durable", "schema"].map(name => `packages/${name}`);
const [mirror, mode, extra] = Bun.argv.slice(2);
if (!mirror || !["--write", "--check"].includes(mode) || extra) {
	throw Error("Usage: bun run test-crosswalk.ts <pinned-git-mirror> --write|--check");
}
const git = (...args: string[]) => execFileSync("git", args, { cwd: mirror, encoding: "utf8" });
const splitNul = (data: string) => data ? data.slice(0, -1).split("\0") : [];
const testPath = (path: string) => areas.some(area => ["test", "test-workers"].some(dir => path.startsWith(`${area}/${dir}/`))) && /\.(test|spec)\.[cm]?[jt]sx?$/.test(path);
const corpus = new Set(splitNul(git("ls-tree", "-rz", "--name-only", pin, "--", ...areas)).filter(testPath));
const changed = new Map<string, string>();
const paths: { status: string; previous: string; path: string }[] = [];
const diff = splitNul(git("diff", "--name-status", "-z", "--find-renames", base, pin));
for (let i = 0; i < diff.length;) {
	const status = diff[i++];
	const old = diff[i++];
	if (status.startsWith("R") || status.startsWith("C")) {
		const next = diff[i++];
		paths.push({status, previous: old, path: next});
		if (testPath(next)) changed.set(next, status);
		if (status.startsWith("R") && testPath(old)) changed.set(old, "renamed-away");
	} else {
		paths.push({status, previous: "", path: old});
		if (testPath(old)) changed.set(old, status);
	}
}
const inventory = paths.map(({status, previous, path}) => [status, ...(previous ? [previous] : []), path].join("\t")).join("\n") + "\n";
if (readFileSync(join(import.meta.dir, "upstream-changes.tsv"), "utf8") !== inventory) {
	throw Error("Changed-path inventory does not match the pinned Git diff");
}

type Evidence = {
	cases: number; originalsWithReceipt: number;
	mapped: number; partial: number; unmatched: number; candidate: number;
};
type Case = { kind: "case"; id: string; file: string; original: string | null };
type Fixture = { kind: "fixture-case"; id: string; file: string; runner: string; group: number; vector: number; original: string | null };
type Assertion = { kind: "assertion"; case: string; line: number; status: "mapped" | "partial" | "candidate" | "unmatched" };
type Row = Case | Fixture | Assertion | { kind: "manifest" | "gap" | "supplemental" };

const empty = (): Evidence => ({ cases: 0, originalsWithReceipt: 0, mapped: 0, partial: 0, unmatched: 0, candidate: 0 });
const sources = new Map<string, Evidence>();
const cases = new Map<string, { key: string; evidence: Evidence }>();
const seen = new Set<string>();
const ledgers = join(import.meta.dir, "assertions");
for (const ledger of readdirSync(ledgers).filter(path => path.endsWith(".jsonl")).sort()) {
	for (const line of readFileSync(join(ledgers, ledger), "utf8").trimEnd().split("\n")) {
		const row = JSON.parse(line) as Row;
		if (row.kind === "case" || row.kind === "fixture-case") {
			const file = row.kind === "case" ? row.file : row.runner;
			if (!corpus.has(file)) throw Error(`${ledger}:${row.id}: test path outside pinned corpus: ${file}`);
			const evidence = sources.get(file) ?? empty();
			sources.set(file, evidence);
			if (cases.has(row.id)) throw Error(`${ledger}:${row.id}: duplicate case`);
			const key = row.kind === "case" ? row.file : `${row.file}[${row.group}][${row.vector}]`;
			cases.set(row.id, { key, evidence });
			evidence.cases++;
			if (row.original !== null) evidence.originalsWithReceipt++;
		} else if (row.kind === "assertion") {
			const found = cases.get(row.case);
			if (!found) throw Error(`${ledger}:${row.case}: assertion without an indexed case`);
			const site = `${found.key}:${row.line}`;
			if (seen.has(site)) throw Error(`${ledger}:${site}: duplicate assertion locator`);
			seen.add(site);
			found.evidence[row.status]++;
		}
	}
}
const files = [...new Set([...corpus, ...changed.keys()])].sort();
const header = "upstream_test_file\tdelta\tledger_state\tscoped_cases\toriginal_cases_with_receipt\tmapped_sites\tpartial_sites\tunmatched_sites\tcandidate_sites";
const states = new Map<string, string>();
const lines = files.map(file => {
	const evidence = sources.get(file) ?? empty();
	const { cases, originalsWithReceipt, mapped, partial, unmatched, candidate } = evidence;
	const state = !corpus.has(file) ? "no-longer-in-corpus"
		: cases === 0 ? "not-indexed"
		: partial + unmatched + candidate > 0 ? "scoped-open-sites" : "scoped-mapped-sites";
	states.set(file, state);
	return [file, changed.get(file) ?? "unchanged", state, cases, originalsWithReceipt, mapped, partial, unmatched, candidate].join("\t");
});
function disposition(path: string): string {
	if (["package.json", "package-lock.json"].includes(path.split("/").at(-1)!)) return "dependency-input";
	const area = areas.find(candidate => path.startsWith(candidate + "/"));
	if (!area) return "outside-four-package-comparison";
	const rest = path.slice(area.length + 1);
	if (testPath(path)) return "upstream-test";
	if (rest.startsWith("test/") || rest.startsWith("test-workers/") || rest.startsWith("vitest")) return "test-support";
	if (rest.startsWith("catalog/") || rest.startsWith("src/providers/data/")) return "catalog-input";
	if (rest.startsWith("src/")) return "runtime-source";
	if (/\.md$/.test(rest)) return "documentation";
	return "package-support";
}
const pathHeader = "delta\tprevious_path\tpath\taudit_disposition\ttest_ledger_state";
const pathLines = paths.map(({status, previous, path}) => [status, previous, path, disposition(path), states.get(path) ?? "-"].join("\t"));
const reports = {
	"test-crosswalk.tsv": [header, ...lines, ""].join("\n"),
	"upstream-disposition.tsv": [pathHeader, ...pathLines, ""].join("\n"),
};
for (const [name, output] of Object.entries(reports)) {
	const destination = join(import.meta.dir, name);
	if (mode === "--write") writeFileSync(destination, output);
	else if (readFileSync(destination, "utf8") !== output) throw Error(`${name} is stale; regenerate with --write`);
}
console.log(`${paths.length} pinned changed-path dispositions.`);
console.log(`${corpus.size} pinned test files, ${sources.size} with scoped ledger entries, ${seen.size} indexed assertion sites. No tests executed.`);
