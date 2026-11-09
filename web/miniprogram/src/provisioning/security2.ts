import { sha512 } from '@noble/hashes/sha2.js'
import { gcm } from '@noble/ciphers/aes.js'

// ESP-IDF fff9895c82d744c7237be8847347bdd1b07c6643, Security 2 patch 1.
// RFC5054 3072-bit group, SHA-512, AES-256-GCM. No downgrade support.
const N = BigInt('0xFFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9DE2BCBF6955817183995497CEA956AE515D2261898FA051015728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6BF12FFA06D98A0864D87602733EC86A64521F2B18177B200CBBE117577A615D6C770988C0BAD946E208E24FA074E5AB3143DB5BFCE0FD108E4B82D120A93AD2CAFFFFFFFFFFFFFFFF')
function join(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0)); let at = 0
  for (const p of parts) { out.set(p, at); at += p.length }; return out
}
function bytes(n: bigint, width = 0): Uint8Array {
  let hex = n.toString(16); if (hex.length % 2) hex = '0' + hex
  const out = new Uint8Array(Math.max(width, hex.length / 2))
  for (let i = 0; i < hex.length / 2; i++) out[out.length - hex.length / 2 + i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  return out
}
function integer(b: Uint8Array): bigint { let n = 0n; for (const v of b) n = (n << 8n) | BigInt(v); return n }
function hash(...parts: Uint8Array[]): Uint8Array { return sha512(join(...parts)) }
function pow(base: bigint, exp: bigint): bigint {
  base = ((base % N) + N) % N; let value = 1n
  while (exp) { if (exp & 1n) value = value * base % N; base = base * base % N; exp >>= 1n }; return value
}
function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let d = 0; for (let i = 0; i < a.length; i++) d |= a[i]! ^ b[i]!; return d === 0
}
function vi(n: number): Uint8Array { const a: number[] = []; do { const v = n & 127; n >>>= 7; a.push(v | (n ? 128 : 0)) } while (n); return new Uint8Array(a) }
function field(n: number, b: Uint8Array): Uint8Array { return join(vi(n * 8 + 2), vi(b.length), b) }
function packet(msg: number, n: number, b: Uint8Array): Uint8Array { return join(new Uint8Array([16, 2]), field(12, join(msg ? new Uint8Array([8, msg]) : new Uint8Array(), field(n, b)))) }
function decode(input: Uint8Array): Map<number, Uint8Array | number> {
  if (input.length > 2048) throw new Error('Invalid Security 2 frame')
  let at = 0; const out = new Map<number, Uint8Array | number>()
  function read(): number { let n = 0; for (let s = 0; s <= 28; s += 7) { const b = input[at++]; if (b === undefined || (s === 28 && b > 15)) throw new Error('Invalid protobuf'); n += (b & 127) * 2 ** s; if (!(b & 128)) return n }; throw new Error('Invalid protobuf') }
  while (at < input.length) {
    const tag = read(), n = tag >>> 3, wire = tag & 7
    if (!n || out.has(n)) throw new Error('Invalid duplicate protobuf field')
    if (wire === 0) out.set(n, read())
    else if (wire === 2) { const len = read(); if (at + len > input.length) throw new Error('Truncated protobuf'); out.set(n, input.slice(at, at + len)); at += len }
    else throw new Error('Unsupported protobuf field')
  }; return out
}
function blob(m: Map<number, Uint8Array | number>, n: number, length?: number): Uint8Array {
  const b = m.get(n); if (!(b instanceof Uint8Array) || (length !== undefined && b.length !== length)) throw new Error('Invalid Security 2 response'); return b
}
function response(b: Uint8Array, msg: number, n: number): Map<number, Uint8Array | number> {
  const session = decode(b); if (session.get(2) !== 2 || session.has(10) || session.has(11)) throw new Error('Security downgrade rejected')
  const payload = decode(blob(session, 12)); if (payload.get(1) !== msg) throw new Error('Unexpected Security 2 message')
  const body = decode(blob(payload, n)); if ((body.get(1) ?? 0) !== 0) throw new Error('Security 2 authentication failed'); return body
}

