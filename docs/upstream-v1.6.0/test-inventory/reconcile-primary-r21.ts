import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const base = "d0f5e86bf5392e4a8027904f888c6e244c2e0284";
const [mirror] = process.argv.slice(2);
if (!mirror || process.argv.length !== 3) throw Error("Usage: bun run reconcile-primary-r21.ts <pinned-git-mirror>");
const root = "docs/upstream-v1.6.0";
const inventory = `${root}/test-inventory`;
const queueFile = `${inventory}/files.jsonl`;
const historicalFile = `${inventory}/primary-queue-reconciled.jsonl`;
const crosswalkFile = `${root}/README.md`;
const sha = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 16 << 20 });
const nativeRepo = git(import.meta.dir, "rev-parse", "--show-toplevel").toString().trim();
const native = (path: string, commit = base) => git(nativeRepo, "show", `${commit}:${path}`);
const lines = (bytes: Buffer) => bytes.toString("utf8").split("\n");
const json = (bytes: Buffer) => JSON.parse(bytes.toString("utf8"));
const jsonl = (bytes: Buffer) => lines(bytes).filter(Boolean).map(line => JSON.parse(line));
const nul = (bytes: Buffer) => bytes.toString("utf8").split("\0").filter(Boolean);
const same = (actual: unknown, expected: unknown) => JSON.stringify(actual) === JSON.stringify(expected);
const sortedUnique = (values: string[]) => [...new Set(values)].sort();
function check(value: unknown, problem: string): asserts value {
	if (!value) throw Error(problem);
}
const committedFiles = (...paths: string[]) => nul(git(nativeRepo, "ls-tree", "-rz", "--name-only", base, "--", ...paths));
const queueBytes = native(queueFile);
const [queueHeader, ...queue] = jsonl(queueBytes);
check(queueHeader.pin === pin, "Wrong inventory pin");
const historicalBytes = native(historicalFile);
const [historicalHeader, ...historical] = jsonl(historicalBytes);
check(historicalHeader.pin === pin, "Wrong historical pin");
const oldByPath = new Map(historical.filter(row => row.kind === "suite").map(row => [row.path, row]));
const changed = queue.filter(row => row.kind === "path" && row.scope === "primary" && row.role === "suite" && ["A", "M"].includes(row.change));
check(changed.length === 42 && oldByPath.size === 37, "Unexpected queue sizes");
check(same(changed.filter(row => row.coverage === "not-in-assertion-ledger").map(row => row.path), [...oldByPath.keys()]), "The historical 37-file queue is not the expected subset");
// Independently enumerate A/M test-name paths in the four primary packages.
const direct = nul(git(mirror, "diff", "--no-renames", "--name-status", "-z", queueHeader.historicalDeltaBase, pin, "--", ...["ai", "codemode", "durable", "schema"].map(name => `packages/${name}`)));
const independentlyChanged: string[] = [];
for (let i = 0; i < direct.length; i += 2) {
	if (["A", "M"].includes(direct[i]) && /\.(?:test|spec)\.[cm]?[jt]sx?$/.test(direct[i + 1])) independentlyChanged.push(`${direct[i]}\t${direct[i + 1]}`);
}
check(same(independentlyChanged.sort(), changed.map(row => `${row.change}\t${row.path}`).sort()), "Primary suite set disagrees with pinned Git diff");

const source = new Map(changed.map(row => {
	const bytes = git(mirror, "show", `${pin}:${row.path}`);
	const blob = git(mirror, "rev-parse", `${pin}:${row.path}`).toString().trim();
	const digest = sha(bytes);
	check(blob === row.pinned.object, `Inventory blob mismatch: ${row.path}`);
	check(!oldByPath.has(row.path) || oldByPath.get(row.path).sourceSha256 === digest, `Historical SHA mismatch: ${row.path}`);
	return [row.path, { blob, sha256: digest, size: bytes.length, lines: lines(bytes) }];
}));
const crosswalkBytes = native(crosswalkFile);
const crosswalk = lines(crosswalkBytes);
function cite(anchor: string) {
	const matches = crosswalk.flatMap((line, offset) => line.includes(anchor) ? [offset + 1] : []);
	check(matches.length === 1, `Crosswalk locator missing or ambiguous: ${anchor}`);
	return { file: crosswalkFile, commit: base, line: matches[0], anchor };
}
const pathFor = (stem: string) => {
	const hits = changed.filter(row => row.path.endsWith(`/${stem}.test.ts`));
	check(hits.length === 1, `Missing or ambiguous suite: ${stem}`);
	return hits[0].path as string;
};

