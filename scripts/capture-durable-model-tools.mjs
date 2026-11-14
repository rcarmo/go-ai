// Capture original internal model-tool metadata without projecting Go fields.
import{readFile,writeFile}from'node:fs/promises';
import{join,resolve}from'node:path';
import{pathToFileURL}from'node:url';
import{createHash}from'node:crypto';
if(process.argv.length!==4)throw Error('Usage: node scripts/capture-durable-model-tools.mjs /pinned/pi /output.json');
const root=resolve(process.argv[2]),source=join(root,'packages/durable/src/codemode/models.ts');
const manifest=JSON.parse(await readFile(join(root,'packages/durable/package.json'),'utf8'));
if(manifest.version!=='1.5.0')throw Error('Wrong source version');
const bytes=await readFile(source),sha256=createHash('sha256').update(bytes).digest('hex');
if(sha256!=='ca43ea7e9bd656766c995b9ff342d3b6d73d3c736beb3c05ed629effefcd02c8')throw Error('Wrong pinned model-tools source');
const {createModelTools}=await import(pathToFileURL(source));
const tools=createModelTools().map(({name,description,parameters,structuredOutputSchema,callers})=>({name,description,parameters,structuredOutputSchema,callers}));
await writeFile(process.argv[3],JSON.stringify({target:'f9a26365568127461c47a07a37dcb42c140a3ac5',source:'packages/durable/src/codemode/models.ts',sha256,producer:'original createModelTools registrations',tools},null,2)+'\n');
console.log('Captured original model-tool metadata',tools.length);
