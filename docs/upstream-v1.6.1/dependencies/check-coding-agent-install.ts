// Usage: bun run check-coding-agent-install.ts <upstream git mirror> <GitHub release assets> <npm _cacache>
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, posix, resolve } from "node:path";

const pin = "ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe";
assert.equal(process.argv.length, 5, "Expected the pinned git mirror, GitHub release assets, and npm _cacache");
const [mirror, assets, cache] = process.argv.slice(2).map(path => resolve(path));
const scope = "packages/coding-agent/install-lock";
const generator = "scripts/generate-coding-agent-install-lock.mjs";
const photonWrapper = "packages/coding-agent/src/utils/photon.ts";
const sdk = "node_modules/@silvia-odwyer/photon-node";
const sha = (data: Buffer) => createHash("sha256").update(data).digest("hex");
const digest = (data: Buffer) => ({ bytes: data.length, sha256: sha(data) });
const jsonl = (file: string) => readFileSync(join(import.meta.dir, file), "utf8").trim().split("\n").map(line => JSON.parse(line));
const git = (...args: string[]) => execFileSync("git", ["-C", mirror, ...args], { maxBuffer: 16 * 1024 * 1024 });
const pinned = (path: string) => git("show", `${pin}:${path}`);
const tarMember = (file: string, member: string) => execFileSync("tar", ["-xOzf", file, member], { maxBuffer: 8 * 1024 * 1024 });

const publishDocs = jsonl("release-assets.jsonl");
const sourceName = "pi-1.6.1-source.tar.gz";
function releaseAsset(name: string) {
	const record = publishDocs.find(row => row.file === name);
	const data = readFileSync(join(assets, name));
	assert(record, `Missing published release digest for ${name}`);
	assert.equal(data.length, record.size, `Release size differs for ${name}`);
	assert.equal(`sha256:${sha(data)}`, record.digest, `Release digest differs for ${name}`);
	return data;
}
const source = releaseAsset(sourceName);
const members = ["package.json", "package-lock.json"].map(file => {
	const asset = `pi-coding-agent-install-${file}`;
	const bytes = releaseAsset(asset);
	const member = `pi-1.6.1/${scope}/${file}`;
	assert.deepEqual(bytes, tarMember(join(assets, sourceName), member), member);
	assert.deepEqual(bytes, pinned(`${scope}/${file}`), `${scope}/${file}`);
	return { asset, member, gitPath: `${scope}/${file}`, ...digest(bytes) };
});

type Specs = Record<string, string>;
type Entry = {
	name?: string; version: string; resolved?: string; integrity?: string; link?: boolean;
	dependencies?: Specs; optionalDependencies?: Specs; peerDependencies?: Specs;
	peerDependenciesMeta?: Record<string, { optional?: boolean }>;
	os?: string[]; cpu?: string[]; libc?: string[]; hasInstallScript?: boolean;
};
type Lock = { lockfileVersion: number; packages: Record<string, Entry> };
const manifest = JSON.parse(pinned(`${scope}/package.json`)) as Entry & { engines: Specs; overrides: Specs };
const install = JSON.parse(pinned(`${scope}/package-lock.json`)) as Lock;
const workspaceBytes = pinned("package-lock.json");
const workspace = JSON.parse(workspaceBytes) as Lock;
assert.equal(install.lockfileVersion, 3);
assert.equal(workspace.lockfileVersion, 3);
assert.deepEqual(install.packages[""].dependencies, manifest.dependencies);

const closureBytes = readFileSync(join(import.meta.dir, "npm-closure.jsonl"));
const ledger = jsonl("npm-closure.jsonl");
const provenance = ledger.find(row => row.kind === "manifest");
assert.equal(provenance.pin, pin);
assert.equal(provenance.inputs["package-lock.json"], sha(workspaceBytes));
const npm = new Map(ledger.filter(row => row.kind === "npm").map(row => [row.lockKey, row]));
const links = new Map(ledger.filter(row => row.kind === "workspace").map(row => [row.linkKey, row]));
const tuple = ({ version, resolved, integrity }: Entry) => ({ version, resolved, integrity });
for (const [key, row] of npm) assert.deepEqual(tuple(row), tuple(workspace.packages[key]), key);
for (const [key, row] of links) {
	assert(workspace.packages[key].link);
	assert.equal(workspace.packages[key].resolved, row.lockKey, key);
	assert.equal(workspace.packages[row.lockKey].version, row.version, key);
}