type Claim = { scope: string; passed?: number; aggregatePassed?: number; locator: ReturnType<typeof cite> };
const originalClaims = new Map<string, Claim[]>();
const nativeClaims = new Map<string, object[]>();
for (const [path, old] of oldByPath) {
	originalClaims.set(path, old.original.map((claim: Claim) => {
		// Keep the earlier prose claim anchored to its original commit.
		const { file, commit, line, anchor } = claim.locator;
		check(lines(native(file, commit))[line - 1]?.includes(anchor), `Historical locator missing: ${anchor}`);
		return claim;
	}));
	nativeClaims.set(path, old.nativeLinks.map((locator: ReturnType<typeof cite>) => {
		check(lines(native(locator.file, locator.commit))[locator.line - 1]?.includes(locator.anchor), `Historical native locator missing: ${locator.anchor}`);
		return locator;
	}));
}
function claim(stems: string[], scope: "reported-whole-file" | "whole-files-aggregate" | "named-aggregate", count: number, anchor: string, hasNative = true) {
	const locator = cite(anchor);
	for (const stem of stems) {
		const path = pathFor(stem);
		originalClaims.set(path, [...(originalClaims.get(path) ?? []), { scope, ...(scope === "reported-whole-file" ? { passed: count } : { aggregatePassed: count }), locator }]);
		if (hasNative) nativeClaims.set(path, [...(nativeClaims.get(path) ?? []), locator]);
	}
}
claim(["cache-retention"], "reported-whole-file", 35, "All 35 byte-verified `cache-retention.test.ts` originals passed");
claim(["max-thinking"], "reported-whole-file", 15, "The 15 unchanged `max-thinking.test.ts` cases and 58 unchanged `supports-xhigh.test.ts` cases passed");
claim(["supports-xhigh"], "reported-whole-file", 58, "The 15 unchanged `max-thinking.test.ts` cases and 58 unchanged `supports-xhigh.test.ts` cases passed");
claim(["openai-responses-compat"], "reported-whole-file", 42, "The full 42-case Responses compatibility file passed unchanged");
claim(["harness-idle-scopes"], "reported-whole-file", 25, "All 25 unchanged `harness-idle-scopes.test.ts` originals passed");
claim(["harness-lifecycle"], "reported-whole-file", 17, "All 17 unchanged `harness-lifecycle.test.ts` originals passed");
claim(["harness-park-children"], "reported-whole-file", 10, "All ten unchanged `harness-park-children.test.ts` cases passed. Four native branches cover");
claim(["codemode", "codemode-describe"], "whole-files-aggregate", 49, "All 49 unchanged cases in `codemode-describe.test.ts` and `codemode.test.ts` passed");
claim(["harness-signals"], "named-aggregate", 61, "`session-group-commit.test.ts` and `harness-signals.test.ts` passed 61", false);

// Named native packet summaries are distinct from assertion-ledger coverage.
for (const [stem, file, anchor] of [
	["fireworks-models", `${inventory}/fireworks-remaining.md`, "passes against the unmodified runtime"],
	["qwen-token-plan-models", `${inventory}/qwen-token-plan-remaining.md`, "Native reasoning and mock-stream comparisons passed"],
	["together-models", `${inventory}/together-models.md`, "native comparisons"],
	["fetch-option", `${inventory}/fetch-option-native.md`, "TestSourceV160FetchOption"],
] as const) {
	const line = lines(native(file)).findIndex(line => line.includes(anchor)) + 1;
	check(line > 0, `Missing native packet anchor in ${file}`);
	const path = pathFor(stem);
	nativeClaims.set(path, [...(nativeClaims.get(path) ?? []), { file, commit: base, line, anchor }]);
}

