import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const base = "ebe8ec7ab0a1f2da0a1641a9b63c633a48f1a707";
const predecessorBase = "d0f5e86bf5392e4a8027904f888c6e244c2e0284";
const root = "docs/upstream-v1.6.0";
const inventory = `${root}/test-inventory`;
const predecessor = `${inventory}/primary-queue-r21.jsonl`;
const [mirror] = process.argv.slice(2);
if (!mirror || process.argv.length !== 3) throw Error("Usage: bun run reconcile-primary-r33.ts <pinned-git-mirror>");
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 16 << 20 });
const repo = git(import.meta.dir, "rev-parse", "--show-toplevel").toString().trim();
const sha = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const jsonl = (bytes: Buffer) => bytes.toString().trimEnd().split("\n").map(line => JSON.parse(line));
const nul = (bytes: Buffer) => bytes.toString().split("\0").filter(Boolean);
const unique = (values: string[]) => [...new Set(values)].sort();
const same = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right);
function check(value: unknown, message: string): asserts value {
	if (!value) throw Error(message);
}
const inputs = new Map<string, { file: string; commit: string; sha256: string }>();
function read(file: string, commit = base) {
	const bytes = git(repo, "show", `${commit}:${file}`);
	inputs.set(`${commit}:${file}`, { file, commit, sha256: sha(bytes) });
	return bytes;
}
function locate(file: string, anchor: string, commit = base) {
	const matches = read(file, commit).toString().split("\n").flatMap((line, i) => line.includes(anchor) ? [i + 1] : []);
	check(matches.length === 1, `Missing or ambiguous locator: ${file} ${anchor}`);
	return { file, commit, line: matches[0], anchor };
}
const [header, ...prior] = jsonl(read(predecessor));
check(header.pin === pin && header.evidenceBase === predecessorBase, "Unexpected predecessor pin");
const changed = prior.filter(row => row.kind === "suite");
check(changed.length === 42 && changed.filter(row => row.inHistoricalQueue).length === 37, "Wrong primary queue size");
const source = new Map(changed.map(row => {
	const bytes = git(mirror, "show", `${pin}:${row.path}`);
	check(row.upstream.sha256 === sha(bytes) && row.upstream.size === bytes.length &&
		row.upstream.blob === git(mirror, "rev-parse", `${pin}:${row.path}`).toString().trim(), `Pinned source differs: ${row.path}`);
	return [row.path, bytes.toString().split("\n")];
}));
const direct = nul(git(mirror, "diff", "--no-renames", "--name-status", "-z", header.historicalDeltaBase, pin, "--",
	...["ai", "codemode", "durable", "schema"].map(name => `packages/${name}`)));
const paths = direct.flatMap((status, i) => i % 2 === 0 && ["A", "M"].includes(status) && /\.(?:test|spec)\.[cm]?[jt]sx?$/.test(direct[i + 1]) ? [`${status}\t${direct[i + 1]}`] : []);
check(same(paths.sort(), changed.map(row => `${row.change}\t${row.path}`).sort()), "Primary path set disagrees with pinned Git diff");

const byPath = new Map(changed.map(row => [row.path, row]));
function byStem(stem: string) {
	const rows = changed.filter(row => row.path.endsWith(`/${stem}.test.ts`));
	check(rows.length === 1, `Unknown or ambiguous suite: ${stem}`);
	return rows[0];
}
for (const row of changed) {
	for (const { file, commit, line, anchor } of [...row.original.namedClaims.map(claim => claim.locator), ...row.native.documented]) {
		check(read(file, commit).toString().split("\n")[line - 1]?.includes(anchor), `Stale Round 21 anchor: ${file}:${line}`);
	}
}
const crosswalk = `${root}/README.md`;
const claims = new Map<string, object[]>();
const comparisons = new Map<string, object[]>();
function addClaim(stem: string, scope: string, count: number, anchor: string, hasNative = true) {
	const { path } = byStem(stem);
	const locator = locate(crosswalk, anchor);
	claims.set(path, [...(claims.get(path) ?? []), { scope, ...((scope === "whole-files-aggregate") ? { aggregatePassed: count } : { passed: count }), locator }]);
	if (hasNative) comparisons.set(path, [...(comparisons.get(path) ?? []), locator]);
}
addClaim("constrained-sampling", "reported-whole-file", 10, "All ten unchanged constrained-sampling cases passed.");
addClaim("openai-completions-tool-choice", "reported-whole-file", 50, "All 50 unchanged upstream cases passed offline.");
addClaim("harness-submissions", "reported-whole-file", 12, "All 12 unchanged upstream cases passed.");
for (const stem of ["harness-after-run", "harness-tool-hooks"]) {
	addClaim(stem, "whole-files-aggregate", 47, "The unchanged `harness-after-run` and `harness-tool-hooks` files passed 47 cases together.", stem === "harness-after-run");
}
addClaim("harness-tool-hooks", "reported-whole-file", 18, "All 18 unchanged tool-hook cases passed.");
addClaim("spec-usage", "reported-whole-file", 1, "The unchanged specification-usage test passed under Vitest.", false);
addClaim("harness-tools", "reported-whole-file", 42, "All 42 unchanged Harness tool cases passed offline.");
addClaim("codemode-describe", "selected", 1, "The unchanged description test passed.");
addClaim("codemode", "reported-whole-file", 42, "All 42 unchanged Durable Codemode tests passed.");
addClaim("harness-nested-tools", "reported-whole-file", 35, "All 35 unchanged nested-tool tests passed offline.");
addClaim("providers", "reported-whole-file", 30, "All 30 unchanged provider tests passed in a network-isolated run.");
addClaim("harness-signals", "reported-whole-file", 21, "All 21 unchanged cases in `harness-signals.test.ts` passed.");

