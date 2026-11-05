// Captures actual official pi-durable 1.5.0 MemoryStorage scan pages and cursors.
import { pathToFileURL } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
const reference = resolve(process.argv[2]);
const manifest = JSON.parse(await readFile(join(reference, 'node_modules/@earendil-works/pi-durable/package.json'), 'utf8'));
if (manifest.name !== '@earendil-works/pi-durable' || manifest.version !== '1.5.0') throw Error('wrong reference package');
const { MemoryStorage } = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/pi-durable/dist/index.js')));
const { BACKGROUND_CONTEXT: context } = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/chord/dist/context/index.js')));
const storage = new MemoryStorage();
const commit = async (writes) => { const r = await storage.commit([writes], () => {}, context); if ('refused' in r) throw Error('refused'); };
await commit([{ type: 'conversation', value: { id: 1 } }]);
const ids = []; for (let i = 0; i < 6; i++) ids.push(await storage.mintId());
const [c2, c3, e1, e2, e3, t1] = ids;
const t2 = await storage.mintId(), s1 = await storage.mintId(), s2 = await storage.mintId();
await commit([{ type: 'conversation', value: { id: c2 } }, { type: 'conversation', value: { id: c3 } }]);
await commit([1, 2, 3].map((n, i) => ({ type: 'entry', value: { id: [e1, e2, e3][i], conversationId: 1, kind: 'message' } })));
const task = (id) => ({ id, conversationId: 1, kind: 'test.task', version: 1, input: null, state: { status: 'pending', checkpoint: {} }, background: false, abortRequested: false });
await commit([{ type: 'task', value: task(t1) }, { type: 'task', value: task(t2) }]);
await commit([s1, s2].map((id, i) => ({ type: 'submission', value: { id, conversationId: 1, requestId: 'r' + i, type: 'input', status: 'placed', entry: e1 } })));
const pages = [];
const run = async (name, method, query, limit, cursor) => {
  try {
    const page = await storage[method](query, limit, cursor, context);
    pages.push({ name, method, query, limit, cursor: cursor ?? null, items: page.items.map((x) => x.id), next: page.next ?? null });
    return page;
  } catch (error) {
    pages.push({ name, method, query, limit, cursor: cursor ?? null, error: error.constructor.name, message: error.message });
  }
};
for (const order of [undefined, 'ascending', 'descending']) {
  const q = order === undefined ? {} : { order };
  const first = await run(`conversations-${order ?? 'default'}-1`, 'scanConversations', q, 2);
  await run(`conversations-${order ?? 'default'}-2`, 'scanConversations', {}, 2, JSON.parse(JSON.stringify(first.next)));
  const eq = { conversationId: 1, ...q };
  const ef = await run(`entries-${order ?? 'default'}-1`, 'scanEntries', eq, 2);
  await run(`entries-${order ?? 'default'}-2`, 'scanEntries', { conversationId: 1 }, 2, ef.next);
  const tf = await run(`tasks-${order ?? 'default'}-1`, 'scanTasks', q, 1);
  await run(`tasks-${order ?? 'default'}-2`, 'scanTasks', {}, 1, tf.next);
  const sf = await run(`submissions-${order ?? 'default'}-1`, 'scanSubmissions', q, 1);
  await run(`submissions-${order ?? 'default'}-2`, 'scanSubmissions', {}, 1, sf.next);
}
// Source cursor contract: filters are not bound to the cursor; only order and after are checked.
await run('tasks-filter-changed', 'scanTasks', { kind: 'test.task' }, 5, { after: t1, order: 'ascending' });
await run('conversations-legacy-no-order', 'scanConversations', {}, 5, { after: 1 });
await run('entries-legacy-no-order', 'scanEntries', { conversationId: 1 }, 5, { after: e3 });
await run('conversations-extra-field', 'scanConversations', {}, 5, { after: 1, order: 'ascending', extra: true });
await run('conversations-order-mismatch', 'scanConversations', { order: 'descending' }, 5, { after: 1, order: 'ascending' });
await run('conversations-bad-after', 'scanConversations', {}, 5, { after: 'x' });
await run('conversations-bad-order', 'scanConversations', {}, 5, { after: 1, order: 'up' });
await storage.close(context);
await writeFile(process.argv[3], JSON.stringify({ target: 'f9a26365568127461c47a07a37dcb42c140a3ac5', package: manifest.name, version: manifest.version, producer: 'original MemoryStorage scans', ids: { c2, c3, e1, e2, e3, t1, t2, s1, s2 }, pages }, null, 2) + '\n');
console.log('Captured original scan pages:', pages.length);
