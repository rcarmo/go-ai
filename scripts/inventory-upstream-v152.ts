#!/usr/bin/env bun
// Record direct release tree differences and verify the focused source checkout.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const base = "f9a26365568127461c47a07a37dcb42c140a3ac5";
const target = "b4afdddf44e059c67ccf5097e5317d913521b470";
const scope = [
	"packages/ai", "packages/codemode", "packages/durable", "packages/schema", "packages/telemetry",
	..."context delta json output sqlite watch".split(" ").map((name) => `packages/chord/src/${name}`),
	"packages/chord/package.json", "package-lock.json",
];
const [repository, snapshot, mode] = process.argv.slice(2);
if (!repository || !snapshot || (mode !== undefined && mode !== "--write")) {
	throw new Error("usage: bun run scripts/inventory-upstream-v152.ts <git-repository-with-tags> <source-snapshot> [--write]");
}
const targetFile = resolve(dirname(fileURLToPath(import.meta.url)), "../docs/upstream-v1.5.2/source-delta.tsv");
function git(...args: string[]): string {
	const result = Bun.spawnSync(["git", "-C", repository, ...args], { stderr: "pipe" });
	if (result.exitCode !== 0) throw new Error(new TextDecoder().decode(result.stderr));
	return new TextDecoder().decode(result.stdout).trimEnd();
}
if (git("rev-parse", "refs/tags/v1.5.0^{commit}") !== base || git("rev-parse", "refs/tags/v1.5.2^{commit}") !== target) {
	throw new Error("unexpected upstream tag resolution");
}
const source = git("ls-tree", "-r", target, "--", ...scope).split("\n");
for (const row of source) {
	const match = /^\d+ blob ([0-9a-f]{40})\t(.+)$/.exec(row);
	if (!match) throw new Error(`unexpected tree row: ${row}`);
	const [, object, path] = match;
	const bytes = readFileSync(join(snapshot, path));
	const hash = new Bun.CryptoHasher("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
	if (hash !== object) throw new Error(`upstream file mismatch: ${path}`);
}
const delta = git("diff", "--no-renames", "--name-status", base, target, "--", ...scope);
const manifest = `# ${base}..${target}\n# status\tupstream path\n${delta}\n`;
if (mode === "--write") writeFileSync(targetFile, manifest);
else if (readFileSync(targetFile, "utf8") !== manifest) throw new Error("upstream release delta changed");
console.log(`PASS: exact release tags, ${source.length} tracked source files and ${delta.split("\n").length} changed paths`);
