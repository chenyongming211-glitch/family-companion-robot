import {test} from 'node:test';import assert from 'node:assert/strict';import {WifiProvisioner,message,field,parse,utf8} from '../src/provisioning/wifi.ts';
import {readFileSync} from 'node:fs';
const official=JSON.parse(readFileSync(new URL('./wifi-vectors/official-negative-rssi.json',import.meta.url),'utf8'));
const hex=(value:string)=>Uint8Array.from(Buffer.from(value,'hex'));
test('official generated negative int32 RSSI responses return scanned SSIDs',async()=>{let i=0;const responses=['start','status','result'].map(key=>hex(official.responses[key]));const wifi=new WifiProvisioner(async()=>responses[i++]);assert.deepEqual(await wifi.scan(),official.expected_ssids);assert.equal(i,3);const entries=parse(parse(responses[2]).get(15)![0] as Uint8Array).get(1)!;assert.deepEqual(entries.map(entry=>parse(entry as Uint8Array).get(3)![0]),official.expected_rssi);});
test('official protobuf set-config must be acknowledged before apply',async()=>{const sent:Uint8Array[]=[];const wifi=new WifiProvisioner(async(ep,p)=>{assert.equal(ep,'prov-config');sent.push(p);const n=sent.length===1?3:5;return message(field(1,n),field(n===3?13:15,new Uint8Array()));});await wifi.apply('test-only','synthetic');assert.equal(parse(sent[0]).get(1)?.[0],2);assert.equal(parse(sent[1]).get(1)?.[0],4);});
test('Wi-Fi error acknowledgement prevents apply',async()=>{let calls=0;const wifi=new WifiProvisioner(async()=>{calls++;return message(field(1,3),field(13,message(field(1,1))));});await assert.rejects(wifi.apply('test-only','synthetic'),/拒绝/);assert.equal(calls,1);});
test('official scan status is outer field2 and result entries nested field1',async()=>{let i=0;const responses=[message(field(1,1),field(11,new Uint8Array())),message(field(1,3),field(13,message(field(1,1),field(2,1)))),message(field(1,5),field(15,message(field(1,message(field(1,utf8('test-only')))))))];const wifi=new WifiProvisioner(async()=>responses[i++]);assert.deepEqual(await wifi.scan(),['test-only']);});
test('malformed/truncated protocol bytes fail closed',()=>{assert.throws(()=>parse(new Uint8Array([10,5,1])),/截断/);assert.throws(()=>parse(new Uint8Array([0])),/无效/);});
test('varints retain exact zero, positive, uint32 and safe-integer boundaries',()=>{
 for(const [encoded,expected] of [['00',0],['7f',127],['8001',128],['ffffffff0f',4294967295],['8080808010',4294967296],['ffffffffffffff0f',Number.MAX_SAFE_INTEGER]] as const)
  assert.equal(parse(hex('08'+encoded)).get(1)?.[0],expected);
 assert.equal(parse(hex('f8ffffff0f01')).get(536870911)?.[0],1);
 assert.deepEqual(parse(hex('0a00')).get(1)?.[0],new Uint8Array());
});
test('sign-extended int32 values decode without BigInt',()=>{
 assert.equal(parse(hex('08ffffffffffffffffff01')).get(1)?.[0],-1);
 assert.equal(parse(hex('0880808080f8ffffffff01')).get(1)?.[0],-2147483648);
});
test('unsafe, overflow and unterminated varints fail closed',()=>{
 for(const encoded of ['8080808080808010','ffffffffffffffffff02','fffffffff7ffffffff01','8080808080808080808000','8080808080','808080808080808080'])
  assert.throws(()=>parse(hex('08'+encoded)),/设备响应(?:无效|截断)/,encoded);
});
test('tags and lengths stay uint32-bounded and payloads stay bounded',()=>{
 for(const encoded of ['888080801001','0a8080808010','808080808000','0a808080808000'])
  assert.throws(()=>parse(hex(encoded)),/设备响应无效/,encoded);
 assert.throws(()=>parse(hex('0affffffff0f')),/截断/);
 assert.throws(()=>parse(hex('0a0200')),/截断/);
 assert.throws(()=>parse(hex('0d00000000')),/格式不兼容/);
 assert.throws(()=>parse(new Uint8Array(8193)),/过大/);
});
test('negative and oversized result counts fail instead of becoming empty scans',async()=>{
 for(const count of ['d6ffffffffffffffff01','8080808010']){
  const responses=[hex(official.responses.start),hex('08036a'+(count.length/2+3).toString(16).padStart(2,'0')+'080110'+count)];let i=0;
  await assert.rejects(new WifiProvisioner(async()=>responses[i++]).scan(),/设备响应(?:无效|格式无效)/);
  assert.equal(i,2);
 }
});
