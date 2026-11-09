import {test} from 'node:test';
import assert from 'node:assert/strict';
import {initial,reduce} from '../src/state.ts';
test('only authenticated cloud confirmation can produce success',()=>{
 let s=reduce(initial,{type:'start'});
 s=reduce(s,{type:'ble_applied',generation:s.generation});
 assert.equal(s.phase,'waiting_cloud');
 s=reduce(s,{type:'cloud_status',generation:s.generation,status:'waiting'});
 assert.equal(s.phase,'waiting_cloud');
 s=reduce(s,{type:'cloud_status',generation:s.generation,status:'confirmed',deviceId:'dev_'+'a'.repeat(32)});
 assert.equal(s.phase,'success');
});
test('cancelled generations cannot overwrite a fresh task',()=>{
 const old=reduce(initial,{type:'start'});
 let s=reduce(old,{type:'cancel'}); s=reduce(s,{type:'start'});
 const before=s;
 s=reduce(s,{type:'cloud_status',generation:old.generation,status:'confirmed',deviceId:'dev_'+'a'.repeat(32)});
 assert.deepEqual(s,before);
});
test('expiry, unauthorized and network errors are visible and never success',()=>{
 for(const error of ['认领已过期','请重新登录','网络连接失败']) {
 const s=reduce(reduce(initial,{type:'start'}),{type:'error',generation:1,error});
 assert.equal(s.phase,'error'); assert.equal(s.error,error);
 }
});
