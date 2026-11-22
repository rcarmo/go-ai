import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const oldPin = "b4afdddf44e059c67ccf5097e5317d913521b470";
const base = "bf65050e8e4cb868ded06b6dac362bf5bfc2684e";
const data = "docs/upstream-v1.6.0/dependencies";
const crosswalk = "docs/upstream-v1.6.0/README.md";
const [mirror, oracle, releaseDir, userCache, projectCache] = process.argv.slice(2);
if (process.argv.length !== 7) throw Error("Usage: bun run verify-native-inputs.ts <pinned-git-mirror> <oracle-dir> <release-archive-dir> <user-npm-cacache> <project-npm-cacache>");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
for (const cache of [userCache, projectCache]) check(statSync(join(cache, "content-v2", "sha512")).isDirectory(), `Invalid npm cache: ${cache}`);
const hash = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 8 << 20 });
const root = git(import.meta.dir, "rev-parse", "--show-toplevel").toString().trim();
const native = (file: string, commit = base) => git(root, "show", `${commit}:${file}`);
const upstream = (file: string, commit = pin) => git(mirror, "show", `${commit}:${file}`);
const rows = (bytes: Buffer) => bytes.toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
const archiveReceipt = rows(native(`${data}/npm-archives.jsonl`));
const graph = rows(native(`${data}/npm-closure.jsonl`));
const lockBytes = upstream("package-lock.json");
const lock = JSON.parse(lockBytes.toString("utf8"));
check(archiveReceipt[0]?.pin === pin && archiveReceipt[0]?.lockSha256 === hash(lockBytes) && graph[0]?.inputs?.["package-lock.json"] === hash(lockBytes), "Lock and archive receipt disagree");

function source(file: string, anchor: string) {
	const pinned = upstream(file);
	check(pinned.equals(readFileSync(join(oracle, file))) && pinned.includes(anchor), `Pinned source mismatch: ${file}`);
	return { file, anchor, sha256: hash(pinned) };
}
function locator(file: string, anchor: string, commit = base) {
	check(native(file, commit).includes(anchor), `Native locator missing: ${commit}:${file}: ${anchor}`);
	return { file, anchor, commit };
}
function goModule(name: string, version: string, testCommit: string) {
	const lookup = `${name} ${version}`;
	const checksum = (file: string, prefix: string, commit: string) => native(file, commit).toString().split("\n").find(line => line.startsWith(prefix));
	const sum = checksum("go.sum", `${lookup} h1:`, base);
	const modSum = checksum("go.sum", `${lookup}/go.mod h1:`, base);
	for (const commit of [base, testCommit]) {
		check(native("go.mod", commit).toString().split("\n").some(line => line.trim() === lookup), `${commit}: Go module declaration missing: ${lookup}`);
		check(sum && modSum && checksum("go.sum", `${lookup} h1:`, commit) === sum && checksum("go.sum", `${lookup}/go.mod h1:`, commit) === modSum, `${commit}: Go checksum mismatch: ${lookup}`);
	}
	return { name, version, goSum: sum!.split(" ").at(-1), goModSum: modSum!.split(" ").at(-1), declaredAt: [base, testCommit] };
}
function lockPlacement(name: string) {
	const id = `npm:node_modules/${name}`;
	const placement = archiveReceipt.find(row => row.kind === "archive" && row.id === id);
	const pinned = lock.packages?.[`node_modules/${name}`];
	check(placement && pinned && placement.version === pinned.version && placement.integrity === pinned.integrity && placement.resolved === pinned.resolved, `Missing pinned placement ${id}`);
	check(/^sha512-[A-Za-z0-9+/]{86}==$/.test(placement.integrity), `Invalid pinned integrity ${id}`);
	return placement;
}
function cachedArchive(placement: ReturnType<typeof lockPlacement>, required: boolean) {
	const hex = Buffer.from(placement.integrity.slice("sha512-".length), "base64").toString("hex");
	const key = `content-v2/sha512/${hex.slice(0, 2)}/${hex.slice(2, 4)}/${hex.slice(4)}`;
	for (const [label, dir] of [["user", userCache], ["project", projectCache]]) {
		let bytes: Buffer;
		try { bytes = readFileSync(join(dir, key)); }
		catch (error) {
			if ((error as { code?: string }).code === "ENOENT") continue;
			throw error;
		}
		check(`sha512-${createHash("sha512").update(bytes).digest("base64")}` === placement.integrity, `Incorrect archive integrity: ${placement.id}`);
		if (required) check(placement.archive?.some((record: { key: string; sha256: string }) => record.key === key && record.sha256 === hash(bytes)), `Archive receipt mismatch: ${placement.id}`);
		return { bytes, cache: label, key, sha256: hash(bytes) };
	}
	check(!required, `Missing retained archive: ${placement.id}`);
}
function oracleMember(archive: Buffer, archiveMember: string, retainedFile: string) {
	const member = execFileSync("tar", ["-xOzf", "-", "--", archiveMember], { input: archive, maxBuffer: 8 << 20 });
	check(member.equals(readFileSync(join(oracle, retainedFile))), `Archive member differs from retained Oracle: ${retainedFile}`);
	return { member, receipt: { archiveMember, retainedFile, bytes: member.length, sha256: hash(member), state: "verified-retained-byte-match" } };
}
function archiveInputs(name: string, files: string[]) {
	const placement = lockPlacement(name);
	const archive = cachedArchive(placement, true)!;
	return {
		placement: placement.id, version: placement.version, integrity: placement.integrity,
		cache: archive.cache, cacheKey: archive.key,
		members: files.map(file => oracleMember(archive.bytes, `package/${file}`, `node_modules/${name}/${file}`)),
	};
}
function releaseInputs(name: string, path: string, files: string[]) {
	const workspace = graph.find(row => row.kind === "workspace" && row.id === `workspace:${path}`);
	check(workspace?.name === name && workspace.version === "1.6.0" && workspace.archive, `Missing workspace archive receipt: ${name}`);
	const bytes = readFileSync(join(releaseDir, workspace.archive.file));
	check(hash(bytes) === workspace.archive.sha256, `Release archive digest differs: ${name}`);
	return {
		name, version: workspace.version, archive: workspace.archive,
		members: files.map(file => oracleMember(bytes, `package/${file}`, `${path}/${file}`).receipt),
	};
}