let sameSlotSameArtifact = 0;
const outsideClosure = [];
const rebased = [];
const internal = [];
const pickedClosureWorkspaces = new Set<string>();
for (const [key, entry] of Object.entries(install.packages).sort(([a], [b]) => a.localeCompare(b))) {
	if (!key) continue;
	const original = workspace.packages[key];
	if (original?.link) {
		const path = original.resolved!;
		const name = workspace.packages[path].name!;
		assert.equal(entry.version, workspace.packages[path].version, key);
		assert.equal(entry.resolved, `https://registry.npmjs.org/${name}/-/${name.split("/")[1]}-${entry.version}.tgz`, key);
		assert.equal(entry.integrity, undefined, key);
		const archive = links.get(key)?.archive;
		if (archive) pickedClosureWorkspaces.add(key);
		internal.push({ key, workspace: path, version: entry.version, url: entry.resolved, installIntegrity: null,
			closureArchiveReceipt: archive ? { sha256: archive.sha256, integrity: archive.integrity } : null });
		continue;
	}
	assert(entry.integrity, `${key} has no external artifact integrity`);
	const prefix = [...links.keys()].find(slot => key.startsWith(`${slot}/node_modules/`));
	const sourceKey = original ? key : prefix ? `${links.get(prefix).lockKey}${key.slice(prefix.length)}` : key;
	assert(workspace.packages[sourceKey], `No pinned source placement for ${key}`);
	assert.deepEqual(tuple(entry), tuple(workspace.packages[sourceKey]), key);
	if (sourceKey !== key) rebased.push({ from: sourceKey, to: key, version: entry.version, inClosure: npm.has(sourceKey) });
	else if (npm.has(key)) sameSlotSameArtifact++;
	else outsideClosure.push({ key, version: entry.version });
}

function nearest(from: string, dependency: string): string | undefined {
	for (let dir = from || "."; ; dir = posix.dirname(dir)) {
		if (posix.basename(dir) !== "node_modules") {
			const candidate = posix.join(dir, "node_modules", dependency);
			if (Object.hasOwn(install.packages, candidate)) return candidate;
		}
		if (dir === ".") return;
	}
}
const edgeCounts = { runtime: 0, optional: 0, peer: 0, "optional-peer": 0 };
const peerEdges = [];
const reached = new Set([""]);
for (const from of reached) {
	const entry = install.packages[from];
	for (const field of ["dependencies", "optionalDependencies", "peerDependencies"] as const) {
		for (const [dependency, request] of Object.entries(entry[field] ?? {})) {
			const kind = field === "dependencies" ? "runtime" : field === "optionalDependencies" ? "optional" :
				entry.peerDependenciesMeta?.[dependency]?.optional ? "optional-peer" : "peer";
			edgeCounts[kind]++;
			const to = nearest(from, dependency);
			if (to) {
				assert(Bun.semver.satisfies(install.packages[to].version, manifest.overrides[dependency] ?? request), `${from} -> ${to} fails ${request}`);
				reached.add(to);
			} else assert(kind === "optional" || kind === "optional-peer", `${from} missing ${dependency}`);
			if (kind !== "runtime") {
				const edge = ledger.find(row => row.kind === "edge" && row.from === `npm:${from}` && row.class === kind && row.dependency === dependency);
				peerEdges.push({ from, dependency, kind, request, install: to ? { slot: to, version: install.packages[to].version } : null,
					closure: edge ? { state: edge.state, slot: edge.lockKey ?? null } : null });
			}
		}
	}
}
const placements = Object.keys(install.packages).length - 1;
assert.equal(reached.size - 1, placements, "An installer placement is unreachable");
assert.equal(sameSlotSameArtifact + outsideClosure.length + rebased.length + internal.length, placements);
const platform = Object.entries(install.packages).filter(([, value]) => value.os || value.cpu || value.libc).map(([key]) => key);
const scripts = Object.entries(install.packages).filter(([, value]) => value.hasInstallScript).map(([key, value]) => ({ key, version: value.version }));
const photon = install.packages[sdk];
const photonIntegrity = photon.integrity!;
assert.match(photonIntegrity, /^sha512-[A-Za-z0-9+/]{86}==$/);
const hash = Buffer.from(photonIntegrity.slice(7), "base64").toString("hex");
const archive = join(cache, "content-v2", "sha512", hash.slice(0, 2), hash.slice(2, 4), hash.slice(4));
const compressed = readFileSync(archive);
assert.equal(createHash("sha512").update(compressed).digest("hex"), hash, "Photon-node npm archive SRI differs");

