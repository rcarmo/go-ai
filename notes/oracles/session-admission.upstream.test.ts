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
 const id=await root.compact(undefined,context);expect((await harness.waitForTask(id,context)).state.outcome.result).toEqual({});await harness.close(context);
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
  const firstStored=Promise.withResolvers<void>();const setup=chatSetup();setup.settings.toolExecution=mode;addTool(setup.registry,tool("tool-c1"));addTool(setup.registry,{...tool("tool-c2"),execute:async()=>{await firstStored.promise;return{output:[]}}});setup.faux.setResponses([assistant("",{calls:["c1","c2"]}),assistant("done")]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);const tasks:any[]=[],entries:any[]=[],ends:any[]=[];
  harness.observeCommits({advance:p=>{for(const change of p.changes){if(change.type==="task")tasks.push(change.value);if(change.type==="entry"){entries.push(change.value);if(change.value.kind==="pi.tool-result"&&(change.value.model?.[0]as any)?.toolCallId==="c1")firstStored.resolve()}}},end:end=>ends.push(end)});
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

it("preserves absent, empty and nonempty manual compaction instructions",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const instructions of [undefined,"","preserve paths"]){
  const setup=chatSetup();setup.settings.compaction={enabled:false,keepRecentTokens:0};let request="";
  setup.faux.setResponses([assistant("a"),assistant("b"),(input:any)=>{const user=input.messages.find((m:any)=>m.role==="user");request=typeof user.content==="string"?user.content:user.content[0].text;return assistant("summary")}]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{
   await (await root.submit({type:"input",content:"one"},context)).wait(context);
   await (await root.submit({type:"input",content:"two"},context)).wait(context);
   const id=await root.compact(instructions,context),record=await harness.waitForTask(id,context);
   expect(record.state.outcome.status).toBe("completed");expect(record.state.outcome.result).toEqual({submissionId:expect.any(Number)});expect(record.input).toEqual(instructions===undefined?{reason:"manual"}:{reason:"manual",instructions});
   expect(request.endsWith(instructions===undefined?"Keep each section concise. Preserve exact file paths, function names, and error messages.":"\n\nAdditional focus: "+instructions)).toBe(true);
  }finally{await harness.close(context)}
 }
});

it("uses model-window compaction thresholds and canonical automatic input",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const contextWindow of [100,0]){
  const setup=chatSetup({models:[{id:"faux-1",contextWindow,maxTokens:256}]});
  setup.settings.compaction={enabled:true,reserveTokens:99,keepRecentTokens:1,backgroundTokens:0};
  setup.faux.setResponses(contextWindow>0?[assistant("summary"),assistant("answer")]:[assistant("answer")]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);const tasks:any[]=[];
  harness.observeCommits({advance:p=>{for(const change of p.changes)if(change.type==="task"&&change.value.kind==="pi.compaction")tasks.push(change.value)}});
  try{
   await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
   expect((await (await root.submit({type:"input",content:"new"},context)).wait(context)).status).toBe("done");
   if(contextWindow<=0)expect(tasks).toEqual([]);
   else{expect(tasks.length).toBeGreaterThan(0);for(const task of tasks){expect(task.input).toEqual({reason:"threshold"});expect(task.background).toBe(false);expect(task.owner).toBeTypeOf("number")}expect(tasks.at(-1).state.outcome.result).toEqual({entryId:expect.any(Number)})}
  }finally{await harness.close(context)}
 }
});

it("ignores legacy retry extras on a compaction task in favour of current settings",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support"),{CompactionTask}=await import("@earendil-works/pi-durable");
 const setup=chatSetup();setup.settings.compaction={enabled:false,keepRecentTokens:0,reserveTokens:125};setup.settings.retry={enabled:true,maxRetries:1,baseDelayMs:1,maxDelayMs:1};
 setup.faux.setResponses([{...assistant("",{stopReason:"error"}),errorMessage:"503 overloaded"},assistant("summary")]);
 const {harness,root}=await openChat(new ControlledStorage(),setup);
 const input={reason:"manual",retry:{enabled:false,maxRetries:9,baseDelayMs:1,maxDelayMs:2}};
 try{
  const id=await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]});return tx.createTask(CompactionTask,input as any,{ownership:{kind:"conversation"},conversationId:root.id})},context);
  harness.resume();const record=await harness.waitForTask(id,context);
  expect(record.state.outcome.status).toBe("completed");expect(record.input).toEqual(input);
 }finally{await harness.close(context)}
});

it("rehydrates current model metadata when resending an orderly closed summary",async()=>{
 const {chatSetup,openChat,unanswered}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support"),{openNodeSqliteStorage}=await import("@pinned/sqlite-node");
 const {mkdtempSync,rmSync}=await import("node:fs"),{tmpdir}=await import("node:os"),{join}=await import("node:path");
 const directory=mkdtempSync(join(tmpdir(),"summary-model-authority-")),path=join(directory,"state.db"),setup=chatSetup({models:[{id:"faux-1",contextWindow:128000,maxTokens:256}]}),blocked=unanswered();
 setup.settings.compaction={enabled:false,keepRecentTokens:0,reserveTokens:125};setup.settings.stream={timeoutMs:1234,deferred:true};setup.faux.setResponses([blocked.step]);
 const seen:any[]=[],complete=setup.models.completeSimple.bind(setup.models);setup.models.completeSimple=(model:any,input:any,options:any)=>{seen.push([model.contextWindow,options.maxTokens]);return complete(model,input,options)};
 let harness:any;
 try{
  let opened=await openChat(await openNodeSqliteStorage(path),setup);harness=opened.harness;
  await opened.root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(opened.root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
  const id=await opened.root.compact(undefined,context);await blocked.reached;
  const {readFileSync}=await import("node:fs");const fixture=JSON.parse(readFileSync(new URL("../../durable/testdata/pi-durable-v150/compaction-request.json",import.meta.url),"utf8"));
  expect((await harness.getTask(id,context)).state.checkpoint).toEqual(fixture.tasks[0].state.checkpoint);
  const {LiveDoc}=await import("@earendil-works/pi-durable");expect(await harness.snapshot(LiveDoc,opened.root.id,context)).toEqual(fixture.live);
  await harness.close(context);harness=undefined;
  const next=chatSetup({models:[{id:"faux-1",contextWindow:123,maxTokens:256}]});next.faux.setResponses([assistant("summary")]);setup.models.setProvider(next.faux.provider);
  opened=await openChat(await openNodeSqliteStorage(path),setup);harness=opened.harness;
  expect((await harness.waitForTask(id,context)).state.outcome.status).toBe("completed");expect(seen).toEqual([[128000,100],[123,100]]);
 }finally{if(harness)await harness.close(context);rmSync(directory,{recursive:true,force:true})}
});

it("uses Number multiplication before flooring the summary reserve budget",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const [reserveTokens,want] of [[101,80],[4503599627370496,3602879701896397],[9007199254740991,7205759403792793]]){
  const setup=chatSetup({models:[{id:"faux-1",contextWindow:128000,maxTokens:0}]});setup.settings.compaction={enabled:false,reserveTokens,keepRecentTokens:0};
  let budget:any;setup.faux.setResponses([(_input:any,options:any)=>{budget=options.maxTokens;return assistant("summary")}]);
  const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
   const id=await root.compact(undefined,context);expect((await harness.waitForTask(id,context)).state.outcome.status).toBe("completed");expect(budget).toBe(want);
  }finally{await harness.close(context)}
 }
});

it("stores and reads provider/modelId when replacing an agent model",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{AgentDoc}=await import("@earendil-works/pi-durable");
 const {harness,root}=await openChat(new ControlledStorage(),chatSetup());
 try{const model={provider:"faux",modelId:"replacement"};await root.configure({model},context);
  expect((await harness.snapshot(AgentDoc,root.id,context))!.model).toEqual(model);
 }finally{await harness.close(context)}
});

it("places empty and whitespace hook summaries verbatim as the first decision",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{addHooks}=await import("@pinned/harness-support"),{CompactionTask}=await import("@earendil-works/pi-durable");
 for(const summary of [""," \t\n ","\u0085","\ufeff"]){const setup=chatSetup();setup.settings.compaction={enabled:false,keepRecentTokens:0};let requests=0,later=0;
  setup.faux.setResponses([()=>{requests++;throw Error("hook summary must not request provider")}]);
  addHooks(setup.registry,CompactionTask,{beforeCompact:()=>({summary})});addHooks(setup.registry,CompactionTask,{beforeCompact:()=>{later++;return {decline:true}}});
  const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
   const id=await root.compact(undefined,context);expect((await harness.waitForTask(id,context)).state.outcome.status).toBe("completed");
   const message=(await root.context(context)).messages[0];expect(message.content[0].text).toBe("The conversation history before this point was compacted into the following summary:\n\n<summary>\n"+summary+"\n</summary>");expect(requests).toBe(0);expect(later).toBe(0);expect(setup.reports).toEqual([]);
  }finally{await harness.close(context)}
 }
});

it("trims provider summary text with ECMAScript whitespace semantics",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const [text,want] of [["\ufeff summary \ufeff","summary"],["\u0085","\u0085"],["\ufeff",null],["\u180e","\u180e"],[" \t\n",null]]){
  const setup=chatSetup();setup.settings.compaction={enabled:false,keepRecentTokens:0};setup.faux.setResponses([assistant(text!)]);const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
   const id=await root.compact(undefined,context),outcome=(await harness.waitForTask(id,context)).state.outcome;
   if(want===null)expect(outcome).toMatchObject({status:"failed",error:{message:"Summarization produced no text"}});
   else{expect(outcome.status).toBe("completed");const message=(await root.context(context)).messages[0];expect(message.content[0].text).toBe("The conversation history before this point was compacted into the following summary:\n\n<summary>\n"+want+"\n</summary>")}
  }finally{await harness.close(context)}
 }
});

