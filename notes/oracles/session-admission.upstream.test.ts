import { expect, it } from "vitest";
import { defineDoc } from "@earendil-works/pi-durable";
import { SessionImpl } from "@pinned/session";
import { ControlledStorage, context } from "@pinned/support";
import { eventually } from "@pinned/tasks";

it("admits blind appends referencing an earlier unpublished head in [1,2] calls",async()=>{
 const entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>();
 class Held extends ControlledStorage {
  calls:number[]=[];held=false;
  override async commit(batches:any,receive:any,ctx:any){
   this.calls.push(batches.length);
   if(this.held){this.held=false;entered.resolve();await release.promise}
   return super.commit(batches,receive,ctx);
  }
 }
 const storage=new Held();const session=new SessionImpl(storage,Date.now,()=>600000);
 const conv=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const start=storage.calls.length;
 storage.held=true;
 let head:any;
 const a=session.commit(async tx=>{head=await tx.appendEntry(conv.id,{kind:"head"})},context);
 await entered.promise;
 let ran=0;
 const b=session.commit(async tx=>{const e=await tx.appendEntry(conv.id,{kind:"tail",head:head.id});ran++;return e},context);
 const c=session.commit(async tx=>{const e=await tx.appendEntry(conv.id,{kind:"tail",head:head.id});ran++;return e},context);
 try{await eventually(()=>ran===2)}finally{release.resolve()}
 const [,second,third]=await Promise.all([a,b,c]);
 expect(storage.calls.slice(start)).toEqual([1,2]);
 const storedB=await storage.entry(second.id,context),storedC=await storage.entry(third.id,context);
 expect(storedB?.entry.head).toBe(head.id);expect(storedC?.entry.head).toBe(head.id);
 expect(storedC!.commitSeq).toBe(storedB!.commitSeq+1);
 await session.close(context);
});

it("rejects a missing conversation before the checkpoint predicate and keeps the Session healthy",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 let predicates=0;
 const Doc=defineDoc({kind:"oracle.predicate",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>{predicates++;return false}});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 await expect(session.commit(async tx=>{(await tx.doc(Doc)).n=1;await tx.appendEntry(999999 as any,{kind:"missing"})},context)).rejects.toThrow(/does not exist/);
 expect(predicates).toBe(0);
 expect((await session.snapshot(Doc,context))?.n).toBe(0);
 await session.commit(()=>{},context);
 await session.close(context);
});

it("rejects a missing document scope before its initializer and leaves the Session usable",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 let initializers=0;
 const Doc=defineDoc({kind:"oracle.scope",scope:"conversation",version:1,initial:()=>{initializers++;return {n:0}},checkpointWhen:()=>false});
 await expect(session.commit(async tx=>{await tx.doc(Doc,999999 as any)},context)).rejects.toThrow(/does not exist/);
 expect(initializers).toBe(0);
 await session.commit(()=>{},context);
 await session.close(context);
});
