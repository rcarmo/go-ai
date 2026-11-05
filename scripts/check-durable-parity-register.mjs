#!/usr/bin/env bun
// Check the register without changing its dispositions or generating test receipts.
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const base = join(root, "docs/upstream-v1.5.0");
const register = JSON.parse(readFileSync(join(base, "durable-parity-gaps.json"), "utf8"));
const readTSV = (name) => {
  const [header, ...lines] = readFileSync(join(base, name), "utf8").trimEnd().split("\n");
  const fields = header.split("\t");
  return lines.map((line) => {
    const values = line.split("\t");
    assert.equal(values.length, fields.length, `column count: ${name}: ${values[0]}`);
    return Object.fromEntries(fields.map((key, index) => [key, values[index]]));
  });
};
const rows = readTSV("durable-review-obligations.tsv");
const inventory = readTSV("upstream-tests-f9a26365.tsv")
  .filter((row) => row.upstream_path.startsWith("packages/durable/"));
const ids = new Set(register.entries.map((entry) => entry.id));
assert.equal(ids.size, register.entries.length, "duplicate gap IDs");
const states = new Set(["confirmed", "unverified", "scope-exclusion", "verification-gap", "fixed"]);
const markdown = readFileSync(join(base, "durable-deviations.md"), "utf8");
for (const entry of register.entries) {
  assert(states.has(entry.state), `state: ${entry.id}`);
  for (const key of ["title", "evidence", "fix", "blocker"]) {
    assert.equal(typeof entry[key], "string", `${entry.id}.${key}`);
    assert(entry[key].trim(), `${entry.id}.${key} empty`);
  }
  for (const key of ["source", "native", "acceptance"]) {
    assert(entry[key].length, `${entry.id}.${key} empty`);
  }
  for (const file of entry.native) assert(existsSync(join(root, file)), `missing native: ${file}`);
  if (entry.state === "fixed") {
    assert(entry.resolution?.trim(), `missing resolution: ${entry.id}`);
    assert(entry.verification?.length, `missing verification: ${entry.id}`);
  }
  for (const dep of entry.depends) assert(ids.has(dep) && dep !== entry.id, `dependency: ${entry.id}/${dep}`);
  assert(markdown.includes(`### ${entry.id} — ${entry.title}`), `missing detail: ${entry.id}`);
}
const paths = rows.map((row) => row.upstream_path).sort();
assert.equal(new Set(paths).size, paths.length, "duplicate obligations");
assert.deepEqual(paths.filter((path) => inventory.some((row) => row.upstream_path === path)),
  inventory.map((row) => row.upstream_path).sort(), "historical inventory coverage");
for (const row of rows) {
  const historical = inventory.find((item) => item.upstream_path === row.upstream_path);
  assert.equal(row.historical_disposition, historical?.current_disposition ?? "not-in-historical-inventory", `history: ${row.upstream_path}`);
  assert.equal(row.review_obligation, row.upstream_path.endsWith(".test.ts")
    ? "assertion-review-required" : "support-or-fixture-review-required");
  assert.match(row.sha256, /^[0-9a-f]{64}$/);
  for (const id of row.gap_ids.split(",")) assert(ids.has(id), `obligation gap: ${id}`);
}
const args = process.argv.slice(2);
assert(args.length === 0 || (args.length === 2 && args[0] === "--upstream"), "usage: bun run scripts/check-durable-parity-register.mjs [--upstream ROOT]");
if (args.length) {
  const upstream = resolve(args[1]);
  if (existsSync(join(upstream, ".git"))) {
    assert.equal(execFileSync("git", ["-C", upstream, "rev-parse", "HEAD"], { encoding: "utf8" }).trim(), register.target, "upstream pin");
  } else {
    console.log("Source snapshot has no .git: checking retained pinned-file hashes; commit identity is not independently reverified.");
  }
  for (const row of rows) {
    assert.equal(createHash("sha256").update(readFileSync(join(upstream, row.upstream_path))).digest("hex"), row.sha256, `source drift: ${row.upstream_path}`);
  }
  for (const entry of register.entries) {
    for (const file of entry.source) assert(existsSync(join(upstream, file)), `missing source: ${file}`);
  }
  const files = [];
  function walk(dir) {
    for (const entry of readdirSync(join(upstream, dir), { withFileTypes: true })) {
      const path = `${dir}/${entry.name}`;
      if (entry.isDirectory()) walk(path);
      else files.push(path);
    }
  }
  walk("packages/durable/test");
  walk("packages/durable/test-workers");
  assert.deepEqual(files.sort(), paths, "pinned durable test/support/fixture tree coverage");
}
console.log(`PASS: ${ids.size} register entries; ${rows.length} review obligations (${rows.filter((row) => row.upstream_path.endsWith(".test.ts")).length} test files); links/dependencies/history${args.length ? "; exact pinned source hashes and complete test tree" : ""}.`);
