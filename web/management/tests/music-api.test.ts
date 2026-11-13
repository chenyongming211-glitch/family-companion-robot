import test from 'node:test';
import assert from 'node:assert/strict';
import { createRemoteApi } from '../src/remote-api.ts';
import { invalidateSensitive } from '../src/sensitive-session.ts';
const track = {id:2**52,title:'私藏曲',artist:'自填歌手',durationMs:1200,bytes:100,format:'flac',bitrate:0,sampleRate:48000,channels:2,addedAt:'2026-11-13T01:00:00Z'};
const library = {deviceId:'dev/x',items:[track],canManage:true,maxBytes:300*1024*1024,usedBytes:100,maxTrackBytes:30*1024*1024};
test('music real API preserves multipart order/boundary, scopes, signal, cookies and JSON headers', async () => {
  const seen: Array<{url:string;init:RequestInit}> = [];
  const api = createRemoteApi((async (url:unknown,init:RequestInit) => {seen.push({url:String(url),init});return new Response(JSON.stringify(init.method==='POST'?track:init.method==='DELETE'?{deleted:track.id}:library));}) as typeof fetch);
  const abort = new AbortController();
  await api.getMusicLibrary('dev/x','family',abort.signal);
  await api.uploadMusicTrack('dev/x',{title:'私藏曲',artist:'自填歌手',personalUse:true,file:new File(['audio'],'secret.flac')},'admin',abort.signal);
  await api.deleteMusicTrack('dev/x',track.id,'family',abort.signal);
  assert.deepEqual(seen.map(x=>x.url),['/api/family/music-library/dev%2Fx','/api/admin/music-library/dev%2Fx',`/api/family/music-library/dev%2Fx?id=${track.id}`]);
  for(const {init} of seen){assert.equal(init.credentials,'same-origin');assert.equal(init.cache,'no-store');assert.equal(init.signal,abort.signal);}
  const body=seen[1]!.init.body as FormData;
  assert.deepEqual([...body.keys()],['title','artist','personalUse','file']);assert.equal(body.get('personalUse'),'true');
  assert.equal(new Headers(seen[1]!.init.headers).get('Content-Type'),null);
  const login=createRemoteApi((async (_url:unknown,init:RequestInit)=>{assert.equal(new Headers(init.headers).get('Content-Type'),'application/json');return new Response('{}');}) as typeof fetch);
  await login.login('admin','x','x');
});
test('music DTO guards metadata/count/capacity/duplicate IDs, allows unknown bitrate and validates receipts',async()=>{
  let result:unknown=library;let calls=0;
  const api=createRemoteApi((async()=>{calls++;return new Response(JSON.stringify(result));}) as typeof fetch);
  assert.equal((await api.getMusicLibrary('dev/x','family')).items[0]!.bitrate,0);
  for(const bad of [null,{}, {...library,deviceId:'other'},{...library,items:null},{...library,canManage:'yes'}, {...library,items:[track,track]}, {...library,items:Array.from({length:101},(_,i)=>({...track,id:track.id+i}))}, {...library,usedBytes:-1}, {...library,maxBytes:0}, {...library,maxTrackBytes:1}]){
    result=bad;await assert.rejects(api.getMusicLibrary('dev/x','family'),/核实/);
  }
  for(const changes of [{id:1},{id:2**53},{id:1.5},{title:''},{artist:4},{format:'wav'},{bytes:0},{durationMs:999},{durationMs:1200001},{sampleRate:7999},{sampleRate:192001},{channels:3},{bitrate:-1},{addedAt:'today'}]){
    result={...library,items:[{...track,...changes}]};await assert.rejects(api.getMusicLibrary('dev/x','family'),/核实/);
  }
  result={deleted:track.id+1};await assert.rejects(api.deleteMusicTrack('dev/x',track.id,'family'),/核实/);
  const before=calls;await assert.rejects(api.deleteMusicTrack('dev/x',1,'family'),/核实/);assert.equal(calls,before);
});
test('music failures retain API error code and HTTP status; invalidated pending uploads reject',async()=>{
  for(const [status,code] of [[403,'forbidden'],[409,'music_library_full'],[429,'upload_busy'],[503,'source_unavailable']] as const){
    const api=createRemoteApi((async()=>new Response(JSON.stringify({code,message:'服务反馈'}),{status})) as typeof fetch);
    await assert.rejects(api.getMusicLibrary('d','family'),(error:any)=>error.code===code&&error.status===status&&error.message==='服务反馈');
  }
  let resolve!:(x:Response)=>void;
  const api=createRemoteApi((async()=>new Promise<Response>(r=>resolve=r)) as typeof fetch);
  const request=api.uploadMusicTrack('d',{title:'曲',artist:'',personalUse:true,file:new File(['x'],'a.mp3')},'family');
  invalidateSensitive();resolve(new Response(JSON.stringify(track)));await assert.rejects(request,/页面已离开|账号已变化/);
});
test('music metadata rejects Unicode control characters from malformed responses',async()=>{
 const api=createRemoteApi((async()=>new Response(JSON.stringify({...library,items:[{...track,title:'曲\u0085名'}]}))) as typeof fetch);
 await assert.rejects(api.getMusicLibrary('dev/x','family'),/核实/);
});
test('library DTO still accepts existing 2000-code-point artist metadata above the new upload form limit',async()=>{
 const artist='中'.repeat(2000);
 const api=createRemoteApi((async()=>new Response(JSON.stringify({...library,items:[{...track,artist}]}))) as typeof fetch);
 assert.equal((await api.getMusicLibrary('dev/x','family')).items[0]!.artist,artist);
});