const acceptance = `${root}/acceptance-gaps.md`;
const exclusionName = "durable tools powershell runs real PowerShell with UTF-8 output";
const exclusionPath = byStem("tools").path;
const exclusionLine = "it.runIf(process.platform === \"win32\")(\"runs real PowerShell with UTF-8 output\", async () => {";
check(source.get(exclusionPath)![616].trim() === exclusionLine, "Windows exclusion case moved or changed");
const exclusion = { path: exclusionPath, line: 617, caseName: exclusionName, source: exclusionLine,
	approved: locate(acceptance, "the user excluded the Windows-only real process check", "cf6f4836ed5e7ce0d88e7a6ac6d477423c839bb1") };
const live = { path: byStem("openai-reasoning-replay-e2e").path, gateLine: 74, required: ["OpenAI", "Codex over SSE", "Codex over WebSocket"],
	model: "gpt-6.2-sol", state: "authorised-awaits-provisioned-access", preflight: locate(acceptance, "The round-29 preflight found no workspace credential grants") };
check(source.get(live.path)![73].includes("it.skipIf(!target.apiKey)"), "Unexpected live credential gate");
for (const name of live.required) check(source.get(live.path)!.some(line => line.includes(`name: "${name}"`)), `Unknown live target: ${name}`);
const otherGates = [locate(acceptance, "A hosted check, if required by the target matrix"), locate(acceptance, "obtain separate approval to run the full checks")];
const imagePath = byStem("tools-images").path;
const imagePacket = `${inventory}/tools-images-preparation.json`;
const imageTail = [
	"createNodePhotonImages prepares in a worker thread, leaving the event loop free",
	"createNodePhotonImages prepares concurrent images, and starts a new worker after the idle one exited",
];
const imageTailClaim = { scope: "selected", passed: 2, caseNames: imageTail, locator: locate(crosswalk, "The two remaining unchanged worker timing cases also passed in a separate isolated run") };
claims.set(imagePath, [imageTailClaim]);
comparisons.set(imagePath, [locate(`${inventory}/tools-images-preparation.md`, "The native packet maps 14 cases from this selection.")]);

