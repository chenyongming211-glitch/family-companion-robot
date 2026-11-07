"""Compare mmap assets by named payload; tolerate only model/emoji ordering."""
import argparse
import hashlib
import json
import pathlib
import struct

def sha(data):return hashlib.sha256(data).hexdigest()

def model_payloads(data):
    count,=struct.unpack_from('<I',data);pos=4;entries={};ranges=[]
    if not 1<=count<=16:raise ValueError('model_count')
    for _ in range(count):
        name,n=struct.unpack_from('<32sI',data,pos);pos+=36
        name=name.split(b'\0')[0].decode('ascii')
        if not 1<=n<=512:raise ValueError('model_files')
        for _ in range(n):
            file,offset,size=struct.unpack_from('<32sII',data,pos);pos+=40
            key=name+'/'+file.split(b'\0')[0].decode('ascii')
            if key in entries or not size or offset+size>len(data):raise ValueError('model_payload')
            entries[key]=sha(data[offset:offset+size]);ranges.append((offset,offset+size))
    ranges.sort()
    if ranges[0][0]<pos or any(b[0]<a[1] for a,b in zip(ranges,ranges[1:])):raise ValueError('model_overlap')
    return entries

def canonical(data):
    n,check,size=struct.unpack_from('<III',data);body=data[12:]
    if not 1<=n<=4096 or size!=len(body) or n*44>size or sum(body)&65535!=check:raise ValueError('asset_header')
    content=body[n*44:];files={};dimensions={}
    for i in range(n):
        name,size,offset,w,h=struct.unpack_from('<32sIIHH',body,i*44);name=name.split(b'\0')[0].decode()
        if name in files or offset+2+size>len(content) or content[offset:offset+2]!=b'ZZ':raise ValueError('asset_entry')
        files[name]=content[offset+2:offset+2+size];dimensions[name]=[w,h]
    index=json.loads(files['index.json']);emojis=index['emoji_collection']
    if len({e['name'] for e in emojis})!=len(emojis):raise ValueError('duplicate_emoji')
    index['emoji_collection']=sorted(emojis,key=lambda e:(e['name'],e['file']))
    models_name=index['srmodels'];models=model_payloads(files[models_name])
    ordinary={name:sha(value) for name,value in files.items() if name not in ('index.json',models_name)}
    return {'index':index,'model_payloads':models,'ordinary_payloads':ordinary,'dimensions':dimensions}

def compare(new,stored):
    a,b=canonical(new),canonical(stored)
    return {'compatible':a==b,'new_sha256':sha(new),'stored_sha256':sha(stored),'bytes':len(stored),'named_assets':len(a['dimensions']),'model_files':len(a['model_payloads']),'allowed_differences':['model payload storage order','emoji collection order by unique name'],'ordinary_payloads_equal':a['ordinary_payloads']==b['ordinary_payloads'],'model_payloads_equal':a['model_payloads']==b['model_payloads'],'canonical_index_equal':a['index']==b['index']}

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--new',required=True);p.add_argument('--stored',required=True);p.add_argument('--output',required=True);a=p.parse_args()
    proof=compare(pathlib.Path(a.new).read_bytes(),pathlib.Path(a.stored).read_bytes())
    with pathlib.Path(a.output).open('x') as f:json.dump(proof,f,indent=2);f.write('\n')
    print(json.dumps(proof));raise SystemExit(0 if proof['compatible'] else 2)
