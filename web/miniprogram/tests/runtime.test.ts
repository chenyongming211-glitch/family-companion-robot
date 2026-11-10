import test from 'node:test';
import assert from 'node:assert/strict';
import { validateRuntime, commandOutcome } from '../src/runtime.ts';
const id='dev_'+ 'a'.repeat(32);
const dto=()=>({deviceId:id,connection:'online',seenAt:'2026-11-10T12:00:00Z',boot:'b'.repeat(32),volume:20,brightness:null,canControl:true,command:null});
test('runtime validates identity, integer capabilities and command protocol',()=>{
 assert.equal(validateRuntime(dto(),id).volume,20);
 for(const patch of [{deviceId:'dev_'+ 'c'.repeat(32)},{volume:1.2},{volume:101},{boot:'x'},{connection:'connected'},{canControl:1},{seenAt:'bad'},{command:{id:'bad'}}]) assert.throws(()=>validateRuntime({...dto(),...patch},id));
});
test('only matching confirmed actual is success',()=>{
 const pending={id:'cmd_'+ 'c'.repeat(32),field:'volume',value:40};
 const command={...pending,status:'confirmed',actual:40,expiresAt:'2026-11-10T12:00:45Z'};
 assert.equal(commandOutcome(command,pending),'已生效，实际值 40');
 for(const patch of [{id:'cmd_'+ 'd'.repeat(32)},{field:'brightness'},{value:41},{actual:39},{status:'sent'}]) assert.notEqual(commandOutcome({...command,...patch},pending),'已生效，实际值 40');
});
import { familyPage } from '../src/family-page.ts';
function page() {
 const p:any=familyPage('settings'); p.data={...p.data}; p.setData=(v:any)=>Object.assign(p.data,v); p.active=true;p.deviceId=id;p.data.loggedIn=true;p.data.runtime=dto();return p;
}
test('telemetry refresh preserves nickname location and slider drafts',async()=>{
 const p=page();p.data.name='正在编辑';p.data.location='书房';p.data.volumeDraft=67;p.api={request:async()=>dto()};
 await p.refreshRuntime();assert.equal(p.data.name,'正在编辑');assert.equal(p.data.location,'书房');assert.equal(p.data.volumeDraft,67);
});
test('hidden runtime clears values and rejects late telemetry',async()=>{
 const p=page();let finish:any;p.api={request:()=>new Promise(r=>finish=r),invalidatePending(){}};
 const work=p.refreshRuntime();p.onHide();finish(dto());await work;
 assert.equal(p.data.runtime,null);assert.equal(p.runtimeTimer,undefined);assert.equal(p.commandTimer,undefined);
});
test('network PUT failure never retries command and refreshes state',async()=>{
 const p=page();let puts=0;p.api={request:async(_p:string,m:string)=>{if(m==='PUT'){puts++;throw Object.assign(new Error('network'),{code:'network_error'});}return dto();}};
 await p.control({currentTarget:{dataset:{field:'volume'}}});assert.equal(puts,1);assert.match(p.data.controlNotice,/未确认/);assert.equal(p.data.runtime.volume,20);
});
test('offline unknown no capability and no permission cannot submit',async()=>{
 for(const patch of [{connection:'offline'},{connection:'unknown'},{volume:null},{canControl:false}]) {
 const p=page();p.data.runtime={...dto(),...patch};let calls=0;p.api={request:async()=>{calls++;}};
 await p.control({currentTarget:{dataset:{field:'volume'}}});assert.equal(calls,0);
 }
});
test('queued PUT is never optimistic success, matching receipt confirms actual',async()=>{
 const p=page();p.data.volumeDraft=40;let n=0;
 const c={id:'cmd_'+ 'e'.repeat(32),field:'volume',value:40,status:'queued',actual:null,expiresAt:'2026-11-10T12:00:45Z'};
 p.api={request:async()=>({...dto(),volume:++n===1?20:40,command:n===1?c:{...c,status:'confirmed',actual:40}})};
 await p.control({currentTarget:{dataset:{field:'volume'}}});assert.equal(n,2);assert.match(p.data.controlNotice,/已生效/);assert.equal(p.data.runtime.volume,40);
});
import { Api } from '../src/api.ts';
test('runtime 409 errors are sanitized and distinct from binding/settings conflicts',async()=>{
 for(const [code,text] of [['runtime_busy','待处理命令'],['runtime_offline','离线'],['runtime_boot_changed','重启'],['injected secret','状态已变化']]) {
 const api=new Api({request:(o:any)=>o.success({statusCode:409,data:{code,message:'secret'}})});
 await assert.rejects(api.request('/device-runtime/'+id,'PUT',{}),new RegExp(text));
 }
});
test('late PUT receipt after hide cannot restore values or start polling',async()=>{
 const p=page();let finish:any;p.api={request:()=>new Promise(r=>finish=r),invalidatePending(){}};
 const work=p.control({currentTarget:{dataset:{field:'volume'}}});p.onHide();finish({...dto(),command:null});await work;
 assert.equal(p.data.runtime,null);assert.equal(p.data.controlling,false);assert.equal(p.pendingCommand,null);assert.equal(p.commandTimer,undefined);
});
test('unrelated confirmed command never reports submission success',async()=>{
 const p=page();p.pendingCommand={id:'cmd_'+ 'a'.repeat(32),field:'volume',value:40};p.commandDeadline=Date.now()+50000;
 p.api={request:async()=>({...dto(),command:{id:'cmd_'+ 'b'.repeat(32),field:'volume',value:40,status:'confirmed',actual:40,expiresAt:'2026-11-10T12:00:45Z'}})};
 await p.observeCommand(p.epoch);assert.match(p.data.controlNotice,/未确认/);assert.equal(p.data.controlling,false);
});
test('50 second deadline ends pending GET immediately and ignores late confirmed receipt',async(t)=>{
 t.mock.timers.enable({apis:['setTimeout','Date'],now:Date.parse('2026-11-10T12:00:00Z')});
 const p=page();p.data.volumeDraft=40;let finish:any;
 const c={id:'cmd_'+ 'e'.repeat(32),field:'volume',value:40,status:'queued',actual:null,expiresAt:'2026-11-10T12:00:45Z'};
 p.api={request:async(_path:string,method:string)=>method==='PUT'?{...dto(),command:c}:new Promise(r=>finish=r)};
 const work=p.control({currentTarget:{dataset:{field:'volume'}}});await Promise.resolve();await Promise.resolve();
 const lateReceipt=finish;
 t.mock.timers.tick(50000);
 assert.equal(p.data.controlling,false);assert.match(p.data.controlNotice,/未确认/);
 lateReceipt({...dto(),volume:40,command:{...c,status:'confirmed',actual:40}});await work;
 assert.doesNotMatch(p.data.controlNotice,/已生效/);assert.equal(p.data.runtime.volume,20);
});
test('old telemetry cannot overwrite confirmed command and refresh does not starve observation',async()=>{
 const p=page();p.data.volumeDraft=40;let old:any,receipt:any;
 const c={id:'cmd_'+ 'e'.repeat(32),field:'volume',value:40,status:'queued',actual:null,expiresAt:'2026-11-10T12:00:45Z'};
 let gets=0;p.api={request:async(_path:string,method:string)=>method==='PUT'?{...dto(),command:c}:new Promise(r=>{if(++gets===1)old=r;else receipt=r;})};
 const telemetry=p.refreshRuntime();const control=p.control({currentTarget:{dataset:{field:'volume'}}});await Promise.resolve();await Promise.resolve();
 await p.refreshRuntime();assert.equal(gets,2);
 receipt({...dto(),volume:40,command:{...c,status:'confirmed',actual:40}});await control;
 old({...dto(),connection:'offline',volume:null,brightness:null});await telemetry;
 assert.equal(p.data.runtime.volume,40);assert.equal(p.data.runtime.connection,'online');assert.match(p.data.controlNotice,/已生效/);
});

