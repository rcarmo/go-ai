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

it("dispatches canonical stored changes after own observers and selects host membership at dispatch",async()=>{
 const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
 const Doc=defineDoc({kind:"oracle.commit.public",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 const order:string[]=[],seen:any[]=[];let late:any;
 const first=session.observeCommits({advance:()=>order.push("first"),end:()=>{}});
 const unfollow=session.followCommits(()=>{order.push("own");first.detach();late??=session.observeCommits({advance:p=>{order.push("late");seen.push(p)},end:()=>order.push("end")})});
 await session.commit(async tx=>{await tx.doc(Doc)},context);
 expect(order).toEqual(["own","late"]);expect(seen).toHaveLength(1);
 const change=seen[0].changes[0];expect(change).toEqual({type:"document",kind:"change",record:{id:expect.any(Number),kind:"oracle.commit.public",createdAt:seen[0].seq,scope:{kind:"session"}},conversationId:undefined,version:1,value:{n:0},ops:[]});
 expect(change.value).toBe(await session.snapshot(Doc,context));
 unfollow();late.detach();await session.close(context);expect(order).toEqual(["own","late"]);
});

it("selects stored observers after a synchronous storage scope and never queues a publication with no hosts",async()=>{
 for(const interested of [false,true]){
  const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
  const order:string[]=[];let first:any,late:any;
  if(interested)first=session.observeCommits({advance:()=>order.push("first"),end:()=>{}});
  (storage as any).publishing=(publish:()=>void)=>{order.push("scope-open");publish();order.push("scope-close");first?.detach();late=session.observeCommits({advance:()=>order.push("late"),end:()=>{}})};
  const Doc=defineDoc({kind:"oracle.scope.members",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(Doc)},context);
  expect(order).toEqual(interested?["scope-open","scope-close","late"]:["scope-open","scope-close"]);
  late.detach();await session.close(context);
 }
});

it("dispatches after a deferred storage scope's synchronous turn has finished",async()=>{
 const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
 const entered=Promise.withResolvers<()=>void>(),order:string[]=[];let late:any;
 const first=session.observeCommits({advance:()=>order.push("first"),end:()=>{}});
 (storage as any).publishing=(publish:()=>void)=>entered.resolve(()=>{order.push("open");publish();order.push("close");first.detach();late=session.observeCommits({advance:()=>order.push("late"),end:()=>{}})});
 const Doc=defineDoc({kind:"oracle.scope.deferred",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 const writing=session.commit(async tx=>{await tx.doc(Doc)},context);
 const finish=await entered.promise;expect(order).toEqual([]);finish();await writing;
 expect(order).toEqual(["open","close","late"]);late.detach();await session.close(context);
});

it("publishes canonical conversation, entry, task and submission records together",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),seen:any[]=[];
 const Work=defineTask({name:"oracle.public.task",version:1,initial:()=>({phase:"run"}),phases:{run:async()=>{}},abort:async()=>{}});
 const guard=session.observeCommits({advance:p=>seen.push(p),end:()=>{}});
 const ids=await session.commit(async tx=>{
  const conversation=await tx.createConversation({ownership:{kind:"ownerless"}});
  const entry=await tx.appendEntry(conversation.id,{kind:"note",data:{n:1}});
  const task=await tx.createTask(Work,null,{conversationId:conversation.id,ownership:{kind:"conversation"}});
  const submission=await tx.createSubmission({conversationId:conversation.id,type:"write",status:"queued",requestId:""});
  return {conversation:conversation.id,entry:entry.id,task,submission:submission.id};
 },context);
 expect(seen).toHaveLength(1);const byType=Object.fromEntries(seen[0].changes.map((change:any)=>[change.type,change.value]));
 expect(byType).toEqual({conversation:{id:ids.conversation},entry:{id:ids.entry,conversationId:ids.conversation,kind:"note",data:{n:1}},task:{id:ids.task,conversationId:ids.conversation,kind:"oracle.public.task",version:1,input:null,background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"run"}}},submission:{id:ids.submission,conversationId:ids.conversation,type:"write",status:"queued",requestId:""}});
 guard.detach();await session.close(context);
});

