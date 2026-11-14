// Capture an executable original pi-durable 1.5.0 summary request, not a Go projection.
import {readFile,writeFile} from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
const retry=process.argv[4]==='--retry';
if(process.argv.length!==4&&!(process.argv.length===5&&retry))throw Error('Usage: node scripts/capture-durable-compaction-request.mjs /reference/package /output.json [--retry]');
const reference=resolve(process.argv[2]);
const manifest=JSON.parse(await readFile(join(reference,'node_modules/@earendil-works/pi-durable/package.json'),'utf8'));
if(manifest.name!=='@earendil-works/pi-durable'||manifest.version!=='1.5.0')throw Error('Wrong reference version');
const {Harness,MemoryStorage,createRegistry}=await import(pathToFileURL(join(reference,'node_modules/@earendil-works/pi-durable/dist/index.js')));
const {createModels,fauxProvider,fauxAssistantMessage}=await import(pathToFileURL(join(reference,'node_modules/@earendil-works/pi-ai/dist/index.js')));
const {BACKGROUND_CONTEXT:context}=await import(pathToFileURL(join(reference,'node_modules/@earendil-works/chord/dist/context/index.js')));
const faux=fauxProvider({models:[{id:'faux-1',contextWindow:128000,maxTokens:256}]}),models=createModels();models.setProvider(faux.provider);
let reached;const entered=new Promise(resolve=>{reached=resolve});
faux.setResponses(retry?[fauxAssistantMessage('',{stopReason:'error',errorMessage:'503 overloaded'})]:[(_input,options)=>new Promise((_resolve,reject)=>{reached();options.signal.addEventListener('abort',()=>reject(options.signal.reason),{once:true})})]);
const storage=new MemoryStorage();
const harness=await Harness.open(storage,{models,registry:createRegistry(),settings:{compaction:{enabled:false,reserveTokens:125,keepRecentTokens:0},stream:{timeoutMs:1234,deferred:true}},now:()=>42},context);
try{
 if(retry)harness.observeCommits({advance:publication=>{for(const change of publication.changes)if(change.type==='task'&&change.value.kind==='pi.compaction'&&change.value.state.checkpoint?.phase==='retry')reached()}});
 const root=await harness.root(context,{agent:{model:{provider:'faux',modelId:'faux-1'}}});
 await root.commit(async tx=>{for(const content of ['old','recent'])await tx.appendEntry(root.id,{kind:'note',model:[{role:'user',content,timestamp:0}]})},context);
 const id=await root.compact('paths',context);await entered;
 const fixture={target:'f9a26365568127461c47a07a37dcb42c140a3ac5',package:manifest.name,version:manifest.version,producer:'original Harness running CompactionTask + MemoryStorage records',conversations:[await storage.conversation(root.id,context)],entries:[...(await root.entries({},100,undefined,context)).items].reverse(),tasks:[await harness.getTask(id,context)]};
 await writeFile(process.argv[3],JSON.stringify(fixture,null,2)+'\n');
 console.log('Captured original compaction checkpoint',retry?'retry':'summarize',id);
}finally{await harness.close(context)}
