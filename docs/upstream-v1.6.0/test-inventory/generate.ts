import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const history = "b4afdddf44e059c67ccf5097e5317d913521b470";
const nativeBase = "ed9f7139ae74a01f99f63988b7467feef2440535";
const [mirror] = process.argv.slice(2);
if (process.argv.length !== 3) throw Error("Usage: bun run generate.ts <pinned-git-mirror>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const git = (repo: string, args: string[], input?: string) => execFileSync("git", ["-C", repo, ...args], { input, maxBuffer: 64 << 20 });
for (const [tag, commit] of [["v1.5.2", history], ["v1.6.0", pin]]) {
	check(git(mirror, ["rev-parse", `refs/tags/${tag}^{commit}`]).toString().trim() === commit, `Unexpected ${tag} pin`);
}
const roots = ["ai", "codemode", "durable", "schema", "chord", "telemetry"].map(pkg => `packages/${pkg}`);
const primary = new Set(roots.slice(0, 4));
const sharedRunnerPaths = ["package-lock.json", "package.json", "pi-test.bat", "pi-test.ps1", "pi-test.sh", "test.sh", "vitest.base.ts"];
const sharedRunner = new Set(sharedRunnerPaths);
const testRoot = /^(?:test|tests|test-[^/]+|__tests__)\//;
const suiteName = /\.(?:test|spec)\.[cm]?[jt]sx?$/;
const testConfig = /^(?:vitest.*\.[cm]?[jt]s|wrangler.*\.(?:toml|jsonc?)|package\.json)$/;
const consumerReference = '(@earendil-works/(pi-(ai|codemode|durable|schema|telemetry)|chord)(/|[^[:alnum:]_-]|$)|\\.\\./(\\.\\./)*(ai|codemode|durable|schema|chord|telemetry)/src/)';
const splitNul = (data: Buffer) => data.toString("utf8").split("\0").filter(Boolean);

type Blob = { mode: string; object: string };
type Role = "suite" | "generated-fixture" | "support" | "asset" | "benchmark" | "example" | "configuration";
type Change = "A" | "M" | "D" | "=";
const tree = (commit: string) => new Map(splitNul(git(mirror, ["ls-tree", "-rz", commit, "--", "apps", "packages", ...sharedRunnerPaths])).map(row => {
	const parsed = /^(\d+) blob ([a-f0-9]{40})\t(.+)$/.exec(row);
	check(parsed, `Unexpected tree row at ${commit}: ${row}`);
	const [, mode, object, path] = parsed;
	return [path, { mode, object }] as const;
}));
const old = tree(history);
const current = tree(pin);
const workspaceOf = (path: string) => path.split("/", 2).join("/");
const relativePath = (path: string) => path.split("/").slice(2).join("/");
const suite = (path: string) => suiteName.test(path) && !/(?:^|\/)(?:fixtures|testdata|examples|bench|benchmarks)\//.test(path);
function adjacentFiles(commit: string) {
	return new Set(splitNul(git(mirror, ["grep", "-lIzE", consumerReference, commit, "--", "apps", "packages"]))
		.map(line => line.slice(commit.length + 1)).filter(path => !roots.includes(workspaceOf(path)) && testRoot.test(relativePath(path)) && suite(path)));
}
const historicalAdjacent = adjacentFiles(history);
const activeAdjacent = adjacentFiles(pin);
const scopedAdjacent = new Set([...activeAdjacent, ...historicalAdjacent]);
const adjacentRoots = [...new Set([...scopedAdjacent].map(workspaceOf))].sort();
function roleFor(local: string): Role {
	if (/^(?:src\/testing\/)/.test(local)) return "support";
	if (/^examples\/|(?:^|\/)examples\//.test(local)) return "example";
	if (/(?:^|\/)(?:bench|benchmarks)\//.test(local) || /\.bench\.[cm]?[jt]sx?$/.test(local)) return "benchmark";
	if (/\/json-schema-test-suite\/tests\/.+\.json$/.test(local)) return "generated-fixture";
	if (suite(local)) return "suite";
	if (/\.(?:[cm]?[jt]sx?|sh|ps1|bat|py)$/.test(local)) return "support";
	return "asset";
}
function classify(path: string): { workspace: string; scope: string; role: Role } | undefined {
	if (sharedRunner.has(path)) return { workspace: "root", scope: "shared-runner", role: "configuration" };
	const workspace = workspaceOf(path);
	const local = relativePath(path);
	if (roots.includes(workspace)) {
		const scope = primary.has(workspace) ? "primary" : "shared-dependency";
		if (testConfig.test(local)) return { workspace, scope, role: "configuration" };
		if (testRoot.test(local) || /^(?:src\/testing\/|bench(?:marks)?\/)/.test(local) || suite(local) ||
			(workspace === "packages/codemode" && local.startsWith("examples/cloudflare/"))) return { workspace, scope, role: roleFor(local) };
	} else if (adjacentRoots.includes(workspace)) {
		if (testConfig.test(local)) return { workspace, scope: "adjacent", role: "configuration" };
		if (scopedAdjacent.has(path)) return { workspace, scope: "adjacent", role: "suite" };
		if (testRoot.test(local) && !suite(local)) return { workspace, scope: "adjacent", role: roleFor(local) };
	}
}
const files = [...new Set([...old.keys(), ...current.keys()])].sort().flatMap(path => {
	const classified = classify(path);
	if (!classified) return [];
	const previous = old.get(path);
	const pinned = current.get(path);
	const change: Change = !previous ? "A" : !pinned ? "D" : previous.object === pinned.object && previous.mode === pinned.mode ? "=" : "M";
	return [{ path, ...classified, change, previous: previous ?? null, pinned: pinned ?? null }];
});
// Cross-check A/M/D against Git's direct diff, not the test-name classifier's assumptions.
const scopedPaths = new Set(files.map(row => row.path));
const directDelta = splitNul(git(mirror, ["diff", "--no-renames", "--name-status", "-z", history, pin, "--", "apps", "packages", ...sharedRunnerPaths]));
const directRows: string[] = [];
for (let i = 0; i < directDelta.length; i += 2) {
	const [change, path] = directDelta.slice(i, i + 2);
	if (scopedPaths.has(path)) directRows.push(`${change}\t${path}`);
}
check(JSON.stringify(directRows.sort()) === JSON.stringify(files.filter(row => row.change !== "=").map(row => `${row.change}\t${row.path}`).sort()), "Scoped diff disagrees with Git name-status");

// Read the exact selected suite blobs with one batch operation. Hints are not test or assertion counts.
const objects = [...new Set(files.filter(row => row.role === "suite" && row.pinned).map(row => row.pinned!.object))].sort();
const bodies = new Map<string, string>();
const batch = git(mirror, ["cat-file", "--batch"], objects.join("\n") + "\n");
let offset = 0;
for (const object of objects) {
	const end = batch.indexOf(0x0a, offset);
	const header = /^([a-f0-9]{40}) blob (\d+)$/.exec(batch.toString("utf8", offset, end));
	check(header?.[1] === object, `Unexpected suite blob ${object}`);
	const next = end + 1 + Number(header[2]);
	check(batch[next] === 0x0a, `Incorrect source length for ${object}`);
	bodies.set(object, batch.toString("utf8", end + 1, next));
	offset = next + 1;
}
check(offset === batch.length, "Trailing suite blob data");

const ledgerRoot = "docs/upstream-v1.6.0/assertions";
const ledgerFiles = splitNul(git(import.meta.dir, ["ls-tree", "-rz", "--full-tree", "--name-only", nativeBase, "--", ledgerRoot])).filter(path => path.endsWith(".jsonl"));
check(ledgerFiles.length > 0, "Pinned native assertion ledgers are absent");
const states = ["candidate", "mapped", "partial", "unmatched"] as const;
type State = typeof states[number];
type Evidence = { ledgers: Set<string>; declarations: number; fixtureVectors: number; runnerVectors: number; originalReceipts: number; assertionRows: Record<State, number> };
const byPath = new Map<string, Evidence>();
const countState = (): Record<State, number> => ({ candidate: 0, mapped: 0, partial: 0, unmatched: 0 });
const evidenceTotals = { declarations: 0, fixtureVectors: 0, originalReceipts: 0, assertionRows: countState() };
function pathEvidence(path: string, ledger: string) {
	check(scopedPaths.has(path) && current.has(path), `Ledger file not in the pinned inventory: ${path}`);
	if (!byPath.has(path)) byPath.set(path, { ledgers: new Set(), declarations: 0, fixtureVectors: 0, runnerVectors: 0, originalReceipts: 0, assertionRows: countState() });
	const result = byPath.get(path)!;
	result.ledgers.add(ledger);
	return result;
}
for (const ledger of ledgerFiles) {
	const [manifest, ...records] = git(import.meta.dir, ["show", `${nativeBase}:${ledger}`]).toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
	check(manifest?.kind === "manifest" && manifest.pin === pin, `Unexpected ledger pin: ${ledger}`);
	for (const [path, digest] of Object.entries(manifest.files)) {
		const blob = current.get(path);
		check(blob && createHash("sha256").update(git(mirror, ["cat-file", "blob", blob.object])).digest("hex") === digest, `Stale ledger source digest: ${ledger}:${path}`);
	}
	const cases = new Map<string, Evidence[]>();
	for (const record of records) {
		if (record.kind === "case" || record.kind === "fixture-case") {
			const entry = pathEvidence(record.file, ledger);
			const field = record.kind === "case" ? "declarations" : "fixtureVectors";
			entry[field]++;
			evidenceTotals[field]++;
			if (record.original) { entry.originalReceipts++; evidenceTotals.originalReceipts++; }
			const links = [entry];
			if (record.kind === "fixture-case") {
				const runner = pathEvidence(record.runner, ledger);
				runner.runnerVectors++;
				if (record.original) runner.originalReceipts++;
				links.push(runner);
			}
			check(!cases.has(record.id), `Duplicate ledger case: ${record.id}`);
			cases.set(record.id, links);
		} else if (record.kind === "assertion") {
			const links = cases.get(record.case);
			check(links && states.includes(record.status), `Unresolved assertion row: ${record.id}`);
			const state = record.status as State;
			evidenceTotals.assertionRows[state]++;
			for (const linked of links) linked.assertionRows[state]++;
		}
	}
}

function hints(path: string, role: Role, body: string) {
	const flags: string[] = [];
	if (role === "generated-fixture" || /\b(?:it|test|describe)\s*\.\s*(?:each|for)\s*\(|\b(?:suiteFiles|register\w*Conformance)\s*\(|\bfor\s*\([^)]*\bof\b[^)]*\)\s*\{[\s\S]{0,220}\b(?:it|test|describe)\s*\(/.test(body)) flags.push("parameterized-or-generated-review");
	if (/(?:^|[-/.])(?:e2e|live|credentialed)(?:[-/.]|$)/.test(path) || /\b(?:hasEnvApiKey|getEnvApiKey|requireApiKey)\s*\(|\bprocess\.env\.[\w]*(?:API_KEY|ACCESS_TOKEN|AUTH_TOKEN|PROJECT_ID)\b/.test(body)) flags.push("live-or-credential-review");
	if (/(?:^|\/)test-workers\/|\/examples\/cloudflare\//.test(path) || /\bprocess\.(?:platform|arch)\b|\b(?:node:sqlite|node:wasi|node:worker_threads|cloudflare:test|workerd|bun:test)\b/.test(body)) flags.push("platform-review");
	if (/\b(?:it|test|describe)\s*\.\s*(?:skip(?:If)?|runIf|fails|todo)\b|\bKNOWN_FAILURES\b/.test(body)) flags.push("gate-or-expected-failure-review");
	return flags;
}
const output = files.map(row => {
	const ledger = byPath.get(row.path);
	const trackedCase = row.role === "suite" || row.role === "generated-fixture";
	const review = row.pinned ? hints(row.path, row.role, bodies.get(row.pinned.object) ?? "") : [];
	const coverage = !row.pinned ? "removed-historical" : ledger ? "ledger-slice" : trackedCase ? "not-in-assertion-ledger" : "support-not-an-assertion-unit";
	return {
		kind: "path", ...row, coverage, review,
		...(row.scope === "adjacent" && row.role === "suite" ? { directReferenceAt: [...(historicalAdjacent.has(row.path) ? ["historical"] : []), ...(activeAdjacent.has(row.path) ? ["pinned"] : [])] } : {}),
		...(ledger ? { ledger: { ...ledger, ledgers: [...ledger.ledgers].sort() } } : {}),
	};
});
const counts = <T>(items: T[], key: (item: T) => string) => Object.fromEntries([...items.reduce((m, item) => m.set(key(item), (m.get(key(item)) ?? 0) + 1), new Map<string, number>())].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0));
const tracked = output.filter(row => row.role === "suite" || row.role === "generated-fixture");
const byWorkspace = Object.fromEntries([...new Set(output.map(row => row.workspace))].sort().map(workspace => {
	const group = output.filter(row => row.workspace === workspace);
	const active = group.filter(row => row.pinned && row.role === "suite");
	return [workspace, { paths: counts(group, row => row.change), suites: counts(active, row => row.coverage), changedUnmappedSuites: active.filter(row => row.change !== "=" && row.coverage === "not-in-assertion-ledger").length }];
}));
console.log(JSON.stringify({ kind: "manifest", format: 1, pin, historicalDeltaBase: history, nativeLedgerBase: nativeBase, roots, sharedRunnerPaths, adjacentRoots, adjacentRule: "pinned-or-historical-direct-test-reference-plus-root-support", consumerReference, ledgerFiles, exclusions: [] }));
for (const row of output) console.log(JSON.stringify(row));
console.log(JSON.stringify({
	kind: "summary", pathsByChange: counts(output, row => row.change), pathsByRole: counts(output, row => row.role),
	suiteAndFixturePathsByChange: counts(tracked, row => row.change), activeSuitesByCoverage: counts(tracked.filter(row => row.pinned && row.role === "suite"), row => row.coverage),
	activeChangedSuitesByCoverage: counts(tracked.filter(row => row.pinned && row.role === "suite" && row.change !== "="), row => row.coverage),
	reviewHintsByType: counts(output.flatMap(row => row.review), value => value), evidenceTotals, byWorkspace,
}));
