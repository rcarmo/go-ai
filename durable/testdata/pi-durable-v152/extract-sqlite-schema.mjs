// node --conditions=source extract-sqlite-schema.mjs <pinned v1.5.2 source>
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
const [root] = process.argv.slice(2);
if (!root) throw Error('expected the pinned v1.5.2 source directory');
const src = join(resolve(root),'packages/durable/src/storage/sqlite');
const { createSqliteSchema } = await import(pathToFileURL(join(src,'schema.ts')));
const { createSqliteMigrations } = await import(pathToFileURL(join(src,'migrations.ts')));
console.log(JSON.stringify(createSqliteMigrations(createSqliteSchema('pi_schema_')),null,2));
