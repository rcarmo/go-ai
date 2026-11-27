import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, posix, resolve } from "node:path";

const [mirror, baseMetadataPath, baseAssets, nextMetadataPath, nextAssets, bundles, npm, cache] = process.argv.slice(2).map(path => resolve(path));
assert.equal(process.argv.length, 10, "Usage: bun run check-release.ts MIRROR BASE_RELEASE_JSON BASE_ASSETS NEXT_RELEASE_JSON NEXT_ASSETS BUNDLE_DIR NEXT_NPM_DIR NPM_CACACHE");
const versions = ["1.6.1", "1.7.0"];
const pins = ["ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe", "fb0fe823539f6d04850f6e34748c01f01f212e1f"];
const read = (path: string) => readFileSync(path);
const json = (data: Buffer) => JSON.parse(data.toString());
const sha = (data: Buffer) => createHash("sha256").update(data).digest("hex");
const sri = (data: Buffer) => `sha512-${createHash("sha512").update(data).digest("base64")}`;
const digest = (data: Buffer) => ({ bytes: data.length, sha256: sha(data) });
const run = (command: string, args: string[], options = {}) => execFileSync(command, args, { maxBuffer: 180 * 1024 * 1024, ...options });
const git = (...args: string[]) => run("git", ["-C", mirror, ...args]);
const pinned = (i: number, path: string) => git("show", `${pins[i]}:${path}`);
const tar = (path: string, member: string) => run("tar", ["-xOzf", path, member]);
const sorted = <T>(record: Record<string, T>) => Object.keys(record).sort().map(key => [key, record[key]] as const);
const union = (a: Record<string, unknown>, b: Record<string, unknown>) => [...new Set([...Object.keys(a), ...Object.keys(b)])].sort();
const same = (a: unknown, b: unknown) => Bun.deepEquals(a, b, true);
const out = (row: object) => console.log(JSON.stringify(row));
const cacheArchive = (integrity: string) => {
	assert.match(integrity, /^sha512-[A-Za-z0-9+/]{86}==$/);
	const hash = Buffer.from(integrity.slice(7), "base64").toString("hex");
	const path = join(cache, "content-v2", "sha512", hash.slice(0, 2), hash.slice(2, 4), hash.slice(4));
	assert.equal(sri(read(path)), integrity);
	return path;
};
const roots = ["packages/ai", "packages/codemode", "packages/durable", "packages/schema"];
const ledgerBytes = read(join(import.meta.dir, "../../upstream-v1.6.1/dependencies/npm-closure.jsonl"));
const baseline = ledgerBytes.toString().trimEnd().split("\n").map(row => JSON.parse(row));
const nextInputs = json(read(join(import.meta.dir, "../inputs.json")));
const nextNpm = new Map([...nextInputs.npm.map((item: any) => ({ ...item, file: basename(item.url) })),
	...json(read(join(import.meta.dir, "additional-npm-inputs.json")))].map(item => [item.name, item]));
const fields = ["dependencies", "optionalDependencies", "peerDependencies", "devDependencies"];
const sameDeps = (a: any, b: any, wanted = fields) => {
	for (const field of wanted) assert.deepEqual(a[field] ?? {}, b[field] ?? {}, field);
	assert.deepEqual(a.peerDependenciesMeta ?? {}, b.peerDependenciesMeta ?? {});
};
const scope = "packages/coding-agent/install-lock";
const generator = "scripts/generate-coding-agent-install-lock.mjs";
const smokeScript = "scripts/smoke-test-codemode-binary.mjs";
const native = "native/linux/prebuilds/linux-arm64/linux-platform-x11.node";
const photon = "node_modules/@silvia-odwyer/photon-node";
const releaseFilenames = ["pi-coding-agent-install-package.json", "pi-coding-agent-install-package-lock.json"];
const assetMetadata = [baseMetadataPath, nextMetadataPath].map(path => json(read(path)));
const historicalAssets = new Map(read(join(import.meta.dir, "../../upstream-v1.6.1/dependencies/release-assets.jsonl")).toString().trimEnd().split("\n")
	.map(line => JSON.parse(line)).map(row => [row.file, row]));