const sliderEvent=(field:string,value:unknown)=>({currentTarget:{dataset:{field}},detail:{value}});
const receipt=(field:string,value:number,status='confirmed')=>({...dto(),brightness:30,[field]:status==='confirmed'?value:20,command:{id:'cmd_'+'e'.repeat(32),field,value,status,actual:status==='confirmed'?value:null,expiresAt:'2026-11-10T12:00:45Z'}});
test('changing previews whole tens without requesting; release submits once and names the control',async()=>{
 for(const [field,label] of [['volume','音量'],['brightness','亮度']]) {
  const p=page();p.data.runtime={...dto(),brightness:30};const calls:any[]=[];
  p.api={request:async(path:string,method:string,body:any)=>{calls.push({path,method,body});return receipt(field,70);}};
  for(const value of [41,58,67]) p.runtimeInput(sliderEvent(field,value));
  assert.equal(p.data[field+'Draft'],70);assert.equal(calls.length,0);
  await p.runtimeCommit(sliderEvent(field,67));
  const puts=calls.filter(c=>c.method==='PUT');assert.equal(puts.length,1);assert.deepEqual(puts[0].body,{field,value:70,boot:dto().boot});
  assert.match(p.data.controlNotice,new RegExp(label+'.*已生效.*70'));
  await p.runtimeCommit(sliderEvent(field,70));assert.equal(calls.filter(c=>c.method==='PUT').length,1);
 }
});
test('release uses its own value even without changing and covers both endpoints',async()=>{
 for(const [raw,value] of [[0,0],[64,60],[65,70],[100,100]]) {
  const p=page();let body:any;p.api={request:async(_path:string,method:string,b:any)=>{if(method==='PUT')body=b;return receipt('volume',value);}};
  await p.runtimeCommit(sliderEvent('volume',raw));assert.equal(body.value,value);
 }
});
test('raw invalid slider events cannot change drafts or submit stale values',async()=>{
 for(const raw of [undefined,null,'', '40',true,{},NaN,Infinity,-1,101,2.5]) {
  const p=page();let calls=0;p.api={request:async()=>{calls++;return dto();}};
  p.runtimeInput(sliderEvent('volume',raw));assert.equal(p.data.volumeDraft,50);
  await p.runtimeCommit(sliderEvent('volume',raw));assert.equal(calls,0);assert.equal(p.data.volumeDraft,50);
 }
 const p=page();let calls=0;p.api={request:async()=>{calls++;}};
 await p.runtimeCommit(sliderEvent('other',40));assert.equal(calls,0);
});
test('hidden logged-out busy offline and forbidden slider input cannot submit or alter drafts',async()=>{
 const patches=[(p:any)=>p.active=false,(p:any)=>p.data.loggedIn=false,(p:any)=>p.data.controlling=true,(p:any)=>p.data.loading=true,(p:any)=>p.data.runtime=null,(p:any)=>p.data.runtime.canControl=false,(p:any)=>p.data.runtime.connection='offline',(p:any)=>p.data.runtime.connection='unknown',(p:any)=>p.data.runtime.volume=null];
 for(const patch of patches) {
  const p=page();patch(p);let calls=0;p.api={request:async()=>{calls++;return dto();}};
  p.runtimeInput(sliderEvent('volume',40));await p.runtimeCommit(sliderEvent('volume',40));
  assert.equal(calls,0);assert.equal(p.data.volumeDraft,50);
 }
});
test('same current value is a no-op with a named notice',async()=>{
 const p=page();let calls=0;p.api={request:async()=>{calls++;return dto();}};
 await p.runtimeCommit(sliderEvent('volume',20));assert.equal(calls,0);assert.match(p.data.controlNotice,/音量.*20.*无需/);
});
test('duplicate release while submitting sends once and waits for matching receipt',async()=>{
 const p=page();let finish:any;let puts=0;
 p.api={request:async(_path:string,method:string)=>{if(method==='PUT'){puts++;return new Promise(r=>finish=r);}return receipt('volume',40,'sent');}};
 const work=p.runtimeCommit(sliderEvent('volume',40));
 await p.runtimeCommit(sliderEvent('volume',50));assert.equal(puts,1);assert.equal(p.data.volumeDraft,40);
 finish(receipt('volume',40,'queued'));await work;
 assert.equal(p.data.controlling,true);assert.match(p.data.controlNotice,/音量.*等待设备回执/);assert.doesNotMatch(p.data.controlNotice,/已生效/);
 p.api.request=async()=>receipt('volume',40);await p.observeCommand(p.epoch);
 assert.equal(p.data.controlling,false);assert.match(p.data.controlNotice,/音量.*已生效/);p.stopRuntime();
});
test('initial submission names the control and target before the PUT receipt',async()=>{
 for(const [field,label] of [['volume','音量'],['brightness','亮度']]) {
  const p=page();p.data.runtime={...dto(),brightness:30};let finish:any;
  p.api={request:async(_path:string,method:string)=>method==='PUT'?new Promise(r=>finish=r):receipt(field,70)};
  const work=p.runtimeCommit(sliderEvent(field,67));
  assert.equal(p.data.controlNotice,'正在设置'+label+'为 70，等待设备回执');
  finish(receipt(field,70));await work;
 }
});
import { readFileSync } from 'node:fs';
test('actual settings WXML commits release only, with eleven non-interactive circle ticks per slider',()=>{
 const wxml=readFileSync(new URL('../src/pages/settings/settings.wxml',import.meta.url),'utf8');
 const css=readFileSync(new URL('../src/app.wxss',import.meta.url),'utf8');
 const sliders=wxml.match(/<slider\b[^>]*\/>/g)||[];assert.equal(sliders.length,2);
 for(const slider of sliders) {
  for(const attribute of ['min="0"','max="100"','step="10"','bindchanging="runtimeInput"','bindchange="runtimeCommit"']) assert.ok(slider.includes(attribute),attribute);
 }
 assert.doesNotMatch(wxml, /bindtap="control"|设置音量<|设置亮度</);
 assert.deepEqual(page().data.runtimeTicks,[0,10,20,30,40,50,60,70,80,90,100]);
 assert.equal((wxml.match(/wx:for="{{runtimeTicks}}"/g)||[]).length,2);
 assert.match(css,/\.runtime-ticks\s*\{[^}]*pointer-events\s*:\s*none/);
 assert.match(css,/\.runtime-tick\s*\{[^}]*border-radius\s*:\s*50%/);
 assert.match(wxml,/松手.*设置/);
});
