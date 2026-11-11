import test from 'node:test';
import assert from 'node:assert/strict';
import { createRuntimeController, runtimeState, createMemoryClearController } from '../src/runtime-control.ts';
const runtime = (extra: any = {}) => ({ deviceId: 'd', connection: 'online', seenAt: '2026-11-11T00:00:00Z', boot: 'b', volume: 30, brightness: null, canControl: true, command: null, ...extra });
const deferred = () => { let resolve!: (x:any)=>void; const promise = new Promise<any>(r=>resolve=r); return {promise,resolve}; };
function setup() {
  let now=0, callback=()=>{}; let reads=0, writes=0;
  let next:any=runtime();
  const state=runtimeState();
  const api={ getDeviceRuntime: async()=>{reads++; return typeof next==='function'?next():next;}, controlDevice: async(_id:any,value:any)=>{writes++; return runtime({command:{id:'c',...value,status:'queued',actual:null}});} };
  const c=createRuntimeController(api,state,{ now:()=>now, every:(fn)=>{callback=fn;return 1;}, cancel:()=>{callback=()=>{};} });
  return {state,c,set:(x:any)=>next=x,tick:async()=>{callback();await new Promise(r=>setImmediate(r));},clock:(n:number)=>now=n,reads:()=>reads,writes:()=>writes};
}
test('unknown remains null, offline and missing permission never submit',async()=>{
 const p=setup(); p.set(runtime({connection:'unknown',volume:null,canControl:false})); await p.c.start('d','admin');
 assert.equal(p.state.volume,null); await p.c.submit('volume',20); assert.equal(p.writes(),0);
 p.set(runtime({connection:'offline'})); await p.c.refresh(); await p.c.submit('volume',20);assert.equal(p.writes(),0);
});
test('input only previews; change submits once, refresh preserves edit, matching confirmed actual required',async()=>{
 const p=setup();await p.c.start('d','admin');p.c.preview('volume',60);await p.c.refresh();assert.equal(p.state.volume,60);assert.equal(p.writes(),0);
 await Promise.all([p.c.submit('volume',60),p.c.submit('volume',60)]);assert.equal(p.writes(),1);assert.equal(p.state.pending,true);
 p.set(runtime({volume:60,command:{id:'c',field:'volume',value:60,status:'confirmed',actual:50}}));await p.c.refresh();assert.doesNotMatch(p.state.feedback,/已生效/);
 p.set(runtime({volume:60,command:{id:'c',field:'volume',value:60,status:'confirmed',actual:60}}));await p.c.refresh();assert.match(p.state.feedback,/已生效/);assert.equal(p.state.pending,false);
});
test('newer command or boot cannot confirm previous command and deadline stops observations',async()=>{
 const p=setup();await p.c.start('d','family');await p.c.submit('volume',60);
 p.set(runtime({command:{id:'newer',field:'volume',value:60,status:'confirmed',actual:60}}));await p.c.refresh();assert.doesNotMatch(p.state.feedback,/已生效/);assert.equal(p.state.pending,false);
 await p.c.submit('volume',60);p.clock(50001);await p.tick();assert.equal(p.state.pending,false);assert.match(p.state.feedback,/未知/);
});
test('stop clears state and rejects late runtime and write responses',async()=>{
 const p=setup();await p.c.start('d','admin');const wait=deferred();p.set(()=>wait.promise);const pending=p.c.refresh();p.c.stop();wait.resolve(runtime());await pending;assert.equal(p.state.runtime,null);assert.equal(p.state.volume,null);const before=p.reads();await p.tick();assert.equal(p.reads(),before);
});
test('memory clear cancellation, double click, stale confirmation and confirmed refresh',async()=>{
 let writes=0,refresh=0;const state={pending:false,feedback:''};const wait=deferred();
 const c=createMemoryClearController({clearMemories:async()=>{writes++;}},state,()=>{refresh++;});
 c.authorize('d','admin',true);await c.clear(()=>false);assert.equal(writes,0);
 const pending=c.clear(()=>wait.promise);await c.clear(()=>true);c.invalidate();wait.resolve(true);await pending;assert.equal(writes,0);
 c.authorize('d','admin',true);await c.clear(()=>true);assert.equal(writes,1);assert.equal(refresh,1);assert.match(state.feedback,/聊天记录保留/);
});

