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

it("shares snapshot, synchronous observer and delayed watch revisions without advancing an unstarted watch",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Doc=defineDoc({kind:"watch.state",scope:"session",version:1,initial:()=>({value:0,items:["a","b"],retained:{label:"stable"}}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 const initial=await session.snapshot(Doc,context);
 const one=(await session.watchDoc(Doc,context))!,two=(await session.watchDoc(Doc,context))!;
 expect(one.value).toBe(initial);expect(two.value).toBe(initial);
 let observed:any;
 const observation=(await session.observeDoc(Doc,{advance:call=>{if(call.kind==="value")observed=call.value},end:()=>{}},context))!;
 for(const value of [1,2]) await session.commit(async tx=>{(await tx.doc(Doc)).value=value},context);
 const latest=await session.snapshot(Doc,context);
 expect(latest).toBe(observed);expect(one.value).toBe(initial);expect(two.value).toBe(initial);
 const deliveries=[one,two].map(watch=>new Promise<void>(resolve=>watch.start(async call=>{
  if(call.kind!=="value")return;
  expect(call.value).toBe(latest);expect(watch.value).toBe(call.value);
  expect(call.value.retained).toBe(initial!.retained);
  expect(call.ops).toEqual([["s",["value"],2]]);resolve();
 })));
 await Promise.all(deliveries);expect(initial!.value).toBe(0);
 await one.stop();await two.stop();observation.detach();await session.close(context);
});

it("shares attached document revisions and retained children with zero idle retention",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>0);
 const Doc=defineDoc({kind:"watch.state",scope:"session",version:1,initial:()=>({value:0,items:["a","b"],retained:{label:"stable"}}),checkpointWhen:()=>false});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 const one=(await session.watchDoc(Doc,context))!,initial=one.value;
 const two=(await session.watchDoc(Doc,context))!;
 expect(two.value).toBe(initial);expect(await session.snapshot(Doc,context)).toBe(initial);
 let observed:any;
 const observation=(await session.observeDoc(Doc,{advance:call=>{if(call.kind==="value")observed=call.value},end:()=>{}},context))!;
 expect(observation.value).toBe(initial);
 for(const value of [1,2])await session.commit(async tx=>{(await tx.doc(Doc)).value=value},context);
 const latest=await session.snapshot(Doc,context);expect(latest).toBe(observed);
 expect(one.value).toBe(initial);expect(two.value).toBe(initial);
 await Promise.all([one,two].map(watch=>new Promise<void>(resolve=>watch.start(async call=>{if(call.kind!=="value")return;expect(call.value).toBe(latest);expect(watch.value).toBe(latest);expect(call.value.retained).toBe(initial!.retained);expect(call.ops).toEqual([["s",["value"],2]]);resolve()}))));
 expect(initial!.value).toBe(0);await one.stop();await two.stop();observation.detach();await session.close(context);
});

it("hydrates a mounted builtin from the loaded tracker but advances its changed branch independently",async()=>{
 const {AgentDoc,MemoryStorage}=await import("@earendil-works/pi-durable");
 const {openChat,chatSetup}=await import("@pinned/chat");
 const {harness,root}=await openChat(new MemoryStorage(),chatSetup());
 const before=await harness.snapshot(AgentDoc,root.id,context);
 const watch=await root.watch(context);expect(watch.value.docs["pi.agent"]).toBe(before);
 let mounted:any;
 const observation=await harness.observeConversation(root.id,{advance:call=>{if(call.kind==="value")mounted=call.value},end:()=>{}},context);
 expect(observation.value.docs).toBe(watch.value.docs);
 await root.configure({thinkingLevel:"high"},context);
 const after=await harness.snapshot(AgentDoc,root.id,context);
 expect(after!.model).toBe(before!.model);
 expect(mounted.docs["pi.agent"]).toEqual(after);
 expect(mounted.docs["pi.agent"]).not.toBe(after);
 expect(mounted.docs["pi.live"]).toBe(watch.value.docs["pi.live"]);
 observation.detach();await watch.stop();await harness.close(context);
});