it("distinguishes absent and present-empty provider errors in compaction failure",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{assistant}=await import("@pinned/harness-support");
 for(const [errorMessage,want] of [[undefined,"Summarization failed: error"],["","Summarization failed: "],[" ","Summarization failed:  "],["bad request","Summarization failed: bad request"]]){
  const message=assistant("",{stopReason:"error"});if(errorMessage===undefined)delete message.errorMessage;else message.errorMessage=errorMessage;
  const setup=chatSetup();setup.settings.compaction={enabled:false,keepRecentTokens:0};setup.faux.setResponses([JSON.parse(JSON.stringify(message))]);const {harness,root}=await openChat(new ControlledStorage(),setup);
  try{await root.commit(async tx=>{for(const content of ["old","recent"])await tx.appendEntry(root.id,{kind:"note",model:[{role:"user",content,timestamp:0}]})},context);
   const id=await root.compact(undefined,context);expect((await harness.waitForTask(id,context)).state.outcome).toMatchObject({status:"failed",error:{message:want}});
  }finally{await harness.close(context)}
 }
});

it("keeps optional errors and usage in actual internal model-tool projections",async()=>{
 const {createModelTools}=await import("@pinned/model-tools"),{createModels,createProvider}=await import("@reference/pi-ai");
 const {readFileSync}=await import("node:fs");const fixture=JSON.parse(readFileSync(new URL("../../durable/testdata/pi-durable-v150/model-tools.json",import.meta.url),"utf8"));
 expect(createModelTools().map(({name,description,parameters,structuredOutputSchema,callers}:any)=>({name,description,parameters,structuredOutputSchema,callers}))).toEqual(fixture.tools);
 const cost={input:0,output:0,cacheRead:0,cacheWrite:0};
 for(const stopReason of ["stop","error"])for(const errorMessage of [undefined,"","failed"]){
  const models=createModels();const result=(model:any,kind:string)=>({api:model.api,provider:model.provider,model:model.id,...(kind==="classifier"?{answers:{}}:{output:[]}),stopReason,timestamp:0,...(errorMessage===undefined?{}:{errorMessage})});
  models.setProvider(createProvider({id:"presence",auth:{apiKey:{name:"Presence",resolve:async()=>({auth:{}})}},models:[{id:"judge",name:"Judge",api:"presence-classifier",provider:"presence",baseUrl:"https://example.invalid",input:["text"],cost,type:"classifier",contextWindow:1000},{id:"painter",name:"Painter",api:"presence-images",provider:"presence",baseUrl:"https://example.invalid",input:["text"],output:["image"],cost,type:"image"}],classifiers:{"presence-classifier":{classify:async(model:any)=>result(model,"classifier")}},images:{"presence-images":{generateImages:async(model:any)=>result(model,"image")}}}));
  for(const tool of createModelTools()){
   const classifier=tool.name.endsWith("classify"),model={provider:"presence",id:classifier?"judge":"painter"};const input=classifier?{state:{},questions:{q:{type:"bool",instructions:"yes?",criteria:{true:"yes",false:"no"}}}}:{input:[{type:"text",text:"draw"}]};
   const response=await tool.execute({model,context:input},{models} as any,context);const value=response.structuredOutput as any;
   expect(Object.hasOwn(value,"errorMessage")).toBe(errorMessage!==undefined);if(errorMessage!==undefined)expect(value.errorMessage).toBe(errorMessage);expect(Object.hasOwn(value,"usage")).toBe(false);expect(response.isError).toBe(stopReason==="stop"?undefined:true);
   expect(response.output[0].text).toBe((classifier?"classify":"generateImages")+" presence/"+model.id+" "+(stopReason==="stop"?"done":"error: "+(errorMessage??"")));
  }
 }
});

it("treats tool names as string map keys rather than section/document identifiers",async()=>{
 const {createRegistry,defineTool,defineExtension,wrapTool}=await import("@earendil-works/pi-durable");
 const names=["","9tool","_tool"," Mixed case: note/v1","工具/Δ","x".repeat(129),"nul\0name"];
 const tools=names.map(name=>defineTool({name,description:name,parameters:{type:"object"} as any,execute:async()=>({output:[]})}));
 const registry=createRegistry();registry.install(defineExtension({name:"names",tools,wraps:tools.map(tool=>wrapTool(tool,inner=>({...inner,description:"wrapped "+tool.name})))}));
 expect(registry.snapshot().tools().map(({tool})=>tool.name)).toEqual(names);
 expect(registry.snapshot().extension("names")!.wraps!.map(wrap=>(wrap as any).tool)).toEqual(names);
 const {resolveAgent,resolveSettings}=await import("@pinned/agent");const reports:unknown[]=[];
 const agent=resolveAgent({tools:names},registry.snapshot(),resolveSettings(undefined),(error)=>reports.push(error));
 expect(agent.tools.map(tool=>[tool.name,tool.description])).toEqual(names.map(name=>[name,"wrapped "+name]));expect(reports).toEqual([]);
 expect(()=>registry.install(defineExtension({name:"duplicate",tools:[tools[0]!,tools[0]!]}))).toThrow("two tools named ");
 expect(registry.snapshot().installed().map(extension=>extension.name)).toEqual(["names"]);
});

it("accepts empty classifier strings and one score level in the original internal tool",async()=>{
 const {createModelTools}=await import("@pinned/model-tools"),{createModels,createProvider}=await import("@reference/pi-ai");
 const seen:any[]=[];const models=createModels();models.setProvider(createProvider({id:"empty",auth:{apiKey:{name:"Empty",resolve:async()=>({auth:{}})}},models:[{id:"judge",name:"Judge",api:"empty-classifier",provider:"empty",baseUrl:"https://example.invalid",input:["text","image"],cost:{input:0,output:0,cacheRead:0,cacheWrite:0},type:"classifier",contextWindow:1000}],classifiers:{"empty-classifier":{classify:async(_model:any,input:any)=>{seen.push(input);return{answers:{},stopReason:"stop",timestamp:0}}}}}));
 const tool=createModelTools().find(tool=>tool.name.endsWith("classify"))!;
 for(const question of[{type:"choice",instructions:"",criteria:{"":""}},{type:"score",instructions:"",criteria:[""]},{type:"bool",instructions:"",criteria:{true:"",false:""}}]){
  const input={state:{},questions:{"":question}};const result=await tool.execute({model:{provider:"empty",id:"judge"},context:input},{models} as any,context);
  expect(result.output[0]).toEqual({type:"text",text:"classify empty/judge done"});expect(seen.at(-1)).toEqual(input);expect(Object.hasOwn(seen.at(-1),"images")).toBe(false);
 }
 expect(seen).toHaveLength(3);
 const base={state:{},questions:{q:{type:"bool",instructions:"",criteria:{true:"",false:""}}}};
 for(const images of [[],[{type:"image",data:"",mimeType:""}]]){const input={...base,images};await tool.execute({model:{provider:"empty",id:"judge"},context:input},{models} as any,context);expect(seen.at(-1)).toEqual(input);expect(Object.hasOwn(seen.at(-1),"images")).toBe(true)}
 expect(seen).toHaveLength(5);
 for(const images of [null,"not-array",[null],[{type:"text",data:"",mimeType:""}],[{type:"image",data:""}],[{type:"image",data:null,mimeType:""}]])await expect(tool.execute({model:{provider:"empty",id:"judge"},context:{...base,images}},{models} as any,context)).rejects.toThrow();
 for(const question of[{type:"choice",instructions:"",criteria:{}},{type:"score",instructions:"",criteria:[]},{type:"bool",instructions:"",criteria:{true:""}},{type:"other",instructions:"",criteria:{}}])await expect(tool.execute({model:{provider:"empty",id:"judge"},context:{state:{},questions:{"":question}}},{models} as any,context)).rejects.toThrow();
 expect(seen).toHaveLength(5);
});

it("retains empty image block strings and rejects missing fields in the original model tool",async()=>{
 const {createModelTools}=await import("@pinned/model-tools"),{createModels,createProvider}=await import("@reference/pi-ai");
 const seen:any[]=[];const blocks=[{type:"text",text:""},{type:"image",data:"",mimeType:""}];const models=createModels();models.setProvider(createProvider({id:"presence",auth:{apiKey:{name:"Presence",resolve:async()=>({auth:{}})}},models:[{id:"painter",name:"Painter",api:"empty-images",provider:"presence",baseUrl:"https://example.invalid",input:["text","image"],output:["text","image"],cost:{input:0,output:0,cacheRead:0,cacheWrite:0},type:"image"}],images:{"empty-images":{generateImages:async(_model:any,input:any)=>{seen.push(input);return{api:"empty-images",provider:"presence",model:"painter",output:blocks,stopReason:"stop",timestamp:0}}}}}));
 const tool=createModelTools().find(tool=>tool.name.endsWith("generateImages"))!;const result=await tool.execute({model:{provider:"presence",id:"painter"},context:{input:blocks}},{models} as any,context);
 expect(seen).toEqual([{input:blocks}]);expect((result.structuredOutput as any).output).toEqual(blocks);expect(result.output).toEqual([{type:"text",text:"generateImages presence/painter done"}]);
 for(const input of [null,[],"not-array",[null],[{type:"text"}],[{type:"text",text:null}],[{type:"image",data:""}],[{type:"image",data:null,mimeType:""}],[{type:"other",text:""}]])await expect(tool.execute({model:{provider:"presence",id:"painter"},context:{input}},{models} as any,context)).rejects.toThrow();
 expect(seen).toHaveLength(1);
});

it("returns callback values and arbitrary host identity from the original Session commit",async()=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);const marker:any={call:()=>"host identity"};marker.self=marker;
 for(const value of [undefined,null,false,0,"","callback result",marker])expect(await session.commit(()=>value,context)).toBe(value);
 const failure={callback:"failure"};await expect(session.commit(()=>{throw failure},context)).rejects.toBe(failure);
 expect(await session.commit(()=>"still usable",context)).toBe("still usable");await session.close(context);
});