type Run = { file: string; sha256: string; scope: "whole-file" | "selected"; passed: number; caseNames: string[]; filteredOut: string[] | null };
const runs = new Map<string, Run[]>();
for (const file of committedFiles(inventory, `${root}/dependencies`).filter(file => file.endsWith(".json"))) {
	const bytes = native(file);
	const receipt = json(bytes);
	const path = receipt.selection?.file ?? receipt.selectedOriginal?.test;
	if (!source.has(path)) continue;
	const selection = receipt.selection;
	const sourceHash = receipt.sourceDigests?.find((digest: { file: string }) => digest.file === path)?.sha256 ?? selection?.sha256 ?? receipt.selectedOriginal?.sha256;
	check(receipt.pin === pin && sourceHash === source.get(path)!.sha256, `Machine receipt didn't bind pinned bytes: ${file}`);
	const caseNames: string[] = selection?.caseNames ?? selection?.reportedCaseNames ?? selection?.reportedCases?.map((row: { name: string }) => row.name) ?? [receipt.selectedOriginal.fullName];
	const filteredOut: string[] | null = selection?.filteredOut ?? (selection?.selector === "whole-file" ? [] : null);
	const passed = receipt.result?.passed ?? receipt.selectedOriginal.passed;
	check(caseNames.length === passed && sortedUnique(caseNames).length === passed && caseNames.every(Boolean), `Invalid machine case names: ${file}`);
	if (receipt.result) check(receipt.result.failed === 0 && (receipt.result.skipped ?? receipt.result.selectedSkipped) === 0 && (receipt.result.todo ?? 0) === 0, `Machine receipt was not green: ${file}`);
	if (selection?.reportedCases) check(selection.reportedCases.every((row: { status: string }) => row.status === "passed"), `Unexpected whole-file case status: ${file}`);
	if (filteredOut) check(sortedUnique([...caseNames, ...filteredOut]).length === caseNames.length + filteredOut.length, `Duplicate or overlapping machine case results: ${file}`);
	const run: Run = { file, sha256: sha(bytes), scope: selection?.selector === "whole-file" ? "whole-file" : "selected", passed, caseNames, filteredOut };
	runs.set(path, [...(runs.get(path) ?? []), run]);
}

check(runs.size === 6 && [...runs.values()].flat().length === 9, "Unexpected number of exact machine receipts");

const ledgerLinks = new Map<string, object[]>();
const ledgerOriginalLinks = new Map<string, object[]>();
const statusCounts = () => ({ mapped: 0, partial: 0, candidate: 0, unmatched: 0 });
const allLedgerInputs: { file: string; sha256: string }[] = [];
for (const file of committedFiles(`${root}/assertions`).filter(file => file.endsWith(".jsonl"))) {
	const bytes = native(file);
	const [manifest, ...data] = jsonl(bytes);
	if (!Object.keys(manifest.files).some(path => source.has(path))) continue;
	check(manifest.pin === pin, `Wrong assertion ledger pin: ${file}`);
	const digest = sha(bytes);
	allLedgerInputs.push({ file, sha256: digest });
	for (const path of Object.keys(manifest.files).filter(path => source.has(path))) {
		const input = source.get(path)!;
		check(manifest.files[path] === input.sha256, `Ledger source digest mismatch: ${file}, ${path}`);
		const cases = data.filter(row => row.kind === "case" && row.file === path);
		const casesByID = new Map(cases.map(row => [row.id, row]));
		const assertions = data.filter(row => row.kind === "assertion" && casesByID.has(row.case));
		const counts = statusCounts();
		for (const assertion of assertions) {
			const parent = casesByID.get(assertion.case);
			check(assertion.line >= parent.first && assertion.line <= parent.last, `Invalid ledger locator range: ${file}:${assertion.id}`);
			check(Object.hasOwn(counts, assertion.status), `Unexpected assertion status: ${assertion.id}`);
			counts[assertion.status as keyof typeof counts]++;
		}
		const selected = cases.filter(row => row.original !== null).map(row => {
			check(Object.hasOwn(manifest.receipts, row.original), `Missing ledger receipt for ${row.id}`);
			return { id: row.id, line: row.first, receipt: row.original };
		});
		const link = { file, sha256: digest, cases: cases.length, assertions: counts };
		ledgerLinks.set(path, [...(ledgerLinks.get(path) ?? []), link]);
		if (selected.length) ledgerOriginalLinks.set(path, [...(ledgerOriginalLinks.get(path) ?? []), { file, selected }]);
	}
}

