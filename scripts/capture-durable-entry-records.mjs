// Captures official pi-durable 1.5.0 MemoryStorage conversation/entry records.
import { pathToFileURL } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
const reference = resolve(process.argv[2]);
const manifest = JSON.parse(await readFile(join(reference, 'node_modules/@earendil-works/pi-durable/package.json'), 'utf8'));
if (manifest.name !== '@earendil-works/pi-durable' || manifest.version !== '1.5.0') throw Error('wrong reference package');
const { MemoryStorage } = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/pi-durable/dist/index.js')));
const { BACKGROUND_CONTEXT: context } = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/chord/dist/context/index.js')));
const s = new MemoryStorage();
const commit = async (writes) => { const r = await s.commit([writes], () => {}, context); if ('refused' in r) throw Error('refused: ' + JSON.stringify(r.refused)); };
await commit([{ type: 'conversation', value: { id: 1 } }]);
const [e1, e2, e3, e4, e5, task, fork, owned] = [await s.mintId(), await s.mintId(), await s.mintId(), await s.mintId(), await s.mintId(), await s.mintId(), await s.mintId(), await s.mintId()];
const user = { role: 'user', content: [{ type: 'text', text: 'hi' }], timestamp: 1 };
await commit([
  { type: 'entry', value: { id: e1, conversationId: 1, kind: 'note' } },
  { type: 'entry', value: { id: e2, conversationId: 1, kind: 'note', data: null } },
  { type: 'entry', value: { id: e3, conversationId: 1, kind: 'message', model: [user], data: { text: 'hi' }, head: e3 } },
  { type: 'task', value: { id: task, conversationId: 1, kind: 't', version: 1, input: null, state: { status: 'pending', checkpoint: {} }, background: false, abortRequested: false } },
]);
await commit([
  { type: 'entry', value: { id: e4, conversationId: 1, kind: 'edit', edits: [{ target: e1, action: 'omit' }, { target: e3, action: 'replace', messages: [user] }], byTaskId: task } },
  { type: 'conversation', value: { id: fork, parent: { conversationId: 1, at: e3 } } },
  { type: 'conversation', value: { id: owned, owner: { conversationId: 1, taskId: task } } },
]);
await commit([{ type: 'entry', value: { id: e5, conversationId: fork, kind: 'note', data: 0 } }]);
const conversations = []; for (const id of [1, fork, owned]) conversations.push(await s.conversation(id, context));
const entries = []; for (const id of [e1, e2, e3, e4, e5]) entries.push((await s.entry(id, context)).entry);
await s.close(context);
await writeFile(process.argv[3], JSON.stringify({ target: 'f9a26365568127461c47a07a37dcb42c140a3ac5', package: manifest.name, version: manifest.version, producer: 'original MemoryStorage.conversation/entry', conversations, entries }, null, 2) + '\n');
console.log('captured', conversations.length, entries.length);
