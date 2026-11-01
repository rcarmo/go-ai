#!/usr/bin/env bun
/** Rebuild a pinned upstream inventory and test crosswalk from exact Git objects.
 * Source scripts/project-env.sh first. Usage:
 * bun scripts/inventory-upstream-main.ts <mirror> <previous-inventory.json> <target-sha> <output-dir>
 * Does not fetch, check out files, run upstream code or change release refs.
 */
import { resolve, join } from "node:path";

const [mirror, previousFile, requestedTarget, output] = process.argv.slice(2);
if (!mirror || !previousFile || !/^[0-9a-f]{40}$/.test(requestedTarget ?? "") || !output) {
  throw new Error("expected mirror, previous inventory, full target SHA and output directory");
}
const git = (...args: string[]): string => {
  const result = Bun.spawnSync(["git", "-C", resolve(mirror), ...args]);
  if (result.exitCode !== 0) throw new Error(result.stderr.toString());
  return result.stdout.toString().trimEnd();
};
const lines = (s: string): string[] => s ? s.split("\n") : [];
const previous = await Bun.file(previousFile).json();
const baseline: string = previous.baseline;
const previousTarget: string = previous.target;
const target = git("rev-parse", "--verify", `${requestedTarget}^{commit}`);
if (target !== requestedTarget) throw new Error("target must name an exact commit");
git("merge-base", "--is-ancestor", previousTarget, target);
const names = ["ai", "durable", "codemode"];
const targetPaths = names.map(name => `packages/${name}/`);
const inTarget = (path: string) => targetPaths.some(prefix => path.startsWith(prefix));
const changes = (from: string) => lines(git("diff", "--name-status", "--no-renames", from, target)).map(line => {
  const [status, path, ...extra] = line.split("\t");
  if (!path || extra.length || !/^[AMDT]$/.test(status)) throw new Error(`unsupported changed path: ${line}`);
  return { status, path };
});
const incremental = changes(previousTarget);
const baselineChanges = changes(baseline);
const baselineChanged = new Set(baselineChanges.map(item => item.path));
const incrementalChanged = new Set(incremental.map(item => item.path));
const tree = lines(git("ls-tree", "-r", "--name-only", target));
// Same scope as the earlier crosswalk: entire package test/ trees and test-*/ cases.
const testPath = (path: string) => /^packages\/[^/]+\/test\//.test(path) ||
  (/^packages\/[^/]+\/test-[^/]+\//.test(path) && path.endsWith(".test.ts"));
const corpus = tree.filter(path => inTarget(path) && testPath(path));
const dependencyCorpus = tree.filter(path => path.startsWith("packages/schema/") && testPath(path));
const previousCrosswalkFile = join(resolve(previousFile, ".."), `upstream-tests-${previousTarget.slice(0, 8)}.tsv`);
const previousRows = lines((await Bun.file(previousCrosswalkFile).text()).trimEnd()).slice(1);
const inherited = new Map(previousRows.map(line => {
  const [path, , , disposition, evidence] = line.split("\t");
  if (!path || !disposition) throw new Error(`invalid previous crosswalk row: ${line}`);
  return [path, { disposition, evidence: evidence && evidence !== "-" ? evidence : "" }];
}));
const packages = Object.fromEntries(names.map(name => [name, {
  incrementalPaths: incremental.filter(item => item.path.startsWith(`packages/${name}/`)).length,
  baselinePaths: baselineChanges.filter(item => item.path.startsWith(`packages/${name}/`)).length,
  testCorpus: corpus.filter(path => path.startsWith(`packages/${name}/`)).length,
  previousManifest: JSON.parse(git("show", `${previousTarget}:packages/${name}/package.json`)),
  targetManifest: JSON.parse(git("show", `${target}:packages/${name}/package.json`)),
}]));
const inventory = {
  baseline, previousTarget, target,
  observedDate: new Date().toISOString().slice(0, 10),
  targetCommitTime: git("show", "-s", "--format=%cI", target),
  commitsSincePrevious: Number(git("rev-list", "--count", `${previousTarget}..${target}`)),
  purpose: "Pinned main inventories; all current dispositions require review. Inherited evidence retains its previous pin; no acceptance claim.",
  counts: {
    incrementalRepositoryPaths: incremental.length,
    incrementalPackagePaths: incremental.filter(item => inTarget(item.path)).length,
    baselinePackagePaths: baselineChanges.filter(item => inTarget(item.path)).length,
    testCorpus: corpus.length,
    repositorySupportDelta: incremental.filter(item => !inTarget(item.path)).length,
    dependencyTestCorpus: dependencyCorpus.length,
  },
  packages,
  incrementalPackageChanges: incremental.filter(item => inTarget(item.path)),
  baselinePackageChanges: baselineChanges.filter(item => inTarget(item.path)),
  repositorySupportDelta: incremental.filter(item => !inTarget(item.path)),
  dependencyManifests: { schema: JSON.parse(git("show", `${target}:packages/schema/package.json`)) },
  diffMode: "--no-renames; renames are represented as delete/add paths",
  testCorpusScope: previous.testCorpusScope,
  dependencyTestCorpusScope: "All packages/schema/test/ paths plus test-*/ .test.ts cases; dependency-review queue, not a native-port acceptance claim.",
};
const reviewFile = Bun.file(join(output, `upstream-review-${target.slice(0, 8)}.json`));
const review = await reviewFile.exists() ? await reviewFile.json() : { target, tests: {}, dependencyTests: {} };
if (review.target !== target) throw new Error("review must name the exact target");
for (const [entries, paths] of [[review.tests, corpus], [review.dependencyTests, dependencyCorpus]] as const) {
  for (const [path, entry] of Object.entries(entries) as [string, { disposition: string; evidence: string }][]) {
    if (!paths.includes(path) || !entry.disposition || !entry.evidence || /[\t\r\n]/.test(entry.disposition + entry.evidence)) {
      throw new Error(`invalid review entry: ${path}`);
    }
  }
}
const header = "upstream_path\tchanged_since_1.4.0\tprevious_pin_disposition\tcurrent_disposition\tnative_evidence";
const rows = corpus.map(path => {
  const old = inherited.get(path);
  const inheritedEvidence = old?.evidence ? `Inherited at ${previousTarget}: ${old.evidence}` : "";
  const current = review.tests[path];
  const evidence = [current?.evidence, inheritedEvidence].filter(Boolean).join("; ");
  return [path, baselineChanged.has(path) ? "yes" : "no", old?.disposition || "new", current?.disposition || "pending-review", evidence || "-"].join("\t");
});
const dependencyRows = dependencyCorpus.map(path => {
  const current = review.dependencyTests[path];
  return [path, current?.disposition || "pending-review", current?.evidence || "new pi-schema dependency; source assertions not yet ported"].join("\t");
});
const short = target.slice(0, 8);
await Bun.write(join(output, `tip-${short}.json`), JSON.stringify(inventory, null, 2) + "\n");
await Bun.write(join(output, `upstream-tests-${short}.tsv`), [header, ...rows].join("\n") + "\n");
await Bun.write(join(output, `dependency-tests-${short}.tsv`), ["upstream_path\tcurrent_disposition\tnative_evidence", ...dependencyRows].join("\n") + "\n");
console.log(JSON.stringify({ target, ...inventory.counts, packages: Object.fromEntries(names.map(name => [name, packages[name].testCorpus])), newlyPresentTests: corpus.filter(path => !inherited.has(path)), removedTests: [...inherited.keys()].filter(path => !corpus.includes(path)), changedTestsSincePrevious: corpus.filter(path => incrementalChanged.has(path)).length }, null, 2));
