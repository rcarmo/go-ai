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

it("serves a cached snapshot without admission, cancellation or an unrelated outcome fence",async()=>{
 const entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>();
 class Held extends ControlledStorage {
  held=false;
  override async commit(batches:any,receive:any,ctx:any){
   if(this.held){this.held=false;entered.resolve();await release.promise}
   return super.commit(batches,receive,ctx);
  }
 }
 const storage=new Held();const session=new SessionImpl(storage,Date.now,()=>600000);
 const Doc=defineDoc({kind:"oracle.cached",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 storage.held=true;
 const writing=session.commit(async tx=>{(await tx.doc(Doc)).n=1},context);
 await entered.promise;
 const cancellation=new AbortController();cancellation.abort();
 const value=await Promise.race([session.snapshot(Doc,{...context,abortSignal:cancellation.signal}),new Promise((_,reject)=>setTimeout(()=>reject(new Error("cached read waited")),1000))]).finally(()=>release.resolve());
 expect(value).toEqual({n:0});
 await writing;
 expect(await session.snapshot(Doc,context)).toEqual({n:1});
 await session.close(context);
});

it("shares one materialized version per address rather than per definition-token identity",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const V1=defineDoc({kind:"oracle.token",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(V1)},context);
 let first=0,second=0;
 const A=defineDoc({kind:"oracle.token",scope:"session",version:2,initial:()=>({n:0}),migrate:()=>{first++;return {n:10}},checkpointWhen:()=>false});
 const B=defineDoc({kind:"oracle.token",scope:"session",version:2,initial:()=>({n:0}),migrate:()=>{second++;return {n:99}},checkpointWhen:()=>false});
 expect(await session.snapshot(A,context)).toEqual({n:10});
 expect(await session.snapshot(B,context)).toEqual({n:10});
 expect([first,second]).toEqual([1,0]);
 await session.close(context);
});

it("shares the adopted document revision between snapshots and synchronous observers",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Doc=defineDoc({kind:"oracle.readonly.snapshot",scope:"session",version:1,initial:()=>({n:0,keep:{text:"original"}}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 const first=await session.snapshot(Doc,context);
 expect(await session.snapshot(Doc,context)).toBe(first);
 const values:any[]=[];
 const observed=await session.observeDoc(Doc,{advance:call=>{if(call.kind==="value")values.push(call.value)},end:()=>{}},context);
 expect(observed!.value).toBe(first);
 await session.commit(async tx=>{(await tx.doc(Doc)).n=1},context);
 const next=await session.snapshot(Doc,context);
 expect(values).toHaveLength(1);
 expect(next).toBe(values[0]);
 expect(next).not.toBe(first);
 expect(next!.keep).toBe(first!.keep);
 expect(first!.n).toBe(0);
 observed!.detach();await session.close(context);
});

it("retains a materialized snapshot identity when its required migration base is stored without edits",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Old=defineDoc({kind:"oracle.readonly.migration",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Old)},context);
 const Next=defineDoc({kind:"oracle.readonly.migration",scope:"session",version:2,initial:()=>({n:0}),migrate:value=>({...value,n:10}),checkpointWhen:()=>false});
 const before=await session.snapshot(Next,context);
 await session.commit(async tx=>{await tx.doc(Next)},context);
 expect(await session.snapshot(Next,context)).toBe(before);
 await session.close(context);
});

it("keeps one held snapshot revision even with zero idle document retention",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>0);
 const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const Doc=defineDoc({kind:"oracle.readonly.held",scope:"conversation",history:"latest",fork:"initial",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc,conversation.id)},context);
 await session.commit(async tx=>{(await tx.doc(Doc,conversation.id)).n=1},context,{persistence:{kind:"deferred",withinMs:Infinity}});
 const before=await session.snapshot(Doc,conversation.id,context);
 expect(await session.snapshot(Doc,conversation.id,context)).toBe(before);
 expect(before).toEqual({n:1});
 await session.unloadDocuments();await session.close(context);
});

it("stores a held document without replacing its already-adopted snapshot revision",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const Doc=defineDoc({kind:"oracle.readonly.persisted",scope:"conversation",history:"latest",fork:"initial",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc,conversation.id)},context);
 await session.commit(async tx=>{(await tx.doc(Doc,conversation.id)).n=1},context,{persistence:{kind:"deferred",withinMs:Infinity}});
 const before=await session.snapshot(Doc,conversation.id,context);
 await session.commit(async tx=>{await tx.doc(Doc,conversation.id);await tx.appendEntry(conversation.id,{kind:"note"})},context);
 expect(await session.snapshot(Doc,conversation.id,context)).toBe(before);
 await session.close(context);
});
