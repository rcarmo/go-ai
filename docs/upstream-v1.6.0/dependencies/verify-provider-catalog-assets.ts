import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const base = "3802c688532ee94a98e757c4ab96ea3aa2205d95";
const regeneration = "d6986cdf08a21c60ab03ba11225c27d2d8c30966";
const [mirror, oracle, archives] = process.argv.slice(2);
if (process.argv.length !== 5) throw Error("Usage: bun run verify-provider-catalog-assets.ts <pinned-git-mirror> <oracle-dir> <release-archive-dir>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 8 << 20 });
const native = (path: string, commit = base) => git(import.meta.dir, "show", `${commit}:${path}`);
const hash = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const data = ["packages/ai/dist/providers/data", "packages/ai/src/providers/data"];
for (const dir of data) {
	check(git(mirror, "ls-tree", "-r", "--name-only", pin, "--", dir).length === 0, `Unexpected Git-tracked asset in ${dir}`);
}
const graph = native("docs/upstream-v1.6.0/dependencies/npm-closure.jsonl").toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
const lockSha256 = hash(git(mirror, "show", `${pin}:package-lock.json`));
check(graph[0]?.pin === pin && graph[0]?.inputs?.["package-lock.json"] === lockSha256, "Dependency ledger does not identify the pinned lock");
const ai = graph.find(row => row.kind === "workspace" && row.id === "workspace:packages/ai");
check(ai?.name === "@earendil-works/pi-ai" && ai.version === "1.6.0" && ai.archive?.file === "earendil-works-pi-ai-1.6.0.tgz", "Missing verified package archive receipt");
const archive = readFileSync(join(archives, ai.archive.file));
check(hash(archive) === ai.archive.sha256, "AI release archive bytes differ from the earlier receipt");
const contents = execFileSync("tar", ["-tzf", "-"], { input: archive, maxBuffer: 8 << 20 }).toString("utf8").trimEnd().split("\n");
function member(path: string): Buffer {
	check(contents.filter(file => file === path).length === 1, `Expected one archive member: ${path}`);
	return execFileSync("tar", ["-xOzf", "-", "--", path], { input: archive, maxBuffer: 8 << 20 });
}
const prefix = "package/dist/providers/data/";
const manifestName = ".manifest.json";
const manifest = member(`${prefix}${manifestName}`);
const schema = JSON.parse(manifest.toString("utf8"));
const fixture = "cmd/generate-models/testdata/catalog-v1.6.0-manifest.json";
check(manifest.equals(native(fixture)) && manifest.equals(native(fixture, regeneration)), "Published manifest differs from the native regeneration fixture");
check(schema.schemaVersion === 6 && typeof schema.files === "object" && schema.files !== null, "Unsupported catalog manifest");
const providers = Object.keys(schema.files).sort();
check(providers.length === 43 && providers.every(file => /^[a-z0-9][a-z0-9-]*\.json$/.test(file) && /^[a-f0-9]{64}$/.test(schema.files[file])), "Unexpected catalog manifest file set or digest");
const expected = [manifestName, ...providers];
const sameSet = (files: string[]) => JSON.stringify(files.sort()) === JSON.stringify(expected);
check(sameSet(contents.filter(path => path.startsWith(prefix)).map(path => path.slice(prefix.length))), "Published archive provider data differs from the exact manifest file set");

function oracleBytes(path: string): Buffer {
	const absolute = join(oracle, path);
	check(lstatSync(absolute).isFile(), `Expected retained regular file: ${path}`);
	return readFileSync(absolute);
}
for (const dir of data) {
	check(lstatSync(join(oracle, dir)).isDirectory(), `Expected retained directory: ${dir}`);
	const entries = readdirSync(join(oracle, dir), { withFileTypes: true });
	check(entries.every(entry => entry.isFile()) && sameSet(entries.map(entry => entry.name)), `Retained provider data file set differs: ${dir}`);
}
const assets = expected.map(file => {
	const archiveMember = `${prefix}${file}`;
	const bytes = member(archiveMember);
	const sha256 = hash(bytes);
	if (file !== manifestName) check(sha256 === schema.files[file], `Provider file differs from manifest digest: ${file}`);
	const retained = data.map(dir => `${dir}/${file}`);
	for (const path of retained) check(bytes.equals(oracleBytes(path)), `Provider asset bytes differ: ${path}`);
	return { kind: "catalog-asset", role: file === manifestName ? "schema-v6-manifest" : "provider-data", archiveMember, retained, bytes: bytes.length, sha256 };
});
const modelScript = "package/dist/models.generated.js";
const modelRetained = "packages/ai/dist/models.generated.js";
const modelBytes = member(modelScript);
check(modelBytes.equals(oracleBytes(modelRetained)), `Published model module bytes differ: ${modelRetained}`);
const crosswalk = "docs/upstream-v1.6.0/README.md";
const regenerationAnchor = "all 1,698 chat, 74 image and 81 classifier records";
check(native(crosswalk, regeneration).includes(regenerationAnchor), "Missing exact model-regeneration receipt");
const rows = [
	{
		kind: "manifest", format: 1, pin, base, lockSha256, package: ai.name, version: ai.version, archive: ai.archive,
		schemaVersion: schema.schemaVersion, structureHash: schema.structureHash, providerFiles: providers.length,
		nativeManifest: { file: fixture, sha256: hash(manifest), comparedAt: [base, regeneration] },
		regenerationReceipt: { commit: regeneration, file: crosswalk, anchor: regenerationAnchor },
		scope: "retained-byte-identity-not-load-provenance-or-a-new-regeneration-run",
	},
	...assets,
	{ kind: "catalog-asset", role: "published-model-module", archiveMember: modelScript, retained: [modelRetained], bytes: modelBytes.length, sha256: hash(modelBytes) },
];
for (const row of rows) console.log(JSON.stringify(row));