const scratch = mkdtempSync(join(tmpdir(), "v161-install-lock."));
function offlineNode(...args: string[]) {
	return execFileSync("sudo", ["-n", "unshare", "--net", `--setuid=${process.getuid!()}`, `--setgid=${process.getgid!()}`,
		"env", "-i", `PATH=${process.env.PATH}`, "node", "--permission", `--allow-fs-read=${scratch}`, ...args],
	{ cwd: scratch, encoding: "utf8", timeout: 15_000 });
}
try {
	const manifests = git("ls-tree", "-r", "--name-only", pin).toString().split("\n").filter(path => /^packages\/[^/]+\/package\.json$/.test(path));
	const paths = ["package-lock.json", generator, photonWrapper, ...members.map(({gitPath}) => gitPath), ...manifests];
	execFileSync("tar", ["-xf", "-", "-C", scratch], { input: git("archive", pin, ...paths) });
	// This upstream --check is read-only and checks the exact generated bytes, not npm installation.
	offlineNode(generator, "--check");
	mkdirSync(join(scratch, sdk), { recursive: true });
	const selected = ["package.json", "photon_rs.js", "photon_rs_bg.wasm"].map(name => {
		const bytes = tarMember(archive, `package/${name}`);
		writeFileSync(join(scratch, sdk, name), bytes);
		return { member: name, ...digest(bytes) };
	});
	const packaged = JSON.parse(readFileSync(join(scratch, sdk, "package.json"), "utf8"));
	assert.equal(packaged.name, "@silvia-odwyer/photon-node");
	assert.equal(packaged.version, photon.version);
	assert.equal(packaged.main, "photon_rs.js");
	copyFileSync(join(import.meta.dir, "coding-agent-photon-node-witness.mjs"), join(scratch, "witness.mjs"));
	const smoke = JSON.parse(offlineNode("witness.mjs"));
	assert.equal(smoke.trace.length, 2);
	for (const { kind, ...member } of smoke.trace) {
		assert(kind === "load-commonjs" || kind === "instantiate-wasm");
		assert.deepEqual(member, selected.find(row => row.member === member.member));
	}
	console.log(JSON.stringify({ pin, assets: { source: { file: sourceName, ...digest(source) }, members },
		workspaceClosure: { roots: provenance.roots, lockSHA256: sha(workspaceBytes), ledgerSHA256: sha(closureBytes),
			npmPlacements: npm.size, linkedWorkspaces: links.size },
		install: { root: manifest.name, engines: manifest.engines, dependencies: manifest.dependencies, overrides: manifest.overrides,
			lockfileVersion: install.lockfileVersion, placements, checkedWithPinnedGenerator: generator,
			generatorSHA256: sha(pinned(generator)), sameSlotSameArtifact, outsideClosure, rebased, internal,
			closureWorkspacesNotShipped: [...links.keys()].filter(key => !pickedClosureWorkspaces.has(key)),
			edgeCounts, optionalPeerAbsent: peerEdges.filter(row => row.kind === "optional-peer" && row.install === null).length,
			peerEdges, platformConstrainedSlots: platform, installScriptSlots: scripts },
		photonNode: { lockKey: sdk, lock: tuple(photon), archive: digest(compressed), selectedMembers: selected,
			wrapper: { path: photonWrapper, sha256: sha(pinned(photonWrapper)) }, offlineSmoke: smoke },
		scope: "Static lock comparison and one Node Photon smoke in an isolated Linux network namespace; no npm install, lifecycle scripts, CLI, Bun binary, or Cloudflare runtime executed." }, null, 2));
} finally {
	rmSync(scratch, { recursive: true, force: true });
}
