import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const base = "022fe0cb78a9c9e17f2c4aa68012f2f58961c919";
const [mirror, retained] = process.argv.slice(2);
if (process.argv.length !== 4) throw Error("Usage: bun run verify-provider-archives.ts <pinned-git-mirror> <retained-archive-dir>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const git = (repo: string, object: string) => execFileSync("git", ["-C", repo, "show", object]);
const sha256 = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const lockBytes = git(mirror, `${pin}:package-lock.json`);
const lock = JSON.parse(lockBytes.toString("utf8"));
const previous = git(import.meta.dir, `${base}:docs/upstream-v1.6.0/dependencies/npm-archives.jsonl`)
	.toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
check(lock.lockfileVersion === 3 && previous[0]?.pin === pin && previous[0]?.lockSha256 === sha256(lockBytes), "Pinned lock differs from the original archive receipt");

const targets = [
	{ name: "@google/genai", version: "2.32.0", file: "genai-2.32.0.tgz" },
	{ name: "openai", version: "7.59.0", file: "openai-7.59.0.tgz" },
];
const records = targets.map(({ name, version, file }) => {
	const lockKey = `node_modules/${name}`;
	const id = `npm:${lockKey}`;
	const url = `https://registry.npmjs.org/${name}/-/${file}`;
	const pinned = lock.packages?.[lockKey];
	const old = previous.find(row => row.kind === "archive" && row.id === id);
	check(pinned && pinned.version === version && pinned.resolved === url && /^sha512-[A-Za-z0-9+/]{86}==$/.test(pinned.integrity), `Unexpected pinned package placement: ${id}`);
	check(old?.state === "unresolved-no-archive-in-checked-caches" && old.version === version && old.resolved === url && old.integrity === pinned.integrity, `Previous archive receipt differs: ${id}`);
	const bytes = readFileSync(join(retained, file));
	const integrity = `sha512-${createHash("sha512").update(bytes).digest("base64")}`;
	check(integrity === pinned.integrity, `Pinned archive integrity mismatch: ${file}`);
	check(bytes[0] === 0x1f && bytes[1] === 0x8b, `Expected a compressed archive: ${file}`);
	return { kind: "archive", id, name, version, lockKey, url, file, bytes: bytes.length, integrity, sha256: sha256(bytes), state: "verified-archive-bytes", priorState: old.state, loadedProvenance: "unverified" };
});
console.log(JSON.stringify({ kind: "manifest", format: 1, pin, base, lockSha256: sha256(lockBytes), verification: "offline-compressed-archive-integrity" }));
for (const row of records) console.log(JSON.stringify(row));