const files = (...dirs: string[]) => nul(git(repo, "ls-tree", "-rz", "--name-only", base, "--", ...dirs));
type Run = { file: string; sha256: string; scope: "whole-file" | "selected"; passed: number; caseNames: string[]; filteredOut: string[] | null };
const runs = new Map<string, Run[]>();
for (const file of files(inventory, `${root}/dependencies`).filter(file => file.endsWith(".json"))) {
	const bytes = git(repo, "show", `${base}:${file}`);
	const receipt = JSON.parse(bytes.toString());
	const selected = receipt.selectedOriginal ?? receipt.original;
	const selection = receipt.selection;
	const path = selection?.file ?? selected?.test;
	if (!source.has(path)) continue;
	const sourceHash = receipt.sourceDigests?.find((digest: { file: string }) => digest.file === path)?.sha256 ?? selection?.sha256 ?? selected?.sha256;
	check(receipt.pin === pin && sourceHash === byPath.get(path).upstream.sha256, `Unbound machine receipt: ${file}`);
	const caseNames = selection?.caseNames ?? selection?.reportedCaseNames ?? selection?.reportedCases?.map((row: { name: string }) => row.name) ?? [selected.fullName ?? selected.selector];
	const passed = receipt.result?.passed ?? selected.passed;
	const scope = selection?.selector === "whole-file" ? "whole-file" : "selected";
	const filteredOut = selection?.filteredOut ?? (scope === "whole-file" ? [] : null);
	check(caseNames.length === passed && unique(caseNames).length === passed && caseNames.every(Boolean), `Invalid machine case names: ${file}`);
	if (receipt.result) check(receipt.result.failed === 0 && (receipt.result.skipped ?? receipt.result.selectedSkipped) === 0 && (receipt.result.todo ?? 0) === 0, `Non-green machine receipt: ${file}`);
	if (selection?.reportedCases) check(selection.reportedCases.every((row: { status: string }) => row.status === "passed"), `Non-green case: ${file}`);
	if (filteredOut) check(unique([...caseNames, ...filteredOut]).length === caseNames.length + filteredOut.length, `Overlapping names: ${file}`);
	const run: Run = { file, sha256: sha(read(file)), scope, passed, caseNames, filteredOut };
	runs.set(path, [...(runs.get(path) ?? []), run]);
}
for (const row of changed) {
	check(row.original.machine.every(old => runs.get(row.path)?.some(run => run.file === old.file && run.sha256 === old.sha256)), `Missing or changed Round 21 machine receipt: ${row.path}`);
}

const ledgerLinks = new Map<string, object[]>();
const ledgerClaims = new Map<string, object[]>();
for (const file of files(`${root}/assertions`).filter(file => file.endsWith(".jsonl"))) {
	const [manifest, ...data] = jsonl(git(repo, "show", `${base}:${file}`));
	for (const row of changed.filter(row => Object.hasOwn(manifest.files, row.path))) {
		const path = row.path as string;
		check(manifest.pin === pin && manifest.files[path] === row.upstream.sha256, `Unbound assertion ledger: ${file}:${path}`);
		const cases = data.filter(item => item.kind === "case" && item.file === path);
		const byID = new Map(cases.map(item => [item.id, item]));
		const counts = { mapped: 0, partial: 0, candidate: 0, unmatched: 0 };
		for (const assertion of data.filter(item => item.kind === "assertion" && byID.has(item.case))) {
			const parent = byID.get(assertion.case);
			check(assertion.line >= parent.first && assertion.line <= parent.last && Object.hasOwn(counts, assertion.status), `Invalid assertion: ${file}:${assertion.id}`);
			counts[assertion.status as keyof typeof counts]++;
		}
		const receiptCases = cases.filter(item => item.original !== null).map(item => {
			check(Object.hasOwn(manifest.receipts, item.original), `Unknown original receipt: ${file}:${item.id}`);
			return { id: item.id, receipt: item.original };
		});
		ledgerLinks.set(path, [...(ledgerLinks.get(path) ?? []), {file, sha256: sha(read(file)), cases: cases.length, assertions: counts }]);
		if (receiptCases.length) ledgerClaims.set(path, [...(ledgerClaims.get(path) ?? []), {file, cases: receiptCases}]);
	}
}

