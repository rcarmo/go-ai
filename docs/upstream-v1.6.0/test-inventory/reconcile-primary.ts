import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const base = "d1bd6c9fc67967345df7755c2606b2a823e3a31f";
const [mirror] = process.argv.slice(2);
if (process.argv.length !== 3) throw Error("Usage: bun run reconcile-primary.ts <pinned-git-mirror>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 8 << 20 });
const native = (path: string) => git(import.meta.dir, "show", `${base}:${path}`);
const sha256 = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const root = "docs/upstream-v1.6.0";
const queueFile = `${root}/test-inventory/files.jsonl`;
const crosswalkFile = `${root}/README.md`;
const crosswalk = native(crosswalkFile).toString("utf8").split("\n");
const queueRows = native(queueFile).toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
check(queueRows[0]?.pin === pin, "Unexpected queue pin");
const queue = queueRows.filter(row => row.kind === "path" && row.scope === "primary" && row.role === "suite" && ["A", "M"].includes(row.change) && row.coverage === "not-in-assertion-ledger");
check(queue.length === 37 && queue.filter(row => row.workspace === "packages/ai").length === 21 && queue.filter(row => row.workspace === "packages/durable").length === 16, "Primary queue differs from the audited slice");

type Receipt = { anchor: string; scope: "reported-whole-file" | "selected" | "named-aggregate"; passed?: number; aggregatePassed?: number; note?: string };
type Documented = { originals: Receipt[]; native: string[] };
// Counts describe the cited packet only. Never add overlapping or aggregated selections together.
const prior: Record<string, Documented> = {
	"anthropic-message-threads": { originals: [{ anchor: "All 33 unchanged `anthropic-message-threads.test.ts` cases passed", scope: "reported-whole-file", passed: 33 }], native: ["Ten native wire branches cover aliases"] },
	"anthropic-strict-tool-schema": { originals: [{ anchor: "The three unchanged strict-tool-schema originals passed", scope: "selected", passed: 3, note: "Named by feature in the Anthropic schema packet." }], native: ["The exact shape test failed on `43e8f1c`"] },
	"cache-retention": { originals: [{ anchor: "The nine selected `cache-retention.test.ts` catalog cases passed", scope: "selected", passed: 9 }], native: ["The native catalog check also found no inferred warming policy"] },
	"fetch-option": { originals: [{ anchor: "The unchanged `google-stop-reason-continuation` and `fetch-option` suites passed eight cases", scope: "named-aggregate", aggregatePassed: 8 }], native: ["The native Google tests exercise both adapters and both transport paths"] },
	"google-stop-reason-continuation": { originals: [{ anchor: "The unchanged `google-stop-reason-continuation` and `fetch-option` suites passed eight cases", scope: "named-aggregate", aggregatePassed: 8 }], native: ["The native Google tests exercise both adapters and both transport paths"] },
	"google-vertex-api-key-resolution": { originals: [{ anchor: "All nine unchanged `google-vertex-api-key-resolution.test.ts` constructor cases passed", scope: "reported-whole-file", passed: 9 }], native: ["have offline native transport probes"] },
	"mistral-tool-schema": { originals: [{ anchor: "The unchanged OpenAI Completions and Mistral tool-schema files passed 52 cases", scope: "named-aggregate", aggregatePassed: 52, note: "Aggregate includes other Completions files; no count assigned to Mistral alone." }], native: ["check the Mistral request before network processing"] },
	"model-data-validation": { originals: [{ anchor: "74 unchanged upstream cases in `models-runtime`, `models-entry` and `model-data-validation`", scope: "named-aggregate", aggregatePassed: 74 }], native: ["The manifest/provider integrity tests and metadata-corruption self-test passed"] },
	"openai-responses-input-transformations": { originals: [{ anchor: "All five unchanged `openai-responses-input-transformations.test.ts` cases passed", scope: "reported-whole-file", passed: 5 }], native: ["The native loopback cases check both adapters, exact details"] },
	"retry": { originals: [{ anchor: 'AI `error-body.test.ts` and `retry.test.ts` passed 73 cases', scope: "named-aggregate", aggregatePassed: 73 }], native: ["map to `TestSourceV160RetryConnectionFailures`"] },
	"harness-hook-tasks": { originals: [{ anchor: "All 12 unchanged `harness-hook-tasks.test.ts` cases passed", scope: "reported-whole-file", passed: 12 }], native: ["Nine native translations cover decisions", "The three remaining `harness-hook-tasks.test.ts` recovery assertions now have native coverage"] },
	"harness-input-entries": { originals: [{ anchor: 'the two input-entry abort originals (`harness-input-entries.test.ts:237`, `:258`) passed', scope: "selected", passed: 2 }], native: ["`harness-input-entries.test.ts:258` maps to `TestSourceV160SubmissionOnAbortKeep`", "the two input-entry abort originals (`harness-input-entries.test.ts:237`, `:258`) passed"] },
	"harness-tool-gates": { originals: [{ anchor: "All nine unchanged `harness-tool-gates.test.ts` originals passed", scope: "reported-whole-file", passed: 9 }], native: ["Invalid waits go through `afterTool` and settle"] },
	"harness-tool-parks": { originals: [
		{ anchor: "Five unchanged `harness-tool-parks.test.ts` cancel cases passed", scope: "selected", passed: 5 },
		{ anchor: "The three unchanged `harness-tool-parks.test.ts` waking cases passed", scope: "selected", passed: 3 },
	], native: ["The corresponding native tests exercise a parked abort", "The native safe and unsafe cases kill the writer process"] },
	"images-fit": { originals: [{ anchor: "All 11 unchanged `images-fit.test.ts` cases passed", scope: "reported-whole-file", passed: 11 }], native: ["Native tests cover pass-through bytes, processor failure"] },
};
const withheld: Record<string, string> = {
	"cache-retention": "Live provider credential gates are open; the nine documented catalog cases are offline only.",
	"openai-reasoning-replay-e2e": "OpenAI and Codex targets have credential gates; no live requests were made.",
	"tools": "The POSIX/Windows gated real process cases need a platform selection and receipts.",
};
const reviewedHints: Record<string, string> = {
	"fireworks-models": "The visible API-key cases set dummy values and/or mock transport; the lexical hint alone does not establish a live requirement.",
	"google-vertex-api-key-resolution": "This suite's nine documented constructor cases mock the SDK; provider credentials are a distinct open gate.",
	"qwen-token-plan-models": "The highlighted key lookup fixtures supply explicit test strings; do not infer live coverage.",
	"together-models": "The highlighted environment checks use a dummy Together key; no live receipt is implied.",
};
function locator(anchor: string) {
	const hits = crosswalk.map((line, index) => [line, index + 1] as const).filter(([line]) => line.includes(anchor));
	check(hits.length === 1, `Missing or ambiguous prior receipt: ${anchor}`);
	return { file: crosswalkFile, commit: base, line: hits[0][1], anchor };
}
const nameFor = (path: string) => path.split("/").at(-1)!.replace(/\.test\.ts$/, "");
for (const name of [...Object.keys(prior), ...Object.keys(withheld), ...Object.keys(reviewedHints)]) check(queue.some(row => nameFor(row.path) === name), `Receipt entry is not on the queue: ${name}`);
const newFile = `${root}/test-inventory/env-node-list-race.json`;
const newRun = JSON.parse(readFileSync(join(import.meta.dir, "env-node-list-race.json"), "utf8"));
check(newRun.pin === pin && newRun.evidenceBase === base && newRun.selection.file === "packages/durable/test/env-node-list-race.test.ts" && newRun.selection.reportedCaseNames.length === 1 && newRun.result.passed === 1 && newRun.result.failed === 0 && newRun.result.skipped === 0 && newRun.result.todo === 0, "Unexpected exact new original receipt");
const listDirCandidate = { file: "durable/tools/filesystem.go", commit: base, anchor: "func (e *Env) ListDir(", state: "source-inspected-only" };
check(native(listDirCandidate.file).includes(listDirCandidate.anchor), "Missing native ListDir candidate");
const googleFile = `${root}/dependencies/google-sdk-load.json`;
const google = JSON.parse(native(googleFile).toString("utf8"));
check(google.pin === pin && google.selectedOriginal.test === "packages/ai/test/google-stop-reason-continuation.test.ts" && google.selectedOriginal.passed === 1, "Unexpected prior Google original receipt");

const rows = queue.map(row => {
	const name = nameFor(row.path);
	const source = git(mirror, "show", `${pin}:${row.path}`);
	const sourceSha256 = sha256(source);
	check(git(mirror, "rev-parse", `${pin}:${row.path}`).toString().trim() === row.pinned.object, `Stale queue source: ${row.path}`);
	const evidence = prior[name];
	const original = evidence?.originals.map(({ anchor, ...claim }) => ({ ...claim, locator: locator(anchor) })) ?? [];
	const actualRuns = [];
	if (row.path === newRun.selection.file) {
		check(newRun.selection.sha256 === sourceSha256 && newRun.selection.selector === "whole-file", "Changed new original input bytes");
		actualRuns.push({ file: newFile, scope: "whole-file", status: "verified-this-packet", passed: 1, name: newRun.selection.reportedCaseNames[0] });
	} else if (row.path === google.selectedOriginal.test) {
		check(google.selectedOriginal.sha256 === sourceSha256, "Changed previous Google input bytes");
		actualRuns.push({ file: googleFile, scope: "selected", status: "prior-exact-run", passed: 1, name: google.selectedOriginal.fullName });
	}
	const nativeLinks = evidence?.native.map(locator) ?? [];
	return {
		kind: "suite", path: row.path, change: row.change, sourceSha256, inputCoverage: row.coverage,
		originalStatus: actualRuns.some(run => run.status === "verified-this-packet") ? "verified-this-packet" : original.length || actualRuns.length ? "receipt-found" : "none-found-in-audited-receipts",
		original, actualRuns,
		nativeStatus: nativeLinks.length ? "documented-native-evidence-not-assertion-indexed" : "no-file-specific-link-found",
		nativeLinks, ...(row.path === newRun.selection.file ? { nativeCandidate: listDirCandidate } : {}), reviewHints: row.review,
		condition: withheld[name] ? { state: "withheld-open-not-excluded", note: withheld[name] } : reviewedHints[name] ? { state: "lexical-hint-reviewed", note: reviewedHints[name] } : null,
	};
});
console.log(JSON.stringify({ kind: "manifest", format: 1, pin, evidenceBase: base, queue: { file: queueFile, commit: base }, auditedCrosswalk: { file: crosswalkFile, commit: base }, exclusions: [] }));
for (const row of rows) console.log(JSON.stringify(row));
console.log(JSON.stringify({ kind: "summary", queueFiles: rows.length, priorReceiptFiles: rows.filter(row => row.originalStatus === "receipt-found").length, newOriginalFiles: rows.filter(row => row.originalStatus === "verified-this-packet").length, noOriginalReceiptFiles: rows.filter(row => row.originalStatus === "none-found-in-audited-receipts").length, nativeEvidenceFiles: rows.filter(row => row.nativeLinks.length).length, withheldFiles: rows.filter(row => row.condition?.state === "withheld-open-not-excluded").length }));