const workspaceLocks = pins.map((_, i) => json(pinned(i, "package-lock.json")));
const workspaceRoots = pins.map((_, i) => json(pinned(i, "package.json")));
const graphKeys = ["version", "resolved", "integrity"];
const tuple = (entry: any) => graphKeys.map(field => entry[field]);
const npmPlacement = (key: string, item: any) => (key.startsWith("node_modules/") || key.includes("/node_modules/")) && item && !item.link;
const nextArchiveProof = new Map();

function verifyArchive(path: string) {
	if (nextArchiveProof.has(path)) return nextArchiveProof.get(path);
	const source = pinned(1, `${path}/package.json`);
	const manifest = json(source);
	const record = nextNpm.get(manifest.name) as any;
	assert(record, manifest.name);
	assert.equal(record.version, manifest.version);
	assert.equal(record.url, `https://registry.npmjs.org/${manifest.name}/-/${record.file}`);
	if (record.gitManifestSHA256) assert.equal(record.gitManifestSHA256, sha(source));
	const filename = join(npm, record.file);
	const bytes = read(filename);
	assert.equal(sha(bytes), record.sha256, record.file);
	assert.equal(sri(bytes), record.integrity, record.file);
	const packed = json(tar(filename, "package/package.json"));
	assert.equal(packed.name, manifest.name);
	assert.equal(packed.version, manifest.version);
	sameDeps(packed, manifest, fields.slice(0, 3));
	const proof = { path, name: record.name, version: record.version, url: record.url, integrity: record.integrity,
		gitManifestSHA256: sha(source), ...digest(bytes) };
	nextArchiveProof.set(path, proof);
	return proof;
}

function nearest(entries: Record<string, any>, from: string, dependency: string) {
	for (let dir = from || "."; ; dir = posix.dirname(dir)) {
		const key = posix.join(dir, "node_modules", dependency);
		if (posix.basename(dir) !== "node_modules" && Object.hasOwn(entries, key)) return key;
		if (dir === ".") return;
	}
}

