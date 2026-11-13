#!/usr/bin/env bun
// Install missing, exact-version reference assets. Never change original tests.
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const target = "f9a26365568127461c47a07a37dcb42c140a3ac5";
const args = process.argv.slice(2);
if (args.length !== 2 || args[0] !== "--upstream" || !process.env.PROJECT_TMP_ROOT) {
  throw new Error("Source scripts/project-env.sh, then use --upstream /path/to/pinned/pi");
}
const upstream = resolve(args[1]);
const manifest = JSON.parse(readFileSync(join(upstream, "packages/durable/package.json"), "utf8"));
if (manifest.name !== "@earendil-works/pi-durable" || manifest.version !== "1.5.0") throw new Error("Wrong source version");
const check = spawnSync(process.execPath, [join(import.meta.dir, "check-durable-parity-register.mjs"), "--upstream", upstream], { stdio: "inherit" });
if (check.error) throw check.error;
if (check.status !== 0) throw new Error("Reference source verification failed");
const cache = join(process.env.PROJECT_TMP_ROOT, "cache/durable-oracle-assets");
mkdirSync(cache, { recursive: true });
const sha = data => createHash("sha256").update(data).digest("hex");
async function asset(url, hash, path) {
  if (existsSync(path)) {
    const data = readFileSync(path);
    if (sha(data) !== hash) throw new Error(`Existing asset differs; refusing overwrite: ${path}`);
    return data;
  }
  const response = await fetch(url);
  if (!response.ok) throw new Error(`Reference asset fetch failed: ${response.status} ${url}`);
  const data = Buffer.from(await response.arrayBuffer());
  if (sha(data) !== hash) throw new Error(`Reference asset hash mismatch: ${url}`);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, data, { flag: "wx" });
  return data;
}
const archive = join(cache, "pi-ai-1.5.0.tgz");
await asset("https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-1.5.0.tgz", "e320fe7bd2b927782ba9a37d356fe5837f1aa96a46432610e1b3b9ccb748ea5c", archive);
const temporary = mkdtempSync(join(cache, "unpack-"));
try {
  const unpack = spawnSync("tar", ["-xzf", archive, "-C", temporary, "package/dist/providers/data"], { stdio: "inherit" });
  if (unpack.error) throw unpack.error;
  if (unpack.status !== 0) throw new Error("Release data extraction failed");
  const source = join(temporary, "package/dist/providers/data");
  const hashes = JSON.parse(readFileSync(join(source, ".manifest.json"), "utf8")).files;
  for (const name of readdirSync(source)) {
    if (name !== ".manifest.json" && sha(readFileSync(join(source, name))) !== hashes[name]) throw new Error(`Catalog manifest mismatch: ${name}`);
    const destination = join(upstream, "packages/ai/src/providers/data", name);
    const data = readFileSync(join(source, name));
    if (existsSync(destination)) {
      if (sha(readFileSync(destination)) !== sha(data)) throw new Error(`Existing source asset differs: ${destination}`);
    } else {
      mkdirSync(dirname(destination), { recursive: true });
      writeFileSync(destination, data, { flag: "wx" });
    }
  }
} finally {
  rmSync(temporary, { recursive: true, force: true });
}
await asset(`https://raw.githubusercontent.com/earendil-works/pi/${target}/packages/chord/test/sqlite/helpers.ts`, "9f41d665ad1d41f237ba549881b8b3c91adc805ab84adc9c070a57cbbb05407e", join(upstream, "packages/chord/test/sqlite/helpers.ts"));
console.log("Verified pi-ai@1.5.0 generated catalogs and exact pinned Chord SQLite helper; original tests unchanged.");
