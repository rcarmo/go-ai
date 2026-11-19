// Generate the literal seed-6 prefix from pi v1.5.2 with its own vitest runner.
// Usage: node record-sqlite-random-prefix.mjs <upstream checkout> <output> [--all|--records|--links|--documents]
// --all records commits; --records records selected public reads.
import { createHash } from 'node:crypto';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

import { gzipSync } from "node:zlib";

const [, , root, output, mode] = process.argv;
if (mode !== undefined && !["--all", "--records", "--links", "--documents"].includes(mode)) throw Error("unknown recording mode");
const all = mode !== undefined;
const records = mode === "--records";
const links = mode === "--links";
const documents = mode === "--documents";
if (!root || !output) throw Error('expected upstream checkout and output path');
const pkg = join(resolve(root), 'packages/durable');
const source = readFileSync(join(pkg, 'test/sqlite-commit.test.ts'), 'utf8');
if (createHash('sha256').update(source).digest('hex') !== '9722217086fe556301114e5662dd2ce34307534f5341bcaecb0f64c970457897') {
  throw Error('expected pinned v1.5.2 sqlite-commit.test.ts');
}
const test = 'test/sqlite-commit-trace-supplement.test.ts';
const anchor = 'const actual = await commitOutcome(sqlite, batches);';
const snapshot = 'await snapshot(reference, world, seqs)';
if (source.split(snapshot).length !== 2 || source.split(anchor).length !== 2) throw Error('unexpected source anchors');
const projection = links
  ? '{ seed, round, conversationIds: [...new Set(world.conversations)], submissionIds: [...new Set(world.submissions)], transcripts: actual.transcripts, requests: actual.requests }'
  : '{ seed, round, entryIds: [...new Set(world.entries)], conversations: actual.conversations, entries: actual.entries, tasks: actual.tasks, submissions: actual.submissions }';
const recordSnapshot = (points, roundNumber) => `await (async () => {
  const actual = (await snapshot(reference, world, ${points})) as Record<string, unknown>;
  appendFileSync(process.env.TRACE_OUT!, JSON.stringify(${documents
    ? `{ seed, round: ${roundNumber}, points: ${points}, conversationIds: world.conversations, taskIds: world.tasks, addresses: world.documents.map(d => d.record), documents: actual.documents, scopes: actual.scopes, foundAddresses: actual.addresses }`
    : projection}) + "\\n");
  return actual;
})()`;
const finalSnapshot = 'await snapshot(reference, world, world.seqs)';
if (source.split(finalSnapshot).length !== 2) throw Error('unexpected final source snapshot');
const copy = 'import { appendFileSync } from "node:fs";\n' + (documents
  ? source.replace(snapshot, recordSnapshot('seqs', 'round')).replace(finalSnapshot, recordSnapshot('world.seqs', '120'))
  : records || links
  ? source.replace(snapshot, recordSnapshot('seqs', 'round'))
  : source.replace(anchor, `${anchor}
    if (${all ? "true" : "seed === 6 && round < 3"}) appendFileSync(process.env.TRACE_OUT!, JSON.stringify({ seed, round, batches, expected }) + "\\n");`));
const fixture = resolve(output);
const scratch = fixture + '.partial';
writeFileSync(join(pkg, test), copy, { flag: 'wx' });
try {
  writeFileSync(scratch, '', { flag: 'wx' });
  try {
    const run = spawnSync(process.execPath, ['../../node_modules/vitest/vitest.mjs', 'run', test, '-t', all ? 'agrees with MemoryStorage' : 'seed 6', '--maxWorkers=1'], {
      cwd: pkg, env: { ...process.env, TRACE_OUT: scratch }, stdio: 'inherit',
    });
    const result = readFileSync(scratch, 'utf8');
    if (run.status !== 0 || result.trimEnd().split('\n').length !== (documents ? 726 : all ? 720 : 3)) throw Error('recording failed');
    writeFileSync(fixture, all ? gzipSync(result, { level: 9 }) : result);
  } finally {
    rmSync(scratch);
  }
} finally {
  rmSync(join(pkg, test));
}