it("returns entry and host values from the original tool execution api commit",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");const {fauxAssistantMessage,fauxText,fauxToolCall}=await import("@reference/pi-ai");
 const setup=chatSetup();let effects=0;let escaped:any;const marker:any={};marker.self=marker;
 addTool(setup.registry,defineTool({name:"commit_value_probe",description:"values",parameters:{type:"object"} as any,execute:async(_args,api,callContext)=>{effects++;escaped=api;const entry=await api.commit(tx=>tx.appendEntry(api.conversationId,{kind:"test.note",data:{probe:true}}),callContext);expect(entry.byTaskId).toBe(api.taskId);expect(await api.commit(()=>marker,callContext)).toBe(marker);const failure=new Error("callback failure");await expect(api.commit(()=>{throw failure},callContext)).rejects.toBe(failure);return{output:[{type:"text",text:"value committed"}]}}}));
 setup.faux.setResponses([fauxAssistantMessage([fauxToolCall("commit_value_probe",{},{id:"source-value"})],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("done")])]);const {harness,root}=await openChat(new MemoryStorage(),setup);
 expect((await (await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects).toBe(1);let called=false;await expect(escaped.commit(()=>{called=true;return"late"},context)).rejects.toThrow();expect(called).toBe(false);await harness.close(context);
});

it("accepts persistence options on original Session Harness and conversation commits",async()=>{
 const {MemoryStorage}=await import("@earendil-works/pi-durable"),{chatSetup,openChat}=await import("@pinned/chat");
 for(const name of ["session","harness","conversation"]){const storage=new MemoryStorage();let api:any,reader:any,owner:any;let close:()=>Promise<void>;
  if(name==="session"){const session=new SessionImpl(storage,Date.now,()=>600000);const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);api=reader=session;owner=conversation.id;close=()=>session.close(context)}else{const opened=await openChat(storage,chatSetup());api=name==="harness"?opened.harness:opened.root;reader=opened.harness;owner=opened.root.id;close=()=>opened.harness.close(context)}
  const Doc=defineDoc({kind:"source.options",scope:"conversation",history:"latest",fork:"initial",version:1,initial:()=>({text:""}),checkpointWhen:()=>false});await api.commit(async(tx:any)=>{await tx.doc(Doc,owner)},context);const record=(await storage.findDocument({kind:Doc.definition.kind,scope:{kind:"conversation",conversationId:owner}},"current",context))!;
  expect(await api.commit(async(tx:any)=>{(await tx.doc(Doc,owner)).text="held";return"held"},context,{persistence:{kind:"deferred",withinMs:Infinity}})).toBe("held");expect((await storage.document(record.id,"current",context))!.value).toEqual({text:""});expect(await reader.snapshot(Doc,owner,context)).toEqual({text:"held"});
  expect(await api.commit(async(tx:any)=>{(await tx.doc(Doc,owner)).text="stored";return"stored"},context,{persistence:{kind:"immediate"}})).toBe("stored");expect((await storage.document(record.id,"current",context))!.value).toEqual({text:"stored"});
  for(const withinMs of [-1,NaN]){let called=false;await expect(api.commit(()=>{called=true},context,{persistence:{kind:"deferred",withinMs}})).rejects.toThrow("CommitPersistence.withinMs");expect(called).toBe(false)}
  for(const kind of ["immediate","unknown"])expect(await api.commit(()=>"immediate",context,{persistence:{kind,withinMs:NaN}})).toBe("immediate");
  expect(await api.commit(()=>"first option",context,{persistence:{kind:"immediate"}},{persistence:{kind:"deferred",withinMs:NaN}})).toBe("first option");
  const entryId=await api.commit(async(tx:any)=>{(await tx.doc(Doc,owner)).text="mixed";return(await tx.appendEntry(owner,{kind:"source.options-note",data:{mixed:true}})).id},context,{persistence:{kind:"deferred",withinMs:Infinity}});expect((await storage.entry(entryId,context))!.entry.kind).toBe("source.options-note");expect((await storage.document(record.id,"current",context))!.value).toEqual({text:"mixed"});
  await close();let called=false;await expect(api.commit(()=>{called=true},context,{persistence:{kind:"deferred",withinMs:NaN}})).rejects.toThrow(/closed/i);expect(called).toBe(false);
 }
});

it("copies original entry drafts after metadata overrides and retains empty arrays and kinds",async()=>{
 const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const payload={nested:{value:1}};const first=await session.commit(async tx=>{const record=await tx.appendEntry(conversation.id,{kind:"note",data:payload});payload.nested.value=2;return record},context);expect((await storage.entry(first.id,context))!.entry.data).toEqual({nested:{value:1}});
 const empty=await session.commit(tx=>tx.appendEntry(conversation.id,{kind:"",data:null,model:[],edits:[],head:"self",id:()=>{},conversationId:NaN,byTaskId:()=>{}} as any),context);expect(empty).toEqual({id:empty.id,conversationId:conversation.id,kind:"",data:null,model:[],edits:[],head:empty.id});expect((await storage.entry(empty.id,context))!.entry).toEqual(empty);const {defineEntry}=await import("@earendil-works/pi-durable");const Token=defineEntry("source.typed");const typed=await session.commit(tx=>tx.appendEntry(Token,conversation.id,{kind:"caller.kind",data:null,edits:[{target:first.id,action:"replace",messages:[]}]} as any),context);expect(typed.kind).toBe(Token.kind);expect(typed.edits).toEqual([{target:first.id,action:"replace",messages:[]}]);expect((await storage.entry(typed.id,context))!.entry).toEqual(typed);const object:any={};object.self=object;const array:any[]=[];array.push(array);for(const data of [object,array])await expect(session.commit(tx=>tx.appendEntry(conversation.id,{kind:"invalid.cycle",data}),context)).rejects.toThrow("Value contains cycles and is not strict JSON");await session.close(context);
});

it("constructs original conversations from ownership options and staged owners",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");const Work=defineTask({name:"source.conversation-owner",version:1,initial:()=>({phase:"start"}),phases:{start:async()=>{}},abort:async()=>{}});const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);
 const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const created=await session.commit(async tx=>{const owner=await tx.createTask(Work,{path:"background"},{ownership:{kind:"conversation"},conversationId:parent.id,background:true});const child=await tx.createConversation({ownership:{kind:"task",taskId:owner}});return{owner,child}},context);expect(created.child.owner).toEqual({conversationId:parent.id,taskId:created.owner});expect(await storage.conversation(created.child.id,context)).toEqual(created.child);
 await expect(session.commit(tx=>tx.createConversation({ownership:{kind:"task",taskId:999}} as any),context)).rejects.toThrow("Conversation owner task 999 does not exist");
 for(const [options,message]of[[null,"Cannot read properties of null (reading 'ownership')"],[{},"Cannot read properties of undefined (reading 'kind')"],[{ownership:null},"Cannot read properties of null (reading 'kind')"]]as const)await expect(session.commit(tx=>tx.createConversation(options as any),context)).rejects.toThrow(message);
 await expect(session.commit(tx=>tx.createConversation({ownership:{kind:"task",taskId:null}}as any),context)).rejects.toThrow("Conversation owner task null does not exist");for(const ownership of[{kind:"unknown",ignored:()=>{}},true,{kind:"task"}]){const record=await session.commit(tx=>tx.createConversation({ownership}as any),context);expect(record).toEqual({id:record.id})}await session.close(context);
});

it("admits original ownerless conversation construction during an unrelated writer flight",async()=>{
 const entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>();class Held extends ControlledStorage{held=false;calls:number[]=[];override async commit(batches:any,receive:any,ctx:any){this.calls.push(batches.length);if(this.held){this.held=false;entered.resolve();await release.promise}return super.commit(batches,receive,ctx)}}
 const storage=new Held(),session=new SessionImpl(storage,Date.now,()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const start=storage.calls.length;storage.held=true;const first=session.commit(tx=>tx.appendEntry(parent.id,{kind:"source.held"}),context);await entered.promise;
 const signals=[Promise.withResolvers<void>(),Promise.withResolvers<void>()];const creates=signals.map(signal=>session.commit(async tx=>{const record=await tx.createConversation({ownership:{kind:"ownerless"}});signal.resolve();return record},context));let timer:ReturnType<typeof setTimeout>|undefined;let barrier:Promise<unknown>|undefined;try{await Promise.race([(async()=>{await Promise.all(signals.map(signal=>signal.promise));const entered=Promise.withResolvers<void>();barrier=session.commit(()=>{entered.resolve()},context);await entered.promise})(),new Promise((_,reject)=>{timer=setTimeout(()=>reject(new Error("constructor drained unrelated writer")),5000)})])}finally{clearTimeout(timer);release.resolve()}await first;await barrier;const records=await Promise.all(creates);expect(new Set(records.map(record=>record.id)).size).toBe(2);expect(storage.calls.slice(start)).toEqual([1,2]);await session.close(context);
});

it("forks original final commit state with ownership options and allocates before a missing cutoff",async()=>{
 class Counted extends ControlledStorage{mints=0;override async mintId(){this.mints++;return super.mintId<any>()}}const storage=new Counted(),session=new SessionImpl(storage,Date.now,()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const Doc=defineDoc({kind:"fork.same-commit",version:1,scope:"conversation",history:"rewindable",fork:"asOf",initial:()=>({value:"initial"}),checkpointWhen:()=>false});let included:any,excluded:any;
 await session.commit(async tx=>{included=await tx.appendEntry(parent.id,{kind:"included"});(await tx.doc(Doc,parent.id)).value="final-state-of-commit";excluded=await tx.appendEntry(parent.id,{kind:"excluded"})},context);const child=await session.commit(tx=>tx.forkConversation(parent.id,included.id,{ownership:{kind:"ownerless"}}),context);expect(child.parent).toEqual({conversationId:parent.id,at:included.id});expect(await session.snapshot(Doc,child.id,context)).toEqual({value:"final-state-of-commit"});const visible=await session.commit(tx=>tx.scanEntries({conversationId:child.id},10),context);expect(visible.items.map(entry=>entry.id)).toContain(included.id);expect(visible.items.map(entry=>entry.id)).not.toContain(excluded.id);
 const {defineTask}=await import("@earendil-works/pi-durable");const Work=defineTask({name:"source.fork-owner",version:1,initial:()=>({phase:"start"}),phases:{start:async()=>{}},abort:async()=>{}});const owned=await session.commit(async tx=>{const task=await tx.createTask(Work,null,{ownership:{kind:"conversation"},conversationId:parent.id,background:true});return{task,child:await tx.forkConversation(parent.id,included.id,{ownership:{kind:"task",taskId:task}})}},context);expect(owned.child.owner).toEqual({conversationId:parent.id,taskId:owned.task});const before=storage.mints;await expect(session.commit(tx=>tx.forkConversation(parent.id,999 as any,{ownership:{kind:"ownerless"}}),context)).rejects.toThrow(`Entry 999 is not visible from conversation ${parent.id}`);expect(storage.mints).toBe(before+1);await session.close(context);
});

it("rejects original fork source writes only during assembly after callback completion",async()=>{
 let predicates=0;const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const Current=defineDoc({kind:"fork.same-transaction-current",version:1,scope:"conversation",history:"latest",fork:"current",initial:()=>({value:"committed"}),checkpointWhen:()=>{predicates++;return false}}),AsOf=defineDoc({kind:"fork.same-transaction-as-of",version:1,scope:"conversation",history:"rewindable",fork:"asOf",initial:()=>({value:"committed"}),checkpointWhen:()=>{predicates++;return false}});let cut:any;await session.commit(async tx=>{cut=(await tx.appendEntry(parent.id,{kind:"point"})).id;await tx.doc(Current,parent.id);await tx.doc(AsOf,parent.id)},context);const commits=storage.commits.length;
 for(const mode of["before-current","after-current","before-asof"]){let finished=false;await expect(session.commit(async tx=>{if(mode==="before-current")(await tx.doc(Current,parent.id)).value=mode;if(mode==="before-asof")(await tx.doc(AsOf,parent.id)).value=mode;await tx.forkConversation(parent.id,cut,{ownership:{kind:"ownerless"}});if(mode==="after-current")(await tx.doc(Current,parent.id)).value=mode;finished=true},context)).rejects.toThrow("Cannot change fork source document");expect(finished).toBe(true);expect(predicates).toBe(0)}expect(storage.commits.length).toBe(commits);expect(await session.snapshot(Current,parent.id,context)).toEqual({value:"committed"});expect(await session.snapshot(AsOf,parent.id,context)).toEqual({value:"committed"});let finished=false;await expect(session.commitWith(async tx=>{(await tx.doc(Current,parent.id)).value="priority";await tx.forkConversation(parent.id,cut,{ownership:{kind:"ownerless"}});tx.setTask({id:999,conversationId:parent.id,kind:"test.work",version:1,input:null,background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"start"}}}as any);finished=true},context)).rejects.toThrow("Cannot change fork source document");expect(finished).toBe(true);expect(predicates).toBe(0);await session.close(context);
});