function graph(i: number, installer?: { manifest: any; lock: any }) {
	const root = installer?.manifest ?? workspaceRoots[i];
	const lock = installer?.lock ?? workspaceLocks[i];
	const entries = lock.packages;
	assert.equal(lock.lockfileVersion, 3);
	assert.equal(entries[""].name, root.name);
	if (installer) sameDeps(entries[""], root);
	const ids = new Set(installer ? [""] : roots);
	const initialLinks = new Map(roots.map(path => [path, `node_modules/${entries[path]?.name}`]));
	const npmSlots = new Set<string>();
	const links = new Map<string, string>();
	const edges = [];
	for (const key of ids) {
		const node = entries[key];
		assert(node && !node.link, key);
		if (!installer && key.startsWith("packages/") && !key.includes("/node_modules/")) {
			const manifest = json(pinned(i, `${key}/package.json`));
			assert.equal(node.version, versions[i]);
			assert.equal(node.name, manifest.name);
			assert.equal(node.version, manifest.version);
			sameDeps(node, manifest);
			const slot = initialLinks.get(key) ?? `node_modules/${node.name}`;
			assert.deepEqual(entries[slot], { resolved: key, link: true }, slot);
			links.set(slot, key);
			if (i === 1) verifyArchive(key);
		} else if (key) npmSlots.add(key);
		for (const field of (installer ? fields.slice(0, 3) : fields)) {
			for (const [dependency, request] of sorted<string>(node[field] ?? {})) {
				const kind = field === "dependencies" ? "runtime" : field === "optionalDependencies" ? "optional" : field === "devDependencies" ? "development" :
					node.peerDependenciesMeta?.[dependency]?.optional ? "optional-peer" : "peer";
				const slot = nearest(entries, key, dependency);
				const locked = slot && entries[slot];
				const to = slot ? (locked.link ? locked.resolved : slot) : undefined;
				const override = root.overrides?.[dependency];
				if (to) {
					assert(Bun.semver.satisfies(entries[to].version, override ?? request), `${key} -> ${to} does not satisfy ${override ?? request}`);
					ids.add(to);
				} else assert(kind === "optional" || kind === "optional-peer", `${key} is missing ${dependency}`);
				edges.push({ from: key, dependency, field, class: kind, request, override, lockKey: slot, to, state: to ? "resolved" : `${kind}-absent` });
			}
		}
	}
	return { npmSlots, links, edges };
}
const graphs = pins.map((_, i) => graph(i));
const [base, next] = graphs;
const nameOf = (key: string) => key.split("/node_modules/").at(-1)!.replace(/^node_modules\//, "");
const edgeId = ({ from, field, dependency }: { from: string; field: string; dependency: string }) => JSON.stringify([from, field, dependency]);
const edgeMap = (edges: ReturnType<typeof graph>["edges"]) => new Map(edges.map(edge => [edgeId(edge), edge]));
const classify = (items: any[], field: string) => Object.fromEntries(sorted(items.reduce((counts, item) => {
	counts[item[field]] = (counts[item[field]] ?? 0) + 1;
	return counts;
}, {})));
const platform = (lock: any, slots: Set<string>) => [...slots].sort().filter(key => {
	const entry = lock.packages[key];
	return entry.os || entry.cpu || entry.libc;
});
const idPath = (id?: string) => id?.replace(/^(npm|workspace):/, "");
assert.equal(nextInputs.upstreamCommit, pins[1]);
assert.equal(nextInputs.packageLockSHA256, sha(pinned(1, "package-lock.json")));
assert.equal(baseline[0].pin, pins[0]);
assert.equal(baseline[0].inputs["package-lock.json"], sha(pinned(0, "package-lock.json")));
assert.deepEqual(baseline[0].roots, roots);
for (const [path, hash] of sorted<string>(baseline[0].inputs)) assert.equal(sha(pinned(0, path)), hash);
assert.deepEqual(base.npmSlots, new Set(baseline.filter(row => row.kind === "npm").map(row => row.lockKey)));
assert.deepEqual(base.links, new Map(baseline.filter(row => row.kind === "workspace").map(row => [row.linkKey, row.lockKey])));
for (const row of baseline.filter(row => row.kind === "npm")) {
	const entry = workspaceLocks[0].packages[row.lockKey];
	assert.deepEqual(tuple(row), tuple(entry), row.lockKey);
	assert.deepEqual(row.platform, Object.fromEntries(["os", "cpu", "libc"].filter(field => entry[field]).map(field => [field, entry[field]])), row.lockKey);
	assert.equal(row.hasInstallScript, entry.hasInstallScript ?? false, row.lockKey);
}
const baseEdges = edgeMap(base.edges);
const nextEdges = edgeMap(next.edges);
const ledgerEdges = baseline.filter(row => row.kind === "edge");
assert.equal(baseEdges.size, ledgerEdges.length);
for (const row of ledgerEdges) {
	const edge = { from: idPath(row.from)!, dependency: row.dependency, field: row.field, class: row.class,
		request: row.request, override: row.override, lockKey: row.lockKey, to: idPath(row.to), state: row.state };
	assert.deepEqual(baseEdges.get(edgeId(edge)), edge);
}
assert.deepEqual(base.npmSlots, next.npmSlots);
assert.deepEqual(base.links, next.links);
assert.equal(baseEdges.size, nextEdges.size);
const workspaceRequestBumps = [];
for (const [key, before] of baseEdges) {
	const after = nextEdges.get(key);
	if (before.request === versions[0] && before.lockKey && base.links.has(before.lockKey)) {
		assert.deepEqual(after, { ...before, request: versions[1] });
		workspaceRequestBumps.push([before.from, before.class, before.dependency]);
	} else assert.deepEqual(after, before, key);
}
const [left, right] = workspaceLocks.map(lock => lock.packages);
let sameExternalPlacements = 0;
const changedExternalPlacements = [];
for (const key of union(left, right)) {
	if (!npmPlacement(key, left[key]) && !npmPlacement(key, right[key])) continue;
	if (same(left[key], right[key])) sameExternalPlacements++;
	else changedExternalPlacements.push({ key, before: left[key] ?? null, after: right[key] ?? null });
}
assert.deepEqual(changedExternalPlacements.map(row => row.key), ["apps/p3/node_modules/@earendil-works/elanor-react"]);
const elanor = changedExternalPlacements[0];
assert.equal(elanor.before, null);
assert(!next.npmSlots.has(elanor.key));
assert.equal(elanor.after.resolved, "https://registry.npmjs.org/@earendil-works/elanor-react/-/elanor-react-0.1.1.tgz");
const elanorPath = join(npm, "elanor-react-0.1.1.tgz");
const elanorArchive = read(elanorPath);
assert.equal(sri(elanorArchive), elanor.after.integrity);
const elanorPackage = json(tar(elanorPath, "package/package.json"));
assert.equal(elanorPackage.name, "@earendil-works/elanor-react");
assert.equal(elanorPackage.version, elanor.after.version);
assert.deepEqual(elanorPackage.peerDependencies, elanor.after.peerDependencies);
assert.equal(json(pinned(1, "apps/p3/package.json")).dependencies[elanorPackage.name], elanor.after.version);

out({ kind: "pins", pins, versions, baselineLedgerSHA256: sha(ledgerBytes), workspaceLockSHA256: pins.map((_, i) => sha(pinned(i, "package-lock.json"))) });
out({ kind: "workspace-closure", roots, npmPlacements: next.npmSlots.size, linkedWorkspaces: [...next.links].sort(), strictlyUnchangedExternalLockRecords: sameExternalPlacements,
	edges: classify(next.edges, "class"), resolution: classify(next.edges, "state"), platformConstrainedNpm: platform(workspaceLocks[1], next.npmSlots).length,
	workspaceRequestBumps: workspaceRequestBumps.sort(), unchangedReachByRoot: baseline.at(-1).reachByRoot });
out({ kind: "outside-closure-lock-delta", ...elanor, archive: digest(elanorArchive) });

const installGraphs = [];
const bundlePaths = [];
const releaseFingerprints = [];
for (const [i, ver] of versions.entries()) {
	const assets = i === 0 ? baseAssets : nextAssets;
	const metadata = assetMetadata[i];
	assert.equal(metadata.tag_name, `v${ver}`);
	const source = `pi-${ver}-source.tar.gz`;
	const bundle = "pi-linux-arm64.tar.gz";
	const bundleDir = join(bundles, ver);
	const paths = Object.fromEntries([source, ...releaseFilenames, bundle, "SHA256SUMS"].map(file => [file, join(file === bundle || file === "SHA256SUMS" ? bundleDir : assets, file)]));
	const sums = read(paths.SHA256SUMS).toString().trimEnd().split("\n").map(line => line.split(/ +/)).map(([hash, file]) => [file, hash]);
	const assetsProof = [];
	for (const [file, path] of Object.entries(paths)) {
		const row = metadata.assets.find((row: any) => row.name === file);
		assert.equal(row.browser_download_url, `https://github.com/earendil-works/pi/releases/download/v${ver}/${file}`);
		const data = read(path);
		assert.equal(data.length, row.size, file);
		assert.equal(`sha256:${sha(data)}`, row.digest, file);
		if (i === 0 && (file === source || releaseFilenames.includes(file))) {
			const receipt = historicalAssets.get(file);
			assert(receipt, file);
			assert.equal(row.digest, receipt.digest, file);
			assert.equal(row.size, receipt.size, file);
		}
		if (file !== "SHA256SUMS") assert.equal(sha(data), new Map(sums).get(file), file);
		assetsProof.push({ file, id: row.id, ...digest(data) });
	}
	const [manifest, lock] = releaseFilenames.map(file => {
		const path = `${scope}/${file.slice("pi-coding-agent-install-".length)}`;
		const contents = read(paths[file]);
		assert.deepEqual(contents, tar(paths[source], `pi-${ver}/${path}`), file);
		assert.deepEqual(contents, pinned(i, path), file);
		return json(contents);
	});
	assert.equal(manifest.version, ver);
	assert.equal(lock.version, ver);
	assert.equal(lock.packages[""].version, ver);
	const closure = graphs[i];
	const rootLock = workspaceLocks[i];
	const installer = graph(i, { manifest, lock });
	installGraphs.push({ lock, manifest, ...installer });
	assert.equal(installer.npmSlots.size, Object.keys(lock.packages).length - 1);
	const placementKinds: Record<string, number> = {};
	const rebased = [];
	const internal = [];
	for (const key of [...installer.npmSlots].sort()) {
		const entry = lock.packages[key];
		const original = rootLock.packages[key];
		let placement;
		if (original?.link) {
			placement = "workspace-registry-replacement";
			const path = original.resolved;
			const workspace = rootLock.packages[path];
			assert.equal(entry.version, workspace.version, key);
			assert.equal(entry.resolved, `https://registry.npmjs.org/${workspace.name}/-/${workspace.name.split("/")[1]}-${ver}.tgz`, key);
			assert.equal(entry.integrity, undefined, key);
			sameDeps(entry, workspace, fields.slice(0, 3));
			if (i === 1) assert.equal(verifyArchive(path).url, entry.resolved);
			internal.push({ key, path, lockIntegrity: null });
		} else {
			assert.match(entry.integrity, /^sha512-[A-Za-z0-9+/]{86}==$/);
			const prefix = [...closure.links.keys()].find(slot => key.startsWith(`${slot}/node_modules/`));
			const sourceKey = original ? key : prefix ? `${closure.links.get(prefix)}${key.slice(prefix.length)}` : key;
			assert.deepEqual(tuple(entry), tuple(rootLock.packages[sourceKey]), key);
			if (sourceKey !== key) {
				placement = "rebased";
				rebased.push({ from: sourceKey, to: key });
			} else placement = closure.npmSlots.has(key) ? "same-slot-in-closure" : "same-slot-outside-closure";
		}
		placementKinds[placement] = (placementKinds[placement] ?? 0) + 1;
	}
	const platformSlots = platform(lock, installer.npmSlots);
	const scripts = [...installer.npmSlots].sort().filter(key => lock.packages[key].hasInstallScript).map(key => [key, lock.packages[key].version]);
	const optionalMissing = installer.edges.filter(edge => edge.state !== "resolved").map(edge => [edge.from, edge.dependency, edge.class]);
	out({ kind: "installer", version: ver, placementKinds, rebased, internal, edges: classify(installer.edges, "class"), optionalMissing, platformSlots, scripts });

	const list = run("tar", ["-tzf", paths[bundle]]).toString().trimEnd().split("\n").sort();
	assert.equal(new Set(list).size, list.length);
	assert(list.every(member => member.startsWith("pi/") && !member.split("/").includes("..")));
	assert(!list.some(member => member.includes("/node_modules/") || member === "pi/package-lock.json"));
	bundlePaths.push(list);
	const packageBytes = tar(paths[bundle], "pi/package.json");
	assert.deepEqual(packageBytes, pinned(i, "packages/coding-agent/package.json"));
	const nativeBytes = tar(paths[bundle], `pi/${native}`);
	assert.deepEqual(nativeBytes, pinned(i, `packages/tui/${native}`));
	const photonIntegrity = lock.packages[photon].integrity;
	assert.equal(photonIntegrity, rootLock.packages[photon].integrity);
	const photonArchive = cacheArchive(photonIntegrity);
	const wasm = tar(photonArchive, "package/photon_rs_bg.wasm");
	assert.deepEqual(wasm, tar(paths[bundle], "pi/photon_rs_bg.wasm"));
	const elf = tar(paths[bundle], "pi/pi");
	assert.equal(elf.subarray(0, 4).toString("hex"), "7f454c46");
	assert.equal(elf[4], 2); // ELF64
	assert.equal(elf[5], 1); // little-endian
	assert.equal(elf.readUInt16LE(18), 183); // AArch64
	const buildFiles = [".github/workflows/build-binaries.yml", "scripts/build-binaries.sh", generator, smokeScript];
	const recipe = buildFiles.map(file => {
		const contents = pinned(i, file);
		assert.deepEqual(contents, tar(paths[source], `pi-${ver}/${file}`), file);
		return { file, sha256: sha(contents) };
	});
	releaseFingerprints.push(recipe);
	out({ kind: "release-assets", version: ver, assets: assetsProof, recipe });
	out({ kind: "bundle", version: ver, entries: list.length, elf: digest(elf), packageManifest: digest(packageBytes), native: digest(nativeBytes),
		photon: { version: lock.packages[photon].version, integrity: photonIntegrity, archive: digest(read(photonArchive)), wasm: digest(wasm) } });

	const scratch = mkdtempSync(join(tmpdir(), "pi-v170-release-check."));
	try {
		const home = join(scratch, "home");
		mkdirSync(home);
		const manifests = git("ls-tree", "-r", "--name-only", pins[i]).toString().split("\n").filter(path => /^packages\/[^/]+\/package\.json$/.test(path));
		const selected = ["package-lock.json", generator, smokeScript, ...releaseFilenames.map(file => `${scope}/${file.slice("pi-coding-agent-install-".length)}`), ...manifests];
		run("tar", ["--no-same-owner", "-xf", "-", "-C", scratch], { input: git("archive", pins[i], ...selected) });
		mkdirSync(join(scratch, "bundle"));
		run("tar", ["--no-same-owner", "-xzf", paths[bundle], "-C", join(scratch, "bundle"), "pi/pi", "pi/package.json", `pi/${native}`, "pi/photon_rs_bg.wasm"]);
		const offline = (script: string, ...args: string[]) => run("env", ["-i", `PATH=${process.env.PATH}`, "LC_ALL=C", `HOME=${home}`, `TMPDIR=${home}`,
			`XDG_CACHE_HOME=${home}`, `XDG_CONFIG_HOME=${home}`, "PI_OFFLINE=1", `PI_CODING_AGENT_DIR=${join(home, "agent")}`, "unshare", "-Urn", "bash", "-euc",
			'ip link set lo up; test -z "$(ip -o route)$(ip -6 -o route)"; test "$(ip -o link show | wc -l)" = 1; exec timeout 35 "$@"', "_", script, ...args], { cwd: scratch, timeout: 38_000 }).toString().trim();
		offline("node", generator, "--check");
		const binary = join(scratch, "bundle/pi/pi");
		assert.equal(offline(binary, "--version"), ver);
		const smoke = offline("node", smokeScript, binary);
		assert.equal(smoke, "PI_CODEMODE_BINARY_SMOKE_OK");
		out({ kind: "offline-smoke", version: ver, platform: `${process.platform}-${process.arch}`, node: offline("node", "--version"),
			installerGenerator: "--check passed", bundleVersion: ver, codemode: smoke });
	} finally {
		rmSync(scratch, { recursive: true, force: true });
	}
}
assert.deepEqual(bundlePaths[0], bundlePaths[1]);
assert.deepEqual(releaseFingerprints[0], releaseFingerprints[1]);
const [baseInstall, nextInstall] = installGraphs;
const a = baseInstall.lock.packages;
const b = nextInstall.lock.packages;
assert.deepEqual(Object.keys(a).sort(), Object.keys(b).sort());
const internalNames = new Set([...nextInstall.npmSlots].filter(key => !b[key].integrity).map(nameOf));
function advance(original: any) {
	const next = structuredClone(original);
	for (const field of fields) {
		for (const dependency of Object.keys(next[field] ?? {})) {
			if (internalNames.has(dependency) && next[field][dependency] === versions[0]) next[field][dependency] = versions[1];
		}
	}
	return next;
}
assert.deepEqual(nextInstall.manifest, { ...advance(baseInstall.manifest), version: versions[1] });
assert.deepEqual(nextInstall.lock, { ...baseInstall.lock, version: versions[1], packages: Object.fromEntries(sorted(a).map(([key, entry]) => {
	const changes = !key || internalNames.has(nameOf(key)) ? { version: versions[1], ...(key ? { resolved: entry.resolved.replace("-1.6.1.tgz", "-1.7.0.tgz") } : {}) } : {};
	return [key, { ...advance(entry), ...changes }];
})) });
const publisherDependencyBumps = baseInstall.edges.filter(edge => internalNames.has(edge.dependency) && edge.request === versions[0]).length;
out({ kind: "installer-delta", equalThirdPartyRecords: nextInstall.npmSlots.size - internalNames.size, publisherArtifactBumps: internalNames.size,
	publisherDependencyBumps, bundleMemberPathsUnchanged: bundlePaths[0].length });
for (const [, archive] of [...nextArchiveProof].sort(([a], [b]) => a.localeCompare(b))) out({ kind: "registry-package", ...archive });
