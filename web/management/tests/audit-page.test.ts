import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import * as vue from 'vue';
import {parse,compileScript} from '@vue/compiler-sfc';
import ts from 'typescript';
import * as controller from '../src/audit-control.ts';
import * as sensitive from '../src/sensitive-session.ts';
const tick=async()=>{await vue.nextTick();await new Promise(r=>setImmediate(r));};
const deferred=()=>{let resolve!:(x:any)=>void;const promise=new Promise<any>(r=>resolve=r);return {promise,resolve};};
const id='dev_'+'a'.repeat(32);
const item={id:'audit_fixture',time:'2026-11-11T01:00:00Z',actor:'管理员',deviceId:id,target:'客厅',action:'device_control',description:'调节音量',status:'unknown',result:'结果未知',scope:'admin',canOpenDevice:false};
async function mount(api:any){
 const prior=Object.getOwnPropertyDescriptor(globalThis,'document');const doc=new EventTarget() as EventTarget & {visibilityState:string};doc.visibilityState='visible';Object.defineProperty(globalThis,'document',{configurable:true,value:doc});
 const source=readFileSync(new URL('../src/pages/AuditPage.vue',import.meta.url),'utf8');const {descriptor}=parse(source);const script=compileScript(descriptor,{id:'audit-fixture',inlineTemplate:true});
 const code=ts.transpileModule(script.content,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;
 const realRequire=createRequire(import.meta.url),module={exports:{} as any};
 new Function('require','module','exports',code)((name:string)=>name==='vue'?vue:name==='../api.ts'?{api}:name==='../audit-control.ts'?controller:name==='../sensitive-session.ts'?sensitive:name==='../navigation.ts'?{linkTo:(_s:string,p:string,q:any)=>`index.html#/${p}?device=${q.device}`}:realRequire(name),module,module.exports);
 const node=(tag:string,text='')=>({tag,text,props:{} as any,children:[] as any[],parent:null as any});
 const renderer=vue.createRenderer<any,any>({createElement:node,createText:text=>node('#text',text),createComment:text=>node('#comment',text),setText:(n,text)=>n.text=text,setElementText:(n,text)=>{n.text=text;n.children=[];},insert:(n,parent,anchor)=>{if(n.parent)n.parent.children.splice(n.parent.children.indexOf(n),1);n.parent=parent;const i=anchor?parent.children.indexOf(anchor):-1;if(i<0)parent.children.push(n);else parent.children.splice(i,0,n);},remove:n=>{n.parent?.children.splice(n.parent.children.indexOf(n),1);},parentNode:n=>n.parent,nextSibling:n=>n.parent?.children[n.parent.children.indexOf(n)+1],patchProp:(n,k,_old,value)=>n.props[k]=value});
 const props=vue.reactive({scope:'admin',route:{page:'audit',device:'',chat:'',memory:'',family:''}});const app=renderer.createApp({setup:()=>()=>vue.h(module.exports.default,props)});const root=node('root');app.mount(root);await tick();
 const all=()=>{const values:any[]=[];function walk(n:any){values.push(n);for(const c of n.children)walk(c);}walk(root);return values;};
 return {props,doc,all,text:()=>all().map(n=>n.text).join(' '),find:(id:string)=>{const n=all().find(n=>n.props.id===id);assert.ok(n,`missing #${id}`);return n;},close:()=>{app.unmount();if(prior)Object.defineProperty(globalThis,'document',prior);else Reflect.deleteProperty(globalThis,'document');}};
}
test('mounted audit form, pagination and authorized links use actual Vue handlers and missing values remain unrecorded',async()=>{
 const calls:any[]=[];const api={listDevices:async()=>[{id,name:'客厅'}],listAuditPage:async(filter:any)=>{calls.push(filter);return {items:[{...item,canOpenDevice:calls.length>1}],hasMore:true,pending:true};}};
 const view=await mount(api);try{
 assert.match(view.text(),/本页显示平台已记录的操作；语音指令和设备按键变化暂未纳入。/);
 assert.match(view.text(),/未记录/);assert.match(view.text(),/结果未知/);assert.match(view.text(),/日志正在写入/);assert.equal(view.all().filter(n=>n.tag==='a').length,0);
 await view.find('audit-next').props.onClick();await tick();assert.equal(calls.at(-1).offset,50);assert.equal(view.all().filter(n=>n.tag==='a').length,1);
 view.find('audit-action').props.onChange({target:{value:'settings'}});view.find('audit-from').props.onInput({target:{value:'2026-11-01'}});view.find('audit-to').props.onInput({target:{value:'2026-11-02'}});await tick();assert.doesNotMatch(view.text(),/管理员/);
 await view.find('audit-form').props.onSubmit({preventDefault(){}});await tick();assert.equal(calls.at(-1).offset,0);assert.equal(calls.at(-1).action,'settings');assert.equal(calls.at(-1).to,new Date(2026,10,3).toISOString());
 }finally{view.close();}
});
test('mounted route/hide/session/unmount cleanup rejects late rows and errors, and repeated refresh cannot overlap',async()=>{
 let next:any={items:[item],hasMore:true,pending:false};let calls=0;
 const api={listDevices:async()=>[{id,name:'客厅'}],listAuditPage:async()=>{calls++;return typeof next==='function'?next():next;}};
 const view=await mount(api);try{
 const late=deferred();next=()=>late.promise;const pending=view.find('audit-refresh').props.onClick();await tick();view.find('audit-refresh').props.onClick();assert.equal(calls,2);
 sensitive.invalidateSensitive();await tick();assert.doesNotMatch(view.text(),/管理员/);assert.doesNotMatch(view.text(),/第 1 页/);late.resolve({items:[item],hasMore:true,pending:true});await pending;await tick();assert.doesNotMatch(view.text(),/管理员/);
 view.props.route={...view.props.route,device:id};next={items:[item],hasMore:true,pending:false};await tick();assert.match(view.text(),/管理员/);
 view.doc.visibilityState='hidden';view.doc.dispatchEvent(new Event('visibilitychange'));await tick();assert.doesNotMatch(view.text(),/管理员/);
 view.props.route={...view.props.route,page:'devices'};await tick();const before=calls;view.close();sensitive.invalidateSensitive();await tick();assert.equal(calls,before);
 }finally{view.close();}
});
test('family routes never activate an admin audit request',async()=>{
 let calls=0;const api={listDevices:async()=>{calls++;return [];},listAuditPage:async()=>{calls++;return {items:[],hasMore:false,pending:false};}};
 const view=await mount(api);try{await tick();const before=calls;view.props.scope='family';view.props.route={...view.props.route,page:'audit'};await tick();assert.equal(calls,before);}finally{view.close();}
});