it("allows original parallel tool results to finish in reverse call order",async()=>{
 const {chatSetup,openChat}=await import("@pinned/chat"),{tool,addTool,assistant}=await import("@pinned/harness-support");const setup=chatSetup();setup.settings.toolExecution="parallel";const secondStored=Promise.withResolvers<void>();addTool(setup.registry,{...tool("tool-c1"),execute:async()=>{await secondStored.promise;return{output:[]}}});addTool(setup.registry,tool("tool-c2"));setup.faux.setResponses([assistant("",{calls:["c1","c2"]}),assistant("done")]);const {harness,root}=await openChat(new ControlledStorage(),setup);const results:any[]=[],tasks:any[]=[];harness.observeCommits({advance:p=>{for(const change of p.changes){if(change.type==="task"&&change.value.kind==="pi.tool")tasks.push(change.value);if(change.type==="entry"&&change.value.kind==="pi.tool-result"){results.push(change.value);if((change.value.model?.[0]as any)?.toolCallId==="c2")secondStored.resolve()}}},end:()=>{}});
 try{expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(results.map(entry=>(entry.model[0]as any).toolCallId)).toEqual(["c2","c1"]);for(const entry of results){const callId=(entry.model[0]as any).toolCallId;const terminal=tasks.find(task=>task.input.callId===callId&&task.state.status==="terminal");expect(terminal.state.outcome.result.entryId).toBe(entry.id)}}finally{await harness.close(context)}
});

it("reads the current original Tx head marker with its public one-argument call",async()=>{
 const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);let first:any,last:any;await session.commit(async tx=>{first=await tx.appendEntry(parent.id,{kind:"first",head:"self"});last=await tx.appendEntry(parent.id,{kind:"last",head:"self"})},context);const marker=await session.commit(tx=>tx.latestHeadMarker(parent.id),context);expect(marker?.id).toBe(last.id);expect(marker?.id).not.toBe(first.id);await session.close(context);
});

it("replaces original internal task metadata completely without exporting a public setter",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");const Work=defineTask({name:"test.work",version:1,initial:()=>({phase:"start"}),phases:{start:async()=>{}},abort:async()=>{}});const storage=new ControlledStorage(),session=new SessionImpl(storage,Date.now,()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const id=await session.commit(tx=>tx.createTask(Work,{path:"old"},{ownership:{kind:"conversation"},conversationId:parent.id}),context);const next={id,conversationId:parent.id,kind:"source.replaced",version:2,input:null,background:true,abortRequested:true,state:{status:"pending",checkpoint:{phase:"custom"}},memos:{choice:"b"}};await session.commitWith(tx=>{tx.setTask(next as any)},context);expect(await storage.task(id,context)).toEqual(next);
 let finished=false;await expect(session.commitWith(tx=>{tx.setTask({...next,id:999}as any);finished=true},context)).rejects.toThrow("Task 999 does not exist");expect(finished).toBe(true);await session.close(context);
});

it("owns original internal task replacement JSON and carries only staged timestamps",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");const Work=defineTask({name:"test.work",version:1,initial:()=>({phase:"start"}),phases:{start:async()=>{}},abort:async()=>{}});let clock=100,calls=0;const storage=new ControlledStorage(),session=new SessionImpl(storage,()=>{calls++;return clock},()=>600000);const parent=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);const id=await session.commit(tx=>tx.createTask(Work,null,{ownership:{kind:"conversation"},conversationId:parent.id}),context);const base={id,conversationId:parent.id,kind:"test.work",version:1,input:null,background:false,abortRequested:false};const payload={nested:{value:"owned"}};let stamps=0;
 await session.commitWith(tx=>{const before=calls;tx.setTask({...base,state:{status:"running",checkpoint:{phase:"start"}}}as any);clock=200;tx.setTask({...base,startedAt:200,memos:{memo:payload},state:{status:"running",checkpoint:{phase:"start"}}}as any);payload.nested.value="caller mutation";stamps=calls-before},context);expect((await storage.task(id,context))!.startedAt).toBe(100);expect((await storage.task(id,context))!.memos).toEqual({memo:{nested:{value:"owned"}}});expect(stamps).toBe(1);
 await session.commitWith(tx=>{tx.setTask({...base,state:{status:"pending",checkpoint:{phase:"start"}}}as any)},context);const record=(await storage.task(id,context))!;expect(Object.hasOwn(record,"startedAt")).toBe(false);expect(Object.hasOwn(record,"memos")).toBe(false);await session.close(context);
});

it("executes canonical imported generation records without a private checkpoint",async()=>{
 const {Harness,createRegistry}=await import("@earendil-works/pi-durable");const {createModels}=await import("@reference/pi-ai");
 for(const version of [1,2]){const storage=new ControlledStorage();await storage.commit([[{type:"conversation",value:{id:1}},{type:"task",value:{id:2,conversationId:1,kind:"pi.generation",version,input:{},background:false,abortRequested:false,state:{status:"pending",checkpoint:{phase:"prepare",attempt:1}}}}]]as any,()=>{},context);const harness=await Harness.open(storage,{models:createModels(),registry:createRegistry()},context);await harness.root({},context);harness.resume();await harness.waitForIdle(context);const task=(await harness.getTask(2 as any,context))!;expect(task.version).toBe(2);expect(task.input).toEqual({});expect(task.state).toEqual({status:"terminal",outcome:{status:"failed",error:{message:"No model is configured",detail:{reason:"no_model"}}}});await harness.close(context)}
});

it("executes tasks whose kind and phase keys are arbitrary strings",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable"),{chatSetup,openChat}=await import("@pinned/chat"),{addTask}=await import("@pinned/harness-support");
 for(const key of ["","UPPER name","π task","nul\0key","k".repeat(129)]){const next=key+" next",Task=defineTask({name:key,version:1,initial:()=>({phase:key}),phases:{[key]:async(_task:any,runtime:any,ctx:any)=>{await runtime.commit(()=>({status:"running",checkpoint:{phase:next}}),ctx)},[next]:async(_task:any,runtime:any,ctx:any)=>{await runtime.commit(()=>({status:"terminal",outcome:{status:"completed",result:"ok"}}),ctx)}},abort:async(_task:any,runtime:any,ctx:any)=>{await runtime.commit(()=>({status:"terminal",outcome:{status:"aborted"}}),ctx)}});const setup=chatSetup();addTask(setup.registry,Task);const {harness,root}=await openChat(new ControlledStorage(),setup);const id=await root.commit(tx=>tx.createTask(Task,null,{ownership:{kind:"conversation"}}),context);const task=await harness.waitForTask(id,context);expect(task.kind).toBe(key);expect(task.state).toEqual({status:"terminal",outcome:{status:"completed",result:"ok"}});await harness.close(context)}
});