function partition(machine: Run[]) {
	const reports = machine.filter(run => run.filteredOut !== null);
	if (!reports.length) return null;
	const names = unique([...reports[0].caseNames, ...reports[0].filteredOut!]);
	check(reports.every(run => same(unique([...run.caseNames, ...run.filteredOut!]), names)), "Machine case denominators disagree");
	const passed = unique(reports.flatMap(run => run.caseNames));
	return { total: names.length, passed: passed.length, noMachinePass: names.filter(name => !passed.includes(name)) };
}
const rows = changed.map(row => {
	const path = row.path as string;
	const machine = runs.get(path) ?? [];
	const exact = partition(machine);
	const named = [...row.original.namedClaims, ...(claims.get(path) ?? [])];
	const ledger = ledgerClaims.get(path) ?? [];
	const additionalPassNames = path === imagePath ? imageTailClaim.caseNames : [];
	if (path === imagePath) {
		check(machine.some(run => run.file === imagePacket) && same(exact?.noMachinePass, unique(imageTail)), "Unproved image tail partition");
		for (const name of imageTail) check(source.get(path)!.some(line => line.includes(name.replace("createNodePhotonImages ", ""))), `Unknown image case: ${name}`);
	}
	const noPassEvidence = exact?.noMachinePass.filter(name => !additionalPassNames.includes(name)) ?? null;
	const approvedExclusions = path === exclusion.path ? [exclusion] : [];
	if (approvedExclusions.length) check(noPassEvidence?.includes(exclusion.caseName), "Excluded case must remain unexecuted");
	const offlineCasesWithoutEvidence = noPassEvidence?.filter(name => !approvedExclusions.some(item => item.caseName === name)) ?? null;
	const machineDisjoint = exact && machine.filter(run => run.filteredOut !== null).reduce((total, run) => total + run.passed, 0) === exact.passed;
	const status = machine.some(run => run.scope === "whole-file") ? "whole-file-machine" :
		exact && exact.total === exact.passed && machineDisjoint ? "complete-disjoint-machine" :
		exact && exact.total === exact.passed + additionalPassNames.length ? "complete-machine-plus-prose" :
		named.some(claim => claim.scope === "reported-whole-file") ? "whole-file-claimed" :
		named.some(claim => claim.scope === "whole-files-aggregate") ? "whole-files-aggregate-claimed" :
		machine.length ? "selected-machine" : ledger.length || named.some(claim => claim.scope === "selected") ? "selected-claimed" :
		named.length ? "aggregate-claimed" : "none-found";
	const ledgers = ledgerLinks.get(path) ?? [];
	const documented = [...row.native.documented, ...(comparisons.get(path) ?? [])];
	const nativeStatus = ledgers.length ? "assertion-indexed" : documented.length ? "documented-comparison" : row.native.candidateGoFiles.length ? "historical-source-lead-only" : "none-found";
	return { kind: "suite", path, change: row.change, upstream: row.upstream, owner: row.owner, inHistoricalQueue: row.inHistoricalQueue,
		prior: { original: row.original.status, native: row.native.status },
		original: { status, machine, machinePartition: exact, additionalProsePassNames: additionalPassNames, namedClaims: named, ledgerClaims: ledger },
		native: { status: nativeStatus, ledgers, documented, round21CandidateGoFiles: row.native.candidateGoFiles },
		conditions: { pinnedSourceGates: row.condition?.gates ?? [], live: path === live.path ? live : null, approvedExclusions,
			priorOfflineNotes: row.condition?.state?.match(/^(offline-|synthetic-)/) ? { state: row.condition.state, note: row.condition.note } : null },
		offlineCasesWithoutEvidence,
	};
});
const tally = (values: string[]) => Object.fromEntries(unique(values).map(value => [value, values.filter(item => item === value).length]));
const summarize = (group: typeof rows) => ({
	files: group.length, owners: tally(group.map(row => row.owner)), original: tally(group.map(row => row.original.status)),
	native: tally(group.map(row => row.native.status)), exactMachineReports: group.reduce((total, row) => total + row.original.machine.length, 0),
	filesWithMachine: group.filter(row => row.original.machine.length).length,
	filesWithProse: group.filter(row => row.original.namedClaims.length).length,
	filesWithLedgerOriginalClaims: group.filter(row => row.original.ledgerClaims.length).length,
});
console.log(JSON.stringify({ kind: "manifest", format: 1, pin, evidenceBase: base, predecessorBase, historicalDeltaBase: header.historicalDeltaBase,
	audit: "Committed evidence only. No tests were executed. Prose and ledger claims are distinct from machine reports and from native parity.",
	authorisedExclusionsIn42: [exclusion], liveGates: [live], otherGates, inputs: [...inputs.values()].sort((a, b) => `${a.file}:${a.commit}` < `${b.file}:${b.commit}` ? -1 : 1) }));
for (const row of rows) console.log(JSON.stringify(row));
console.log(JSON.stringify({ kind: "summary", all: summarize(rows), historical37: summarize(rows.filter(row => row.inHistoricalQueue)),
	originalEvidenceAbsent: rows.filter(row => row.original.status === "none-found").map(row => row.path),
	exactNamedOfflineGaps: rows.flatMap(row => row.offlineCasesWithoutEvidence?.length ? [{ path: row.path, cases: row.offlineCasesWithoutEvidence }] : []),
	notEstablishedAtWholeFileWidth: rows.filter(row => ["selected-machine", "selected-claimed", "aggregate-claimed", "none-found"].includes(row.original.status)).map(row => row.path) }));