test('confirmed response arriving past deadline never reports success',async()=>{
 const p=setup();await p.c.start('d','admin');await p.c.submit('volume',60);
 const wait=deferred();p.set(()=>wait.promise);const pending=p.c.refresh();p.clock(50001);
 wait.resolve(runtime({volume:60,command:{id:'c',field:'volume',value:60,status:'confirmed',actual:60}}));await pending;
 assert.doesNotMatch(p.state.feedback,/已生效/);assert.equal(p.state.pending,false);assert.match(p.state.feedback,/未知/);
});
test('forbidden memory read revokes clear and cancellation produces zero writes',async()=>{
 let writes=0;const s={pending:false,feedback:''};const c=createMemoryClearController({clearMemories:async()=>{writes++;}},s,()=>{});
 c.authorize('d','family',false);await c.clear(()=>true);assert.equal(writes,0);
 c.authorize('d','family',true);c.invalidate();await c.clear(()=>true);assert.equal(writes,0);
});
test('a failed refresh does not forget an unresolved command and allow a second write',async()=>{
 const p=setup();await p.c.start('d','admin');await p.c.submit('volume',60);
 p.set(()=>Promise.reject(new Error('network unavailable')));await p.c.refresh();
 p.set(runtime());await p.c.refresh();await p.c.submit('volume',70);assert.equal(p.writes(),1);
});

test('unresolved PUT expires independently and late confirmed PUT cannot revive success',async()=>{
 let tick=()=>{},now=0;const wait=deferred();const state=runtimeState();
 const c=createRuntimeController({getDeviceRuntime:async()=>runtime(),controlDevice:async()=>wait.promise},state,{now:()=>now,every:fn=>{tick=fn;return 1;},cancel:()=>{}});
 await c.start('d','admin');const pending=c.submit('volume',60);now=50001;tick();await new Promise(r=>setImmediate(r));assert.equal(state.pending,false);assert.match(state.feedback,/未知/);
 wait.resolve(runtime({volume:60,command:{id:'c',field:'volume',value:60,status:'confirmed',actual:60}}));await pending;assert.doesNotMatch(state.feedback,/已生效/);c.stop();
});
test('unresolved observation GET cannot prevent deadline or revive a timed out command',async()=>{
 const p=setup();await p.c.start('d','admin');await p.c.submit('volume',60);const wait=deferred();p.set(()=>wait.promise);const reading=p.c.refresh();p.clock(50001);await p.tick();assert.equal(p.state.pending,false);assert.match(p.state.feedback,/未知/);
 wait.resolve(runtime({volume:60,command:{id:'c',field:'volume',value:60,status:'confirmed',actual:60}}));await reading;assert.doesNotMatch(p.state.feedback,/已生效/);
});
test('unresolved clear transport becomes unknown and cannot repeat until a fresh permission read',{timeout:1000},async()=>{
 let expire=()=>{},writes=0;const wait=deferred();const s={pending:false,feedback:''};
 const c=createMemoryClearController({clearMemories:async()=>{writes++;return wait.promise;}},s,()=>{}, {after:fn=>{expire=fn;return 1;},cancel:()=>{}});
 c.authorize('d','admin',true);const pending=c.clear(()=>true);await new Promise(r=>setImmediate(r));expire();await pending;assert.equal(s.pending,false);assert.match(s.feedback,/未知/);await c.clear(()=>true);assert.equal(writes,1);wait.resolve(undefined);assert.doesNotMatch(s.feedback,/已清除/);
});
test('existing queued command from initial GET blocks additional submit',async()=>{
 const p=setup();p.set(runtime({command:{id:'another',field:'brightness',value:80,status:'sent',actual:null}}));await p.c.start('d','admin');await p.c.submit('volume',60);assert.equal(p.writes(),0);assert.doesNotMatch(p.state.feedback,/已生效/);p.c.stop();
});
