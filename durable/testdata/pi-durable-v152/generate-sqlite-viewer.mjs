// Run with Node --conditions=source against the exact upstream v1.5.2 checkout.
// The output path must not exist; the fixture is produced by public Storage calls.
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
const [upstream, output] = process.argv.slice(2);
if (!upstream || !output || existsSync(output)) throw Error('usage: <pinned upstream directory> <absent output path>');
const { openNodeSqliteStorage } = await import(pathToFileURL(join(resolve(upstream), 'packages/durable/src/storage/sqlite/node.ts')));
const { BACKGROUND_CONTEXT: context } = await import(pathToFileURL(join(resolve(upstream), 'packages/chord/src/context/index.ts')));
const storage = await openNodeSqliteStorage(resolve(output));
const receipts = [];
try {
  const result = await storage.commit([
    [
      { type: 'conversation', value: { id: 1 } },
      { type: 'entry', value: { id: 3, conversationId: 1, kind: 'note', data: 'first' } },
      { type: 'task', value: { id: 4, conversationId: 1, kind: 'test.task', version: 1, input: { x: 2 }, background: false, abortRequested: false, state: { status: 'running', checkpoint: { phase: 'run' } } } },
      { type: 'submission', value: { id: 5, conversationId: 1, requestId: 'request', type: 'input', status: 'placed', entry: 3 } },
      { type: 'document.create', record: { id: 6, kind: 'test.document', scope: { kind: 'conversation', conversationId: 1 }, history: 'rewindable', fork: 'asOf' }, content: { kind: 'base', version: 1, value: { text: 'before' } } },
    ],
    [
      { type: 'entry', value: { id: 7, conversationId: 1, kind: 'summary', head: 7, data: { done: true } } },
      { type: 'document.change', id: 6, content: { kind: 'delta', version: 1, ops: [['s', ['text'], 'after']] } },
    ],
  ], (index, seq) => receipts.push([index, seq]), context);
  if (result.stored !== 2 || result.refused !== undefined || JSON.stringify(receipts) !== '[[0,1],[1,2]]') throw Error(JSON.stringify({ result, receipts }));
} finally {
  await storage.close(context);
}
