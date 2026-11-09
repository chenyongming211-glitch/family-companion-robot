#!/usr/bin/env python3
"""Derive a private candidate NVS image using locked Espressif SRP and NVS tools.
Cloud only. Never flashes hardware or prints credentials; refuses output replacement.
This image is for NEW factory units, not an overlay for already-provisioned devices.
"""
import argparse, csv, hashlib, importlib.util, json, os, pathlib, re, stat, subprocess, sys, tempfile
FIRMWARE=pathlib.Path(__file__).resolve().parents[1]
def private_file(path):
    st=path.lstat()
    if not stat.S_ISREG(st.st_mode) or st.st_mode & 0o077 or st.st_size>8192: raise ValueError('private regular input required')
    return path.read_bytes()
def unique(pairs):
    out={}
    for key,value in pairs:
        if key in out: raise ValueError('duplicate input field')
        out[key]=value
    return out
def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--bundle',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);p.add_argument('--official-srp',type=pathlib.Path,required=True);p.add_argument('--idf-source',type=pathlib.Path,required=True);a=p.parse_args()
    lock=json.loads((FIRMWARE/'onboarding.lock.json').read_text())
    import importlib.metadata
    if importlib.metadata.version('esp-idf-nvs-partition-gen') != lock['nvs_generator_version']: raise ValueError('NVS package version mismatch')
    bundle=json.loads(private_file(a.bundle),object_pairs_hook=unique)
    expected={'schema_version','device_id','mac','serial','model','device_key','claim_code','bootstrap_url','voice_url'}
    if set(bundle)!=expected or bundle['schema_version']!=1: raise ValueError('invalid bundle schema')
    for key,pattern in [('device_id',r'dev_[0-9a-f]{32}'),('mac',r'(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}'),('serial',r'[A-Za-z0-9_.-]{1,64}'),('device_key',r'[A-Za-z0-9_-]{43}'),('claim_code',r'[0-9a-f]{32}')]:
        if not isinstance(bundle[key],str) or not re.fullmatch(pattern,bundle[key]): raise ValueError('invalid identity field')
    if bundle['model'] not in ['微雪 ESP32-S3','M5 ESP32-S3'] or bundle['bootstrap_url']!='https://robot.songguoxue.com/api/device/bootstrap' or bundle['voice_url']!='wss://robot.songguoxue.com/xiaozhi/v1/': raise ValueError('unsupported target')
    official=a.official_srp.resolve()
    for name in ['srp6a.py','utils.py']:
        if hashlib.sha256((official/name).read_bytes()).hexdigest()!=lock['official_python'][name]: raise ValueError('official SRP source hash mismatch')
    sys.path.insert(0,str(official)); spec=importlib.util.spec_from_file_location('factory_official_srp',official/'srp6a.py'); srp=importlib.util.module_from_spec(spec); spec.loader.exec_module(srp)
    salt=verifier=b''
    # Device C requires fixed verifier width; keep the official client integer encoding
    # unambiguous by generating a full-width salt/verifier rather than changing math.
    for _ in range(128):
        salt,verifier=srp.generate_salt_and_verifier(bundle['serial'],bundle['claim_code'],16)
        if len(salt)==16 and len(verifier)==384: break
    else: raise ValueError('SRP generation failed')
    generator=a.idf_source/'components/nvs_flash/nvs_partition_generator/nvs_partition_gen.py'
    if hashlib.sha256(generator.read_bytes()).hexdigest()!=lock['nvs_generator_sha256']: raise ValueError('NVS generator source hash mismatch')
    if not a.output.parent.is_dir() or a.output.parent.stat().st_mode & 0o077: raise ValueError('output directory must be private0700')
    os.umask(0o077)
    with tempfile.TemporaryDirectory(dir=a.output.parent,prefix='.factory-') as directory:
        directory=pathlib.Path(directory); (directory/'salt.bin').write_bytes(salt);(directory/'verifier.bin').write_bytes(verifier)
        with (directory/'factory.csv').open('w',newline='') as f:
            writer=csv.writer(f); writer.writerow(['key','type','encoding','value']);writer.writerow(['family_factory','namespace','',''])
            for key,value in [('device_id',bundle['device_id']),('mac',bundle['mac'].lower()),('serial',bundle['serial']),('key',bundle['device_key'])]:writer.writerow([key,'data','string',value])
            writer.writerow(['activated','data','i32','0'])
            for key in ['salt','verifier']:writer.writerow([key,'file','binary',str(directory/(key+'.bin'))])
        # Captured diagnostics may contain paths; never relay output or bundle values.
        subprocess.run([sys.executable,str(generator),'generate',str(directory/'factory.csv'),str(directory/'factory.bin'),'0x4000'],check=True,capture_output=True)
        fd=os.open(a.output,os.O_CREAT|os.O_EXCL|os.O_WRONLY|os.O_NOFOLLOW,0o600)
        with os.fdopen(fd,'wb') as f:f.write((directory/'factory.bin').read_bytes());f.flush();os.fsync(f.fileno())
    print('Private ESP32-S3 factory NVS candidate created; not flashed.')
if __name__=='__main__':
    try: main()
    except Exception: print('Factory NVS generation failed; no credentials emitted.',file=sys.stderr);sys.exit(1)
