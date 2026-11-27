import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { lstatSync, readdirSync, readFileSync, realpathSync } from "node:fs";
import { basename, join, resolve } from "node:path";

const [stageDir, npmDist, extra] = Bun.argv.slice(2);
if (!stageDir || !npmDist || extra) throw Error("Usage: bun run verify-ai-stage.ts <private npm-ci stage> <verified pi-ai dist>");
const stage = resolve(stageDir);
const read = (path: string) => JSON.parse(readFileSync(path, "utf8"));
const hash = (path: string) => createHash("sha256").update(readFileSync(path)).digest("hex");
const pin = read(join(import.meta.dir, "inputs.json"));
assert.equal(hash(join(stage, "package-lock.json")), pin.packageLockSHA256);
const lock = read(join(stage, "package-lock.json")).packages;
const installed = read(join(stage, "node_modules/.package-lock.json")).packages;
const links: string[] = [];
let packages = 0;
for (const [key, row] of Object.entries<any>(installed)) {
	if (!/(^|\/)node_modules\//.test(key)) continue;
	assert(!key.startsWith("/") && !key.split("/").some(s => s === ".." || s === "." || !s));
	const want = lock[key];
	assert(want, `${key}: missing from pinned lock`);
	for (const field of ["link", "version", "resolved", "integrity"]) assert.equal(row[field], want[field], `${key}: ${field}`);
	const path = join(stage, key);
	if (row.link) {
		assert(lstatSync(path).isSymbolicLink(), `${key}: not a workspace link`);
		assert.match(row.resolved, /^packages\/(ai|schema|telemetry)$/);
		assert.equal(realpathSync(path), join(stage, row.resolved));
		links.push(row.resolved);
	} else {
		assert(!lstatSync(path).isSymbolicLink(), `${key}: unexpected symlink`);
		assert.equal(read(join(path, "package.json")).version, want.version, key);
		packages++;
	}
}
assert.deepEqual(links.sort(), ["packages/ai", "packages/schema", "packages/telemetry"]);
assert.equal(lock["node_modules/vitest"].version, "4.1.12");
assert.equal(read(join(stage, "node_modules/vitest/package.json")).version, "4.1.12");

const sourceData = join(stage, "packages/ai/src/providers/data");
const packedData = join(npmDist, "providers/data");
const manifestPath = ".manifest.json";
const { generatedAt: _, ...sourceManifest } = read(join(sourceData, manifestPath));
const { generatedAt: __, ...packedManifest } = read(join(packedData, manifestPath));
assert.equal(sourceManifest.schemaVersion, 6);
assert.deepEqual(sourceManifest, packedManifest);
const names = Object.keys(sourceManifest.files).sort();
assert.deepEqual(readdirSync(sourceData).sort(), [...names, manifestPath].sort());
assert.deepEqual(readdirSync(packedData).sort(), [...names, manifestPath].sort());
for (const name of names) {
	assert.equal(name, basename(name));
	for (const path of [sourceData, packedData]) assert.equal(hash(join(path, name)), sourceManifest.files[name], name);
}
console.log(JSON.stringify({ vitest: "4.1.12", npmPlacements: packages, workspaces: links, providerDataFiles: names.length, modelsModuleSHA256: hash(join(npmDist, "models.generated.js")) }));