it("publishes definition-free document copies and retirement metadata without native payloads",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const Doc=defineDoc({kind:"oracle.public.copy",scope:"conversation",history:"rewindable",fork:"asOf",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
 const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const entry=await session.commit(async tx=>{await tx.doc(Doc,parent.id);return tx.appendEntry(parent.id,{kind:"note"})},context);
 const seen:any[]=[];const guard=session.observeCommits({advance:p=>seen.push(p),end:()=>{}});
 const child=await session.commit(tx=>tx.forkConversation(parent.id,entry.id,{ownership:{kind:"ownerless"}}),context);
 const copy=seen[0].changes.find((c:any)=>c.type==="document.copy");expect(copy).toBeDefined();expect(Object.keys(copy).sort()).toEqual(["conversationId","record","source","type"]);expect(copy.conversationId).toBe(child.id);expect(copy.source.at).toBe(seen[0].seq-1);
 await session.commit(tx=>tx.retireDoc(Doc,parent.id),context);
 const retired=seen[1].changes.find((c:any)=>c.type==="document");expect(Object.keys(retired).sort()).toEqual(["conversationId","kind","record","type"]);expect(retired.kind).toBe("retire");expect(retired.record.retiredAt).toBe(seen[1].seq);
 guard.detach();await session.close(context);
});

it("publishes held document changes only when stored, reusing the adopted value before its close ending",async()=>{
 for(const finish of ["read","unload","close"]){
  const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
  const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
  const Doc=defineDoc({kind:"oracle.public.held",scope:"conversation",history:"latest",fork:"initial",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(Doc,conversation.id)},context);
  const seen:any[]=[],order:string[]=[];const guard=session.observeCommits({advance:p=>{seen.push(p);order.push("stored")},end:()=>order.push("end")});
  await session.commit(async tx=>{(await tx.doc(Doc,conversation.id)).n=1},context,{persistence:{kind:"deferred",withinMs:Infinity}});
  const value=await session.snapshot(Doc,conversation.id,context);expect(seen).toHaveLength(0);
  if(finish==="read")await session.commit(async tx=>{await tx.doc(Doc,conversation.id);await tx.appendEntry(conversation.id,{kind:"note"})},context);else if(finish==="unload")await session.unloadDocuments();else await session.close(context);
  expect(seen,finish).toHaveLength(1);const change=seen[0].changes.find((c:any)=>c.type==="document");expect(change.value).toBe(value);expect(change.ops).toEqual([["s",["n"],1]]);
  expect(order).toEqual(finish==="close"?["stored","end"]:["stored"]);
  guard.detach();await session.close(context);
 }
});

it("dispatches a terminal stored task before the task waiter continuation",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");const {openTasks,completed}=await import("@pinned/tasks");
 const entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>(),order:string[]=[];
 const Work=defineTask({name:"oracle.public.wait",version:1,initial:()=>({phase:"run"}),phases:{run:async(_task:any,runtime:any,ctx:any)=>{entered.resolve();await release.promise;await runtime.commit(()=>completed(null),ctx)}},abort:async()=>{}});
 const {harness}=await openTasks(new ControlledStorage(),[Work]);const root=await harness.root(context);
 const id=await root.commit(tx=>tx.createTask(Work,null,{ownership:{kind:"conversation"}}),context);
 const guard=harness.observeCommits({advance:p=>{if(p.changes.some(c=>c.type==="task"&&c.value.id===id&&c.value.state.status==="terminal"))order.push("stored")},end:()=>{}});
 const waiting=harness.waitForTask(id,context).then(()=>order.push("wait"));await entered.promise;release.resolve();await waiting;
 expect(order).toEqual(["stored","wait"]);guard.detach();await harness.close(context);
});

