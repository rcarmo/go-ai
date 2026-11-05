// Captures the StorageWrite batches the official pi-durable 1.5.0 Session hands to Storage.commit.
import { pathToFileURL } from 'node:url';
import { readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
const reference = resolve(process.argv[2]);
const manifest = JSON.parse(await readFile(join(reference, 'node_modules/@earendil-works/pi-durable/package.json'), 'utf8'));
if (manifest.name !== '@earendil-works/pi-durable' || manifest.version !== '1.5.0') throw Error('wrong reference package');
const d = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/pi-durable/dist/index.js')));
const { BACKGROUND_CONTEXT: context } = await import(pathToFileURL(join(reference, 'node_modules/@earendil-works/chord/dist/context/index.js')));
const storage = new d.MemoryStorage();
const batches = [];
const commit = storage.commit.bind(storage);
storage.commit = (bs, receive, ctx) => { for (const b of bs) batches.push(JSON.parse(JSON.stringify(b))); return commit(bs, receive, ctx); };
const session = d.createSession(storage, { now: () => 1000 });
const SessionDoc = d.defineDoc({ checkpointWhen: () => false, kind: 't.session', version: 1, scope: 'session', initial: () => ({ value: 0 }) });
const Rewind = d.defineDoc({ checkpointWhen: () => false, kind: 't.rewind', version: 1, scope: 'conversation', history: 'rewindable', fork: 'asOf', initial: () => ({ value: 0 }) });
const Work = d.defineTask({ name: 't.work', version: 1, initial: () => ({ phase: 'start' }), phases: { start: async () => {} } });
const root = d.ROOT_CONVERSATION_ID;
await session.commit(async (tx) => { (await tx.doc(SessionDoc)).value = 1; }, context);
const conv = await session.commit(async (tx) => (await tx.createConversation({ ownership: { kind: 'ownerless' } })).id, context);
const first = await session.commit(async (tx) => {
  (await tx.doc(Rewind, conv)).value = 1;
  return (await tx.appendEntry(conv, { kind: 'note', data: { n: 1 } })).id;
}, context);
await session.commit(async (tx) => { (await tx.doc(Rewind, conv)).value = 2; await tx.appendEntry(conv, { kind: 'note', head: 'self', model: [{ role: 'user', content: [{ type: 'text', text: 'hi' }], timestamp: 1 }] }); }, context);
const task = await session.commit(async (tx) => tx.createTask(Work, { path: 'a' }, { ownership: { kind: 'conversation' }, conversationId: conv }), context);
await session.commit(async (tx) => { await tx.createConversation({ ownership: { kind: 'task', taskId: task } }); }, context);
await session.commit(async (tx) => { await tx.createSubmission({ conversationId: conv, type: 'write', status: 'queued', requestId: 'r1' }); }, context);
await session.commit(async (tx) => { await tx.forkConversation(conv, first, { ownership: { kind: 'ownerless' } }); }, context);
await session.commit(async (tx) => { await tx.retireDoc(SessionDoc); }, context);
// Exercise every SubmissionRecord lifecycle branch through original raw creation,
// including explicitly empty request/reason strings and a present null detail.
await session.commit(async (tx) => {
  const states = [
    { type: 'input', status: 'queued', requestId: '' },
    { type: 'input', status: 'placed', entry: first },
    { type: 'input', status: 'done', entry: first, answer: first },
    { type: 'input', status: 'unanswered', reason: '', detail: null },
    { type: 'input', status: 'unanswered', entry: first, reason: 'failed', detail: { n: 9007199254740991 } },
    { type: 'write', status: 'done', entry: first },
    { type: 'write', status: 'unanswered', reason: 'failed', detail: false },
    { type: 'continue', status: 'placed', trace: {} },
    { type: 'continue', status: 'done', answer: first },
    { type: 'continue', status: 'unanswered', reason: 'failed', detail: ['x'] },
  ];
  for (const state of states) await tx.createSubmission({ conversationId: conv, ...state });
}, context);
const TaskDoc = d.defineDoc({ checkpointWhen: () => true, kind: 't.task', version: 1, scope: 'task', initial: () => ({ value: 0 }) });
await session.commit(async (tx) => { (await tx.doc(TaskDoc, task)).value = 1; }, context);
// An explicit checkpoint policy emits a full-base document.change here.
await session.commit(async (tx) => { (await tx.doc(TaskDoc, task)).value = 2; }, context);
await session.close?.(context);
const types = [...new Set(batches.flat().map((w) => w.type))].sort();
await writeFile(process.argv[3], JSON.stringify({ target: 'f9a26365568127461c47a07a37dcb42c140a3ac5', package: manifest.name, version: manifest.version, producer: 'original Session -> MemoryStorage.commit batches', types, batches }, null, 2) + '\n');
console.log('captured', batches.length, types.join(' '));