it("executes and migrates canonical model tool tasks and fails a throwing executor",async()=>{
 const {ToolTask,defineTool}=await import("@earendil-works/pi-durable"),{chatSetup,openChat}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support"),{fauxAssistantMessage,fauxToolCall}=await import("@reference/pi-ai");
 for(const version of [1,2,3]){const setup=chatSetup();let calls=0;addTool(setup.registry,defineTool({name:"echo",description:"echo",parameters:{type:"object",properties:{text:{type:"string"}},required:["text"]}as any,execute:async(args:any)=>{calls++;return{output:[{type:"text",text:args.text}]}}}));const {harness,root}=await openChat(new ControlledStorage(),setup);const entry=await root.commit(tx=>tx.appendEntry(root.id,{kind:"pi.assistant",model:[fauxAssistantMessage([fauxToolCall("echo",{text:"owned"},{id:"c1"})],{stopReason:"toolUse"})]}),context);const input={...(version===1?{}:{kind:"model"}),assistant:entry.id,callId:"c1"};const id=await root.commit(tx=>tx.createTask({definition:{...ToolTask.definition,version}},input as any,{ownership:{kind:"conversation"}}),context);const task=await harness.waitForTask(id,context);expect(task.version).toBe(3);expect(calls).toBe(1);expect(task.state).toMatchObject({status:"terminal",outcome:{status:"completed",result:{kind:"model",entryId:expect.any(Number)}}});await harness.close(context)}
 const setup=chatSetup();addTool(setup.registry,defineTool({name:"thrower",description:"throws",parameters:{type:"object",properties:{}}as any,execute:async()=>{throw new Error("private failure")}}));const {harness,root}=await openChat(new ControlledStorage(),setup);const entry=await root.commit(tx=>tx.appendEntry(root.id,{kind:"pi.assistant",model:[fauxAssistantMessage([fauxToolCall("thrower",{},{id:"throw-1"})],{stopReason:"toolUse"})]}),context);const id=await root.commit(tx=>tx.createTask(ToolTask,{kind:"model",assistant:entry.id,callId:"throw-1"},{ownership:{kind:"conversation"}}),context);expect((await harness.waitForTask(id,context)).state).toMatchObject({status:"terminal",outcome:{status:"failed",error:{message:"Tool thrower threw"},result:{kind:"model",entryId:expect.any(Number)}}});await harness.close(context);
});

// Source-derived runtime probe, including strings outside the TypeScript
// severity union. tool.ts copies strict JSON and renders the supplied value;
// it does not run an enum validator on extension-returned diagnostics.
it("preserves JSON diagnostic severities through reported progress and a returned result",async()=>{
 const {MemoryStorage,defineTool,LiveDoc}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat");
 const {addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText,fauxToolCall}=await import("@reference/pi-ai");
 for(const severity of ["invalid","","ERROR","🧪","error"]){
  const setup=chatSetup();setup.settings.progress={outputIntervalMs:0,persistWithinMs:0};
  const progress={severity,message:"progress"},returned={severity,message:"returned"};
  addTool(setup.registry,defineTool({name:"diagnostic_json",description:"diagnostics",parameters:{type:"object"}as any,execute:async(_args,api,ctx)=>{
   api.diagnostic(progress as any);await api.details(null,ctx);
   const live=await api.snapshot(LiveDoc,api.conversationId,ctx);
   expect(live?.tools?.[0]?.diagnostics).toEqual([progress]);
   await api.commit(tx=>tx.appendEntry(api.conversationId,{kind:"probe.diagnostic-effect",data:{severity}}),ctx);
   return{output:[{type:"text"as const,text:"result"}],diagnostics:[returned as any]};
  }}));
  setup.faux.setResponses([fauxAssistantMessage([fauxToolCall("diagnostic_json",{},{id:"diagnostic-json"})],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("done")])]);
  const {harness,root}=await openChat(new MemoryStorage(),setup);
  try{
   expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");
   const entries=await allEntries(root),results=entries.filter(entry=>entry.kind==="pi.tool-result");
   expect(entries.filter(entry=>entry.kind==="probe.diagnostic-effect")).toHaveLength(1);
   expect(results).toHaveLength(1);expect(results[0]!.data).toEqual({diagnostics:[progress,returned]});
   expect(results[0]!.model?.[0]).toMatchObject({role:"toolResult",isError:false,content:[{type:"text",text:"result"},{type:"text",text:`<harness>\n[${severity}] progress\n[${severity}] returned\n</harness>`}]});
  }finally{await harness.close(context)}
 }
});

it("aborts the queued record and removes only the first matching inbox position",async()=>{
 const {InboxDoc}=await import("@earendil-works/pi-durable"),{chatSetup,openChat}=await import("@pinned/chat");
 const {harness,root}=await openChat(new ControlledStorage(),chatSetup());
 try{
  const {target,other}=await root.commit(async tx=>{
   const target=await tx.createSubmission({conversationId:root.id,type:"input",status:"queued"});
   const other=await tx.createSubmission({conversationId:root.id,type:"input",status:"queued"});
   (await tx.doc(InboxDoc,root.id)).items.push({id:target.id,mode:"followUp",content:"first"},{id:other.id,mode:"followUp",content:"other"},{id:target.id,mode:"followUp",content:"duplicate"});
   return{target,other};
  },context);
  const decided=Promise.withResolvers<any>();
  const guard=harness.observeCommits({advance:p=>{if(p.changes.some(change=>change.type==="submission"&&change.value.id===target.id&&change.value.status==="unanswered"))decided.resolve(p)},end:()=>{}});
  try{
   expect(await harness.abortSubmission(target.id,context)).toBe("aborted");
   const publication=await decided.promise;
   expect(publication.changes.filter((change:any)=>change.type==="submission").map((change:any)=>change.value)).toEqual([{...target,status:"unanswered",reason:"aborted"}]);
   const inbox=publication.changes.find((change:any)=>change.type==="document"&&change.record.kind==="pi.inbox");
   expect(inbox.value).toEqual({items:[{id:other.id,mode:"followUp",content:"other"},{id:target.id,mode:"followUp",content:"duplicate"}]});
   expect(inbox.ops).toEqual([["p",["items"],0,1,[]]]);
   expect(await harness.snapshot(InboxDoc,root.id,context)).toEqual(inbox.value);
   expect(await(await harness.submission(other.id,context))!.status(context)).toEqual(other);
   expect(await harness.abortSubmission(target.id,context)).toBe("settled");
  }finally{guard.detach()}
 }finally{await harness.close(context)}
});

it("latches constructor reservation failures even when the callback catches them",async()=>{
 const {defineTask}=await import("@earendil-works/pi-durable");
 const Doc=defineDoc({kind:"oracle.alloc.failure",scope:"session",version:1,initial:()=>({}),checkpointWhen:()=>false});
 const Task=defineTask({name:"oracle.alloc.task",version:1,initial:()=>({phase:"run"}),phases:{run:async()=>{}},abort:async()=>{}} as any);
 for(const operation of ["submission","entry","conversation","task","document"]){
  for(const caught of [false,true]){
   const failure={operation,caught};
   class Failing extends ControlledStorage{
    fail=false;
    override async mintId():Promise<any>{if(this.fail)throw failure;return super.mintId()}
   }
   const storage=new Failing(),session=new SessionImpl(storage,Date.now,()=>600000);
   const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
   storage.fail=true;
   const writing=session.commit(async tx=>{
    const construct=async()=>{
     switch(operation){
      case"submission":return tx.createSubmission({conversationId:conversation.id,type:"input",status:"queued"});
      case"entry":return tx.appendEntry(conversation.id,{kind:"note"});
      case"conversation":return tx.createConversation({ownership:{kind:"ownerless"}});
      case"task":return tx.createTask(Task,{}, {ownership:{kind:"conversation"},conversationId:conversation.id});
      default:return tx.doc(Doc);
     }
    };
    if(caught){try{await construct()}catch{};return undefined}
    return construct();
   },context);
   if(caught)await expect(writing).rejects.toMatchObject({name:"SessionFailed",cause:failure});
   else await expect(writing).rejects.toBe(failure);
   expect(await session.closed).toEqual({kind:"failed",error:failure});
   await session.close(context);
  }
 }
});

it("replays already-rendered section values without implicit tags or stale values",async()=>{
 const {getCurrentSystemPrompt}=await import("@pinned/ai-transcript");
 const ordered=JSON.parse('{"role":"system","content":[],"sections":{"b":"B","a":"A"},"timestamp":0}');
 expect(getCurrentSystemPrompt([ordered])).toBe("B\n\nA");
 const numeric=JSON.parse('{"role":"system","content":[],"sections":{"10":"ten","2":"two","01":"leading","a":"A"},"timestamp":0}');
 expect(getCurrentSystemPrompt([numeric])).toBe("two\n\nten\n\nleading\n\nA");
 expect(getCurrentSystemPrompt([{role:"system",content:"",sections:{z:"before"},timestamp:0},numeric])).toBe("two\n\nten\n\nbefore\n\nleading\n\nA");
 expect(getCurrentSystemPrompt([ordered,{role:"system",content:"",sections:{b:null},timestamp:0},{role:"system",content:"",sections:{b:"second"},timestamp:0}])).toBe("A\n\nsecond");
 for(const [content,want] of [["  ","  "],[[{type:"text",text:""},{type:"text",text:"x"},{type:"text",text:""}],"\nx\n"]] as const){
  expect(getCurrentSystemPrompt([{role:"system",content,timestamp:0}])).toBe(want);
 }
 for(const value of ["plain","<policy>\nrendered\n</policy>",""]){
  expect(getCurrentSystemPrompt([{role:"system",content:"base",sections:{policy:value},timestamp:0}])).toBe(value?"base\n\n"+value:"base");
 }
 expect(getCurrentSystemPrompt([
  {role:"system",content:"",sections:{a:"first"},timestamp:0},
  {role:"system",content:"",sections:{b:"kept"},timestamp:0},
  {role:"system",content:"",sections:{a:null},timestamp:0},
  {role:"system",content:"",sections:{a:"second"},timestamp:0}
 ])).toBe("kept\n\nsecond");
});