it("dispatches the stored abort request before the invocation's abort signal",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");const {openTasks,aborted}=await import("@pinned/tasks");
 const entered=Promise.withResolvers<void>(),stopped=Promise.withResolvers<void>(),order:string[]=[];
 const Work=defineTask({name:"oracle.public.abort",version:1,initial:()=>({phase:"run"}),phases:{run:async(_task:any,_runtime:any,ctx:any)=>{entered.resolve();await new Promise<void>(resolve=>ctx.abortSignal.addEventListener("abort",()=>{order.push("signal");stopped.resolve();resolve()},{once:true}))}},abort:async(_task:any,runtime:any,ctx:any)=>{await runtime.commit(()=>aborted("test"),ctx)}});
 const {harness}=await openTasks(new ControlledStorage(),[Work]);const root=await harness.root(context);const id=await root.commit(tx=>tx.createTask(Work,null,{ownership:{kind:"conversation"}}),context);
 const guard=harness.observeCommits({advance:p=>{if(p.changes.some(c=>c.type==="task"&&c.value.id===id&&c.value.abortRequested))order.push("stored-abort")},end:()=>{}});
 harness.resume();await entered.promise;await harness.abortTask(id,context);await stopped.promise;
 expect(order.indexOf("stored-abort")).toBeGreaterThanOrEqual(0);expect(order.indexOf("signal")).toBeGreaterThan(order.indexOf("stored-abort"));guard.detach();await harness.close(context);
});

it("reports original falsy stored-observer throws and normalizes their listener_error ending",async()=>{
 for(const reason of [false,0,"",null]){
  const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),reports:any[]=[],ends:any[]=[];let called=0;
  session.report=(error:any,info:any)=>reports.push({error,info});
  const guard=session.observeCommits({advance:()=>{called++;throw reason},end:end=>ends.push(end)});
  const Doc=defineDoc({kind:"oracle.public.throw",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(Doc)},context);await session.commit(async tx=>{(await tx.doc(Doc)).n=1},context);
  expect(called).toBe(1);expect(reports).toHaveLength(1);expect(reports[0].error).toBe(reason);expect(ends).toEqual([{kind:"listener_error",error:new Error(String(reason))}]);guard.detach();await session.close(context);
 }
});

it("retains falsy throws from stored-observer ending callbacks in reports",async()=>{
 for(const reason of [false,0,"",null]){
  const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),reports:any[]=[],ends:any[]=[];
  session.report=(error:any,info:any)=>reports.push({error,info});
  session.observeCommits({advance:()=>{},end:end=>{ends.push(end);throw reason}});
  await session.close(context);
  expect(ends).toEqual([{kind:"failed",error:new Error("session closed")}]);expect(reports).toHaveLength(1);expect(reports[0].error).toBe(reason);
 }
});

it("honours reentrant stored-observer detach during advance/report and snapshots dispatch membership",async()=>{
 for(const mode of ["self-detach","report-detach","replace-peer"]){
  const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),calls:string[]=[],ends:string[]=[],reports:any[]=[];const bug=new Error("observer fault");let late:any;
  session.report=(error:any)=>{reports.push(error);if(mode==="report-detach")first.detach()};
  const first=session.observeCommits({advance:()=>{calls.push("first");if(mode==="replace-peer"){if(!late){peer.detach();late=session.observeCommits({advance:()=>calls.push("late"),end:()=>ends.push("late")})}}else{if(mode==="self-detach")first.detach();throw bug}},end:()=>ends.push("first")});
  const peer=session.observeCommits({advance:()=>calls.push("peer"),end:()=>ends.push("peer")});
  const Doc=defineDoc({kind:"oracle.public.reentrant",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(Doc)},context);await session.commit(async tx=>{(await tx.doc(Doc)).n=1},context);
  expect(calls).toEqual(mode==="replace-peer"?["first","first","late"]:["first","peer","peer"]);
  expect(reports).toEqual(mode==="replace-peer"?[]:[bug]);expect(ends).toEqual([]);
  await session.close(context);expect(ends).toEqual(mode==="replace-peer"?["first","late"]:["peer"]);
 }
});

