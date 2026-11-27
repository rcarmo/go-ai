// Usage: bun run stage-durable-workers.ts <pinned git mirror> <npm _cacache> <v1.6.1 npm releases> <OpenAI tarball> <Google tarball> <empty private stage>
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, symlinkSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const pin = "ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe";
const roots = ["workspace:packages/durable", "npm:node_modules/vitest", "npm:node_modules/@cloudflare/vitest-plugin"];
const platform = { os: "linux", cpu: "arm64", libc: "glibc" };
assert.equal(process.argv.length, 8);
const [mirror, cache, releases, openai, google, stage] = process.argv.slice(2).map(path => resolve(path));
assert.equal(process.platform, platform.os);
assert.equal(process.arch, platform.cpu);
assert((process.report?.getReport() as { header?: { glibcVersionRuntime?: string } }).header?.glibcVersionRuntime, "Expected a glibc host");
assert.equal(readdirSync(stage).length, 0, "Stage must be empty");
const digest = (bytes: Buffer | string, algorithm = "sha256", encoding: "hex" | "base64" = "hex") => createHash(algorithm).update(bytes).digest(encoding);
const readRows = (name: string) => readFileSync(join(import.meta.dir, name), "utf8").trim().split("\n").map(line => JSON.parse(line));
const git = (...args: string[]) => execFileSync("git", ["-C", mirror, ...args], { maxBuffer: 16 * 1024 * 1024 });
const pinnedLock = git("show", `${pin}:package-lock.json`);
const lock = JSON.parse(pinnedLock.toString()).packages;
const rows = readRows("npm-closure.jsonl");
const manifest = rows.find(row => row.kind === "manifest");
assert.equal(manifest.pin, pin);
assert.equal(manifest.inputs["package-lock.json"], digest(pinnedLock));
const nodes = new Map(rows.filter(row => row.kind === "npm" || row.kind === "workspace").map(row => [row.id, row]));
const edges = Map.groupBy(rows.filter(row => row.kind === "edge"), row => row.from);
const present = new Set(roots);
const incompatible = new Set<string>();
const optionalPeers = [];
function supports(values: string[] | undefined, actual: string) {
	return !values?.includes(`!${actual}`) && (!values?.some(value => !value.startsWith("!")) || values.includes(actual));
}
for (const id of present) {
	const node = nodes.get(id);
	assert(node, `No locked node for ${id}`);
	if (node.kind === "npm" && !Object.entries(platform).every(([key, value]) => supports(node.platform[key], value))) {
		incompatible.add(id);
		continue;
	}
	for (const edge of edges.get(id) ?? []) {
		if (edge.class === "development") continue;
		if (edge.class === "optional-peer") {
			optionalPeers.push(edge);
			continue;
		}
		if (edge.state === "resolved") present.add(edge.to);
		else assert(edge.class === "optional" && edge.state === "optional-absent", `Unresolved ${id} -> ${edge.dependency}`);
	}
}
const included = [...present].filter(id => !incompatible.has(id)).sort().map(id => nodes.get(id));
for (const node of included) {
	for (const edge of edges.get(node.id) ?? []) {
		if (edge.class === "runtime" || edge.class === "peer") assert(!incompatible.has(edge.to), `Required incompatible edge ${node.id} -> ${edge.to}`);
	}
}
const workspaces = included.filter(node => node.kind === "workspace");
const packages = included.filter(node => node.kind === "npm");
const extras = new Map([["node_modules/openai", openai], ["node_modules/@google/genai", google]]);
const arch = (node: { lockKey: string; integrity: string }) => {
	assert.match(node.integrity, /^sha512-[A-Za-z0-9+/]{86}==$/);
	const hex = Buffer.from(node.integrity.slice(7), "base64").toString("hex");
	const fromCache = join(cache, "content-v2", "sha512", hex.slice(0, 2), hex.slice(2, 4), hex.slice(4));
	const file = existsSync(fromCache) ? fromCache : extras.get(node.lockKey);
	assert(file && existsSync(file), `Missing retained archive for ${node.lockKey}: ${node.integrity}`);
	return { file, source: file === fromCache ? "npm-content-cache" : "retained-tarball" };
};
const archives = packages.map(node => {
	assert.match(node.lockKey, /^(?:(?:packages\/[^/]+|node_modules\/(?:@[\w.-]+\/)?[\w.-]+)\/)*node_modules\/(?:@[\w.-]+\/)?[\w.-]+$/);
	assert(node.lockKey.split("/").every((part: string) => part !== "." && part !== ".."));
	for (const field of ["version", "resolved", "integrity"]) assert.equal(node[field], lock[node.lockKey][field], node.lockKey);
	const { file, source } = arch(node);
	const bytes = readFileSync(file);
	assert.equal(`sha512-${digest(bytes, "sha512", "base64")}`, node.integrity, node.lockKey);
	return { key: node.lockKey, name: node.name, version: node.version, integrity: node.integrity, sha256: digest(bytes), file, source };
});