const codemodeCommit = "c06946bb8f72df18b96ce5c8ca667eac90cab2b6";
const imagesCommit = "847641b07f56f3ca703e6d4d8f2703dd0038a59e";
const sqliteCommit = "91f7adeb4630e820d7dcb29de4096f7db39d2420";
const quickjs = archiveInputs("quickjs-wasi", ["quickjs.wasm"]);
const embedded = "internal/quickjs/guest/quickjs.wasm";
check(quickjs.members[0].member.equals(native(embedded)) && quickjs.members[0].member.equals(native(embedded, codemodeCommit)), "Oracle QuickJS WASM differs from native embedded asset");
const photon = archiveInputs("@silvia-odwyer/photon", ["photon_rs.js", "photon_rs_bg.wasm"]);
const runner = archiveInputs("vitest", ["vitest.mjs", "dist/cli.js"]);
const archiveRow = ({ members, ...rest }: ReturnType<typeof archiveInputs>) => ({ ...rest, members: members.map(entry => entry.receipt) });

const sqliteSources = [
	source("packages/durable/src/storage/sqlite/schema.ts", "export function createSqliteSchema"),
	source("packages/durable/src/storage/sqlite/migrations.ts", "export function createSqliteMigrations"),
];
for (const { file } of sqliteSources) check(upstream(file).equals(upstream(file, oldPin)), `SQLite schema input changed since v1.5.2: ${file}`);
const sqlAsset = "durable/storage_sqlite_schema_v152.json";
const extractor = "durable/testdata/pi-durable-v152/extract-sqlite-schema.mjs";
check(native(extractor).equals(readFileSync(join(root, extractor))), "SQLite extractor differs from the recorded native base");
const regenerated = execFileSync(process.execPath, ["--conditions=source", join(root, extractor), oracle], { maxBuffer: 8 << 20 });
check(native(sqlAsset).equals(regenerated) && native(sqlAsset, sqliteCommit).equals(regenerated), "Regenerated pinned SQLite migration asset differs");

