import {expect,it} from "vitest";
import {createStorageConformance} from "@earendil-works/pi-durable/testing";
import {openNodeSqliteStorage} from "@pinned/sqlite-node";
import {mkdtempSync,rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {context} from "@pinned/support";

// Execute the original runner-independent assertions unchanged, against SQLite.
const cases=createStorageConformance({
 assertions:{ok:(v,m)=>expect(v,m).toBeTruthy(),strictEqual:(a,b)=>expect(a).toBe(b),deepEqual:(a,b)=>expect(a).toEqual(b),partialDeepEqual:(a,b)=>expect(a).toMatchObject(b),greaterThan:(a,b)=>expect(a).toBeGreaterThan(b),rejects:async(p,m)=>{await expect(p).rejects.toThrow(m)}},
 withStorage:async use=>{const dir=mkdtempSync(join(tmpdir(),"storage-tasks-conformance-"));let storage;try{storage=await openNodeSqliteStorage(join(dir,"state.db"));await use(storage)}finally{if(storage)await storage.close(context);rmSync(dir,{recursive:true,force:true})}},
});
const selected=["replaces complete task records and pages filtered task scans","stores owners and scans waiting and completing tasks by status","stores and scans sleeping tasks by status","scans deep fork history newest-first through every ancestor cap","paginates conversations by opaque cursor in ascending ID order","indexes request IDs per conversation and replaces complete submission records","stores passive write submissions without input-only lifecycle states","stores continue submissions, which have no entry and are never queued"];
for(const name of selected){const test=cases.find(test=>test.name===name);if(!test)throw Error(`Missing source conformance: ${name}`);it(name,()=>test.run())}

it("keeps complete raw task replacement independent of Session execution rules",async()=>{
 const {readFileSync}=await import("node:fs"),{ControlledStorage}=await import("@pinned/support");
 const {records}=JSON.parse(readFileSync(new URL("../../durable/testdata/pi-durable-v150/task-storage-replacements.json",import.meta.url),"utf8"));
 const dir=mkdtempSync(join(tmpdir(),"storage-replacement-probe-")),path=join(dir,"state.db");let sqlite:any;
 try{
  for(const storage of [new ControlledStorage(),await openNodeSqliteStorage(path)]){
   if(!(storage instanceof ControlledStorage))sqlite=storage;
   try{
    expect(await storage.commit([[{type:"conversation",value:{id:1}},{type:"conversation",value:{id:2}},{type:"task",value:{id:3,conversationId:1,kind:"owner",version:1,input:null,background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"ready"}}}}]],()=>{},context)).toEqual({stored:1});
    for(const record of records){expect(await storage.commit([[{type:"task",value:record}]],()=>{},context)).toEqual({stored:1});expect(await storage.task(4,context)).toEqual(record)}
   }finally{await storage.close(context);if(storage===sqlite)sqlite=undefined}
  }
  sqlite=await openNodeSqliteStorage(path);expect(await sqlite.task(4,context)).toEqual(records.at(-1));
 }finally{if(sqlite)await sqlite.close(context);rmSync(dir,{recursive:true,force:true})}
});

it("keeps document access outside the table-write fence and fences reads after a caught failed table write",async()=>{
 const {SessionImpl}=await import("@pinned/session"),{ControlledStorage}=await import("@pinned/support"),{defineDoc}=await import("@earendil-works/pi-durable");
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Doc=defineDoc({kind:"v150.table.read.fence",scope:"session",version:1,initial:()=>({text:"before"}),checkpointWhen:()=>false});
 try{
  await session.commit(async tx=>{
   (await tx.doc(Doc)).text="document write";
   expect(await tx.task(99 as any)).toBeUndefined();expect(await tx.scanTasks({},10)).toEqual({items:[]});
   await expect(tx.appendEntry(12345 as any,{kind:"note"})).rejects.toThrow("Conversation 12345 does not exist");
   await expect(tx.task(99 as any)).rejects.toThrow("Tx.task() cannot read tables after the first table write");
   expect((await tx.doc(Doc)).text).toBe("document write");
  },context);
 }finally{await session.close(context)}
});

it("reads transaction entries globally with kind-only narrowing and the table-write fence",async()=>{
 const {SessionImpl}=await import("@pinned/session"),{ControlledStorage}=await import("@pinned/support"),{defineEntry}=await import("@earendil-works/pi-durable");
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),Note=defineEntry("foreign.note"),Wrong=defineEntry("other.kind");
 try{
  const {conversation,entry}=await session.commit(async tx=>{const conversation=await tx.createConversation({ownership:{kind:"ownerless"}});const entry=await tx.appendEntry(conversation.id,{kind:Note.kind,data:{value:"owned"}});return {conversation,entry}},context);
  await session.commit(async tx=>{
   expect(await tx.conversation(conversation.id)).toEqual(conversation);expect(await tx.entry(entry.id)).toEqual(entry);expect(await tx.entry(Note,entry.id)).toEqual(entry);expect(await tx.entry(Wrong,entry.id)).toBeUndefined();
   await tx.appendEntry(conversation.id,{kind:"note"});
   await expect(tx.entry(Note,entry.id)).rejects.toThrow("Tx.entry() cannot read tables after the first table write");await expect(tx.conversation(conversation.id)).rejects.toThrow("Tx.conversation() cannot read tables after the first table write");
  },context);
 }finally{await session.close(context)}
});