const paths = ["package.json", "package-lock.json", "packages/durable/test-workers", ...workspaces.flatMap(node => [`${node.lockKey}/package.json`, `${node.lockKey}/src`])];
execFileSync("tar", ["-xf", "-", "-C", stage], { input: git("archive", pin, ...paths) });
for (const node of workspaces) {
	assert.equal(lock[node.linkKey].resolved, node.lockKey);
	const target = join(stage, node.linkKey);
	const dir = resolve(target, "..");
	mkdirSync(dir, { recursive: true });
	symlinkSync(relative(dir, join(stage, node.lockKey)), target, "dir");
}
for (const archive of archives) {
	const target = join(stage, archive.key);
	mkdirSync(target, { recursive: true });
	execFileSync("tar", ["-xzf", archive.file, "-C", target, "--strip-components=1", "--no-same-owner", "--no-same-permissions", "--warning=no-unknown-keyword"]);
	const packed = JSON.parse(readFileSync(join(target, "package.json"), "utf8"));
	assert.equal(packed.name, archive.name, archive.key);
	assert.equal(packed.version, archive.version, archive.key);
}
// AI's packed main entry has no `source` condition.
const ai = readRows("package-archives.jsonl").find(row => row.workspace === "packages/ai");
const aiFile = join(releases, ai.file);
const aiBytes = readFileSync(aiFile);
assert.equal(ai.sha256, digest(aiBytes));
assert.equal(ai.integrity, `sha512-${digest(aiBytes, "sha512", "base64")}`);
execFileSync("tar", ["-xzf", aiFile, "-C", join(stage, "packages/ai"), "--strip-components=1", "package/dist"]);

const records = archives.map(({ file: _file, ...receipt }) => receipt);
const inputs = ["storage.test.ts", "vitest.config.ts", "worker.ts", "wrangler.jsonc"].map(file => {
	const path = `packages/durable/test-workers/${file}`;
	const bytes = readFileSync(join(stage, path));
	assert.deepEqual(bytes, git("show", `${pin}:${path}`), path);
	return { path, sha256: digest(bytes) };
});
console.log(JSON.stringify({ pin, lockSHA256: digest(pinnedLock), roots, platform, npmPlacements: records.length,
	workspaceLinks: workspaces.map(node => ({ key: node.linkKey, path: node.lockKey })), platformOmitted: incompatible.size,
	optionalPeerEdgesNotTraversed: optionalPeers.length,
	optionalPeerOnlyOmitted: optionalPeers.filter(edge => edge.to && !present.has(edge.to)).map(({ from, to }) => ({ from, to })),
	archiveRecordsSHA256: digest(JSON.stringify(records)),
	selectedNpmArchives: records.filter(row => /^(?:@cloudflare\/vitest-plugin|@cloudflare\/workerd-linux-arm64|@google\/genai|miniflare|openai|vitest|workerd|wrangler)$/.test(row.name)),
	aiArchiveSHA256: ai.sha256, inputs, scope: "Private Linux arm64/glibc staging; selected runtime, optional dependency and required peer edges only. No npm installation or lifecycle scripts." }, null, 2));