const report: object[] = [
	{ kind: "manifest", format: 1, pin, base, lockSha256: hash(lockBytes), priorRunAttribution: "receipt-and-source-seam-not-load-trace" },
	{
		kind: "correspondence", id: "quickjs-wasi", oracle: archiveRow(quickjs),
		sourceSeams: [source("packages/durable/test/codemode.test.ts", "new CodemodeSandbox("), source("packages/codemode/src/runtime/host.ts", "options.wasm ?? loadQuickJSWasm()"), source("packages/codemode/src/wasm.ts", 'resolve("quickjs-wasi/quickjs.wasm")')],
		nativeAsset: { file: embedded, sha256: hash(native(embedded)), comparedWith: quickjs.members[0].receipt.retainedFile, state: "verified-byte-match" },
		nativeModule: goModule("github.com/tetratelabs/wazero", "v1.12.0", codemodeCommit),
		nativeSeams: [locator("internal/quickjs/engine.go", "//go:embed guest/quickjs.wasm"), locator("durable/codemode_extension_namespaces_120_test.go", "func TestV160CodemodeExtensionNamespaceReplay(", codemodeCommit)],
		focusedReceipt: locator(crosswalk, "Seven selected unchanged originals passed: `codemode.test.ts`"),
		behavior: "focused-namespace-and-replay-only",
	},
	{
		kind: "correspondence", id: "photon-image-fitting", oracle: archiveRow(photon),
		sourceSeams: [source("packages/durable/test/images-fit.test.ts", "photon = await createNodePhotonImages()"), source("packages/durable/test/image-fixtures.ts", 'resolve("@silvia-odwyer/photon/photon_rs_bg.wasm")')],
		nativeModule: goModule("golang.org/x/image", "v0.47.0", imagesCommit),
		nativeSeams: [locator("durable/tools/image_fit.go", "func NativeImageProcessor("), locator("durable/tools/image_prepare.go", "golang.org/x/image/bmp"), locator("durable/tools/image_model_120_test.go", "func TestSourceV160FitImagesFormats(", imagesCommit)],
		focusedReceipt: locator(crosswalk, "All 11 unchanged `images-fit.test.ts` cases passed", imagesCommit),
		behavior: "focused-image-outputs-only-not-photon-implementation-equivalence",
	},
	{
		kind: "correspondence", id: "published-dist-and-test-runner", runner: archiveRow(runner),
		workspaceReleaseSamples: [releaseInputs("@earendil-works/pi-ai", "packages/ai", ["dist/index.js"]), releaseInputs("@earendil-works/chord", "packages/chord", ["dist/context/index.js", "dist/sqlite/node.js"])],
		sourceSeams: [source("packages/ai/package.json", '"import": "./dist/index.js"'), source("packages/durable/vitest.config.ts", 'conditions: ["source"]'), source("packages/ai/vitest.config.ts", 'conditions: ["source"]')],
		nativeSeams: [locator("Makefile", "test: ## Run ordinary pure-Go tests"), locator("Makefile", "override CGO_ENABLED := 0")],
		focusedReceipt: locator(crosswalk, "Vitest's incomplete Bun install was replaced with the exact locked tarball"),
		behavior: "historical-setup-and-selected-entrypoint-bytes-not-complete-module-load-trace",
	},
	{
		kind: "correspondence", id: "sqlite-schema-and-driver", sourceSchema: sqliteSources, originalSchemaPin: oldPin,
		sourceSeams: [source("packages/durable/test/sqlite-storage.test.ts", 'from "node:sqlite"'), source("packages/durable/src/storage/sqlite/node.ts", "export async function openNodeSqliteStorage"), source("packages/chord/src/sqlite/node.ts", 'import { DatabaseSync } from "node:sqlite"')],
		nativeAsset: { file: sqlAsset, extractor, sha256: hash(regenerated), state: "verified-regeneration-at-pinned-schema-source" },
		nativeModule: goModule("modernc.org/sqlite", "v1.61.1", sqliteCommit),
		nativeSeams: [locator("durable/storage_sqlite_driver.go", "sqlite.NewConnector(dsn)"), locator("durable/storage_sqlite_upstream_migrate.go", "func (s *SQLiteStorage) migrateUpstreamSQLite()"), locator("durable/storage_sqlite_copy_test.go", "func TestSourceV160SQLiteCopyRefusalOrdering(", sqliteCommit)],
		focusedReceipt: locator(crosswalk, "Eight selected SQLite originals passed", sqliteCommit),
		behavior: "focused-copy-rollback-and-regenerated-migrations-not-viewer-or-platform-acceptance",
	},
];
for (const name of ["@google/genai", "openai"]) {
	const placement = lockPlacement(name);
	const found = cachedArchive(placement, false);
	report.push({ kind: "local-archive-search", id: placement.id, version: placement.version, integrity: placement.integrity, checkedCaches: ["user", "project"], state: found ? "verified-archive-bytes" : "unresolved-no-retained-archive-at-integrity-key", ...(found ? { cache: found.cache, key: found.key, sha256: found.sha256 } : {}) });
}
for (const row of report) console.log(JSON.stringify(row));