check(ledgerLinks.size === 14, "Unexpected number of primary suites with assertion ledgers");

// Evidence of cited Go source alone is a lead, not proof the candidate ran at v1.6.0.
function nativeCandidates(path: string) {
	const literal = path.split("/").at(-1)!.replaceAll(".", "\\.");
	let found: string;
	try {
		found = git(nativeRepo, "grep", "-n", "-E", `(^|[^a-zA-Z0-9_.-])${literal}([^a-zA-Z0-9_.-]|$)`, base, "--", "*.go", ":!**/testdata/**").toString("utf8");
	} catch (error) {
		if ((error as { status: number }).status === 1) return [];
		throw error;
	}
	const locations = new Map<string, number>();
	for (const line of found.trimEnd().split("\n")) {
		const match = /^[a-f0-9]+:([^:]+):(\d+):/.exec(line);
		check(match, `Unrecognized candidate locator for ${path}`);
		if (!locations.has(match[1])) locations.set(match[1], Number(match[2]));
	}
	return [...locations].map(([file, line]) => ({ file, line }));
}

const conditions: Record<string, { state: string; note: string }> = {
	"cache-retention": { state: "synthetic-guards-reported", note: "Four credential guards ran with dummy keys; the payload hooks stop before transport. Live behavior is not verified." },
	"openai-reasoning-replay-e2e": { state: "withheld-open-not-excluded", note: "Credentialed OpenAI and Codex replay needs a separate authorized run." },
	"tools": { state: "withheld-open-not-excluded", note: "POSIX and Windows real-process branches require separate host receipts." },
	"fireworks-models": { state: "offline-dummy-or-loopback", note: "Credential-looking fixtures use dummy keys; the wire tests use IPv4 loopback." },
	"qwen-token-plan-models": { state: "offline-dummy-or-sdk-mock", note: "Dummy scoped credentials and a mocked OpenAI SDK; exact Go discovery API remains unmapped." },
	"google-vertex-api-key-resolution": { state: "offline-sdk-mock-reported", note: "Constructor SDK mocks are distinct from live provider and credential discovery behavior." },
	"together-models": { state: "offline-dummy-case-pending", note: "The filtered key-resolution test only uses an explicit dummy key and does not need credentials." },
};

