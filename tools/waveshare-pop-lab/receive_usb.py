"""Cloud-only, exclusive private receiver for bounded lab USB captures."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import sys

class Receiver:
    def __init__(self,root,expected):
        self.root=pathlib.Path(root);self.root.mkdir(mode=0o700)
        if not 1<=expected<=8:raise ValueError('phase_count')
        self.expected=expected;self.states={};self.results={};self.done=False
        self.meta=self.private('metadata.jsonl')
    def private(self,name):
        fd=os.open(self.root/name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        os.fchmod(fd,0o600);return os.fdopen(fd,'wb')
    def phase(self,value):
        if type(value) is not int or not 0<=value<self.expected:raise ValueError('phase')
        return value
    def accept(self,d):
        if not isinstance(d,dict):raise ValueError('message')
        kind=d.get('kind')
        if kind=='capture':
            p=self.phase(d.get('phase'));n=d.get('samples')
            if p in self.states or type(n) is not int or not 0<=n<=480000 or n%2 or d.get('rate')!=24000 or d.get('channels')!=2:raise ValueError('capture_header')
            self.states[p]={'file':self.private(f'phase-{p}.pcm'),'expected':n*2,'bytes':0,'hash':hashlib.sha256(),'complete':False}
        elif kind=='data':
            p=self.phase(d.get('phase'));s=self.states.get(p)
            if s is None or s['complete'] or d.get('offset')!=s['bytes']:raise ValueError('capture_gap')
            value=d.get('hex')
            if not isinstance(value,str) or not re.fullmatch('[0-9a-f]{2,768}',value) or len(value)%4:raise ValueError('capture_data')
            raw=bytes.fromhex(value)
            if s['bytes']+len(raw)>s['expected']:raise ValueError('capture_overflow')
            s['file'].write(raw);s['hash'].update(raw);s['bytes']+=len(raw)
            return
        elif kind=='capture_end':
            p=self.phase(d.get('phase'));s=self.states.get(p)
            if s is None or s['complete'] or d.get('bytes')!=s['bytes'] or s['bytes']!=s['expected']:raise ValueError('capture_incomplete')
            s['file'].close();s['complete']=True
        elif kind=='result':
            line=d.get('line','')
            m=re.fullmatch(r'WPL_RESULT ([0-7]) [a-z0-9_= -]{1,1800}',line)
            if not m:raise ValueError('result')
            p=self.phase(int(m[1]))
            if p in self.results:raise ValueError('duplicate_result')
            self.results[p]=line
        elif kind=='meta':
            if not isinstance(d.get('line'),str) or not re.fullmatch(r'WPL_[A-Z_]+[a-zA-Z0-9_ =.-]{0,1900}',d['line']):raise ValueError('metadata')
        elif kind=='done':self.done=True
        else:raise ValueError('message_kind')
        self.meta.write((json.dumps(d,separators=(',',':'))+'\n').encode());self.meta.flush()
    def finish(self,error=None):
        for s in self.states.values():
            if not s['file'].closed:s['file'].close()
        self.meta.close()
        summary={'complete':not error and self.done and len(self.states)==self.expected and len(self.results)==self.expected and all(s['complete'] for s in self.states.values()),'expected_phases':self.expected,'done':self.done,'error':error,'captures':{str(p):{'bytes':s['bytes'],'expected_bytes':s['expected'],'complete':s['complete'],'sha256':s['hash'].hexdigest()} for p,s in self.states.items()},'results':self.results,'claim':'capture integrity only; not acoustic pass'}
        with self.private('summary.json') as f:f.write((json.dumps(summary,indent=2)+'\n').encode())
        return summary

def main():
    os.umask(0o077)
    p=argparse.ArgumentParser();p.add_argument('--output',required=True);p.add_argument('--expected-phases',type=int,default=8);a=p.parse_args()
    r=Receiver(a.output,a.expected_phases);error=None
    try:
        for line in sys.stdin:
            if len(line)>4096:raise ValueError('message_size')
            r.accept(json.loads(line))
    except (ValueError,OSError):error='receiver_input_error'
    summary=r.finish(error);print(json.dumps(summary),flush=True)
    return 0 if summary['complete'] else 2

if __name__=='__main__':sys.exit(main())
