#!/usr/bin/env bun
// Run unchanged pinned assertions plus the focused admission probes. Do not edit
// upstream files or inherit its "source" condition for published dependencies.
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const all = args.length === 3 && args[2] === "--all";
const testCase = args.length === 6 && args[4] === "--case" ? args[5] : undefined;
const only = (args.length === 4 || testCase) && args[2] === "--file" ? args[3] : undefined;
if ((args.length !== 2 && !all && !only) || args[0] !== "--upstream") {
  console.error("Usage: bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi [--all | --file test/name.test.ts [--case pattern]]");
  process.exit(2);
}
const upstream = resolve(args[1]);
const durable = join(upstream, "packages/durable");
const vitest = join(upstream, "node_modules/vitest");
for (const file of [join(durable, "src/session/session.ts"), join(vitest, "vitest.mjs")]) {
  if (!existsSync(file)) throw new Error(`Required pinned source or test runner missing: ${file}`);
}
let childImport;
function run(command, args, collectFailure = false, heapMiB = 384) {
  const result = spawnSync(command, args, { cwd: root, stdio: "inherit", env: { ...process.env, NODE_OPTIONS: `${process.env.NODE_OPTIONS ?? ""} --max-old-space-size=${heapMiB} ${childImport ? `--import=${childImport}` : ""}`.trim() } });
  if (result.error) throw result.error;
  if (result.status !== 0 && !collectFailure) throw new Error(`Verification failed (${result.status}): ${command}`);
  return result.status;
}
run(process.execPath, [join(root, "scripts/check-durable-parity-register.mjs"), "--upstream", upstream]);
const scratch = mkdtempSync(join(tmpdir(), "durable-session-oracles-"));
// Original subprocess fixtures request --conditions=source. The installed Chord
// artifact ships dist, not src; keep the same published resolution as Vitest.
// This changes module loading only, never the upstream files or assertions.
childImport = pathToFileURL(join(scratch, "published-chord.mjs")).href;
writeFileSync(fileURLToPath(childImport), `import { registerHooks } from 'node:module';\nregisterHooks({resolve(specifier,context,nextResolve){if(specifier==='@earendil-works/chord'||specifier.startsWith('@earendil-works/chord/'))return nextResolve(specifier,{...context,conditions:context.conditions.filter(c=>c!=='source')});return nextResolve(specifier,context)}});\n`);
// Resolve every durable export to this pinned source, most-specific first.
// Do not enable the source condition for unrelated published dependencies.
const sourceExports = Object.entries(JSON.parse(readFileSync(join(durable, "package.json"), "utf8")).exports)
  .filter(([, target]) => typeof target?.source === "string")
  .map(([key, target]) => [key === "." ? "@earendil-works/pi-durable" : `@earendil-works/pi-durable${key.slice(1)}`, join(durable, target.source)])
  .sort((a, b) => b[0].length - a[0].length);
const aliases = [
  ...sourceExports,
  ["@pinned/session", join(durable, "src/session/session.ts")],
  ["@pinned/support", join(durable, "test/session-support.ts")],
  ["@pinned/tasks", join(durable, "test/task-support.ts")],
  ["@pinned/chat", join(durable, "test/chat-support.ts")],
  ["@pinned/harness-support", join(durable, "test/harness-support.ts")],
  ["@pinned/sqlite-node", join(durable, "src/storage/sqlite/node.ts")],
  ["@pinned/views", join(durable,"src/harness/view.ts")],
  ["vitest", join(vitest, "dist/index.js")],
].map(([name, replacement]) => ({ find: name, replacement }));
try {
  // A fresh Vitest process per file bounds retained transform/module heaps.
  // maxWorkers=1 alone does not bound the parent across the complete corpus.
  const originalFiles = readdirSync(join(durable, "test"), { recursive: true })
    .filter(file => file.endsWith(".test.ts"))
    .sort()
    .map(file => `test/${file}`);
  const pinnedSetup = [join(durable, "test/setup-document-retention.ts")];
  if (only && !originalFiles.includes(only)) throw new Error(`Not an original pinned test file: ${only}`);
  const groups = all ? originalFiles.map((file, index) => [`pinned-${index + 1}`, durable, [file], pinnedSetup]) : only ? [["pinned-file", durable, [only], pinnedSetup]] : [
    ["pinned", durable, ["test/session-group-commit.test.ts", "test/session-checkpoints-migrations.test.ts", "test/session-tables.test.ts", "test/harness-view.test.ts", "test/harness-hook-reads.test.ts", "test/harness-input-entries.test.ts", "test/harness-submissions.test.ts", "test/harness-submit-in-commit.test.ts", "test/session-observers.test.ts", "test/session-watches.test.ts"], pinnedSetup],
  ];
  groups.push(["probes", root, ["notes/oracles/session-admission.upstream.test.ts", "notes/oracles/storage-tasks.upstream.test.ts"], []]);
  let cases = 0;
  let skipped = 0;
  const failed = [];
  for (const [name, cwd, include, setupFiles] of groups) {
    console.log(`Oracle ${name}: ${include.join(", ")}`);
    const config = join(scratch, `${name}.mjs`);
    writeFileSync(config, `import { defineConfig } from ${JSON.stringify(pathToFileURL(join(vitest, "dist/config.js")).href)};\nexport default defineConfig(${JSON.stringify({ root: cwd, cacheDir: join(scratch, "cache", name), resolve: { alias: aliases }, test: { include, setupFiles, maxWorkers: 1, fileParallelism: false, testTimeout: 30000 } })});\n`);
    const report = join(scratch, `${name}.json`);
    // Large image equality assertions need more than 384 MiB. This is still
    // one fresh process; the original child test's own 128 MB CLI cap is intact.
    const heapMiB = include.includes("test/tools-images.test.ts") ? 768 : 384;
    const status = run("node", [join(vitest, "vitest.mjs"), "run", "--config", config, all ? "--reporter=dot" : "--reporter=default", "--reporter=json", `--outputFile.json=${report}`, ...(testCase && name === "pinned-file" ? ["--testNamePattern", testCase] : [])], all, heapMiB);
    if (!existsSync(report)) {
      failed.push(`${name}: ${include.join(", ")} (no report; status ${status})`);
      if (!all) throw new Error(failed.at(-1));
      continue;
    }
    const result = JSON.parse(readFileSync(report, "utf8"));
    if (status !== 0 || !result.success || result.numFailedTests) {
      failed.push(`${name}: ${include.join(", ")} (${result.numFailedTests} failed tests; status ${status})`);
      if (!all) throw new Error(failed.at(-1));
    }
    if (testCase && name === "pinned-file" && result.numPassedTests === 0) throw new Error("Case selector executed no assertions");
    cases += result.numPassedTests;
    skipped += (result.numPendingTests ?? 0) + (result.numTodoTests ?? 0);
  }
  console.log(`${failed.length ? "FAIL" : "PASS"}: ${all ? originalFiles.length : only ? 1 : 10} unchanged source files plus probes; ${cases} passed test cases; ${skipped} skipped/todo/filtered cases. Original execution is not native parity; skipped/filtered cases remain unverified.`);
  if (failed.length) throw new Error(`Oracle failures (${failed.length} groups):\n${failed.join("\n")}`);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
