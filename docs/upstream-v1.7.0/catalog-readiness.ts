// Usage: bun run docs/upstream-v1.7.0/catalog-readiness.ts <pi-ai-1.7.0.tgz> <verified dist dir>
// The only candidate generator and catalog changes are Go overlays in disposable scratch.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const base = "4bbf033e360745c3f41bceaaec84f381d6c880db";
const pin = "fb0fe823539f6d04850f6e34748c01f01f212e1f";
const root = resolve(import.meta.dir, "../..");
assert.equal(process.argv.length, 4, "Expected the verified pi-ai tarball and dist directory");
const [tarball, stagedDist] = process.argv.slice(2).map(path => resolve(path));
const kinds = { chat: "MODELS", image: "IMAGE_MODELS", classifier: "CLASSIFIER_MODELS" } as const;
type Kind = keyof typeof kinds;
type RecordJSON = { id: string; provider: string; api: string; type?: string; [key: string]: unknown };
const generated = { chat: "models_generated.go", image: "image_models_generated.go", classifier: "classifier_models_generated.go" };
const readJSON = (path: string) => JSON.parse(readFileSync(path, "utf8"));
const sha = (value: string | Buffer) => createHash("sha256").update(value).digest("hex");
const jsonObject = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value);
function canonical(value: unknown): unknown {
	if (Array.isArray(value)) return value.map(canonical);
	if (jsonObject(value)) return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
	return value;
}
const equal = (a: unknown, b: unknown) => JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
const idOf = (model: RecordJSON) => JSON.stringify([model.provider, model.id]);
const order = (a: string, b: string) => a < b ? -1 : a > b ? 1 : 0;
execFileSync("git", ["diff", "--quiet", base, "--", "*.go", "go.mod", "go.sum", "scripts/project-env.sh"], { cwd: root });
const inputs = readJSON(join(import.meta.dir, "inputs.json"));
assert.equal(inputs.upstreamCommit, pin);
const archive = inputs.npm.find((row: { name: string }) => row.name === "@earendil-works/pi-ai");
assert.equal(archive.version, "1.7.0");
const compressed = readFileSync(tarball);
assert.equal(sha(compressed), archive.sha256);
assert.equal(`sha512-${createHash("sha512").update(compressed).digest("base64")}`, archive.integrity);