it("records requested thinking level without losing response metadata or an unoffered call",async()=>{
 const {MemoryStorage}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const content=[
  {type:"text",text:"text",textSignature:"signature"},
  {type:"thinking",thinking:"thought",thinkingSignature:"",redacted:false},
  {type:"toolCall",id:"call",name:"missing",arguments:{},thoughtSignature:"signed",namespace:""}
 ];
 const setup=chatSetup();
 setup.faux.setResponses([Object.assign(fauxAssistantMessage(content as any,{stopReason:"toolUse",responseId:"response-id"}),{responseModel:"actual-model",providerThinkingLevel:"provider-low",thinkingLevel:"low",rawStopReason:"raw"}),fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 try{
  expect((await(await root.submit({type:"input",content:"question"},context)).wait(context)).status).toBe("done");
  const entries=await allEntries(root),assistant=entries.find(entry=>entry.kind==="pi.assistant")!;
  expect(assistant.model).toEqual([expect.objectContaining({content,responseId:"response-id",responseModel:"actual-model",providerThinkingLevel:"provider-low",thinkingLevel:"off",rawStopReason:"raw"})]);
  const tasks=await harness.commit(tx=>tx.scanTasks({conversationId:root.id,kind:"pi.tool"},20),context);
  expect(tasks.items).toHaveLength(1);expect(tasks.items[0]!.input).toMatchObject({callId:"call",unoffered:true});
  const result=entries.find(entry=>entry.kind==="pi.tool-result")!;
  expect(result.model).toEqual([expect.objectContaining({toolCallId:"call",isError:true})]);
 }finally{await harness.close(context)}
});

it("retains isolated UTF-16 keys and values in a real tool execute checkpoint and hook calls",async()=>{
 const {MemoryStorage,defineTool,ToolTask}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat}=await import("@pinned/chat");
 const {addTool,addHooks}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const key="\ud800",original="\ud800",prepared="\udfff",rewritten="\ud801";
 const entered=Promise.withResolvers<any>(),release=Promise.withResolvers<void>();
 const setup=chatSetup();let before=0,after=0;
 addTool(setup.registry,defineTool({name:"unicode_scope",description:"scope",parameters:{type:"object",additionalProperties:{type:"string",minLength:1,maxLength:1,pattern:"^.$"}}as any,replay:"safe",prepareArguments:args=>{expect(args).toEqual({[key]:original});return{[key]:prepared}},execute:async(args,api)=>{expect(args).toEqual({[key]:rewritten});entered.resolve(api.taskId);await release.promise;return{output:[{type:"text"as const,text:"done"}]}}}));
 addHooks(setup.registry,ToolTask,{beforeTool:call=>{before++;expect(call.arguments).toEqual({[key]:prepared});return{arguments:{[key]:rewritten}}},afterTool:call=>{after++;expect(call.arguments).toEqual({[key]:original})}});
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"unicode",name:"unicode_scope",arguments:{[key]:original}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 try{
  const submission=await root.submit({type:"input",content:"go"},context),id=await entered.promise;
  const record=await harness.commit(tx=>tx.task(id),context);
  expect(record?.state).toEqual({status:"running",checkpoint:{phase:"execute",arguments:{[key]:rewritten},replay:"safe"}});
  expect(JSON.stringify((record!.state as any).checkpoint.arguments)).toBe('{"\\ud800":"\\ud801"}');
  release.resolve();expect((await submission.wait(context)).status).toBe("done");expect([before,after]).toEqual([1,1]);
 }finally{release.resolve();await harness.close(context)}
});

it("attributes invalid model structured output to the phase tool installer, not its rewriting hook",async()=>{
 const {MemoryStorage,defineTool,defineExtension,hook,ToolTask}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat}=await import("@pinned/chat");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 for(const rewrite of [false,true]){
  const setup=chatSetup();let producer:any;
  producer=defineExtension({name:"producer",tools:[defineTool({name:"origin",description:"origin",parameters:{type:"object"}as any,structuredOutputSchema:{type:"integer"}as any,execute:async()=>{setup.registry.uninstall(producer);return{output:[{type:"text"as const,text:"visible"}],structuredOutput:rewrite?1:"invalid"}}})]});
  setup.registry.install(producer);
  if(rewrite)setup.registry.install(defineExtension({name:"replacement",hooks:[hook(ToolTask,{afterTool:(_call,result)=>({...result,structuredOutput:"invalid"})})]}));
  setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"origin",name:"origin",arguments:{}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("done")])]);
  const {harness,root}=await openChat(new MemoryStorage(),setup);
  try{
   expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");
   expect(setup.infos).toEqual([expect.objectContaining({kind:"extension_error",extension:"producer",task:expect.objectContaining({kind:"pi.tool",conversationId:root.id})})]);
  }finally{await harness.close(context)}
 }
});

it("reports a throwing model getter as an adopted generation task fault",async()=>{
 const {Harness,MemoryStorage,createRegistry}=await import("@earendil-works/pi-durable");
 const cause=new Error("model getter failed"),reported=Promise.withResolvers<any>();
 let harness:any;
 harness=await Harness.open(new MemoryStorage(),{registry:createRegistry(),models:()=>{throw cause},onReport:report=>reported.resolve(report)},context);
 try{
  const root=await harness.root(context,{agent:{model:{provider:"faux",modelId:"faux-1"}}});
  const submission=await root.submit({type:"input",content:"go"},context),report=await reported.promise;
  expect(report.error).toBe(cause);expect(report).toMatchObject({kind:"task_faulted",reason:"threw",task:{kind:"pi.generation",conversationId:root.id}});
  const stored=await harness.commit((tx:any)=>tx.task(report.task.id),context);
  expect(stored.state).toMatchObject({status:"terminal",outcome:{status:"faulted"}});
  await submission.wait(context);
 }finally{await harness.close(context)}
});

it("links a model tool outcome to its usage-bearing entry and exact tool ledger",async()=>{
 const {MemoryStorage,defineTool,UsageDoc}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const usage={input:0,output:1,cacheRead:0,cacheWrite:0,totalTokens:1,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}};
 const setup=chatSetup();addTool(setup.registry,defineTool({name:"billed",description:"billed",parameters:{type:"object"}as any,execute:async()=>({output:[{type:"text"as const,text:"5"}],usage})}));
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"billed-call",name:"billed",arguments:{}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 try{
  expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");
  const entries=await allEntries(root),entry=entries.find(entry=>entry.kind==="pi.tool-result")!;
  expect(entry.model).toEqual([expect.objectContaining({toolCallId:"billed-call",toolName:"billed",usage})]);
  const tasks=await harness.commit(tx=>tx.scanTasks({conversationId:root.id,kind:"pi.tool"},20),context);
  expect(tasks.items).toHaveLength(1);expect(tasks.items[0]!.state).toEqual({status:"terminal",outcome:{status:"completed",result:{kind:"model",entryId:entry.id}}});
  expect((await harness.snapshot(UsageDoc,root.id,context))?.tools.billed).toEqual(usage);
 }finally{await harness.close(context)}
});

it("accepts complete parameter schemas and coerces detached tool arguments at use",async()=>{
 const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const cases=[
  [{},{anything:true},{anything:true}],
  [{type:"object",unknown:true},{x:"kept"},{x:"kept"}],
  [{type:"object",required:true,unknown:true},{},{}],
  [{type:"object",properties:{n:{type:"integer"}},required:["n"]},{n:"2"},{n:2}],
  [{type:"object",properties:{n:{$ref:"#/$defs/int"}},required:["n"],$defs:{int:{type:"integer",minimum:1}}},{n:"3"},{n:3}],
  [{type:"object",patternProperties:{"^x":{type:"string"}},additionalProperties:false},{x:4},{x:"4"}],
  [{anyOf:[{type:"object",properties:{n:{type:"integer"}},required:["n"]},{type:"object",properties:{text:{type:"string"}},required:["text"]}]},{n:"5"},{n:5}],
  [{type:"object",properties:{n:{type:["integer","null"]}},required:["n"]},{n:null},{n:null}]
 ];
 for(const [parameters,args,want] of cases){
  const setup=chatSetup();let effects=0;
  addTool(setup.registry,defineTool({name:"schema",description:"schema",parameters:parameters as any,execute:async(input,api,ctx)=>{effects++;expect(input).toEqual(want);const record=await api.commit(tx=>tx.task(api.taskId),ctx);expect((record!.state as any).checkpoint).toEqual({phase:"execute",arguments:want,replay:"unsafe"});(input as any)["host-mutation"]=true;return{output:[{type:"text"as const,text:"done"}]}}}));
  setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"schema",name:"schema",arguments:args as any}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
  const {harness,root}=await openChat(new MemoryStorage(),setup);
  try{
   expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects).toBe(1);expect(setup.faux.state.callCount).toBe(2);
   const entries=await allEntries(root),assistant=entries.find(entry=>entry.kind==="pi.assistant")!;
   expect(assistant.model?.[0].content).toEqual([{type:"toolCall",id:"schema",name:"schema",arguments:args}]);
  }finally{await harness.close(context)}
 }
});

it("defers parameter schema failures to an own invalid-arguments tool result",async()=>{
 const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 for(const [parameters,args] of [
  [{type:"object",required:["absent"]},{}],
  [{type:"object",properties:{x:{type:"string",pattern:"["}}},{x:"value"}],
  [{type:"object",properties:{n:{type:"number",minimum:10,maximum:1}}},{n:2}]
 ]){
  const setup=chatSetup();let effects=0;
  addTool(setup.registry,defineTool({name:"invalid_schema",description:"invalid",parameters:parameters as any,execute:async()=>{effects++;return{output:[]}}}));
  setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"invalid-schema",name:"invalid_schema",arguments:args as any}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
  const {harness,root}=await openChat(new MemoryStorage(),setup);
  try{
   expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects,JSON.stringify(parameters)).toBe(0);
   const entries=await allEntries(root),entry=entries.find(entry=>entry.kind==="pi.tool-result")!;
   expect(entry.model).toEqual([expect.objectContaining({toolCallId:"invalid-schema",isError:true})]);expect(entry.data).toMatchObject({diagnostics:[{code:"invalid_arguments"}]});
  }finally{await harness.close(context)}
 }
});

