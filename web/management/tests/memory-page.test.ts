import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import * as vue from 'vue';
import {parse,compileScript} from '@vue/compiler-sfc';
import ts from 'typescript';
import * as control from '../src/runtime-control.ts';
import * as sensitive from '../src/sensitive-session.ts';
const tick=()=>new Promise(r=>setImmediate(r));
const deferred=()=>{let resolve!:(x:any)=>void;const promise=new Promise<any>(r=>resolve=r);return {promise,resolve};};
async function page(clock?: {after:(fn:()=>void)=>unknown;cancel:(id:unknown)=>void}){
 const keys=['location','document','window','confirm'] as const;const previous=keys.map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)] as const);
 Object.defineProperty(globalThis,'location',{configurable:true,value:{hash:'#/memories?device=d'}});
 Object.defineProperty(globalThis,'document',{configurable:true,value:{visibilityState:'visible'}});
 Object.defineProperty(globalThis,'window',{configurable:true,value:new EventTarget()});
 Object.defineProperty(globalThis,'confirm',{configurable:true,value:()=>true});
 let reads=0,writes=0,denied=false;let receipt=async()=>{};
 const memory={id:'m',deviceId:'d',conversationId:'source-record',chatId:'chat_business',turnId:'turn-1',text:'private memory',date:''};
 const api={getAccount:async()=>({email:'owner'}),listDevices:async()=>[{id:'d'}],listMemories:async()=>{reads++;if(denied)throw new Error('正文未授权');return [memory];},getMemory:async()=>memory,getDeviceRuntime:async()=>({deviceId:'d',canControl:true}),clearMemories:async()=>{writes++;await receipt();}};
 const source=readFileSync(new URL('../src/pages/DataPages.vue',import.meta.url),'utf8');const {descriptor}=parse(source);const script=compileScript(descriptor,{id:'memory-fixture'});const code=ts.transpileModule(script.content,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;
 const realRequire=createRequire(import.meta.url),module={exports:{} as any};
 new Function('require','module','exports',code)((name:string)=>name==='vue'?vue:name==='../api.ts'?{api}:name==='../runtime-control.ts'?(clock?{...control,createMemoryClearController:(api:any,state:any,onCleared:any)=>control.createMemoryClearController(api,state,onCleared,clock)}:control):name==='../sensitive-session.ts'?sensitive:name==='../navigation.ts'?{linkTo:(_scope:any,_page:any,query:any)=>JSON.stringify(query)}:name.endsWith('.css')?{}:realRequire(name),module,module.exports);
 const component=module.exports.default;component.render=()=>null;
 const renderer=vue.createRenderer<any,any>({createElement:()=>({}),insert:()=>{},remove:()=>{},createText:()=>({}),createComment:()=>({}),setText:()=>{},setElementText:()=>{},parentNode:()=>null,nextSibling:()=>null,patchProp:()=>{}});
 const props=vue.reactive({scope:'admin',route:{page:'memories',device:'d',memory:'m',family:'',chat:''}});const app=renderer.createApp({setup:()=>()=>vue.h(component,props)});app.mount({});await tick();const state=(app as any)._instance.subTree.component.setupState;
 return {state,props,reads:()=>reads,writes:()=>writes,deny:()=>denied=true,receipt:(f:typeof receipt)=>receipt=f,ask:(f:()=>any)=>Object.defineProperty(globalThis,'confirm',{configurable:true,value:f}),close:()=>{app.unmount();for(const [key,old] of previous){if(old)Object.defineProperty(globalThis,key,old);else Reflect.deleteProperty(globalThis,key);}}};
}
test('mounted memory page clears body/button on denied current read and uses business chat source',async()=>{
 const p=await page();try{assert.equal(p.state.memories.length,1);assert.equal(p.state.canClear,true);assert.deepEqual(JSON.parse(p.state.chatLink(p.state.memories[0])),{device:'d',chat:'chat_business',turn:'turn-1'});p.deny();await p.state.load();assert.equal(p.state.memories.length,0);assert.equal(p.state.memory,null);assert.equal(p.state.canClear,false);assert.match(p.state.error,/未授权/);}finally{p.close();}
});
test('mounted memory page isolates stale confirmation on route and session changes',async()=>{
 const p=await page();try{const modal=deferred();p.ask(()=>modal.promise);const pending=p.state.clear();p.props.route={...p.props.route,page:'family',family:'no-family'};await vue.nextTick();sensitive.invalidateSensitive();modal.resolve(true);await pending;assert.equal(p.writes(),0);assert.equal(p.state.memories.length,0);assert.equal(p.state.canClear,false);}finally{p.close();}
});
test('mounted memory page cancellation and repeated click send zero/one POST and reread after confirmation',async()=>{
 const p=await page();try{p.ask(()=>false);await p.state.clear();assert.equal(p.writes(),0);const wait=deferred();p.receipt(()=>wait.promise);p.ask(()=>true);const first=p.state.clear();await p.state.clear();await tick();assert.equal(p.writes(),1);const before=p.reads();wait.resolve(undefined);await first;assert.equal(p.reads(),before+1);assert.match(p.state.clearState.feedback,/聊天记录保留/);}finally{p.close();}
});
test('revoked body permission during clear removes cached memory and the clear button',async()=>{
 const p=await page();try{p.receipt(async()=>{throw Object.assign(new Error('正文未授权'),{code:'forbidden'});});await p.state.clear();assert.equal(p.state.memories.length,0);assert.equal(p.state.canClear,false);assert.match(p.state.clearState.feedback,/未授权/);}finally{p.close();}
});

test('same-view refresh cannot release a held clear POST guard or cancel its deadline', async () => {
 let expire=()=>{},cancels=0,confirms=0;
 const p=await page({after:fn=>{expire=fn;return 1;},cancel:()=>{cancels++;}});
 const wait=deferred();let first:Promise<void>|undefined,second:Promise<void>|undefined;
 try {
  p.receipt(()=>wait.promise);p.ask(()=>{confirms++;return true;});
  first=p.state.clear();await tick();assert.equal(p.writes(),1);
  const beforeReads=p.reads();await p.state.load();
  second=p.state.clear();await tick();
  assert.equal(p.writes(),1,'manual refresh must not permit a second POST');
  assert.equal(confirms,1,'manual refresh must not permit a second confirmation');
  assert.equal(p.reads(),beforeReads,'same-view refresh is guarded while clearing');
  assert.equal(p.state.clearState.pending,true);assert.equal(cancels,0,'original deadline remains scheduled');
  expire();await first;assert.equal(p.state.clearState.pending,false);assert.match(p.state.clearState.feedback,/未知/);
  wait.resolve(undefined);await second;await tick();assert.doesNotMatch(p.state.clearState.feedback,/已清除/);assert.equal(p.reads(),beforeReads);
 } finally {wait.resolve(undefined);await first;await second;p.close();}
});
