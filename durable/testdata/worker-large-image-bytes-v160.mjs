import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { publishedSandbox } from './published-sandbox-v160.mjs';

// The actual script includes a 1 MiB literal, too large for one argv field.
const script = readFileSync(0, 'utf8');
const sandbox = publishedSandbox(process.argv[2], { timeoutMs: 10000, memoryLimitBytes: 33554432 });
try {
  const result = await sandbox.execute(script);
  const output = result.output.map(block => ({
    keys: Object.keys(block).sort(),
    type: block.type,
    mimeType: block.mimeType,
    bytes: typeof block.data === 'string' ? Buffer.byteLength(block.data, 'utf8') : -1,
    sha256: typeof block.data === 'string' ? createHash('sha256').update(block.data, 'utf8').digest('hex') : '',
  }));
  process.stdout.write(JSON.stringify({ ok: result.ok, output, error: result.error?.message }));
} finally {
  await sandbox.close();
}