// Public source value-union assertions; native execution was red before refactor.
it("coerces an object call to an array parameter value",async()=>{
 const {MemoryStorage,defineTool,ToolTask}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool,addHooks}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText,validateToolArguments}=await import("@reference/pi-ai");
 const original={n:"2"};
 const parsed=validateToolArguments({name:"array",description:"array",parameters:{type:"array",items:{type:"object",properties:{n:{type:"integer"}},required:["n"]}}}as any,{type:"toolCall",id:"direct",name:"array",arguments:original});
 expect(parsed).toEqual([{n:2}]);(parsed as any)[0]["host-mutation"]=true;expect(original).toEqual({n:"2"});
 for(const parameters of [{},{type:"array"}]){
  const input={free:{value:"original"}},result=validateToolArguments({name:"clone",description:"clone",parameters}as any,{type:"toolCall",id:"clone",name:"clone",arguments:input});
  (Array.isArray(result)?result[0]:result).free.value="host mutation";expect(input).toEqual({free:{value:"original"}});
 }
 const setup=chatSetup();let effects=0,before=0,after=0;
 addTool(setup.registry,defineTool({name:"array_schema",description:"array",parameters:{type:"array"}as any,execute:async(args,api,ctx)=>{
  effects++;expect(args).toEqual([{}]);const record=await api.commit(tx=>tx.task(api.taskId),ctx);
  expect((record!.state as any).checkpoint).toEqual({phase:"execute",arguments:[{}],replay:"unsafe"});
  (args as any)[0]="host mutation";return{output:[]}
 }}));
 addHooks(setup.registry,ToolTask,{beforeTool:call=>{before++;expect(call.arguments).toEqual([{}])},afterTool:call=>{after++;expect(call.arguments).toEqual({})}});
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"array",name:"array_schema",arguments:{}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 try{
  expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects).toBe(1);expect(before).toBe(1);expect(after).toBe(1);expect(setup.faux.state.callCount).toBe(2);
  const entries=await allEntries(root),assistant=entries.find(entry=>entry.kind==="pi.assistant")!;
  expect(assistant.model?.[0].content).toEqual([{type:"toolCall",id:"array",name:"array_schema",arguments:{}}]);
  expect(entries.filter(entry=>entry.kind==="pi.tool-result")).toHaveLength(1);
 }finally{await harness.close(context)}
});

it("checks string formats without replacing isolated UTF-16 values",async()=>{
 const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 for(const [format,valid] of [["uuid",false],["regex",true]]as const){
  const setup=chatSetup();let effects=0;
  addTool(setup.registry,defineTool({name:"utf16_format",description:"format",parameters:{type:"object",properties:{text:{type:"string",format}}}as any,execute:async(args)=>{effects++;expect(args.text).toBe("\ud800");return{output:[]}}}));
  setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"format",name:"utf16_format",arguments:{text:"\ud800"}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
  const {harness,root}=await openChat(new MemoryStorage(),setup);
  try{expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects).toBe(valid?1:0)}finally{await harness.close(context)}
 }
});

it.each([false,true])("replays a coerced UTF-16 array checkpoint without repeating preparation or hooks (throwing=%s)",async(throwing)=>{
 const {defineTool,ToolTask}=await import("@earendil-works/pi-durable");
 const {openNodeSqliteStorage}=await import("@earendil-works/pi-durable/storage/sqlite/node");
 const {mkdtemp,rm}=await import("node:fs/promises"),{tmpdir}=await import("node:os"),{join}=await import("node:path");
 const directory=await mkdtemp(join(tmpdir(),"array-replay-")),path=join(directory,"state.sqlite");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool,addHooks}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const key="\ud800",original="\ud800",prepared="\udfff",rewritten="\ud801",entered=Promise.withResolvers<void>();
 const setup=chatSetup();let effects=0,prepares=0,before=0,after=0,taskId:any;
 addTool(setup.registry,defineTool({name:"array_replay",description:"array",replay:"safe",parameters:{type:"array",items:{type:"object",additionalProperties:{type:"string",minLength:1,maxLength:1,pattern:"^.$"}}}as any,
  prepareArguments:args=>{prepares++;expect(args).toEqual({[key]:original});return{[key]:prepared}},
  execute:async(args,api,ctx)=>{
   effects++;taskId=api.taskId;expect(args).toEqual([{[key]:rewritten}]);expect(JSON.stringify(args)).toBe('[{"\\ud800":"\\ud801"}]');
   const record=await api.commit(tx=>tx.task(api.taskId),ctx);expect((record!.state as any).checkpoint).toEqual({phase:"execute",arguments:[{[key]:rewritten}],replay:"safe"});expect(JSON.stringify((record!.state as any).checkpoint.arguments)).toBe('[{"\\ud800":"\\ud801"}]');
   (args as any)[0]="host mutation";
   if(effects===1){entered.resolve();await new Promise<void>(resolve=>{const signal=ctx.abortSignal!;if(signal.aborted)resolve();else signal.addEventListener("abort",()=>resolve(),{once:true})});if(throwing)throw new Error("aborted tool")}
   return{output:[]}
  }
 }));
 addHooks(setup.registry,ToolTask,{beforeTool:call=>{before++;expect(call.arguments).toEqual([{[key]:prepared}]);return{arguments:[{[key]:rewritten}]as any}},afterTool:call=>{after++;expect(call.arguments).toEqual({[key]:original})}});
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"array-replay",name:"array_replay",arguments:{[key]:original}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
 let opened=await openChat(await openNodeSqliteStorage(path),setup);
 const submission=await opened.root.submit({type:"input",content:"go"},context);
 try{
  await entered.promise;
  const record=await opened.harness.getTask(taskId,context);expect((record!.state as any).checkpoint).toEqual({phase:"execute",arguments:[{[key]:rewritten}],replay:"safe"});
  await opened.harness.close(context);opened=await openChat(await openNodeSqliteStorage(path),setup);
  expect((await(await opened.harness.submission(submission.id,context))!.wait(context)).status).toBe("done");
  expect({effects,prepares,before,after,requests:setup.faux.state.callCount}).toEqual({effects:2,prepares:1,before:1,after:throwing?1:2,requests:2});
  const assistant=(await allEntries(opened.root)).find(entry=>entry.kind==="pi.assistant")!;
  expect(assistant.model?.[0].content).toEqual([{type:"toolCall",id:"array-replay",name:"array_replay",arguments:{[key]:original}}]);
 }finally{await opened.harness.close(context);await rm(directory,{recursive:true,force:true})}
});

