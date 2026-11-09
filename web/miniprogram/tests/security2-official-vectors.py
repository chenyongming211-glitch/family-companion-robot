#!/usr/bin/env python3
"""Run unmodified pinned Espressif Python Security2 and emit PUBLIC synthetic vectors.
Usage: venv/bin/python security2-official-vectors.py --official DIR --output FILE
DIR contains fetched official security2.py,srp6a.py,security.py,utils.py and protos.
No production credential, radio or network calls. Dependency/source hashes in lock.
"""
import argparse, hashlib, importlib.util, json, pathlib, subprocess, sys, types
p = argparse.ArgumentParser(); p.add_argument('--official', type=pathlib.Path, required=True); p.add_argument('--output', type=pathlib.Path, required=True); p.add_argument('--lock', type=pathlib.Path, required=True); args=p.parse_args()
root=args.official.resolve(); lock=json.loads(args.lock.read_text())
for name, expected in lock['official_python'].items():
 assert hashlib.sha256((root/name).read_bytes()).hexdigest()==expected, 'official Python hash mismatch'
for path in (root/'protos').glob('*.proto'):
 assert hashlib.sha256(path.read_bytes()).hexdigest()==lock['idf_files']['components/protocomm/proto/'+path.name], 'official proto hash mismatch'
generated=root/'generated'; generated.mkdir(exist_ok=True)
subprocess.run([sys.executable,'-m','grpc_tools.protoc','-I'+str(root/'protos'),'--python_out='+str(generated),*[str(p) for p in sorted((root/'protos').glob('*.proto'))]],check=True)
sys.path[:0]=[str(root),str(generated)]
proto=types.ModuleType('proto')
for name in ['constants','sec0','sec1','sec2','session']:
 setattr(proto,name+'_pb2',__import__(name+'_pb2'))
sys.modules['proto']=proto
pkg=types.ModuleType('official_security'); pkg.__path__=[str(root)]; sys.modules[pkg.__name__]=pkg
from official_security import srp6a, security2
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.exceptions import InvalidTag
from utils import long_to_bytes as bn
user='SYNTHETIC-S3-001'; password='0123456789abcdef0123456789abcdef'; random=bytes(range(1,33)); salt=bytes(range(33,49)); b=int.from_bytes(bytes(range(65,97)),'big')
srp6a.get_random_of_length=lambda n:int.from_bytes(random,'big')|(1<<(n*8-1))
client=security2.Security2(1,user,password,False)
cmd0=client.setup0_request().encode('latin1'); wire=proto.session_pb2.SessionData(); wire.ParseFromString(cmd0)
N,g=srp6a.get_ng(srp6a.NG_3072); H=hashlib.sha512; k=srp6a.H(H,N,g,width=384); x=srp6a.calculate_x(H,salt,user,password); v=pow(g,x,N); B=(k*v+pow(g,b,N))%N
r0=proto.session_pb2.SessionData(sec_ver=2); r0.sec2.msg=1; r0.sec2.sr0.device_pubkey=bn(B); r0.sec2.sr0.device_salt=salt; resp0=r0.SerializeToString()
client.setup0_response(resp0.decode('latin1')); cmd1=client.setup1_request().encode('latin1')
A=int.from_bytes(wire.sec2.sc0.client_pubkey,'big'); u=srp6a.H(H,A,B,width=384); K=H(bn(pow(A*pow(v,u,N),b,N))).digest(); M=srp6a.calculate_M(H,N,g,user,int.from_bytes(salt,'big'),A,B,K)
wire.ParseFromString(cmd1); assert wire.sec2.sc1.client_proof==M
nonce=bytes(range(8))+b'\0\0\0\1'; r1=proto.session_pb2.SessionData(sec_ver=2); r1.sec2.msg=3; r1.sec2.sr1.device_proof=srp6a.calculate_H_AMK(H,A,M,K); r1.sec2.sr1.device_nonce=nonce; resp1=r1.SerializeToString(); client.setup1_response(resp1.decode('latin1'))
plain=b'{"id":"public-task","challenge":"public-synthetic-challenge"}'; encrypted=client.encrypt_data(plain); assert AESGCM(K[:32]).decrypt(nonce,encrypted,None)==plain
reply=b'{"ok":true}'; cipher_reply=AESGCM(K[:32]).encrypt(nonce[:8]+(2).to_bytes(4,'big'),reply,None); assert client.decrypt_data(cipher_reply)==reply
try:
 AESGCM(K[:32]).decrypt(nonce,encrypted[:-1]+bytes([encrypted[-1]^1]),None)
 raise AssertionError('tamper accepted')
except InvalidTag: pass
out=dict(source_commit='fff9895c82d744c7237be8847347bdd1b07c6643',username=user,password=password,random=random.hex(),cmd0=cmd0.hex(),resp0=resp0.hex(),cmd1=cmd1.hex(),resp1=resp1.hex(),plain=plain.hex(),encrypted=encrypted.hex(),reply=reply.hex(),cipher_reply=cipher_reply.hex())
args.output.write_text(json.dumps(out,indent=2)+'\n'); print('official Security2 handshake + Python AES-GCM bidirectional + tamper PASS')