const scratch = mkdtempSync(join(tmpdir(), "v170-catalog-audit."));
try {
	execFileSync("tar", ["-xzf", tarball, "-C", scratch,
		"package/package.json", "package/dist/models.generated.js", "package/dist/model-catalog.js", "package/dist/providers/data",
		"--wildcards", "package/dist/providers/*.models.js"]);
	const dist = join(scratch, "package/dist");
	const data = join(dist, "providers/data");
	const module = join(dist, "models.generated.js");
	const packageJSON = readJSON(join(scratch, "package/package.json"));
	assert.equal(packageJSON.name, archive.name);
	assert.equal(packageJSON.version, archive.version);
	const manifestPath = join(data, ".manifest.json");
	const manifest = readJSON(manifestPath);
	const manifestSHA = sha(readFileSync(manifestPath));
	const names = Object.keys(manifest.files).sort();
	assert.equal(manifest.schemaVersion, 6);
	assert.equal(names.length, 43);
	assert.deepEqual(readdirSync(data).sort(), [".manifest.json", ...names]);
	const providers = readdirSync(join(dist, "providers")).filter(file => file.endsWith(".models.js")).sort();
	assert.deepEqual(providers, names.map(name => `${name.slice(0, -5)}.models.js`).sort());
	const selectedMembers = ["models.generated.js", "model-catalog.js", "providers/data/.manifest.json",
		...names.map(name => `providers/data/${name}`), ...providers.map(name => `providers/${name}`)];
	for (const member of selectedMembers) assert.deepEqual(readFileSync(join(dist, member)), readFileSync(join(stagedDist, member)), member);

	const source = new Map<Kind, Map<string, RecordJSON>>();
	for (const kind of Object.keys(kinds) as Kind[]) source.set(kind, new Map());
	const structure: Record<string, Record<string, string>> = {};
	for (const name of names) {
		assert.equal(name, basename(name));
		const provider = name.slice(0, -5);
		const bytes = readFileSync(join(data, name));
		assert.equal(sha(bytes), manifest.files[name], name);
		const providerGroup = JSON.parse(bytes.toString()) as Record<string, Record<string, RecordJSON>>;
		structure[provider] = {};
		for (const [api, models] of Object.entries(providerGroup)) {
			for (const [key, model] of Object.entries(models)) {
				const kind = model.type as Kind;
				const index = source.get(kind);
				assert(index && model.id && model.api === api && model.provider === provider && key === `${kind}:${model.id}`, key);
				assert(!index.has(idOf(model)), key);
				index.set(idOf(model), model);
				structure[provider][key] = api;
			}
		}
	}
	// The Go validation uses json.Marshal: preserve its HTML and U+2028/U+2029 escaping.
	const compactStructure = JSON.stringify(canonical(structure)).replace(/[<>&\u2028\u2029]/g, c => `\\u${c.charCodeAt(0).toString(16).padStart(4, "0")}`);
	assert.equal(sha(compactStructure), manifest.structureHash);
	const exports = await import(pathToFileURL(module).href);
	for (const [kind, symbol] of Object.entries(kinds) as [Kind, string][]) {
		const raw = source.get(kind)!;
		const values = exports[symbol] as Record<string, Record<string, RecordJSON>>;
		let count = 0;
		for (const [provider, models] of Object.entries(values)) {
			for (const [id, model] of Object.entries(models)) {
				assert.equal(model.provider, provider);
				assert.equal(model.id, id);
				assert(equal(model, raw.get(idOf(model))), `${kind}: exported ${provider}/${id} differs from its data payload`);
				count++;
			}
		}
		assert.equal(count, raw.size, kind);
	}

	const main = join(root, "cmd/generate-models/main.go");
	const goSource = readFileSync(main, "utf8");
	const reject = '\n\tdefault:\n\t\treturn nil, fmt.Errorf("unsupported catalog structure hash %q", manifest.StructureHash)';
	assert.equal(goSource.split(reject).length, 2, "Expected the stock structure allowlist branch");
	// Admit exactly the verified structure, manifest bytes, and provider count. All subsequent checks remain stock.
	assert.match(manifest.structureHash, /^[0-9a-f]{64}$/);
	const extra = `\n\tcase "${manifest.structureHash}":\n\t\tif len(manifest.Files) != 43 || fmt.Sprintf("%x", sha256.Sum256(data)) != "${manifestSHA}" {\n\t\t\treturn nil, fmt.Errorf("audit manifest digest differs")\n\t\t}`;
	writeFileSync(join(scratch, "generator-main.go"), goSource.replace(reject, `${extra}${reject}`));
	writeFileSync(join(scratch, "manifest-overlay.json"), JSON.stringify({ Replace: { [main]: join(scratch, "generator-main.go") } }));
	writeFileSync(join(scratch, "candidate-overlay.json"), JSON.stringify({ Replace: Object.fromEntries(Object.values(generated).map(file => [join(root, file), join(scratch, file)])) }));
	writeFileSync(join(scratch, "export.go"), `package main
import ("encoding/json"; "os"; goai "github.com/rcarmo/go-ai")
func main() {
 goai.RegisterBuiltinModels(); goai.RegisterBuiltinImageModels(); goai.RegisterBuiltinClassifierModels()
 records := map[string]any{"chat": goai.ListModels(""), "image": goai.ListImageModels(""), "classifier": goai.ListClassifierModels("")}
 if err := json.NewEncoder(os.Stdout).Encode(records); err != nil { panic(err) }
}
`);
	// Both executables use the base checkout. The second admits just the diagnostic manifest in its private overlay.
	const runner = join(scratch, "generate.sh");
	writeFileSync(runner, `#!/usr/bin/env bash
set -euo pipefail
source scripts/project-env.sh
work=$1 data=$2
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOPROXY=off GOSUMDB=off
export GOTOOLCHAIN=local
go version > "$work/go.version"
go build -o "$work/stock" ./cmd/generate-models
for item in chat:models_generated.go image:image_models_generated.go classifier:classifier_models_generated.go; do
  kind=\${item%:*}; file=\${item#*:}
  set +e
  "$work/stock" -data-dir "$data" -kind "$kind" -output "$work/stock-$file" > "$work/stock-$kind.log" 2>&1
  code=$?
  set -e
  printf '%s\\n' "$code" > "$work/stock-$kind.status"
done
go build -overlay="$work/manifest-overlay.json" -o "$work/diagnostic" ./cmd/generate-models
for item in chat:models_generated.go image:image_models_generated.go classifier:classifier_models_generated.go; do
  kind=\${item%:*}; file=\${item#*:}
  "$work/diagnostic" -data-dir "$data" -kind "$kind" -output "$work/$file"
done
go run -overlay="$work/candidate-overlay.json" "$work/export.go" > "$work/candidate.json"
set +e
go run -overlay="$work/candidate-overlay.json" ./cmd/check-model-catalog -data-dir "$data" > "$work/strict.log" 2>&1
code=$?
set -e
printf '%s\\n' "$code" > "$work/strict.status"
`);
	const build = spawnSync("flock", ["-w", "900", "/tmp/go-ai/v160-focused-verify.lock", "bash", runner, scratch, data], { cwd: root, encoding: "utf8" });
	assert.equal(build.status, 0, build.error?.message ?? build.stderr);
	const stockError = `unsupported catalog structure hash "${manifest.structureHash}"`;
	for (const [kind, file] of Object.entries(generated)) {
		assert.equal(readFileSync(join(scratch, `stock-${kind}.status`), "utf8").trim(), "1");
		assert(readFileSync(join(scratch, `stock-${kind}.log`), "utf8").split("\n").includes(stockError));
		assert(!readdirSync(scratch).includes(`stock-${file}`), `${kind} wrote on rejection`);
	}
	assert.equal(readFileSync(join(scratch, "strict.status"), "utf8").trim(), "1");
	assert.match(readFileSync(join(scratch, "strict.log"), "utf8"), /^full record differs:/m);

	const variants = ["completionsCompat", "responsesCompat", "anthropicCompat", "mistralCompat", "bedrockCompat", "googleCompat"];
	function normalize(kind: Kind, native: RecordJSON): RecordJSON {
		const copy = { ...native };
		if (kind === "chat" && !Object.hasOwn(copy, "type")) copy.type = kind;
		for (const field of variants) {
			if (!Object.hasOwn(copy, field)) continue;
			assert(!Object.hasOwn(copy, "compat"), `Multiple compat variants for ${idOf(copy)}`);
			copy.compat = copy[field];
			delete copy[field];
		}
		return copy;
	}
	type Field = { path: string; change: "missing" | "extra" | "different"; expected?: unknown; actual?: unknown };
	function diffs(want: unknown, got: unknown, path: string, out: Field[]) {
		if (equal(want, got)) return;
		if ((jsonObject(want) || want === undefined) && (jsonObject(got) || got === undefined)) {
			const expected = want ?? {}, actual = got ?? {};
			const keys = [...new Set([...Object.keys(expected), ...Object.keys(actual)])].sort();
			if (keys.length > 0) {
				for (const key of keys) diffs(expected[key], actual[key], `${path}/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`, out);
				return;
			}
		}
		out.push({ path, change: got === undefined ? "missing" : want === undefined ? "extra" : "different", expected: want, actual: got });
	}
	// Check the inventory discriminates omission, value drift, and reordered arrays.
	const control: Field[] = [];
	diffs({ nested: { flag: false }, size: 3, array: [1, 2], empty: {} }, { nested: {}, size: 4, array: [2, 1], empty: {} }, "", control);
	assert.deepEqual(control.map(field => [field.path, field.change]), [["/array", "different"], ["/nested/flag", "missing"], ["/size", "different"]]);

	const publicRecords = readJSON(join(scratch, "candidate.json")) as Record<Kind, RecordJSON[]>;
	const census = {} as Record<Kind, object>;
	const groups = new Map<string, { kind: Kind; path: string; change: Field["change"]; records: object[] }>();
	for (const kind of Object.keys(kinds) as Kind[]) {
		const raw = source.get(kind)!;
		const native = new Map(publicRecords[kind].map(model => [idOf(model), normalize(kind, model)]));
		assert.equal(native.size, publicRecords[kind].length, `${kind}: duplicate native identity`);
		let matched = 0, differed = 0;
		const missing = [], extra = [];
		for (const key of [...raw.keys()].sort()) {
			const want = raw.get(key)!;
			const got = native.get(key);
			if (!got) { missing.push(key); continue; }
			const fields: Field[] = [];
			diffs(want, got, "", fields);
			if (fields.length === 0) matched++;
			else differed++;
			for (const { path, change, ...values } of fields) {
				const key = JSON.stringify([kind, path, change]);
				if (!groups.has(key)) groups.set(key, { kind, path, change, records: [] });
				groups.get(key)!.records.push({ provider: want.provider, api: want.api, id: want.id, ...values });
			}
		}
		for (const key of [...native.keys()].sort()) if (!raw.has(key)) extra.push(key);
		census[kind] = { source: raw.size, candidate: native.size, matched, differed, missing, extra,
			candidateGoSHA256: sha(readFileSync(join(scratch, generated[kind]))) };
	}
	const images = [...source.get("image")!.values()];
	const imageAPIs = Object.fromEntries([...new Set(images.map(model => model.api))].sort().map(api => [api, images.filter(model => model.api === api).length]));
	console.log(JSON.stringify({ base, pin, archive: { name: archive.name, version: archive.version, sha256: archive.sha256, integrity: archive.integrity },
		input: { schemaVersion: manifest.schemaVersion, providerDataFiles: names.length, manifestSHA256: manifestSHA,
			structureHash: manifest.structureHash, modelsModuleSHA256: sha(readFileSync(module)), selectedArchiveMembersMatchStage: selectedMembers.length,
			exportsMatchProviderRecords: true, imageAPIs },
		stockOfflineGenerator: { chat: 1, image: 1, classifier: 1, error: stockError, generatedFilesWritten: 0 },
		diagnostic: { method: "exact manifest allowlist arm in a scratch Go overlay; all later validation and Go field projection unchanged",
			go: readFileSync(join(scratch, "go.version"), "utf8").trim(), publicRecordCensus: census,
			strictPublicComparator: { exit: 1, reason: "full record differs" },
			fieldDifferences: [...groups].sort(([a], [b]) => order(a, b)).map(([, group]) => group) },
		scope: "Root Go chat, image and classifier registries. No new release pin, generated source, provider execution, or separate images adapter claim." }, null, 2));
} finally {
	rmSync(scratch, { recursive: true, force: true });
}
