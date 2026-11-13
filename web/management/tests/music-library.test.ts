import test from 'node:test';
import assert from 'node:assert/strict';
import { createMusicLibraryController, musicLibraryState } from '../src/music-library.ts';
import type { LibraryTrack, MusicLibrary } from '../src/types.ts';
const track:LibraryTrack={id:2**52,title:'私藏曲',artist:'自填歌手',durationMs:2000,bytes:100,format:'mp3',bitrate:128000,sampleRate:44100,channels:2,addedAt:'2026-11-13T00:00:00Z'};
const dto=(items:LibraryTrack[]=[],canManage=true,deviceId='d'):MusicLibrary=>({deviceId,items,canManage,maxBytes:300*1024*1024,maxTrackBytes:30*1024*1024,usedBytes:items.reduce((n,t)=>n+t.bytes,0)});
const tick=()=>new Promise(r=>setImmediate(r));
function deferred<T>(){let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>resolve=r);return{resolve,promise};}
function setup(){
 let read=async()=>dto(),write=async()=>track,remove=async()=>({deleted:track.id});let expire=()=>{},delay=0,now=0;
 const calls:Array<{method:string;args:any[]}>=[];const state=musicLibraryState();
 const control=createMusicLibraryController({
   getMusicLibrary:async(...args)=>{calls.push({method:'GET',args});return read();},
   uploadMusicTrack:async(...args)=>{calls.push({method:'POST',args});return write();},
   deleteMusicTrack:async(...args)=>{calls.push({method:'DELETE',args});return remove();},
 },state,{after:(fn,ms)=>{expire=fn;delay=ms;return 1;},cancel:()=>{},now:()=>now});
 const fill=()=>Object.assign(state,{title:' 私藏曲 ',artist:' 自填歌手 ',personalUse:true,file:new File(['sound'],'private.mp3')});
 return{state,control,calls,fill,setRead:(fn:typeof read)=>{read=fn;},setWrite:(fn:typeof write)=>{write=fn;},setRemove:(fn:typeof remove)=>{remove=fn;},expire:()=>expire(),delay:()=>delay,clock:(t:number)=>{now=t;}};
}
test('import remains pending through POST then matching GET; no double write and form clears after confirmation',async()=>{
 const p=setup();await p.control.start('d','family');p.fill();const wait=deferred<MusicLibrary>();p.setRead(()=>wait.promise);
 const uploading=p.control.upload();await tick();await p.control.upload();await p.control.refresh();
 assert.deepEqual(p.calls.map(x=>x.method),['GET','POST','GET']);assert.equal(p.delay(),120000);assert.equal(p.state.pending,true);assert.doesNotMatch(p.state.feedback,/已导入/);
 assert.equal(p.calls[1]!.args[1].title,'私藏曲');assert.equal(p.calls[1]!.args[1].artist,'自填歌手');assert.equal(p.calls[1]!.args[2],'family');
 wait.resolve(dto([track]));await uploading;assert.match(p.state.feedback,/已导入/);assert.equal(p.state.library?.items.length,1);assert.equal(p.state.file,null);assert.equal(p.state.title,'');assert.equal(p.state.personalUse,false);
});
test('missing or changed import readback never claims completion and requires refresh',async()=>{
 for(const items of [[],[{...track,bitrate:0}],[{...track,title:'other'}]]){
 const p=setup();await p.control.start('d','family');p.fill();p.setRead(async()=>dto(items));await p.control.upload();assert.equal(p.state.needsRead,true);assert.match(p.state.feedback,/未核实|未确认/);await p.control.upload();assert.equal(p.calls.filter(x=>x.method==='POST').length,1);
 }
});
test('delete cancellation never writes; confirmation deletes once then verifies absent',async()=>{
 const p=setup();p.setRead(async()=>dto([track]));await p.control.start('d','admin');p.control.askDelete(track.id);p.control.cancelDelete();await p.control.confirmDelete();assert.equal(p.calls.length,1);
 p.control.askDelete(track.id);p.setRead(async()=>dto());await p.control.confirmDelete();assert.deepEqual(p.calls.map(x=>x.method),['GET','DELETE','GET']);assert.match(p.state.feedback,/已删除/);
 const q=setup();q.setRead(async()=>dto([track]));await q.control.start('d','family');q.control.askDelete(track.id);await q.control.confirmDelete();assert.match(q.state.feedback,/未核实/);assert.equal(q.state.needsRead,true);
});
test('readonly verified reads clear chosen form and never allow mutations',async()=>{
 const p=setup();await p.control.start('d','family');p.fill();p.setRead(async()=>dto([track],false));await p.control.refresh();assert.equal(p.state.file,null);assert.equal(p.state.title,'');assert.equal(p.state.personalUse,false);
 await p.control.upload();p.control.askDelete(track.id);await p.control.confirmDelete();assert.equal(p.calls.filter(x=>x.method!=='GET').length,0);
});
test('invalid local metadata, absent acknowledgement and size/media limits are visible without sending',async()=>{
 const p=setup();await p.control.start('d','family');
 for(const change of [{title:''},{artist:''},{personalUse:false},{file:null},{file:new File(['x'],'bad.wav')},{file:{name:'large.mp3',size:30*1024*1024+1} as File}]){
 p.fill();Object.assign(p.state,change);await p.control.upload();assert.ok(p.state.feedback);assert.equal(p.calls.length,1);
 }
});
test('403, shared cap, invalid media and unavailable responses stay visible without retry',async()=>{
 for(const message of ['HTTP 403 无管理权限','曲库空间已满','媒体无效','服务不可用']){
 const p=setup();await p.control.start('d','family');p.fill();p.setWrite(async()=>{throw new Error(message);});await p.control.upload();assert.ok(p.state.feedback.includes(message));assert.match(p.state.feedback,/重新读取/);await p.control.upload();assert.equal(p.calls.length,2);
 }
});
test('stop aborts upload, clears private form and prevents late POST from reading or filling another scope',async()=>{
 const p=setup();await p.control.start('d','family');p.fill();const wait=deferred<LibraryTrack>();p.setWrite(()=>wait.promise);const uploading=p.control.upload();await tick();const signal=p.calls[1]!.args[3] as AbortSignal;
 p.control.stop();assert.equal(signal.aborted,true);assert.equal(p.state.file,null);assert.equal(p.state.title,'');p.setRead(async()=>dto([],false,'other'));await p.control.start('other','admin');wait.resolve(track);await uploading;await tick();assert.equal(p.state.library?.deviceId,'other');assert.equal(p.state.feedback,'');assert.equal(p.calls.length,3);
});
test('late GET on device switch and after timeout cannot repopulate or claim success',async()=>{
 const p=setup();const wait=deferred<MusicLibrary>();p.setRead(()=>wait.promise);const reading=p.control.start('d','family');p.control.stop();wait.resolve(dto([track]));await reading;assert.equal(p.state.library,null);
 const q=setup();await q.control.start('d','family');q.fill();const upload=deferred<LibraryTrack>();q.setWrite(()=>upload.promise);const saving=q.control.upload();q.clock(120001);upload.resolve(track);await saving;assert.equal(q.state.library,null);assert.match(q.state.feedback,/120秒/);assert.equal(q.state.pending,false);assert.equal(q.calls.length,2);
});
