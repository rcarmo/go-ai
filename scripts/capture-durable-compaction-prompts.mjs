// Capture exact non-interpolated prompt literals from the pinned source.
import {readFileSync,writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {resolve,join} from 'node:path';
import {spawnSync} from 'node:child_process';
const upstream=resolve(process.argv[2]??'');
if(process.argv.length!==3)throw Error('Usage: bun run scripts/capture-durable-compaction-prompts.mjs /path/to/pinned/pi');
const check=spawnSync(process.execPath,[new URL('./check-durable-parity-register.mjs',import.meta.url).pathname,'--upstream',upstream],{stdio:'inherit'});
if(check.error)throw check.error;if(check.status!==0)throw Error('Source verification failed');
const source=readFileSync(join(upstream,'packages/durable/src/harness/compaction.ts'),'utf8');
// Independently compared with the raw GitHub file at the exact target commit.
const sourceSha256='22e5e341e7e6aede218eb4f4c48784d59031c0660a2dea62bdb6da5fd9e8a2c4';
if(createHash('sha256').update(source).digest('hex')!==sourceSha256)throw Error('Pinned compaction source drift');
const literal=name=>{const match=source.match(new RegExp('const '+name+' = `([\\s\\S]*?)`;'));if(!match||match[1].includes('${'))throw Error('Not a non-interpolated source literal: '+name);return match[1]};
writeFileSync(new URL('../durable/testdata/pi-durable-v150/compaction-prompts.json',import.meta.url),JSON.stringify({target:'f9a26365568127461c47a07a37dcb42c140a3ac5',sourceSha256,system:literal('SUMMARIZATION_SYSTEM_PROMPT'),user:literal('SUMMARIZATION_PROMPT')},null,2)+'\n');
