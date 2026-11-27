import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const base = "ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe";
const pin = "fb0fe823539f6d04850f6e34748c01f01f212e1f";
const areas = ["ai", "codemode", "durable", "schema"];
const [mirror, mode, extra] = Bun.argv.slice(2);
if (!mirror || (mode !== "--write" && mode !== "--check") || extra) {
	throw Error("Usage: bun run inventory.ts <pinned-git-mirror> --write|--check");
}
const git = (...args: string[]) => execFileSync("git", args, { cwd: mirror });
const sha256 = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const nul = (bytes: Buffer) => bytes.length ? bytes.toString().slice(0, -1).split("\0") : [];
assert.equal(git("rev-parse", "refs/tags/v1.6.1^{commit}").toString().trim(), base);
assert.equal(git("rev-parse", "refs/tags/v1.7.0^{commit}").toString().trim(), pin);
git("merge-base", "--is-ancestor", base, pin);

const inputs = JSON.parse(readFileSync(join(import.meta.dir, "inputs.json"), "utf8"));
assert.equal(inputs.upstreamCommit, pin);
assert.equal(inputs.packageLockSHA256, sha256(git("show", `${pin}:package-lock.json`)));
assert.equal(inputs.npm.length, areas.length);
for (const area of areas) {
	const manifest = git("show", `${pin}:packages/${area}/package.json`);
	const { name, version } = JSON.parse(manifest.toString());
	const archive = inputs.npm.find((entry: { name: string }) => entry.name === name);
	assert.equal(name, `@earendil-works/pi-${area}`);
	assert.equal(version, "1.7.0");
	assert.equal(archive?.version, version);
	assert.equal(archive.gitManifestSHA256, sha256(manifest));
	assert.match(archive.sha256, /^[a-f0-9]{64}$/);
	assert.match(archive.integrity, /^sha512-[A-Za-z0-9+/]{86}==$/);
	assert.equal(archive.url, `https://registry.npmjs.org/${name}/-/pi-${area}-${version}.tgz`);
}

const testPath = /^packages\/(ai|codemode|durable|schema)\/(test|test-workers)\/.+\.(test|spec)\.[cm]?[jt]sx?$/;
const corpus = new Set(nul(git("ls-tree", "-rz", "--name-only", pin, "--", ...areas.map(a => `packages/${a}`))).filter(path => testPath.test(path)));
const diff = nul(git("diff", "--no-ext-diff", "--no-textconv", "--name-status", "-z", "--find-renames=50%", base, pin));
const changes: string[][] = [];
const roles: string[][] = [];
const changedTests = new Map<string, string>();
function role(path: string) {
	if (/(?:^|\/)(?:package\.json|package-lock\.json)$/.test(path)) return "dependency-input";
	const match = /^packages\/(ai|codemode|durable|schema)\/(.*)$/.exec(path);
	if (!match) return "outside-four-package-comparison";
	const rest = match[2];
	if (testPath.test(path)) return "upstream-test";
	if (/^(test\/|test-workers\/|vitest)/.test(rest)) return "test-support";
	if (/^(catalog\/|src\/providers\/data\/)/.test(rest)) return "catalog-input";
	if (rest.startsWith("src/")) return "runtime-source";
	return rest.endsWith(".md") ? "documentation" : "package-support";
}
for (let i = 0; i < diff.length;) {
	const status = diff[i++];
	const old = diff[i++];
	const moved = /^[RC]/.test(status);
	const path = moved ? diff[i++] : old;
	assert(path && !/[\t\r\n]/.test(path) && !/[\t\r\n]/.test(old), "Path cannot be represented in TSV");
	changes.push(moved ? [status, old, path] : [status, path]);
	roles.push([status, moved ? old : "", path, role(path)]);
	if (testPath.test(path)) changedTests.set(path, status);
	if (status.startsWith("R") && testPath.test(old)) changedTests.set(old, "renamed-away");
}
const tests = [...new Set([...corpus, ...changedTests.keys()])].sort().map(path => [path, changedTests.get(path) ?? "unchanged", corpus.has(path) ? "present" : "no-longer-in-corpus"]);
const tsv = (rows: string[][]) => rows.map(row => row.join("\t")).join("\n") + "\n";
const reports = {
	"upstream-changes.tsv": tsv(changes),
	"upstream-disposition.tsv": tsv([["delta", "previous_path", "path", "role"], ...roles]),
	"test-inventory.tsv": tsv([["upstream_test_file", "delta", "corpus_state"], ...tests]),
};
for (const [name, bytes] of Object.entries(reports)) {
	const path = join(import.meta.dir, name);
	if (mode === "--write") writeFileSync(path, bytes);
	else assert(readFileSync(path, "utf8") === bytes, `${name} is stale; regenerate with --write`);
}
console.log(`${changes.length} changed paths; ${corpus.size} pinned test files and ${changedTests.size} changed test-file paths. No tests executed.`);