it("selects a current tool before execution and rolls back a throwing public commit with bounded partial output",async()=>{
 const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const setup=chatSetup(),entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>();let oldCalls=0,newCalls=0;
 addTool(setup.registry,defineTool({name:"atomic_pin",description:"pin",parameters:{type:"object"}as any,execute:async()=>{oldCalls++;return{output:[]}}}));
 setup.faux.setResponses([async()=>{entered.resolve();await release.promise;return fauxAssistantMessage([{type:"toolCall",id:"atomic",name:"atomic_pin",arguments:{}}],{stopReason:"toolUse"})},fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 try{
  const submission=await root.submit({type:"input",content:"go"},context);await entered.promise;
  addTool(setup.registry,defineTool({name:"atomic_pin",description:"pin",parameters:{type:"object"}as any,execute:async(_args,api,ctx)=>{
   newCalls++;api.output("x".repeat(50*1024+1));await api.commit(async tx=>{await tx.appendEntry(api.conversationId,{kind:"test.must-rollback"});throw new Error("applicationfailure")},ctx);return{output:[]}
  }}));release.resolve();
  expect((await submission.wait(context)).status).toBe("done");expect({oldCalls,newCalls,requests:setup.faux.state.callCount}).toEqual({oldCalls:0,newCalls:1,requests:2});
  const entries=await allEntries(root);expect(entries.filter(entry=>entry.kind==="test.must-rollback")).toHaveLength(0);
  const result=entries.filter(entry=>entry.kind==="pi.tool-result");expect(result).toHaveLength(1);
  const tasks=await harness.commit(tx=>tx.scanTasks({conversationId:root.id,kind:"pi.tool"},20),context);expect(tasks.items).toHaveLength(1);
  expect(tasks.items[0].state).toEqual({status:"terminal",outcome:{status:"failed",error:{message:"Tool atomic_pin threw"},result:{kind:"model",entryId:result[0].id}}});
  expect(result[0].byTaskId).toBe(tasks.items[0].id);
  expect((result[0].data as any).diagnostics).toEqual([{severity:"error",code:"tool_error",message:"applicationfailure"},{severity:"warn",code:"truncated",message:"Output truncated to its beginning: 0 lines, 1 bytes dropped"}]);
  expect(result[0].model).toEqual([expect.objectContaining({toolCallId:"atomic",toolName:"atomic_pin",isError:true,content:[{type:"text",text:"x".repeat(50*1024)},{type:"text",text:"<harness>\n[error] applicationfailure\n[warn] Output truncated to its beginning: 0 lines, 1 bytes dropped\n</harness>"}]})]);
 }finally{release.resolve();await harness.close(context)}
});

it.each(["dependent","unload","close"])("retains held operations when a deferred checkpoint selected a storage base (%s)",async(finish)=>{
 const session=new SessionImpl(new ControlledStorage(),Date.now,()=>600000);
 const conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
 const Doc=defineDoc({kind:"oracle.public.held-base",scope:"conversation",history:"latest",fork:"initial",version:1,initial:()=>({text:"",base:false}),checkpointWhen:(value:any)=>value.base===true});
 await session.commit(async tx=>{await tx.doc(Doc,conversation.id)},context);
 const seen:any[]=[];const guard=session.observeCommits({advance:p=>seen.push(p),end:()=>{}});
 try{
  const deferred={persistence:{kind:"deferred"as const,withinMs:Infinity}};
  await session.commit(async tx=>{const doc=await tx.doc(Doc,conversation.id);doc.text="x";doc.base=true},context,deferred);
  await session.commit(async tx=>{(await tx.doc(Doc,conversation.id)).text+="y"},context,deferred);
  expect(seen).toHaveLength(0);
  const value=await session.snapshot(Doc,conversation.id,context);expect(value).toEqual({text:"xy",base:true});
  if(finish==="dependent")await session.commit(async tx=>{await tx.doc(Doc,conversation.id);await tx.appendEntry(conversation.id,{kind:"note"})},context);else if(finish==="unload")await session.unloadDocuments();else await session.close(context);
  expect(seen).toHaveLength(1);const changes=seen[0].changes.filter((change:any)=>change.type==="document");expect(changes).toHaveLength(1);
  expect(changes[0].value).toBe(value);expect(changes[0].ops).toEqual([["a",["text"],"xy"],["s",["base"],true]]);
 }finally{guard.detach();await session.close(context)}
});

it("keeps abort ownership until a late model response returns and starts no offered tool",async()=>{
 const {MemoryStorage,defineTool,UsageDoc}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage}=await import("@reference/pi-ai");
 const setup=chatSetup(),entered=Promise.withResolvers<void>(),release=Promise.withResolvers<void>(),marked=Promise.withResolvers<any>();let effects=0;
 addTool(setup.registry,defineTool({name:"boundary_effect",description:"effect",parameters:{type:"object"}as any,execute:async()=>{effects++;return{output:[]}}}));
 setup.faux.setResponses([async()=>{entered.resolve();await release.promise;return fauxAssistantMessage([{type:"toolCall",id:"boundary-call",name:"boundary_effect",arguments:{}}],{stopReason:"toolUse"})}]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);
 const guard=harness.observeCommits({advance:frame=>{for(const change of frame.changes)if(change.type==="task"&&change.value.kind==="pi.generation"&&change.value.abortRequested)marked.resolve(change.value)},end:()=>{}});
 try{
  const submission=await root.submit({type:"input",content:"go"},context);await entered.promise;
  const controller=new AbortController(),gone=new Error("caller stopped waiting");
  const abort=root.abort({...context,abortSignal:controller.signal}).then(()=>({resolved:true}),error=>({error}));
  const record=await marked.promise;expect(record.state.status).not.toBe("terminal");controller.abort(gone);expect(await abort).toEqual({error:gone});
  expect((await harness.getTask(record.id,context))?.state.status).not.toBe("terminal");
  release.resolve();
  expect(await submission.wait(context)).toMatchObject({status:"unanswered",reason:"aborted"});expect(effects).toBe(0);expect(setup.faux.state.callCount).toBe(1);
  expect((await harness.getTask(record.id,context))?.state).toEqual({status:"terminal",outcome:{status:"aborted"}});
  const tasks=await harness.commit(tx=>tx.scanTasks({conversationId:root.id,kind:"pi.tool"},20),context);expect(tasks.items).toHaveLength(0);
  const entries=await allEntries(root);expect(entries.filter(entry=>entry.kind==="pi.assistant"||entry.kind==="pi.tool-result")).toHaveLength(0);
  expect((await harness.snapshot(UsageDoc,root.id,context))?.models).toEqual({});
 }finally{release.resolve();guard.detach();await harness.close(context)}
});

it("joins an aborted noncooperative tool before its owner without adopting its late returned usage",async()=>{
 const {MemoryStorage,defineTool,UsageDoc}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage}=await import("@reference/pi-ai");
 const setup=chatSetup(),entered=Promise.withResolvers<any>(),release=Promise.withResolvers<void>(),marked=Promise.withResolvers<void>();let effects=0,staged=0;
 const usage={input:1,output:0,cacheRead:0,cacheWrite:0,totalTokens:1,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}};
 addTool(setup.registry,defineTool({name:"owned_block",description:"block",parameters:{type:"object"}as any,execute:async(_args,api,ctx)=>{effects++;api.output("retained prefix");await api.details({retained:true},ctx);entered.resolve(api.taskId);await release.promise;try{await api.commit(async tx=>{staged++;return tx.appendEntry(api.conversationId,{kind:"test.late-abort-effect"})},ctx)}catch{}return{output:[{type:"text"as const,text:"late"}],usage}}}));
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"owned-call",name:"owned_block",arguments:{}}],{stopReason:"toolUse"})]);
 const {harness,root}=await openChat(new MemoryStorage(),setup);const seen=new Set<string>(),terminal:any[]=[];
 const guard=harness.observeCommits({advance:frame=>{for(const change of frame.changes)if(change.type==="task"&&(change.value.kind==="pi.tool"||change.value.kind==="pi.generation")){if(change.value.abortRequested)seen.add(change.value.kind);if(change.value.state.status==="terminal")terminal.push({id:change.value.id,kind:change.value.kind,seq:frame.seq});}if(seen.size===2)marked.resolve()},end:()=>{}});
 try{
  const submission=await root.submit({type:"input",content:"go"},context),toolId=await entered.promise;
  const controller=new AbortController(),gone=new Error("caller stopped waiting");const abort=root.abort({...context,abortSignal:controller.signal}).then(()=>({resolved:true}),error=>({error}));
  await marked.promise;controller.abort(gone);expect(await abort).toEqual({error:gone});
  const tasks=await harness.commit(tx=>tx.scanTasks({conversationId:root.id},20),context),generation=tasks.items.find(task=>task.kind==="pi.generation")!;
  expect(generation.abortRequested).toBe(true);expect(generation.state.status).toBe("waiting");expect((generation.state as any).checkpoint.phase).toBe("tools");expect((await harness.getTask(toolId,context))?.state.status).not.toBe("terminal");
  release.resolve();expect(await submission.wait(context)).toMatchObject({status:"unanswered",reason:"aborted"});expect(effects).toBe(1);expect(staged).toBe(0);
  expect(terminal.map(task=>task.kind)).toEqual(["pi.tool","pi.generation"]);expect(terminal[0].seq).toBeLessThan(terminal[1].seq);
  const entries=await allEntries(root),results=entries.filter(entry=>entry.kind==="pi.tool-result");expect(results).toHaveLength(1);expect(entries.filter(entry=>entry.kind==="test.late-abort-effect")).toHaveLength(0);
  expect(results[0].model).toEqual([expect.objectContaining({toolCallId:"owned-call",toolName:"owned_block",isError:true,content:[{type:"text",text:"retained prefix"},{type:"text",text:"<harness>\n[error] Tool owned_block was aborted\n</harness>"}],details:{retained:true}})]);
  expect((results[0].data as any).diagnostics).toEqual([{severity:"error",code:"aborted",message:"Tool owned_block was aborted"}]);
  expect(results[0].model![0]).not.toHaveProperty("usage");expect((await harness.snapshot(UsageDoc,root.id,context))?.tools).not.toHaveProperty("owned_block");
  expect((await harness.getTask(toolId,context))?.state).toEqual({status:"terminal",outcome:{status:"aborted",result:{kind:"model",entryId:results[0].id}}});
  expect((await harness.getTask(generation.id,context))?.state).toEqual({status:"terminal",outcome:{status:"aborted"}});
 }finally{release.resolve();guard.detach();await harness.close(context)}
});

it("uses a public-only Storage decorator for reservations, commits, refusal and close",async()=>{
 const {openNodeSqliteStorage}=await import("@earendil-works/pi-durable/storage/sqlite/node");
 const {mkdtemp,rm}=await import("node:fs/promises"),{tmpdir}=await import("node:os"),{join}=await import("node:path");
 const directory=await mkdtemp(join(tmpdir(),"storage-wrapper-")),path=join(directory,"state.sqlite");
 const raw=await openNodeSqliteStorage(path);let mints=0,commits=0,closes=0,refusal:any,conversation:any,entry:any;
 const storage=new Proxy(raw,{get(target,key){if(key==="mintId")return async()=>{mints++;return target.mintId()};if(key==="commit")return async(...args:any[])=>{commits++;if(refusal!==undefined)return{stored:0,refused:refusal};return(target.commit as any)(...args)};if(key==="close")return async(...args:any[])=>{closes++;return(target.close as any)(...args)};const value=Reflect.get(target,key);return typeof value==="function"?value.bind(target):value}});
 const session=new SessionImpl(storage,Date.now,()=>600000);
 try{
  conversation=await session.commit(tx=>tx.createConversation({ownership:{kind:"ownerless"}}),context);
  entry=await session.commit(tx=>tx.appendEntry(conversation.id,{kind:"note",data:{kept:true}}),context);
  expect((await raw.entry(entry.id,context))?.entry).toMatchObject({kind:"note",data:{kept:true}});expect({mints,commits}).toEqual({mints:2,commits:2});
  const cause=new Error("decorator refuses this write");refusal=cause;
  await expect(session.commit(tx=>tx.appendEntry(conversation.id,{kind:"must-not-store"}),context)).rejects.toBe(cause);
 }finally{await session.close(context)}
 expect(closes).toBe(1);
 const reopened=await openNodeSqliteStorage(path);
 try{expect((await reopened.scanEntries({conversationId:conversation.id},10,undefined,context)).items.map(entry=>entry.id)).toEqual([entry.id])}finally{await reopened.close(context);await rm(directory,{recursive:true,force:true})}
});

it("executes a model/tool chain through a public Storage decorator",async()=>{
 const {MemoryStorage,defineTool}=await import("@earendil-works/pi-durable");
 const {chatSetup,openChat,allEntries}=await import("@pinned/chat"),{addTool}=await import("@pinned/harness-support");
 const {fauxAssistantMessage,fauxText}=await import("@reference/pi-ai");
 const raw=new MemoryStorage();let mints=0,commits=0,effects=0;
 const storage=new Proxy(raw,{get(target,key){if(key==="mintId")return async()=>{mints++;return target.mintId()};if(key==="commit")return async(...args:any[])=>{commits++;return(target.commit as any)(...args)};const value=Reflect.get(target,key);return typeof value==="function"?value.bind(target):value}});
 const setup=chatSetup();addTool(setup.registry,defineTool({name:"wrapped",description:"wrapped",parameters:{type:"object"}as any,execute:async()=>{effects++;return{output:[{type:"text"as const,text:"wrapped result"}]}}}));
 setup.faux.setResponses([fauxAssistantMessage([{type:"toolCall",id:"wrapped-call",name:"wrapped",arguments:{}}],{stopReason:"toolUse"}),fauxAssistantMessage([fauxText("answer")])]);
 const {harness,root}=await openChat(storage,setup);
 try{expect((await(await root.submit({type:"input",content:"go"},context)).wait(context)).status).toBe("done");expect(effects).toBe(1);expect(setup.faux.state.callCount).toBe(2);expect(mints).toBeGreaterThan(0);expect(commits).toBeGreaterThan(0);expect(await allEntries(root)).toHaveLength(5)}finally{await harness.close(context)}
});