it("reports object and numeric throws by identity while normalizing listener_error messages",async()=>{
 const cycle:any[]=[];cycle.push(cycle);
 const indirectA:any[]=[],indirectB:any[]=[];indirectA.push(indirectB,1);indirectB.push(indirectA,2);
 for(const [reason,message] of [
  [{n:1},"[object Object]"],[[],""],[[null,false,{n:1},[1,2]],",false,[object Object],1,2"],
  [-0,"0"],[1e-7,"1e-7"],[1e20,"100000000000000000000"],[1e21,"1e+21"],[NaN,"NaN"],[Infinity,"Infinity"],[-Infinity,"-Infinity"],
  [cycle,""],[indirectA,",2,1"],[[[],[1,2],[1,2]],",1,2,1,2"],[10n**30n,"1000000000000000000000000000000"],[[new Error("nested error")],"Error: nested error"],[{toString:()=>"custom"},"custom"],
  [{toString:()=>{throw new Error("stringification failed")}},"[object Object]"],
  [[{toString:()=>{throw null}}],"[object Array]"],
 ] as const){
  const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000),reports:any[]=[],ends:any[]=[];
  session.report=(error:any)=>reports.push(error);
  session.observeCommits({advance:()=>{throw reason},end:end=>ends.push(end)});
  const Doc=defineDoc({kind:"oracle.public.throw.object",scope:"session",version:1,initial:()=>({n:0}),checkpointWhen:()=>false});
  await session.commit(async tx=>{await tx.doc(Doc)},context);
  expect(reports).toHaveLength(1);expect(reports[0]).toBe(reason);expect(ends).toHaveLength(1);expect(ends[0].kind).toBe("listener_error");expect(ends[0].error.message).toBe(message);
  await session.close(context);expect(ends).toHaveLength(1);
 }
});

it("publishes the canonical builtin compaction kind and initial version/checkpoint",async()=>{
 const {openChat,chatSetup}=await import("@pinned/chat");
 const {harness,root}=await openChat(new ControlledStorage(),chatSetup());const tasks:any[]=[],ends:any[]=[];
 const guard=harness.observeCommits({advance:p=>{for(const change of p.changes)if(change.type==="task")tasks.push(change.value)},end:end=>ends.push(end)});
 const id=await root.compact(undefined,context);await harness.waitForTask(id,context);await harness.close(context);
 const first=tasks.find(task=>task.id===id);expect(first).toBeDefined();expect(first.kind).toBe("pi.compaction");expect(first.version).toBe(1);expect(first.input.reason).toBe("manual");expect(first.state).toEqual({status:"pending",checkpoint:{phase:"select"}});
 expect(ends).toHaveLength(1);expect(ends[0].kind).toBe("failed");guard.detach();
});

it("runs an application task whose kind uses the pi namespace",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable"),{openTasks,completed}=await import("@pinned/tasks");
 const Work=defineTask({name:"pi.example",version:1,initial:()=>({phase:"work"}),phases:{work:async(_task:any,runtime:any,ctx:any)=>{await runtime.commit(()=>completed("done"),ctx)}},abort:async()=>{}});
 const {harness}=await openTasks(new ControlledStorage(),[Work]);const root=await harness.root(context);
 const id=await root.commit(tx=>tx.createTask(Work,null,{ownership:{kind:"conversation"}}),context);
 const record=await harness.waitForTask(id,context);expect(record.state.outcome).toEqual({status:"completed",result:"done"});expect(record.kind).toBe("pi.example");await harness.close(context);
});

it("accepts raw application and compaction records in the pi namespace",async()=>{
 const storage=new ControlledStorage(),receipts:any[]=[];
 const records=[{id:2,conversationId:1,kind:"pi.example",version:1,input:{reason:"manual"},background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"select"}}},{id:3,conversationId:1,kind:"pi.compaction",version:1,input:{reason:"manual"},background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"select"}}}];
 expect(await storage.commit([[{type:"conversation",value:{id:1}}],records.map(value=>({type:"task",value}))] as any,(index,seq)=>receipts.push([index,seq]),context)).toEqual({stored:2});
 expect(receipts).toEqual([[0,1],[1,2]]);for(const record of records)expect(await storage.task(record.id as any,context)).toEqual(record);await storage.close(context);
});

