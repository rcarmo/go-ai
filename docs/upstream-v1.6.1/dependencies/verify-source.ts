import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync, readlinkSync } from "node:fs";
import { join } from "node:path";

const pin = "ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe";
const [mirror, root] = process.argv.slice(2);
if (process.argv.length !== 4) throw Error("Usage: verify-source.ts <git-mirror> <clean-oracle>");
const git = (...args: string[]) => execFileSync("git", ["-C", mirror, ...args], { maxBuffer: 10 << 20 }).toString();
if (git("rev-parse", "--show-object-format").trim() !== "sha1") throw Error("Expected a Git SHA-1 tree");
const expected = new Set<string>();
for (const record of git("ls-tree", "-r", "-z", pin).split("\0").filter(Boolean)) {
	const tab = record.indexOf("\t");
	const [mode, type, hash] = record.slice(0, tab).split(" ");
	const path = record.slice(tab + 1);
	if (type !== "blob" || !["100644", "100755", "120000"].includes(mode)) throw Error(`Unexpected Git entry: ${record}`);
	const full = join(root, path);
	const stat = lstatSync(full);
	const symlink = mode === "120000";
	const matchingMode = symlink ? stat.isSymbolicLink() : stat.isFile() && Boolean(stat.mode & 0o100) === (mode === "100755");
	if (!matchingMode) throw Error(`Mode differs: ${path}`);
	const bytes = symlink ? readlinkSync(full, { encoding: "buffer" }) : readFileSync(full);
	const got = createHash("sha1").update(`blob ${bytes.length}\0`).update(bytes).digest("hex");
	if (got !== hash) throw Error(`Blob bytes differ: ${path}`);
	expected.add(path);
}
function scan(path: string) {
	for (const entry of readdirSync(join(root, path), { withFileTypes: true })) {
		const relative = join(path, entry.name);
		if (entry.isDirectory()) scan(relative);
		else if (!expected.has(relative)) throw Error(`Extra source entry: ${relative}`);
	}
}
scan("");
console.log(JSON.stringify({ pin, verifiedBlobs: expected.size, exactNonDirectoryPaths: true }));
