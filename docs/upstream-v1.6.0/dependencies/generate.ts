import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join, posix } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const roots = ["packages/ai", "packages/codemode", "packages/durable", "packages/schema"];
const archiveReceipt = "7ee53351a9ac2dde4503a223e04f3b1d8bc1b22e";
const archiveDigests: Record<string, string> = {
	"@earendil-works/pi-ai": "d68312ec574bc959cb7fee26da4f4876d067aed9bbbe76e876359e60d2294747",
	"@earendil-works/pi-durable": "e76ddd7fdf50d53a9945de87adfd620f5734df6d57a075ef8168948ba6ddd855",
	"@earendil-works/pi-codemode": "22f7dabd30eeaac7e38a133b72008ccfc379732f2ed86d760297cb9d385a42db",
	"@earendil-works/pi-schema": "a308dfc8981c5badc8e113a97d5390788d6bcc82ed4e2aa2708844438ef88d25",
	"@earendil-works/chord": "3967193a8f1bdb79881e4f0715a170853a5946fbb4326f2403529a5a9faedc17",
	"@earendil-works/pi-telemetry": "7a28233ebfdd751940619be6a879bda0fe6eb31ac2056e8798e16293c0c39ec5",
};

const [mirror, oracle, archives] = process.argv.slice(2);
if (process.argv.length !== 5) throw Error("Usage: bun run generate.ts <pinned-git-mirror> <oracle-dir> <release-archive-dir>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const sha256 = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const inputs = new Map<string, string>();
function pinnedFile(file: string): string {
	const bytes = readFileSync(join(oracle, file));
	check(bytes.equals(execFileSync("git", ["-C", mirror, "show", `${pin}:${file}`])), `Oracle bytes differ from ${pin}:${file}`);
	inputs.set(file, sha256(bytes));
	return bytes.toString("utf8");
}

const dependencyFields = ["dependencies", "optionalDependencies", "peerDependencies", "devDependencies"] as const;
type Field = typeof dependencyFields[number];
type Specs = Record<string, string>;
type Metadata = Partial<Record<Field, Specs>> & {
	peerDependenciesMeta?: Record<string, { optional?: boolean }>;
	engines?: Specs;
	os?: string[];
	cpu?: string[];
	libc?: string[];
	hasInstallScript?: boolean;
};
type Manifest = Metadata & {
	name: string;
	version: string;
	overrides?: Specs;
};
type LockEntry = Metadata & {
	name?: string;
	version?: string;
	resolved?: string;
	integrity?: string;
	link?: boolean;
	dev?: boolean;
	devOptional?: boolean;
	optional?: boolean;
	peer?: boolean;
};
type Lock = { lockfileVersion: number; packages: Record<string, LockEntry> };
type Node = {
	id: string;
	type: "workspace" | "npm";
	name: string;
	version: string;
	key: string;
	metadata: Metadata;
};
type EdgeKind = "runtime" | "optional" | "peer" | "optional-peer" | "development";
type Edge = {
	kind: "edge";
	from: string;
	dependency: string;
	field: Field;
	class: EdgeKind;
	request: string;
	override?: string;
	lockKey?: string;
	to?: string;
	state: "resolved" | "missing" | "optional-absent" | "optional-peer-absent" | "version-mismatch";
};
const lock = JSON.parse(pinnedFile("package-lock.json")) as Lock;
const root = JSON.parse(pinnedFile("package.json")) as Manifest;
check(lock.lockfileVersion === 3 && lock.packages[""]?.name === root.name, "Unsupported npm lock format or root identity");
const orderedEntries = <T>(record: Record<string, T>) => Object.keys(record).sort().map(key => [key, record[key]] as const);
function sameSpecs(actual: Specs | undefined, expected: Specs | undefined, label: string) {
	check(JSON.stringify(orderedEntries(actual ?? {})) === JSON.stringify(orderedEntries(expected ?? {})), `${label}: manifest/lock dependency declarations differ`);
}
// Resolve by recorded placement, starting at the real workspace directory for linked packages.
function nearestSlot(consumer: string, dependency: string): string | undefined {
	for (let dir = consumer; ; dir = posix.dirname(dir)) {
		if (posix.basename(dir) !== "node_modules") {
			const candidate = posix.join(dir, "node_modules", dependency);
			if (Object.hasOwn(lock.packages, candidate)) return candidate;
		}
		if (dir === ".") return undefined;
	}
}
const nodes = new Map<string, Node>();
const records = new Map<string, object>();
const edges: Edge[] = [];
const workspaceId = (path: string) => `workspace:${path}`;

function addWorkspace(path: string, key: string): Node {
	const id = workspaceId(path);
	const known = nodes.get(id);
	if (known) return known;
	const file = `${path}/package.json`;
	const manifest = JSON.parse(pinnedFile(file)) as Manifest;
	const locked = lock.packages[path];
	check(locked && manifest.name === locked.name && manifest.version === locked.version && `/${key}`.endsWith(`/node_modules/${manifest.name}`), `${file}: lock workspace identity differs`);
	for (const field of dependencyFields) sameSpecs(manifest[field], locked[field], `${path}:${field}`);
	const optionalPeers = (metadata: Metadata) => orderedEntries(metadata.peerDependenciesMeta ?? {}).filter(([, meta]) => meta.optional).map(([name]) => name);
	check(JSON.stringify(optionalPeers(manifest)) === JSON.stringify(optionalPeers(locked)), `${path}: optional peer declarations differ`);
	const expected = archiveDigests[manifest.name];
	check(expected, `${file}: no pinned release archive receipt`);
	const archiveName = `${manifest.name.slice(1).replace("/", "-")}-${manifest.version}.tgz`;
	const digest = sha256(readFileSync(join(archives, archiveName)));
	check(digest === expected, `${archiveName}: release archive SHA-256 differs`);
	const node: Node = { id, type: "workspace", name: manifest.name, version: manifest.version, key: path, metadata: manifest };
	nodes.set(id, node);
	records.set(id, {
		kind: "workspace", id, manifest: file, name: node.name, version: node.version, lockKey: path, linkKey: key,
		engines: manifest.engines, provenance: "pinned-source-manifest-bytes",
		archive: { file: archiveName, sha256: digest, proof: "bytes-match-release-receipt", receipt: archiveReceipt },
	});
	return node;
}

function targetFor(key: string, dependency: string): Node {
	const entry = lock.packages[key];
	check(entry, `Missing npm lock entry at ${key}`);
	if (entry.link) {
		check(entry.resolved?.startsWith("packages/"), `Unsupported workspace link at ${key}`);
		return addWorkspace(entry.resolved, key);
	}
	const id = `npm:${key}`;
	const known = nodes.get(id);
	if (known) return known;
	const { version, resolved, integrity, os, cpu, libc, hasInstallScript, engines, dev, devOptional, optional, peer } = entry;
	check(version && /^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$/.test(version) && resolved?.startsWith("https://registry.npmjs.org/"), `Unsupported npm resolution at ${key}`);
	check(integrity && /^sha512-[A-Za-z0-9+/]{86}==$/.test(integrity), `Invalid npm integrity at ${key}`);
	check(!entry.name || entry.name === dependency, `Unsupported npm alias at ${key}`);
	const node: Node = { id, type: "npm", name: dependency, version, key, metadata: entry };
	nodes.set(id, node);
	records.set(id, {
		kind: "npm", id, lockKey: key, name: node.name, version, resolved, integrity, provenance: "lock-record-only",
		platform: { os, cpu, libc }, engines, lockFlags: { dev, devOptional, optional, peer }, hasInstallScript: hasInstallScript ?? false,
	});
	return node;
}

function edgeClass(field: Field, metadata: Metadata, dependency: string): EdgeKind {
	if (field === "devDependencies") return "development";
	if (field === "optionalDependencies") return "optional";
	if (field === "peerDependencies") return metadata.peerDependenciesMeta?.[dependency]?.optional ? "optional-peer" : "peer";
	return "runtime";
}
for (const path of roots) {
	const name = lock.packages[path]?.name;
	check(name, `Missing workspace ${path}`);
	const node = targetFor(`node_modules/${name}`, name);
	check(node.id === workspaceId(path), `Wrong root workspace link for ${path}`);
}
// Map iteration sees nodes added during the walk. IDs represent placement slots, not just names/versions.
for (const node of nodes.values()) {
	for (const field of dependencyFields) {
		for (const [dependency, request] of orderedEntries(node.metadata[field] ?? {})) {
			const klass = edgeClass(field, node.metadata, dependency);
			const override = root.overrides?.[dependency];
			const edge: Edge = { kind: "edge", from: node.id, dependency, field, class: klass, request, ...(override ? { override } : {}), state: "missing" };
			const key = nearestSlot(node.key, dependency);
			if (key) {
				const target = targetFor(key, dependency);
				edge.lockKey = key;
				edge.to = target.id;
				edge.state = Bun.semver.satisfies(target.version, override ?? request) ? "resolved" : "version-mismatch";
			} else if (klass === "optional-peer") {
				edge.state = "optional-peer-absent";
			} else if (klass === "optional") {
				edge.state = "optional-absent";
			}
			edges.push(edge);
		}
	}
}

// Preserve resolved and unresolved observations distinctly; do not infer installation or equivalence.
const out = (value: object) => console.log(JSON.stringify(value));
out({ kind: "manifest", format: 1, pin, roots, lockfileVersion: lock.lockfileVersion, inputs: Object.fromEntries(orderedEntries(Object.fromEntries(inputs))), overrides: root.overrides ?? {}, nativeCorrespondence: "unresolved", targetPlatform: "unfiltered" });
for (const [, record] of orderedEntries(Object.fromEntries(records))) out(record);
for (const edge of edges.sort((a, b) => {
	const x = `${a.from}\0${a.field}\0${a.dependency}`;
	const y = `${b.from}\0${b.field}\0${b.dependency}`;
	return x < y ? -1 : x > y ? 1 : 0;
})) out(edge);
const countBy = <T>(items: readonly T[], key: (item: T) => string) => {
	const counts: Record<string, number> = {};
	for (const item of items) { const value = key(item); counts[value] = (counts[value] ?? 0) + 1; }
	return Object.fromEntries(orderedEntries(counts));
};
function reachable(start: string, include: (klass: EdgeKind) => boolean) {
	const reached = new Set([start]);
	for (const node of reached) {
		for (const edge of edges) {
			if (edge.from === node && edge.state === "resolved" && edge.to && include(edge.class)) reached.add(edge.to);
		}
	}
	return countBy([...reached], id => nodes.get(id)!.type);
}
const reachByRoot = Object.fromEntries(roots.map(path => [path, {
	runtime: reachable(workspaceId(path), kind => kind === "runtime"),
	withOptionalAndPeers: reachable(workspaceId(path), kind => kind !== "development"),
	withDevelopment: reachable(workspaceId(path), () => true),
}]));
out({
	kind: "summary", nodes: countBy([...nodes.values()], node => node.type),
	edges: countBy(edges, edge => edge.class), resolution: countBy(edges, edge => edge.state),
	platformConstrainedNpm: [...nodes.values()].filter(({ metadata, type }) => type === "npm" && (metadata.os || metadata.cpu || metadata.libc)).length,
	reachByRoot, nativeCorrespondence: "not-evaluated",
});
