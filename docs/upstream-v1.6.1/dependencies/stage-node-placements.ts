import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, mkdirSync, readlinkSync, symlinkSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

// A production-only workspace installation adds the Node suite's runtime
// dependencies without reinstalling its already pinned Vitest runner.
if (process.argv.length !== 4) throw Error("Usage: stage-node-placements.ts <npm-runtime-install> <git-oracle>");
const [stage, oracle] = process.argv.slice(2).map(path => resolve(path));
const locked = (await Bun.file(join(oracle, "package-lock.json")).json()).packages;
const installed = (await Bun.file(join(stage, "node_modules/.package-lock.json")).json()).packages;
const placements = Object.keys(installed).filter(key => `/${key}`.includes("/node_modules/")).sort();
const rows = [];
for (const key of placements) {
	if (key.startsWith("/") || key.split("/").some(part => part === "" || part === ".." || part === ".")) {
		throw Error(`Invalid package placement: ${key}`);
	}
	const source = join(stage, key), dest = join(oracle, key), entry = installed[key], expected = locked[key];
	if (!expected || entry.link !== expected.link || entry.integrity !== expected.integrity || entry.resolved !== expected.resolved || entry.version !== expected.version) {
		throw Error(`Installed metadata differs: ${key}`);
	}
	mkdirSync(dirname(dest), { recursive: true });
	const migrated = !existsSync(dest);
	if (entry.link) {
		const target = entry.resolved;
		if (!target.startsWith("packages/") || resolve(dirname(source), readlinkSync(source)) !== join(stage, target)) {
			throw Error(`Wrong source link: ${key}`);
		}
		if (migrated) symlinkSync(relative(dirname(dest), join(oracle, target)), dest, "dir");
		if (resolve(dirname(dest), readlinkSync(dest)) !== join(oracle, target)) throw Error(`Wrong oracle link: ${key}`);
	} else {
		if (!expected.integrity || !lstatSync(source).isDirectory()) throw Error(`Unverified package: ${key}`);
		if (migrated) execFileSync("cp", ["-al", "--", source, dest]);
		if (!lstatSync(dest).isDirectory()) throw Error(`Wrong oracle package: ${key}`);
		execFileSync("diff", ["-qr", "--", source, dest]);
	}
	rows.push({ key, link: entry.link ?? false, version: entry.version, integrity: entry.integrity });
}
for (const row of rows) console.log(JSON.stringify(row));