export class Security2Session {
  private state: 'new' | 'handshake' | 'ready' | 'closed' = 'new'
  private key: Uint8Array = new Uint8Array(); private nonce: Uint8Array = new Uint8Array()
  private username: string; private password: string; private randomBytes: (length: number) => Promise<Uint8Array>
  constructor(username: string, password: string, randomBytes: (length: number) => Promise<Uint8Array>) {
    this.username = username; this.password = password; this.randomBytes = randomBytes
    // Factory serial and QR values are ASCII; avoid browser-only TextEncoder dependency.
    if (!/^[A-Za-z0-9_.-]{1,64}$/.test(username) || !/^[0-9a-f]{32}$/.test(password)) throw new Error('Invalid device proof credentials')
  }
  async establish(exchange: (payload: Uint8Array) => Promise<Uint8Array>): Promise<void> {
    if (this.state !== 'new') throw new Error('Security session cannot be reused')
    this.state = 'handshake'; let K: Uint8Array = new Uint8Array(); let random: Uint8Array = new Uint8Array()
    const alive = () => { if (this.state !== 'handshake') throw new Error('Security session closed') }
    try {
      random = await this.randomBytes(32); alive()
      if (!(random instanceof Uint8Array) || random.length !== 32) throw new Error('Secure random source failed')
      const a = integer(random) | (1n << 255n), A = pow(5n, a)
      const ascii = (s: string) => Uint8Array.from(s, c => c.charCodeAt(0))
      const user = ascii(this.username)
      const r0 = response(await exchange(packet(0, 20, join(field(1, user), field(2, bytes(A, 384))))), 1, 21); alive()
      const rawB = blob(r0, 2), salt = blob(r0, 3)
      if (!rawB.length || rawB.length > 384) throw new Error('Invalid SRP public key length')
      if (salt.length < 16 || salt.length > 64) throw new Error('Invalid SRP salt')
      const B = integer(rawB), u = integer(hash(bytes(A, 384), bytes(B, 384)))
      if (B <= 0n || B >= N || u === 0n) throw new Error('Invalid SRP public key')
      // Official Python represents integer hash inputs minimally; fixed factory salts are generated accordingly.
      const x = integer(hash(bytes(integer(salt)), bytes(integer(hash(ascii(this.username + ':' + this.password))))))
      const k = integer(hash(bytes(N), bytes(5n, 384)))
      const S = pow(B - k * pow(5n, x), a + u * x); K = hash(bytes(S))
      const hN = hash(bytes(N)), hg = hash(bytes(5n, 384)); for (let i = 0; i < hN.length; i++) hN[i] = hN[i]! ^ hg[i]!
      const M = hash(hN, hash(user), salt, bytes(A, 384), rawB, K)
      const r1 = response(await exchange(packet(2, 22, field(1, M))), 3, 23); alive()
      if (!equal(blob(r1, 2, 64), hash(bytes(A, 384), M, K))) throw new Error('Device authentication failed')
      const nonce = blob(r1, 3, 12)
      if (new DataView(nonce.buffer, nonce.byteOffset, 12).getUint32(8, false) !== 1) throw new Error('Security 2 patch 1 required')
      this.key = K.slice(0, 32); this.nonce = nonce.slice(); this.state = 'ready'
    } catch { this.destroy(); throw new Error('Security 2 authentication failed') }
    finally { K.fill(0); random.fill(0); this.password = ''; this.username = '' }
  }
  private crypt(data: Uint8Array, decrypt: boolean): Uint8Array {
    if (this.state !== 'ready') throw new Error('Security session not authenticated')
    try {
      if (data.length > 2048 || (decrypt && data.length < 16)) throw new Error('Invalid encrypted frame')
      const view = new DataView(this.nonce.buffer, this.nonce.byteOffset, 12), counter = view.getUint32(8, false)
      if (!counter || counter === 0xffffffff) throw new Error('Security counter exhausted')
      const cipher = gcm(this.key, this.nonce)
      const result = decrypt ? cipher.decrypt(data) : cipher.encrypt(data)
      view.setUint32(8, counter + 1, false); return result
    } catch { this.destroy(); throw new Error('Security 2 encrypted exchange failed') }
  }
  encrypt(plain: Uint8Array): Uint8Array { return this.crypt(plain, false) }
  decrypt(cipher: Uint8Array): Uint8Array { return this.crypt(cipher, true) }
  destroy(): void { this.key.fill(0); this.nonce.fill(0); this.password = ''; this.username = ''; this.state = 'closed' }
}