it("publishes source generation v2 and model-tool v3 records without ending the observer",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{tool,addTool,assistant}=await import("@pinned/harness-support");
 const setup=chatSetup();addTool(setup.registry,tool("tool-c1"));setup.faux.setResponses([assistant("",{calls:["c1"]}),assistant("done")]);
 const {harness,root}=await openChat(new ControlledStorage(),setup);const changes:any[]=[],ends:any[]=[];
 const guard=harness.observeCommits({advance:publication=>changes.push(...publication.changes),end:end=>ends.push(end)});
 expect((await (await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");
 const tasks=changes.filter(change=>change.type==="task").map(change=>change.value),generation=tasks.find(task=>task.kind==="pi.generation"),work=tasks.find(task=>task.kind==="pi.tool");
 const request=changes.find(change=>change.type==="entry"&&change.value.kind==="pi.assistant"&&change.value.model?.[0]?.content.some((item:any)=>item.type==="toolCall"&&item.id==="c1"))?.value;
 expect(generation).toBeDefined();expect(generation.version).toBe(2);expect(generation.input).toEqual({});expect(generation.state).toEqual({status:"pending",checkpoint:{phase:"prepare",attempt:1}});
 expect(request).toBeDefined();expect(work).toBeDefined();expect(work.version).toBe(3);expect(work.input).toEqual({kind:"model",assistant:request.id,callId:"c1"});expect(work.state).toEqual({status:"pending",checkpoint:{phase:"call"}});
 expect(ends).toEqual([]);await harness.close(context);expect(ends).toHaveLength(1);expect(ends[0].kind).toBe("failed");guard.detach();
});

it("retains exact source builtin state progression for parallel and sequential model tools",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{tool,addTool,assistant}=await import("@pinned/harness-support");
 for(const mode of ["parallel","sequential"] as const){
  const setup=chatSetup();setup.settings.toolExecution=mode;addTool(setup.registry,tool("tool-c1"));addTool(setup.registry,tool("tool-c2"));setup.faux.setResponses([assistant("",{calls:["c1","c2"]}),assistant("done")]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);const tasks:any[]=[],entries:any[]=[],ends:any[]=[];
  harness.observeCommits({advance:p=>{for(const change of p.changes){if(change.type==="task")tasks.push(change.value);if(change.type==="entry")entries.push(change.value)}},end:end=>ends.push(end)});
  try{
   expect((await (await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(ends).toEqual([]);
   const ids=(kind:string)=>[...new Set(tasks.filter(task=>task.kind===kind).map(task=>task.id))];const generations=ids("pi.generation"),tools=ids("pi.tool");
   const entryIds=(kind:string)=>entries.filter(entry=>entry.kind===kind).map(entry=>entry.id);const [system]=entryIds("pi.system"),[answer,final]=entryIds("pi.assistant"),results=entryIds("pi.tool-result");
   expect(generations).toHaveLength(2);expect(tools).toHaveLength(2);expect(results).toHaveLength(2);
   const prepare=(status:string)=>({version:2,input:{},state:{status,checkpoint:{phase:"prepare",attempt:1}}});
   const request=(cutoff:any)=>({version:2,input:{},state:{status:"running",checkpoint:{phase:"request",attempt:1,model:{provider:"faux",modelId:"faux-1"},thinkingLevel:"off",streamOptions:{},cutoff}}});
   const done=(entryId:any)=>({version:2,input:{},state:{status:"terminal",outcome:{status:"completed",result:{entryId}}}});
   const round=(status:string,ids:any[],pending:string[],on?:any[])=>({version:2,input:{},state:{status,checkpoint:{phase:"tools",assistant:answer,tools:ids,pending},...(on===undefined?{}:{on,policy:"allSettled"})}});
   const states=(id:any)=>{const unique:any[]=[];for(const task of tasks.filter(task=>task.id===id)){const value={version:task.version,input:task.input,state:task.state};if(JSON.stringify(unique.at(-1))!==JSON.stringify(value))unique.push(value)}return unique};
   const first=mode==="parallel"?[prepare("pending"),prepare("running"),request(system),round("waiting",tools,[],tools),round("running",tools,[]),done(answer)]:[prepare("pending"),prepare("running"),request(system),round("waiting",tools.slice(0,1),["c2"],tools.slice(0,1)),round("running",tools.slice(0,1),["c2"]),round("waiting",tools,[],tools.slice(1)),round("running",tools,[]),done(answer)];
   expect(states(generations[0])).toEqual(first);expect(states(generations[1])).toEqual([prepare("pending"),prepare("running"),request(results[1]),done(final)]);
   tools.forEach((id,i)=>{const input={kind:"model",assistant:answer,callId:`c${i+1}`};expect(states(id)).toEqual([
    {version:3,input,state:{status:"pending",checkpoint:{phase:"call"}}},{version:3,input,state:{status:"running",checkpoint:{phase:"call"}}},
    {version:3,input,state:{status:"running",checkpoint:{phase:"execute",arguments:{},replay:"unsafe"}}},{version:3,input,state:{status:"terminal",outcome:{status:"completed",result:{kind:"model",entryId:results[i]}}}},
   ])});
  }finally{await harness.close(context)}
  expect(ends).toHaveLength(1);expect(ends[0].kind).toBe("failed");
 }
});

it("resends an orderly closed SQLite request with the same attempt and cutoff",async()=>{
 const {mkdtempSync,rmSync}=await import("node:fs"),{tmpdir}=await import("node:os"),{join}=await import("node:path");
 const {openNodeSqliteStorage}=await import("@pinned/sqlite-node"),{chatSetup,openChat,unanswered}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 const dir=mkdtempSync(join(tmpdir(),"source-clean-resend-")),path=join(dir,"state.db"),setup=chatSetup(),blocked=unanswered();let calls=0,harness:any;
 setup.faux.setResponses([(ctx:any,options:any)=>{calls++;return blocked.step(ctx,options)},()=>{calls++;return assistant("done")}]);
 const requests:any[]=[],ends:any[]=[];
 const observe=(h:any)=>h.observeCommits({advance:(p:any)=>{for(const change of p.changes)if(change.type==="task"&&change.value.kind==="pi.generation"&&change.value.state.checkpoint?.phase==="request")requests.push(change.value)},end:(end:any)=>ends.push(end)});
 try{
  let opened=await openChat(await openNodeSqliteStorage(path),setup);harness=opened.harness;observe(harness);
  const submission=await opened.root.submit({type:"input",content:"go"},context);await blocked.reached;
  const original=requests.at(-1);expect(original.state.checkpoint.attempt).toBe(1);await harness.close(context);harness=undefined;
  opened=await openChat(await openNodeSqliteStorage(path),setup);harness=opened.harness;observe(harness);
  expect((await (await harness.submission(submission.id,context)).wait(context)).status).toBe("done");expect(calls).toBe(2);
  expect(requests.length).toBeGreaterThanOrEqual(2);for(const task of requests){expect(task.state.checkpoint).toEqual(original.state.checkpoint);expect(task.restarts??0).toBe(0)}
 }finally{if(harness)await harness.close(context);rmSync(dir,{recursive:true,force:true})}
 expect(ends).toHaveLength(2);expect(ends.map(end=>end.kind)).toEqual(["failed","failed"]);
});

it("stores builtin task records definition-free with opaque inputs/checkpoints and every state union",async()=>{
 const {readFileSync}=await import("node:fs"),{openNodeSqliteStorage}=await import("@pinned/sqlite-node");
 const {mkdtempSync,rmSync}=await import("node:fs"),{tmpdir}=await import("node:os"),{join}=await import("node:path");
 const {records}=JSON.parse(readFileSync(new URL("../../durable/testdata/pi-durable-v150/builtin-storage-records.json",import.meta.url),"utf8"));
 const dir=mkdtempSync(join(tmpdir(),"source-raw-builtins-")),path=join(dir,"state.db");let sqlite:any;
 for(const storage of [new ControlledStorage(),await openNodeSqliteStorage(path)]){
  if(!(storage instanceof ControlledStorage))sqlite=storage;
  try{
   const receipts:any[]=[];
   expect(await storage.commit([[{type:"conversation",value:{id:1}}],records.map((value:any)=>({type:"task",value}))],(index:any,seq:any)=>receipts.push([index,seq]),context)).toEqual({stored:2});
   expect(receipts).toEqual([[0,1],[1,2]]);
   for(const record of records)expect(await storage.task(record.id,context)).toEqual(record);
   for(const status of ["pending","running","waiting","sleeping","completing","terminal"]){const page=await storage.scanTasks({status},20,undefined,context);expect(page.items).toEqual(records.filter((r:any)=>r.state.status===status))}
  }finally{await storage.close(context);if(storage===sqlite)sqlite=undefined}
 }
 try{sqlite=await openNodeSqliteStorage(path);for(const record of records)expect(await sqlite.task(record.id,context)).toEqual(record)}finally{if(sqlite)await sqlite.close(context);rmSync(dir,{recursive:true,force:true})}
});

it("preserves empty string and empty/nonempty block input arms at placement", async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const content of ["",[],[{type:"text",text:""}]] as const){
  const setup=chatSetup();setup.now=()=>42;setup.faux.setResponses([assistant("done")]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{
   const settled=await (await root.submit({type:"input",content},context)).wait(context);
   expect(settled.status).toBe("done");
   const {allEntries}=await import("@pinned/chat");
   const entry=(await allEntries(root)).find(entry=>entry.id===settled.entry)!;
   expect(entry.model).toEqual([{role:"user",content,timestamp:42}]);
  }finally{await harness.close(context)}
 }
});

it("places content and metadata from the canonical Inbox item, not a submission payload",async()=>{
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support"),{InboxDoc}=await import("@earendil-works/pi-durable");
 const {deferred}=await import("@pinned/tasks"),reached=deferred(),release=deferred(),setup=chatSetup();setup.now=()=>42;
 setup.faux.setResponses([async()=>{reached.resolve();await release.promise;return assistant("done")},assistant("done")]);
 const {harness,root}=await openChat(new ControlledStorage(),setup);
 try{
  const first=await root.submit({type:"input",content:"first"},context);await reached.promise;
  const queued=await root.submit({type:"input",content:"private checkpoint",entry:{kind:"app.private"}},context);
  await root.commit(async tx=>{const inbox=await tx.doc(InboxDoc,root.id);const item=inbox.items.find(item=>item.id===queued.id)!;Object.assign(item,{content:[],entry:{kind:"app.canonical",data:null}})},context);
  release.resolve();await first.wait(context);const settled=await queued.wait(context);
  const entry=(await allEntries(root)).find(entry=>entry.id===settled.entry)!;
  expect(entry.kind).toBe("app.canonical");expect(entry.data).toBe(null);expect(entry.model).toEqual([{role:"user",content:[],timestamp:42}]);
 }finally{release.resolve();await harness.close(context)}
});

it("truncates a tool result at the exact UTF-16 slice boundary",async()=>{
 const {serializeConversation}=await import("@pinned/compaction");
 const text="x".repeat(1999)+"🙂";
 const serialized=serializeConversation([{role:"toolResult",toolCallId:"c",toolName:"read",content:[{type:"text",text}],isError:false,timestamp:0}]);
 expect(serialized).toBe("[Tool result]: "+"x".repeat(1999)+"\ud83d\n\n[... 1 more characters truncated]");
});
