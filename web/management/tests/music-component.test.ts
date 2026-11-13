import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import * as vue from 'vue';
import { parse, compileScript, compileTemplate } from '@vue/compiler-sfc';
import ts from 'typescript';
import * as controller from '../src/music-library.ts';
import * as sensitive from '../src/sensitive-session.ts';
const source=readFileSync(new URL('../src/components/MusicLibrary.vue',import.meta.url),'utf8');
const tick=async()=>{await new Promise(r=>setImmediate(r));await vue.nextTick();};
const track={id:2**52,title:'家庭私藏',artist:'自填名字',durationMs:61000,bytes:100,format:'flac',bitrate:0,sampleRate:48000,channels:2,addedAt:'2026-11-13T00:00:00Z'};
const dto=(deviceId='d',items:any[]=[],canManage=true)=>({deviceId,items,canManage,maxBytes:300*1024*1024,maxTrackBytes:30*1024*1024,usedBytes:items.length*100});
type El={tag:string;props:Record<string,any>;children:El[];text:string;parent?:El};
function mount(api:any){
 const {descriptor}=parse(source);const script=compileScript(descriptor,{id:'music-test'});
 const template=compileTemplate({source:descriptor.template!.content,filename:'MusicLibrary.vue',id:'music-test',compilerOptions:{bindingMetadata:script.bindings}});assert.deepEqual(template.errors,[]);
 const load=(input:string)=>{const m={exports:{} as any};const output=ts.transpileModule(input,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText;const realRequire=createRequire(import.meta.url);new Function('require','module','exports',output)((name:string)=>name==='vue'?vue:name==='../api.ts'?{api}:name==='../music-library.ts'?controller:name==='../sensitive-session.ts'?sensitive:realRequire(name),m,m.exports);return m.exports;};
 const component=load(script.content).default;component.render=load(template.code).render;
 const element=(tag:string,text=''):El=>({tag,text,props:{},children:[],addEventListener(){},getRootNode(){return document;}} as El);
 const renderer=vue.createRenderer<El,El>({createElement:tag=>element(tag),createText:text=>element('#text',text),createComment:()=>element('#comment'),insert:(el,parent,anchor)=>{if(el.parent){const i=el.parent.children.indexOf(el);if(i>=0)el.parent.children.splice(i,1);}el.parent=parent;const index=anchor?parent.children.indexOf(anchor):-1;if(index<0)parent.children.push(el);else parent.children.splice(index,0,el);},remove:el=>{const i=el.parent?.children.indexOf(el)??-1;if(i>=0)el.parent!.children.splice(i,1);},setText:(el,text)=>{el.text=text;},setElementText:(el,text)=>{el.text=text;el.children=[];},parentNode:el=>el.parent??null,nextSibling:el=>el.parent?.children[(el.parent.children.indexOf(el))+1]??null,patchProp:(el,key,_old,value)=>{el.props[key]=value;}});
 const props=vue.reactive({deviceId:'d',scope:'family'});const app=renderer.createApp({setup:()=>()=>vue.h(component,props)});const root=element('root');app.mount(root);
 const nodes=()=>{const all:El[]=[];const visit=(e:El)=>{all.push(e);e.children.forEach(visit);};visit(root);return all;};
 return{app,props,nodes,state:()=>((app as any)._instance.subTree.component.setupState.state as ReturnType<typeof controller.musicLibraryState>),text:()=>nodes().map(n=>n.text).join(' '),button:(text:string)=>nodes().find(n=>n.tag==='button'&&n.text===text)!};
}
async function withDocument(action:(doc:EventTarget&{visibilityState:string})=>Promise<void>){const previousDocument=Object.getOwnPropertyDescriptor(globalThis,'Document');Object.defineProperty(globalThis,'Document',{configurable:true,value:EventTarget});const previous=Object.getOwnPropertyDescriptor(globalThis,'document');const doc=Object.assign(new EventTarget(),{visibilityState:'visible'});Object.defineProperty(globalThis,'document',{configurable:true,value:doc});try{await action(doc);}finally{if(previous)Object.defineProperty(globalThis,'document',previous);else Reflect.deleteProperty(globalThis,'document');if(previousDocument)Object.defineProperty(globalThis,'Document',previousDocument);else Reflect.deleteProperty(globalThis,'Document');}}
test('compiled library form submits actual title/artist/file/ack, confirms GET, cancels and confirms delete',async()=>withDocument(async()=>{
 let items:any[]=[];const calls:string[]=[];let upload:any;
 const m=mount({getMusicLibrary:async()=>{calls.push('GET');return dto('d',items);},uploadMusicTrack:async(_id:string,input:any)=>{upload=input;calls.push('POST');items=[track];return track;},deleteMusicTrack:async()=>{calls.push('DELETE');items=[];return{deleted:track.id};}});
 try{await tick();assert.match(m.text(),/暂无歌曲/);assert.match(m.text(),/私人点歌后的本次对话仅保留在聊天档案/);
 const title=m.nodes().find(n=>n.props.id==='music-title')!;title.props['onUpdate:modelValue']('家庭私藏');m.nodes().find(n=>n.props.id==='music-artist')!.props['onUpdate:modelValue']('自填名字');
 // Checkbox v-model is a native directive; its model update is exercised directly.
 m.nodes().find(n=>n.props.id==='music-personal')!.props['onUpdate:modelValue'](true);
 const file=new File(['audio'],'private.flac');m.nodes().find(n=>n.props.type==='file')!.props.onChange({target:{files:[file]}});
 m.nodes().find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await tick();
 assert.equal(upload.file,file);assert.equal(upload.personalUse,true);assert.equal(upload.title,'家庭私藏');assert.equal(upload.artist,'自填名字');assert.deepEqual(calls,['GET','POST','GET']);assert.match(m.text(),/已导入/);assert.match(m.text(),/码率未知/);assert.match(m.text(),/48000/);assert.match(m.text(),/1:01/);assert.equal(m.state().file,null);
 m.button('删除').props.onClick();await tick();m.button('取消').props.onClick();await tick();assert.equal(calls.length,3);
 m.button('删除').props.onClick();await tick();m.button('确认删除').props.onClick();await tick();assert.deepEqual(calls,['GET','POST','GET','DELETE','GET']);assert.match(m.text(),/已删除/);
 }finally{m.app.unmount();}
}));
test('compiled library presents readonly real empty state and unavailable feedback, without player or shared paths',async()=>withDocument(async()=>{
 const m=mount({getMusicLibrary:async()=>dto('d',[],false)});try{await tick();assert.match(m.text(),/暂无歌曲/);assert.match(m.text(),/只能查看/);assert.equal(m.nodes().some(n=>n.tag==='form'),false);assert.equal(m.nodes().some(n=>n.tag==='audio'||n.tag==='a'),false);}finally{m.app.unmount();}
 const bad=mount({getMusicLibrary:async()=>{throw new Error('HTTP 503 服务不可用');}});try{await tick();assert.match(bad.text(),/503 服务不可用/);assert.doesNotMatch(bad.text(),/暂无歌曲/);}finally{bad.app.unmount();}
}));
test('compiled library clears file/form on hidden/logout, ignores late GET/POST, switches scopes and stops after unmount',async()=>withDocument(async doc=>{
 let resolve!:(x:any)=>void;let delayed=false;let calls=0;let writes=0;const seen:string[]=[];
 const m=mount({getMusicLibrary:async(id:string,scope:string)=>{calls++;seen.push(`${scope}:${id}`);return delayed?new Promise(r=>resolve=r):dto(id);},uploadMusicTrack:async()=>{writes++;return new Promise(r=>resolve=r);}});
 try{await tick();Object.assign(m.state(),{title:'家庭私藏',artist:'自填名字',file:new File(['x'],'p.mp3'),personalUse:true});
 m.nodes().find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await tick();assert.equal(writes,1);doc.visibilityState='hidden';doc.dispatchEvent(new Event('visibilitychange'));await tick();assert.equal(m.state().title,'');assert.equal(m.state().file,null);resolve(track);await tick();assert.equal(m.state().library,null);assert.equal(calls,1);
 doc.visibilityState='visible';doc.dispatchEvent(new Event('visibilitychange'));await tick();delayed=true;m.props.scope='admin';m.props.deviceId='other';await tick();const old=resolve;sensitive.invalidateSensitive();old(dto('other',[track]));await tick();assert.equal(m.state().library,null);assert.equal(m.state().personalUse,false);assert.ok(seen.includes('admin:other'));
 }finally{m.app.unmount();}const before=calls;doc.visibilityState='visible';doc.dispatchEvent(new Event('visibilitychange'));sensitive.invalidateSensitive();await tick();assert.equal(calls,before);
}));
test('both settings branches embed selected device library beside device settings',()=>{const core=readFileSync(new URL('../src/pages/CorePages.vue',import.meta.url),'utf8');assert.match(core,/import MusicLibrary/);assert.match(core,/<MusicLibrary[^>]*:device-id="device.id"[^>]*:scope="scope"/);});
test('unmount during actual component upload aborts and erases sensitive state before a late write receipt',async()=>withDocument(async()=>{
 let resolve!:(x:any)=>void;let signal!:AbortSignal;let reads=0;
 const m=mount({getMusicLibrary:async()=>{reads++;return dto();},uploadMusicTrack:async(_id:string,_input:any,_scope:string,s:AbortSignal)=>{signal=s;return new Promise(r=>resolve=r);}});
 await tick();const state=m.state();Object.assign(state,{title:'家庭私藏',artist:'自填名字',file:new File(['x'],'p.mp3'),personalUse:true});m.nodes().find(n=>n.tag==='form')!.props.onSubmit({preventDefault(){}});await tick();m.app.unmount();assert.equal(signal.aborted,true);assert.equal(state.file,null);assert.equal(state.title,'');resolve(track);await tick();assert.equal(state.library,null);assert.equal(state.feedback,'');assert.equal(reads,1);
}));
test('compiled upload form advertises the same 500-code-point artist boundary as the controller',async()=>withDocument(async()=>{
 const m=mount({getMusicLibrary:async()=>dto()});try{await tick();assert.match(m.text(),/曲名最多500字，歌手最多500字/);assert.doesNotMatch(m.text(),/歌手最多2000字/);}finally{m.app.unmount();}
}));
