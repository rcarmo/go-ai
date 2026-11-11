#!/usr/bin/env bun
// Run unchanged pinned assertions plus the focused admission probes. Do not edit
// upstream files or inherit its "source" condition for published dependencies.
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
if (args.length !== 2 || args[0] !== "--upstream") {
  console.error("Usage: bun run scripts/check-durable-session-oracles.mjs --upstream /path/to/pinned/pi");
  process.exit(2);
}
const upstream = resolve(args[1]);
const durable = join(upstream, "packages/durable");
const vitest = join(upstream, "node_modules/vitest");
for (const file of [join(durable, "src/session/session.ts"), join(vitest, "vitest.mjs")]) {
  if (!existsSync(file)) throw new Error(`Required pinned source or test runner missing: ${file}`);
}
function run(command, args) {
  const result = spawnSync(command, args, { cwd: root, stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Verification failed (${result.status}): ${command}`);
}
run(process.execPath, [join(root, "scripts/check-durable-parity-register.mjs"), "--upstream", upstream]);
const scratch = mkdtempSync(join(tmpdir(), "durable-session-oracles-"));
const aliases = [
  ["@earendil-works/pi-durable/testing", join(durable, "src/testing/index.ts")],
  ["@earendil-works/pi-durable", join(durable, "src/index.ts")],
  ["@pinned/session", join(durable, "src/session/session.ts")],
  ["@pinned/support", join(durable, "test/session-support.ts")],
  ["@pinned/tasks", join(durable, "test/task-support.ts")],
  ["@pinned/chat", join(durable, "test/chat-support.ts")],
  ["vitest", join(vitest, "dist/index.js")],
].map(([name, replacement]) => ({ find: name, replacement }));
try {
  for (const [name, cwd, include, setupFiles] of [
    ["pinned", durable, ["test/session-group-commit.test.ts", "test/session-checkpoints-migrations.test.ts", "test/session-tables.test.ts", "test/harness-view.test.ts", "test/session-observers.test.ts", "test/session-watches.test.ts"], [join(durable, "test/setup-document-retention.ts")]],
    ["probes", root, ["notes/oracles/session-admission.upstream.test.ts"], []],
  ]) {
    const config = join(scratch, `${name}.mjs`);
    writeFileSync(config, `import { defineConfig } from ${JSON.stringify(pathToFileURL(join(vitest, "dist/config.js")).href)};\nexport default defineConfig(${JSON.stringify({ root: cwd, cacheDir: join(scratch, "cache", name), resolve: { alias: aliases }, test: { include, setupFiles, maxWorkers: 1, fileParallelism: false, testTimeout: 30000 } })});\n`);
    run("node", [join(vitest, "vitest.mjs"), "run", "--config", config]);
  }
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
