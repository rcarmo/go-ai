import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

type Receipt = { commit: string; file: string; heading: string };
type NativeTest = { file: string; test: string; receipt?: string };
type Manifest = {
	kind: "manifest"; schema: 1; lane: string; pin: string; base: string;
	files: Record<string, string>; receipts: Record<string, Receipt>; nativeTests: Record<string, NativeTest>;
};
type Case = {
	kind: "case"; id: string; file: string; name: string; first: number; last: number;
	nameLine?: number; original: string | null; variants?: string[]; restricted?: Record<string, string[]>;
};
type FixtureCase = {
	kind: "fixture-case"; id: string; file: string; runner: string; group: number; vector: number;
	groupName: string; name: string; valid: boolean; original: string | null;
};
type FixtureGroup = { description: string; tests: { description: string; valid: boolean }[] };
type Assertion = {
	kind: "assertion"; id: string; case: string; line: number; expect: string;
	status: "mapped" | "partial" | "candidate" | "unmatched";
	native?: { test: string; anchor: string }; note?: string;
};
type Probe = { kind: "gap" | "supplemental"; id: string; note: string; native?: string; related?: string };
type Row = Manifest | Case | FixtureCase | Assertion | Probe;

const pin = "fb0fe823539f6d04850f6e34748c01f01f212e1f";
const [oracle, mirror] = process.argv.slice(2);
if (!oracle) throw Error("Usage: bun run docs/upstream-v1.7.0/assertions/validate.ts <oracle-dir> [pinned-git-mirror]");
const git = (cwd: string, ...args: string[]) => execFileSync("git", args, { cwd });
const root = git(import.meta.dir, "rev-parse", "--show-toplevel").toString().trim();
const hash = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const sha = /^[0-9a-f]{40}$/;
// Include direct assertions guarded by a simple inline condition or loop.
const expectLine = /^\s*(?:(?:if|for)\s*\([^()\n]*\)\s*)?(?:await\s+)?expect\(/;
const conformanceRunner = "packages/schema/test/conformance.test.ts";
const conformanceChecks = [40, 45];
const testLine = /^\s*it(?:\(|\.each(?:<\[[^\n>]+\]>)?\(|\.skipIf\()/;
function requireCheck(ok: unknown, message: string): asserts ok {
	if (!ok) throw Error(message);
}

function requireReachable(commit: string, label: string) {
	const ancestry = spawnSync("git", ["merge-base", "--is-ancestor", commit, "HEAD"], { cwd: root });
	requireCheck(ancestry.status === 0, `${label}: ${commit} is not in the current branch history`);
}

let total = 0;
const globalIds = new Set<string>();
const maps = readdirSync(import.meta.dir).filter(name => name.endsWith(".jsonl")).sort();
requireCheck(maps.length > 0, "no assertion ledgers");
for (const name of maps) {
	const [manifest, ...rows] = readFileSync(join(import.meta.dir, name), "utf8").trimEnd().split("\n").map(line => JSON.parse(line) as Row);
	requireCheck(manifest?.kind === "manifest" && manifest.schema === 1 && manifest.pin === pin && sha.test(manifest.base), `${name}: manifest`);
	requireReachable(manifest.base, `${name}: base`);
	const sources = new Map<string, string[]>();
	for (const [file, digest] of Object.entries(manifest.files)) {
		const bytes = readFileSync(join(oracle, file));
		requireCheck(hash(bytes) === digest, `${file}: Oracle bytes differ`);
		if (mirror) requireCheck(bytes.equals(git(mirror, "show", `${pin}:${file}`)), `${file}: pinned git bytes differ`);
		sources.set(file, bytes.toString().split("\n"));
	}
	for (const [id, receipt] of Object.entries(manifest.receipts)) {
		requireCheck(sha.test(receipt.commit), `${name}:${id}: receipt commit`);
		requireReachable(receipt.commit, `${name}:${id}: receipt`);
		const doc = git(root, "show", `${receipt.commit}:${receipt.file}`).toString().split("\n");
		requireCheck(doc.some(line => line.replace(/^#+ /, "") === receipt.heading), `${name}:${id}: receipt heading absent`);
	}
	const nativeBodies = new Map<string, string>();
	for (const [id, test] of Object.entries(manifest.nativeTests)) {
		const topLevel = test.test.split("/")[0];
		requireCheck(/^Test\w+$/.test(topLevel) && test.file.endsWith("_test.go"), `${name}:${id}: invalid native test`);
		const body = readFileSync(join(root, test.file), "utf8");
		requireCheck(body.includes(`func ${topLevel}(`), `${name}:${id}: native test absent`);
		if (test.receipt !== undefined) requireCheck(manifest.receipts[test.receipt], `${name}:${id}: unknown native receipt`);
		nativeBodies.set(id, body);
	}
	const cases = new Map<string, { test: Case | FixtureCase; lines: Set<number> }>();
	const fixtures = new Map<string, FixtureGroup[]>();
	const seenLocators = new Set<string>();
	const status = { mapped: 0, partial: 0, candidate: 0, unmatched: 0 };
	for (const row of rows) {
		requireCheck("id" in row && typeof row.id === "string" && !globalIds.has(row.id), `${name}: duplicate or missing ID`);
		globalIds.add(row.id);
		if (row.kind === "case") {
			const lines = sources.get(row.file);
			requireCheck(lines && Number.isInteger(row.first) && Number.isInteger(row.last) && row.first >= 1 && row.first < row.last && row.last <= lines.length, `${row.id}: invalid source span`);
			const declaration = row.nameLine ?? row.first;
			const nameSource = lines[declaration - 1] ?? "";
			// Template names are matched as written; interpolation is never evaluated.
			const nameMatches = nameSource.includes(`"${row.name}"`) || nameSource.includes("`" + row.name + "`");
			requireCheck(declaration >= row.first && declaration <= row.last && testLine.test(lines[row.first - 1]) && nameMatches, `${row.id}: source test declaration differs`);
			requireCheck(!lines.slice(row.first, row.last).some(line => testLine.test(line)), `${row.id}: span contains another case`);
			requireCheck(row.original === null || manifest.receipts[row.original], `${row.id}: unknown original receipt`);
			const expected = new Set<number>();
			for (let line = row.first; line <= row.last; line++) if (expectLine.test(lines[line - 1])) expected.add(line);
			requireCheck(expected.size > 0, `${row.id}: no assertion sites`);
			if (row.variants) requireCheck(row.variants.length > 1 && row.variants.length === new Set(row.variants).size, `${row.id}: invalid variants`);
			for (const [line, variants] of Object.entries(row.restricted ?? {})) {
				requireCheck(expected.has(Number(line)) && variants.length > 0 && variants.every(v => row.variants?.includes(v)), `${row.id}: invalid variant restriction`);
			}
			cases.set(row.id, { test: row, lines: expected });
		} else if (row.kind === "fixture-case") {
			const source = sources.get(row.file);
			const runner = sources.get(row.runner);
			requireCheck(source && row.runner === conformanceRunner && runner, `${row.id}: unresolved fixture or conformance runner`);
			requireCheck(Number.isSafeInteger(row.group) && row.group >= 0 && Number.isSafeInteger(row.vector) && row.vector >= 0, `${row.id}: invalid fixture indices`);
			if (!fixtures.has(row.file)) fixtures.set(row.file, JSON.parse(source.join("\n")) as FixtureGroup[]);
			const group = fixtures.get(row.file)?.[row.group];
			const vector = group?.tests[row.vector];
			requireCheck(group && vector && group.description === row.groupName && vector.description === row.name && typeof row.valid === "boolean" && vector.valid === row.valid, `${row.id}: fixture group, case or validity differs`);
			requireCheck(row.original === null || manifest.receipts[row.original], `${row.id}: unknown original receipt`);
			cases.set(row.id, { test: row, lines: new Set(conformanceChecks) });
		} else if (row.kind === "assertion") {
			const entry = cases.get(row.case);
			requireCheck(row.id === `${row.case}#L${row.line}` && entry?.lines.delete(row.line), `${row.id}: unexpected or duplicate assertion`);
			const test = entry.test;
			const fixture = test.kind === "fixture-case";
			const locator = fixture ? `${test.file}[${test.group}].tests[${test.vector}]:${row.line}` : `${test.file}:${row.line}`;
			const source = sources.get(fixture ? test.runner : test.file)?.[row.line - 1];
			requireCheck(!seenLocators.has(locator) && source && expectLine.test(source) && source.trim() === row.expect, `${row.id}: duplicate or stale source locator`);
			seenLocators.add(locator);
			requireCheck(Object.hasOwn(status, row.status), `${row.id}: unknown status`);
			if (row.status === "unmatched") requireCheck(!row.native, `${row.id}: unmatched row claims native evidence`);
			else {
				requireCheck(row.native?.anchor && nativeBodies.get(row.native.test)?.includes(row.native.anchor), `${row.id}: native assertion anchor absent`);
				if (row.status === "mapped" || row.status === "partial") requireCheck(manifest.nativeTests[row.native.test].receipt, `${row.id}: missing native run receipt`);
				if (row.status !== "mapped") requireCheck(row.note, `${row.id}: specify the evidence gap`);
			}
			status[row.status]++;
			total++;
		} else {
			requireCheck((row.kind === "gap" || row.kind === "supplemental") && row.note, `${row.id}: invalid row`);
			if (row.native) requireCheck(nativeBodies.has(row.native), `${row.id}: unknown native probe`);
			if (row.related) requireCheck(cases.has(row.related), `${row.id}: unknown related case`);
		}
	}
	for (const [id, entry] of cases) requireCheck(entry.lines.size === 0, `${id}: unrecorded assertion sites ${[...entry.lines]}`);
	console.log(`${name}: ${cases.size} case records, ${Object.values(status).reduce((sum, count) => sum + count, 0)} assertion sites`, status);
}
console.log(`Validated ${total} scoped assertion locators${mirror ? " against pinned git bytes" : " against Oracle files"}. No tests executed.`);
