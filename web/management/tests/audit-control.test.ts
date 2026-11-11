import test from 'node:test';
import assert from 'node:assert/strict';
import { auditFilter, auditState, createAuditController } from '../src/audit-control.ts';
const deferred=()=>{let resolve!:(x:any)=>void;let reject!:(x:any)=>void;const promise=new Promise<any>((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject};};
const item={id:'audit_safe',time:'2026-11-11T01:00:00Z',actor:'管理员',deviceId:'dev_'+'a'.repeat(32),target:'客厅',action:'device_control',description:'音量',status:'unknown',result:'结果未知',scope:'admin'};
const page={items:[item],hasMore:true,pending:true};
function setup(){const state=auditState();let next:any=page;const calls:any[]=[];const control=createAuditController({listAuditPage:async(filter:any)=>{calls.push(filter);return typeof next==='function'?next():next;},listDevices:async()=>[]},state);return {state,control,calls,set:(x:any)=>next=x};}
test('local inclusive dates become exclusive RFC3339 interval, invalid/reversed days reject',()=>{
 const filter=auditFilter({device:'dev_a',from:'2026-11-01',to:'2026-11-02',action:'memory_clear'},50,50);
 assert.equal(filter.from,new Date(2026,10,1).toISOString());assert.equal(filter.to,new Date(2026,10,3).toISOString());assert.equal(filter.device,'dev_a');assert.equal(filter.offset,50);
 assert.throws(()=>auditFilter({device:'',from:'2026-02-30',to:'',action:''}),/日期/);
 assert.throws(()=>auditFilter({device:'',from:'2026-11-03',to:'2026-11-01',action:''}),/日期/);
});
test('query and pagination keep drafts, reset offset on filter changes, and preserve unknown fields',async()=>{
 const p=setup();await p.control.activate();assert.equal(p.state.items[0].value,undefined);assert.equal(p.state.items[0].status,'unknown');assert.equal(p.state.pending,true);
 await p.control.next();assert.equal(p.calls[1].offset,50);assert.equal(p.state.offset,50);
 p.state.draft.action='settings';p.control.filtersChanged();assert.equal(p.state.offset,0);assert.deepEqual(p.state.items,[]);await p.control.refresh();assert.equal(p.calls[2].action,'settings');assert.equal(p.state.draft.action,'settings');
});
test('duplicate refresh is blocked, changed filter and invalidation ignore late success/error',async()=>{
 const p=setup();const wait=deferred();p.set(()=>wait.promise);const pending=p.control.activate();await p.control.refresh();assert.equal(p.calls.length,1);
 p.state.draft.action='access';p.control.filtersChanged();p.set({...page,items:[{...item,id:'new'}]});await p.control.refresh();wait.resolve(page);await pending;assert.equal(p.state.items[0].id,'new');
 const late=deferred();p.set(()=>late.promise);const reading=p.control.refresh();p.control.invalidate();assert.equal(p.state.loading,false);assert.equal(p.state.hasMore,false);assert.equal(p.state.pending,false);assert.equal(p.state.offset,0);assert.deepEqual(p.state.devices,[]);late.reject(new Error('private old error'));await reading;assert.equal(p.state.error,'');assert.deepEqual(p.state.items,[]);
});
test('errors and empty/pending pages stay explicit without fallback, invalid dates never request',async()=>{
 const p=setup();p.set(()=>Promise.reject(new Error('服务暂不可用')));await p.control.activate();assert.equal(p.state.error,'服务暂不可用');assert.deepEqual(p.state.items,[]);
 p.set({items:[],hasMore:false,pending:true});await p.control.refresh();assert.equal(p.state.error,'');assert.equal(p.state.pending,true);assert.deepEqual(p.state.items,[]);
 p.state.draft.from='2026-02-30';p.control.filtersChanged();const before=p.calls.length;await p.control.refresh();assert.match(p.state.error,/日期/);assert.equal(p.calls.length,before);
});
