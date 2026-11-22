import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const pin = "faac585884ae2c013a6b1400f3e9e77f10b34bac";
const nativeBase = "ea3b25710f18a4d9e4de52715cf5c311b91cd165";
const [mirror, ...cacheArgs] = process.argv.slice(2);
if (!mirror || !cacheArgs.length) throw Error("Usage: bun run verify-archives.ts <pinned-git-mirror> <cache-label=/path/to/_cacache>...");
function check(condition: unknown, message: string): asserts condition {
	if (!condition) throw Error(message);
}
const caches = new Map<string, string>();
for (const arg of cacheArgs) {
	const split = arg.indexOf("=");
	const label = arg.slice(0, split);
	check(split > 0 && /^[a-z0-9-]+$/.test(label) && arg.length > split + 1 && !caches.has(label), `Invalid or duplicate cache label: ${label}`);
	const root = arg.slice(split + 1);
	check(statSync(join(root, "content-v2", "sha512")).isDirectory(), `Invalid npm cache root: ${label}`);
	caches.set(label, root);
}
const git = (repo: string, ...args: string[]) => execFileSync("git", ["-C", repo, ...args], { maxBuffer: 8 << 20 });
const digest = (algorithm: string, bytes: Buffer, encoding: "hex" | "base64" = "hex") => createHash(algorithm).update(bytes).digest(encoding);
const lockBytes = git(mirror, "show", `${pin}:package-lock.json`);
const lockHash = digest("sha256", lockBytes);
const lock = JSON.parse(lockBytes.toString("utf8"));
const graphBytes = git(import.meta.dir, "show", `${nativeBase}:docs/upstream-v1.6.0/dependencies/npm-closure.jsonl`);
const graph = graphBytes.toString("utf8").trimEnd().split("\n").map(line => JSON.parse(line));
check(lock.lockfileVersion === 3 && graph[0]?.pin === pin && graph[0]?.inputs["package-lock.json"] === lockHash, "Closure and pinned npm lock disagree");

// Locators identify where native comparison work belongs; they do not prove correspondence.
const tranche: Record<string, ReadonlyArray<readonly [file: string, anchor: string]>> = {
	"@anthropic-ai/sdk": [["inference/provider/anthropic/anthropic.go", "func streamAnthropic("]],
	"@aws-sdk/client-bedrock-runtime": [["go.mod", "github.com/aws/aws-sdk-go-v2/service/bedrockruntime v1.63.4"], ["inference/provider/bedrock/bedrock.go", "bedrockruntime.NewFromConfig"]],
	"@google/genai": [["inference/provider/google/google.go", "func streamGoogle("]],
	"@silvia-odwyer/photon": [["durable/tools/image_fit.go", "func NativeImageProcessor("], ["durable/tools/image_model_120_test.go", "func TestSourceV160FitImagesFormats("]],
	"@smithy/node-http-handler": [["go.mod", "github.com/aws/smithy-go v1.28.5"], ["inference/provider/bedrock/bedrock.go", "o.HTTPClient = &http.Client"]],
	canvas: [],
	openai: [["inference/provider/openai/openai.go", "func streamOpenAI("], ["inference/provider/openairesponses/responses.go", "func streamResponses("]],
	"quickjs-wasi": [["go.mod", "github.com/tetratelabs/wazero v1.12.0"], ["internal/quickjs/engine.go", "//go:embed guest/quickjs.wasm"], ["internal/quickjs/guest/PROVENANCE.md", "quickjs-wasi@3.6.2"]],
	vitest: [["Makefile", "test: ## Run ordinary pure-Go tests"]],
};
const asset = "internal/quickjs/guest/quickjs.wasm";
const archiveMember = "package/quickjs.wasm";
// Resolve by the exact integrity digest, never by reading an unpacked install tree.
function retainedArchive(cache: string, relative: string, integrity: string) {
	const path = join(cache, relative);
	let bytes: Buffer;
	try {
		bytes = readFileSync(path);
	} catch (error) {
		if ((error as { code?: string }).code === "ENOENT") return undefined;
		throw error;
	}
	check(`sha512-${digest("sha512", bytes, "base64")}` === integrity, `Archive integrity mismatch: ${relative}`);
	check(bytes[0] === 0x1f && bytes[1] === 0x8b, `Invalid gzip archive: ${relative}`);
	return bytes;
}
const rows: object[] = [{
	kind: "manifest", format: 1, pin, nativeBase, lockSha256: lockHash, closureSha256: digest("sha256", graphBytes),
	cacheType: "npm-cacache-content-v2", caches: [...caches.keys()].sort(), tranche: Object.keys(tranche),
}];
let verified = 0;
let absent = 0;
for (const [name, anchors] of Object.entries(tranche)) {
	const placements = graph.filter(row => row.kind === "npm" && row.name === name);
	check(placements.length > 0, `Missing placement for ${name}`);
	const locators = anchors.map(([file, anchor]) => {
		check(git(import.meta.dir, "show", `${nativeBase}:${file}`).includes(anchor), `Missing native candidate locator: ${file}`);
		return { file, anchor };
	});
	for (const { id, lockKey, version, resolved, integrity } of placements) {
		const entry = lock.packages[lockKey];
		check(entry && !entry.link && id === `npm:${lockKey}` && `/${lockKey}`.endsWith(`/node_modules/${name}`) && entry.version === version && entry.resolved === resolved && entry.integrity === integrity, `Closure row differs from pinned npm lock: ${id}`);
		check(/^sha512-[A-Za-z0-9+/]{86}==$/.test(integrity), `Unsupported integrity: ${id}`);
		const hex = Buffer.from(integrity.slice("sha512-".length), "base64").toString("hex");
		const relative = `content-v2/sha512/${hex.slice(0, 2)}/${hex.slice(2, 4)}/${hex.slice(4)}`;
		const archive = [];
		let quickjsArchive: Buffer | undefined;
		for (const label of [...caches.keys()].sort()) {
			const found = retainedArchive(caches.get(label)!, relative, integrity);
			if (!found) continue;
			if (name === "quickjs-wasi") quickjsArchive ??= found;
			archive.push({ cache: label, key: relative, bytes: found.length, sha256: digest("sha256", found) });
		}
		if (archive.length) verified++;
		else absent++;
		let assetComparison;
		if (quickjsArchive) {
			const member = execFileSync("tar", ["-xOzf", "-", "--", archiveMember], { input: quickjsArchive, maxBuffer: 8 << 20 });
			const native = git(import.meta.dir, "show", `${nativeBase}:${asset}`);
			assetComparison = {
				archiveMember, nativeFile: asset, archiveSha256: digest("sha256", member), nativeSha256: digest("sha256", native),
				state: member.equals(native) ? "verified-byte-match" : "bytes-differ",
			};
		}
		rows.push({
			kind: "archive", id, name, version, lockKey, resolved, integrity,
			state: archive.length ? "verified-archive-bytes" : "unresolved-no-archive-in-checked-caches", archive,
			nativeCorrespondence: { status: locators.length ? "candidate" : "unresolved", locators }, assetComparison,
		});
	}
}
rows.push({ kind: "summary", verified, unresolved: absent, nativeSemanticsVerified: false });
for (const row of rows) console.log(JSON.stringify(row));
