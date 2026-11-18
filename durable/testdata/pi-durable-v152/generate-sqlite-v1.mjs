// Generate an old schema using upstream's unmodified version-one migration.
// A pre-existing running task is seeded as a canonical row, since the public
// Storage constructor would immediately migrate the database to version two.
import { existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
const [upstream, output] = process.argv.slice(2);
if (!upstream || !output || existsSync(output)) throw Error('usage: <pinned upstream directory> <absent output path>');
const root = resolve(upstream);
const { openNodeSqliteDatabase } = await import(pathToFileURL(join(root, 'packages/chord/src/sqlite/node.ts')));
const { applySqliteMigrations, SQLITE_MIGRATIONS } = await import(pathToFileURL(join(root, 'packages/durable/src/storage/sqlite/migrations.ts')));
const db = await openNodeSqliteDatabase(resolve(output));
try {
  await applySqliteMigrations(db, SQLITE_MIGRATIONS.slice(0, 1));
  await db.transaction(async (tx) => {
    await tx.exec(`INSERT INTO record_ids VALUES (1,'conversation'),(2,'task');
      INSERT INTO conversations VALUES (1,NULL,NULL,'{"id":1}');
      INSERT INTO tasks VALUES (2,1,'"test.task"','running',0,0,'{"id":2,"conversationId":1,"kind":"test.task","version":1,"input":{},"background":false,"abortRequested":false,"state":{"status":"running","checkpoint":{"phase":"run"}}}');
      UPDATE durable_metadata SET next_id='3', next_seq=2 WHERE singleton=1;`);
  });
} finally {
  await db.close();
}
