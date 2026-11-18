// Generate the literal seed-6 prefix from pi v1.5.2 with its own vitest runner.
// Usage: node record-sqlite-random-prefix.mjs <upstream checkout> <output>
import { createHash } from 'node:crypto';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const [, , root, output] = process.argv;
if (!root || !output) throw Error('expected upstream checkout and output path');
const pkg = join(resolve(root), 'packages/durable');
const source = readFileSync(join(pkg, 'test/sqlite-commit.test.ts'), 'utf8');
if (createHash('sha256').update(source).digest('hex') !== '9722217086fe556301114e5662dd2ce34307534f5341bcaecb0f64c970457897') {
  throw Error('expected pinned v1.5.2 sqlite-commit.test.ts');
}
const test = 'test/sqlite-commit-trace-supplement.test.ts';
const anchor = 'const actual = await commitOutcome(sqlite, batches);';
const copy = 'import { appendFileSync } from "node:fs";\n' + source.replace(anchor, `${anchor}
    if (seed === 6 && round < 3) appendFileSync(process.env.TRACE_OUT!, JSON.stringify({ seed, round, batches, expected }) + "\\n");`);
const fixture = resolve(output);
const scratch = fixture + '.partial';
writeFileSync(join(pkg, test), copy, { flag: 'wx' });
try {
  writeFileSync(scratch, '', { flag: 'wx' });
  try {
    const run = spawnSync(process.execPath, ['../../node_modules/vitest/vitest.mjs', 'run', test, '-t', 'seed 6', '--maxWorkers=1'], {
      cwd: pkg, env: { ...process.env, TRACE_OUT: scratch }, stdio: 'inherit',
    });
    const result = readFileSync(scratch, 'utf8');
    if (run.status !== 0 || result.trimEnd().split('\n').length !== 3) throw Error('recording failed');
    writeFileSync(fixture, result);
  } finally {
    rmSync(scratch);
  }
} finally {
  rmSync(join(pkg, test));
}
