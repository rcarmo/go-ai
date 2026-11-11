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
const selected=["replaces complete task records and pages filtered task scans","stores owners and scans waiting and completing tasks by status","stores and scans sleeping tasks by status"];
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