it("accepts non-empty entry kinds without a native identifier grammar",async()=>{
 const {defineEntry}=await import("@earendil-works/pi-durable"),{ControlledStorage}=await import("@pinned/support");
 const dir=mkdtempSync(join(tmpdir(),"entry-kind-probe-"));let sqlite:any;
 try{
  for(const storage of [new ControlledStorage(),await openNodeSqliteStorage(join(dir,"state.db"))]){
   if(!(storage instanceof ControlledStorage))sqlite=storage;
   try{
    expect(await storage.commit([[{type:"conversation",value:{id:1}}]],()=>{},context)).toEqual({stored:1});
    for(const kind of ["Mixed Case: note/v1","ノート/Δ","k".repeat(129)," spaced "]){
     const token=defineEntry(kind),id=await storage.mintId(),entry={id,conversationId:1,kind,data:{value:"owned"}};
     expect(await storage.commit([[{type:"entry",value:entry}]],()=>{},context)).toEqual({stored:1});
     const stored=await storage.entry(id,context);expect(stored?.entry).toEqual(entry);expect(token.is(stored?.entry)).toBe(true);
    }
    expect(()=>defineEntry("")).toThrow("Entry kind must be a non-empty string");
   }finally{await storage.close(context);if(storage===sqlite)sqlite=undefined}
  }
 }finally{if(sqlite)await sqlite.close(context);rmSync(dir,{recursive:true,force:true})}
});

it("replaces raw submission identities and request keys without Session admission",async()=>{
 const {ControlledStorage}=await import("@pinned/support");const dir=mkdtempSync(join(tmpdir(),"submission-replacement-"));let sqlite:any;
 try{for(const storage of [new ControlledStorage(),await openNodeSqliteStorage(join(dir,"state.db"))]){
  if(!(storage instanceof ControlledStorage))sqlite=storage;
  try{
   expect(await storage.commit([[{type:"conversation",value:{id:1}},{type:"conversation",value:{id:2}},{type:"submission",value:{id:3,conversationId:1,requestId:"old",type:"input",status:"queued"}}]],()=>{},context)).toEqual({stored:1});
   const replacement={id:3,conversationId:2,requestId:"",type:"write",status:"unanswered",reason:"",detail:null};
   expect(await storage.commit([[{type:"submission",value:replacement}]],()=>{},context)).toEqual({stored:1});
   expect(await storage.submission(3,context)).toEqual(replacement);expect(await storage.submissionByRequest(1,"old",context)).toBeUndefined();expect(await storage.submissionByRequest(2,"",context)).toEqual(replacement);
  }finally{await storage.close(context);if(storage===sqlite)sqlite=undefined}
 }}finally{if(sqlite)await sqlite.close(context);rmSync(dir,{recursive:true,force:true})}
});

it("uses the non-unique pinned SQLite request index and reindexes complete replacements",async()=>{
 const dir=mkdtempSync(join(tmpdir(),"submission-duplicate-"));let storage:any;
 try{storage=await openNodeSqliteStorage(join(dir,"state.db"));const first={id:2,conversationId:1,requestId:"",type:"write",status:"queued"},second={...first,id:3};
 expect(await storage.commit([[{type:"conversation",value:{id:1}},{type:"submission",value:second},{type:"submission",value:first}]],()=>{},context)).toEqual({stored:1});
 for(let i=0;i<20;i++)expect(await storage.submissionByRequest(1,"",context)).toEqual(first);
 const moved={...first,requestId:"moved"};expect(await storage.commit([[{type:"submission",value:moved}]],()=>{},context)).toEqual({stored:1});expect(await storage.submissionByRequest(1,"",context)).toEqual(second);expect(await storage.submissionByRequest(1,"moved",context)).toEqual(moved);
 }finally{if(storage)await storage.close(context);rmSync(dir,{recursive:true,force:true})}
});

it("creates raw duplicate submissions without applying Harness admission rules",async()=>{
 const {SessionImpl}=await import("@pinned/session"),{ControlledStorage}=await import("@pinned/support");const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 try{const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const records=await session.commit(async tx=>[await tx.createSubmission({conversationId:conversation.id,requestId:"same",type:"write",status:"queued"}),await tx.createSubmission({conversationId:conversation.id,requestId:"same",type:"write",status:"queued"})],context);expect(records[0].id).not.toBe(records[1].id);
 }finally{await session.close(context)}
});

it("reads submission requests from committed tables and applies the read-after-write fence",async()=>{
 const {SessionImpl}=await import("@pinned/session"),{ControlledStorage}=await import("@pinned/support");const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 try{const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const record=await session.commit(tx=>tx.createSubmission({conversationId:conversation.id,requestId:"",type:"continue",status:"placed"}),context);
 await session.commit(async tx=>{expect(await tx.submission(record.id)).toEqual(record);expect(await tx.submissionByRequest(conversation.id,"")).toEqual(record);expect(await tx.submissionByRequest(999 as any,"")).toBeUndefined();
 await tx.appendEntry(conversation.id,{kind:"note"});await expect(tx.submission(record.id)).rejects.toThrow("Tx.submission() cannot read tables after the first table write");await expect(tx.submissionByRequest(conversation.id,"")).rejects.toThrow("Tx.submissionByRequest() cannot read tables after the first table write");},context);
 }finally{await session.close(context)}
});