const rows = changed.map(item => {
	const path = item.path as string;
	const stem = path.split("/").at(-1)!.replace(/\.test\.ts$/, "");
	const input = source.get(path)!;
	const machine = runs.get(path) ?? [];
	const named = originalClaims.get(path) ?? [];
	const cases = ledgerOriginalLinks.get(path) ?? [];
	const allNames = machine.flatMap(run => run.caseNames);
	const unionIsComplete = machine.length > 1 && machine.every(run => run.filteredOut !== null &&
		same([...run.filteredOut].sort(), allNames.filter(name => !run.caseNames.includes(name)).sort())) && sortedUnique(allNames).length === allNames.length;
	const originalStatus = machine.some(run => run.scope === "whole-file") ? "whole-file-machine" : unionIsComplete ? "complete-disjoint-machine" :
		named.some(receipt => receipt.scope === "reported-whole-file") ? "whole-file-claimed" : named.some(receipt => receipt.scope === "whole-files-aggregate") ? "whole-files-aggregate-claimed" :
		machine.length ? "selected-machine" : cases.length || named.some(receipt => receipt.scope === "selected") ? "selected-claimed" : named.length ? "aggregate-claimed" : "none-found";
	const ledgers = ledgerLinks.get(path) ?? [];
	const documented = nativeClaims.get(path) ?? [];
	const candidates = nativeCandidates(path);
	const nativeStatus = ledgers.length ? "assertion-indexed" : documented.length ? "documented-comparison" : candidates.length ? "source-candidate-only" : "none-found";
	const gates = input.lines.flatMap((line, index) => /\b(?:it|test|describe)\.(?:skipIf|runIf|skip|todo|fails)\b|process\.platform/.test(line) ? [{ line: index + 1, text: line.trim() }] : []);
	const condition = conditions[stem] ? { ...conditions[stem], gates } : gates.length ? { state: "review-needed-not-excluded", gates } : null;
	const owner = path.startsWith("packages/ai/") ? "AI" : stem === "codemode" || stem === "codemode-describe" ? "Codemode with Durable" : "Durable";
	const next = condition?.state === "withheld-open-not-excluded" ? "Separate safe offline cases from gated cases; seek authorization or an appropriate host for the withheld cases." :
		originalStatus === "none-found" ? "Run a byte-verified unchanged original packet; audit candidate Go tests before translating new assertions." :
		originalStatus.startsWith("selected") || originalStatus === "aggregate-claimed" ? "Recover exact per-file evidence or run the remaining unchanged cases; map unproved native assertions." :
		originalStatus.endsWith("claimed") ? "Locate or reproduce the test report; compare documented native evidence with the full source assertion set." :
		nativeStatus === "assertion-indexed" ? "Compare the indexed rows with the complete source assertion set; review partial and unmatched rows." :
		"Index exact native comparisons and any omissions; preserve the distinction from source-run evidence.";
	return {
		kind: "suite", path, change: item.change,
		upstream: { blob: input.blob, sha256: input.sha256, size: input.size },
		inHistoricalQueue: oldByPath.has(path), historicalOriginalStatus: oldByPath.get(path)?.originalStatus ?? null,
		original: { status: originalStatus, ...(unionIsComplete ? { disjointCases: allNames.length } : {}), machine, namedClaims: named, ledgerClaims: cases },
		native: { status: nativeStatus, ledgers, documented, candidateGoFiles: candidates },
		condition, reviewHints: item.review, owner, next,
	};
});
check(rows.filter(row => row.owner === "AI").length === 22 && rows.filter(row => row.owner !== "AI").length === 20, "Unexpected workspace distribution");
const tally = (values: string[]) => Object.fromEntries(sortedUnique(values).map(value => [value, values.filter(item => item === value).length]));
function summary(group: typeof rows) {
	return { files: group.length, original: tally(group.map(row => row.original.status)), native: tally(group.map(row => row.native.status)), owners: tally(group.map(row => row.owner)), conditions: tally(group.flatMap(row => row.condition ? [row.condition.state] : [])) };
}
console.log(JSON.stringify({ kind: "manifest", format: 1, pin, evidenceBase: base, historicalDeltaBase: queueHeader.historicalDeltaBase,
	inputs: [{ file: queueFile, sha256: sha(queueBytes) }, { file: historicalFile, sha256: sha(historicalBytes) }, { file: crosswalkFile, sha256: sha(crosswalkBytes) }, ...allLedgerInputs],
	audit: "Committed evidence at evidenceBase only; no tests were executed. Native candidate comments and prose claims are unverified in this packet.", exclusions: [] }));
for (const row of rows) console.log(JSON.stringify(row));
console.log(JSON.stringify({ kind: "summary", all: summary(rows), historicalQueue: summary(rows.filter(row => row.inHistoricalQueue)), originalEvidenceAbsent: rows.filter(row => row.original.status === "none-found").map(row => row.path) }));
