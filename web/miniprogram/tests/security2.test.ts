import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { Security2Session } from '../src/provisioning/security2.ts'
const v = JSON.parse(readFileSync(new URL('./security2-vectors/official.json', import.meta.url), 'utf8'))
const b = (h: string) => Uint8Array.from(Buffer.from(h, 'hex'))
function session() { return new Security2Session(v.username, v.password, async () => b(v.random)) }
async function ready() { const s = session(); let step=0; await s.establish(async p => { assert.deepEqual(p,b(step===0?v.cmd0:v.cmd1)); return b(step++===0?v.resp0:v.resp1) }); return s }
test('unmodified official Python Security2 full transcript and bidirectional AES-GCM',async()=>{ const s=await ready(); assert.deepEqual(s.encrypt(b(v.plain)),b(v.encrypted)); assert.deepEqual(s.decrypt(b(v.cipher_reply)),b(v.reply)); s.destroy(); assert.throws(()=>s.encrypt(b(v.plain))) })
test('wrong password rejects authenticated device proof',async()=>{const s=new Security2Session(v.username,'f'.repeat(32),async()=>b(v.random));let n=0;await assert.rejects(s.establish(async()=>b(n++===0?v.resp0:v.resp1))); assert.throws(()=>s.encrypt(new Uint8Array()))})
test('tampered ciphertext permanently closes session',async()=>{const s=await ready();s.encrypt(b(v.plain));const bad=b(v.cipher_reply);bad[bad.length-1]^=1;assert.throws(()=>s.decrypt(bad));assert.throws(()=>s.decrypt(b(v.cipher_reply)))})
test('out of order nonce and replay close session',async()=>{const s=await ready();assert.throws(()=>s.decrypt(b(v.cipher_reply)));const r=await ready();r.encrypt(b(v.plain));r.decrypt(b(v.cipher_reply));assert.throws(()=>r.decrypt(b(v.cipher_reply)))})
test('downgrade, malformed and nonzero status reject',async()=>{for(const input of [new Uint8Array([16,1]),new Uint8Array([16,2,98,255]),new Uint8Array([16,2,98,7,8,1,170,1,2,8,1])]) await assert.rejects(session().establish(async()=>input))})
test('destroy during async handshake cannot revive session',async()=>{const s=session();await assert.rejects(s.establish(async()=>{s.destroy();return b(v.resp0)}));assert.throws(()=>s.encrypt(b(v.plain)))})
test('random failure and reuse rejected',async()=>{const s=new Security2Session(v.username,v.password,async()=>new Uint8Array(1));await assert.rejects(s.establish(async()=>b(v.resp0)));await assert.rejects(s.establish(async()=>b(v.resp0)))})
test('SRP zero or out-of-group server public key rejected',async()=>{
  for(const value of [0,255]) { const bad=b(v.resp0);let at=-1;for(let i=0;i<24;i++)if(bad[i]===18&&bad[i+1]===128&&bad[i+2]===3){at=i+3;break};assert.ok(at>0);bad.fill(value,at,at+384);await assert.rejects(session().establish(async()=>bad)) }
})
test('unsupported initial nonce counter rejected',async()=>{const bad=b(v.resp1);bad[bad.length-1]=2;let n=0;await assert.rejects(session().establish(async()=>n++===0?b(v.resp0):bad))})
