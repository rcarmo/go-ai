import { pathToFileURL } from 'node:url';
import { readFile,writeFile } from 'node:fs/promises';
import { join,resolve } from 'node:path';
const reference=resolve(process.argv[2]);
const manifest=JSON.parse(await readFile(join(reference,'node_modules/@earendil-works/pi-durable/package.json'),'utf8'));
if(manifest.name!=='@earendil-works/pi-durable'||manifest.version!=='1.5.0')throw Error('wrong reference package');
const {MemoryStorage}=await import(pathToFileURL(join(reference,'node_modules/@earendil-works/pi-durable/dist/index.js')));
const {BACKGROUND_CONTEXT:context}=await import(pathToFileURL(join(reference,'node_modules/@earendil-works/chord/dist/context/index.js')));
const cases=[
 ['pending-null-input',{status:'pending',checkpoint:{phase:'work'}},null,{}],
 ['running-raw-memos',{status:'running',checkpoint:{phase:'work'}},{a:1},{memos:{nil:null,false:false,empty:''},startedAt:0}],
 ['waiting-empty-on',{status:'waiting',checkpoint:{phase:'next'},on:[],policy:'allSettled'},false,{}],
 ['sleeping-zero-deadline',{status:'sleeping',checkpoint:{phase:'next'},until:0},'',{}],
 ['completing-null-result',{status:'completing',outcome:{status:'completed',result:null}},[],{}],
 ['terminal-completed-object',{status:'terminal',outcome:{status:'completed',result:{ok:true}}},null,{}],
 ['terminal-failed-null-detail',{status:'terminal',outcome:{status:'failed',error:{message:'failure',detail:null},result:false}},null,{}],
 ['terminal-aborted-no-reason',{status:'terminal',outcome:{status:'aborted'}},null,{}],
 ['terminal-aborted-empty-reason',{status:'terminal',outcome:{status:'aborted',reason:''}},null,{}],
 ['terminal-orphaned',{status:'terminal',outcome:{status:'orphaned',reason:'missing_task'}},null,{}],
 ['terminal-faulted-detail',{status:'terminal',outcome:{status:'faulted',error:{message:'contract',detail:{safe:true}}}},null,{}],
 ['optional-zero-metadata',{status:'terminal',outcome:{status:'completed',result:false}},null,{startedAt:0,endedAt:0,restarts:0,abandonOnRestart:true,trace:{},links:[]}],
 ['opaque-stored-checkpoint',{status:'pending',checkpoint:42},null,{}],
];
const fixtures=[];
for(const [name,state,input,extras] of cases){
 const storage=new MemoryStorage();
 let result=await storage.commit([[{type:'conversation',value:{id:1}}]],()=>{},context);
 if(result.stored!==1||'refused'in result)throw Error('conversation fixture refused');
 const id=await storage.mintId();
 const record={id,conversationId:1,kind:'compat.task',version:1,input,background:false,abortRequested:false,state,...extras};
 result=await storage.commit([[{type:'task',value:record}]],()=>{},context);
 if(result.stored!==1||'refused'in result)throw Error('task fixture refused: '+name);
 const stored=await storage.task(id,context);
 fixtures.push({name,record:stored});
 await storage.close(context);
}
await writeFile(process.argv[3],JSON.stringify({target:'f9a26365568127461c47a07a37dcb42c140a3ac5',package:manifest.name,version:manifest.version,producer:'original MemoryStorage.commit/task',fixtures},null,2)+'\n');
console.log('Captured original task records:',fixtures.length);