it("retains Error cancellation identity at document attachment but ignores cancellation on a warm snapshot",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Doc=defineDoc({kind:"oracle.cancel.identity",scope:"session",version:1,initial:()=>({count:0}),checkpointWhen:()=>false});
 await session.commit(async tx=>{(await tx.doc(Doc)).count=1},context);
 const before=await session.snapshot(Doc,context);
 const controller=new AbortController(),gone=new Error("gone");controller.abort(gone);
 const cancelled={...context,abortSignal:controller.signal};
 await expect(session.observeDoc(Doc,{advance:()=>{},end:()=>{}},cancelled)).rejects.toBe(gone);
 await expect(session.watchDoc(Doc,cancelled)).rejects.toBe(gone);
 expect(session.documentCacheSizes.attached).toBe(0);
 expect(await session.snapshot(Doc,cancelled)).toBe(before);
 await session.close(context);
});

it("hydrates cold builtin trackers for a mount without creating documents or storing migrations",async()=>{
 const {AgentDoc,LiveDoc,InboxDoc,ProviderDoc,UsageDoc,MemoryStorage}=await import("@earendil-works/pi-durable");
 const {openChat,chatSetup}=await import("@pinned/chat");
 const {harness,root}=await openChat(new MemoryStorage(),chatSetup());
 await (harness as any).unloadDocuments();
 const watch=await root.watch(context);
 for(const token of [AgentDoc,LiveDoc,InboxDoc,ProviderDoc,UsageDoc]){
  const value=await harness.snapshot(token,root.id,context);
  expect(watch.value.docs[token.definition.kind]).toBe(value);
 }
 await watch.stop();await harness.close(context);
});

it("hydrates a legacy live mount with the pinned version3 migration without storing it",async()=>{
 const {LiveDoc}=await import("@earendil-works/pi-durable");const {ConversationViews}=await import("@pinned/views");
 for(const version of [1,2]){
  const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
  const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
  const old=defineDoc({kind:"pi.live",version,scope:"conversation",history:"latest",fork:"initial",initial:()=>({run:{taskId:7,inputs:[],turns:version===1?1e30:"old"},compactions:[{taskId:9,reason:"manual",attempt:1}],compactionFailure:{taskId:9,reason:"threshold",error:"old"}}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(old,conversation.id)},context);await session.unloadDocuments();
  const stored=storage.commits.length;
  const watch=await new ConversationViews(session).watch(conversation.id,context);
  const expected={run:{taskId:7,inputs:[],turns:version===1?1e30:0},compactions:[{taskId:9,trigger:"manual",attempt:1}],compactionFailure:{taskId:9,trigger:"threshold",error:"old"}};
  expect(watch.value.docs["pi.live"]).toEqual(expected);
  expect(await session.snapshot(LiveDoc,conversation.id,context)).toBe(watch.value.docs["pi.live"]);
  expect(storage.commits).toHaveLength(stored);
  await session.commit(async tx=>{await tx.doc(LiveDoc,conversation.id)},context);
  expect(storage.commits).toHaveLength(stored+1);
  expect(await session.snapshot(LiveDoc,conversation.id,context)).toBe(watch.value.docs["pi.live"]);
  await watch.stop();await session.close(context);
 }
});

it("initializes the builtin live/inbox/usage documents idempotently at their pinned versions",async()=>{
 const {LiveDoc,InboxDoc,UsageDoc}=await import("@earendil-works/pi-durable");
 const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
 const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const initialize=()=>session.commit(async tx=>{for(const token of [LiveDoc,InboxDoc,UsageDoc])await tx.doc(token,conversation.id)},context);
 await initialize();const count=storage.commits.length;
 await initialize();expect(storage.commits).toHaveLength(count);
 expect(await session.snapshot(LiveDoc,conversation.id,context)).toEqual({});
 await session.close(context);
});
