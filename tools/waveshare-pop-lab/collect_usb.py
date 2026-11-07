"""Mac USB test client. Sends PCM directly to cloud; never opens a Mac mic."""
import argparse
import datetime
import json
import pathlib
import re
import shlex
import subprocess
import sys
import time
import serial
from serial.tools import list_ports

def main():
    p=argparse.ArgumentParser();p.add_argument('--device',required=True);p.add_argument('--ssh-host',required=True);p.add_argument('--ssh-key',required=True);p.add_argument('--receiver',required=True);p.add_argument('--output',required=True);p.add_argument('--duration',type=int,default=900);a=p.parse_args()
    if not 1<=a.duration<=1200:p.error('duration')
    ports=[v for v in list_ports.comports() if (v.serial_number or '').lower()==a.device.lower()]
    if len(ports)!=1:p.error('target USB identity must be unique')
    command=shlex.join(['python3',a.receiver,'--output',a.output,'--expected-phases','8'])
    child=subprocess.Popen(['ssh','-F','/dev/null','-i',str(pathlib.Path(a.ssh_key).resolve()),'-o','IdentitiesOnly=yes','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes',a.ssh_host,command],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
    port=serial.Serial(port=None,baudrate=115200,timeout=.1,exclusive=True);port.dtr=False;port.rts=False;port.port=ports[0].device
    pending=b'';done=False;error=None;counts={'discarded_lines':0,'pcm_bytes_forwarded':0};started=time.monotonic()
    def emit(value):
        value['observed_at']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        child.stdin.write(json.dumps(value,separators=(',',':'))+'\n');child.stdin.flush()
    try:
        port.open();print(json.dumps({'event':'usb_collector_ready','port':port.port}),flush=True)
        while time.monotonic()-started<a.duration:
            pending+=port.read(32768)
            if len(pending)>1024*1024:raise ValueError('serial_line_limit')
            lines=pending.split(b'\n');pending=lines.pop()
            for raw in lines:
                line=re.sub(r'\x1b\[[0-9;]*m','',raw.decode('ascii',errors='replace')).strip()
                if line.startswith('WPL_DATA '):
                    m=re.fullmatch(r'WPL_DATA ([0-7]) (\d+) ([0-9a-f]{2,768})',line)
                    if not m:raise ValueError('serial_data_format')
                    emit({'kind':'data','phase':int(m[1]),'offset':int(m[2]),'hex':m[3]});counts['pcm_bytes_forwarded']+=len(m[3])//2
                elif line.startswith('WPL_CAPTURE_END '):
                    m=re.fullmatch(r'WPL_CAPTURE_END ([0-7]) (\d+)',line)
                    if not m:raise ValueError('serial_capture_end')
                    emit({'kind':'capture_end','phase':int(m[1]),'bytes':int(m[2])});print(line,flush=True)
                elif line.startswith('WPL_CAPTURE '):
                    m=re.fullmatch(r'WPL_CAPTURE ([0-7]) (\d+) (\d+) (\d+)',line)
                    if not m:raise ValueError('serial_capture_header')
                    emit({'kind':'capture','phase':int(m[1]),'samples':int(m[2]),'rate':int(m[3]),'channels':int(m[4])})
                elif line.startswith('WPL_RESULT '):emit({'kind':'result','line':line});print(line,flush=True)
                elif line=='WPL_DONE':emit({'kind':'done'});done=True;break
                elif line.startswith('WPL_'):emit({'kind':'meta','line':line});print(line,flush=True)
                elif 'ELF file SHA256:' in line:
                    m=re.search(r'ELF file SHA256:\s+([0-9a-f]+)',line)
                    if m:emit({'kind':'meta','line':'WPL_ELF '+m[1]});print('WPL_ELF',m[1],flush=True)
                else:counts['discarded_lines']+=1
            if done:break
    except (serial.SerialException,BrokenPipeError,ValueError,KeyboardInterrupt) as e:
        error=type(e).__name__
    finally:
        if port.is_open:port.close()
        if child.stdin and not child.stdin.closed:child.stdin.close()
    try:child.wait(timeout=30)
    except subprocess.TimeoutExpired:child.kill();child.wait();error='receiver_timeout'
    output=child.stdout.read(16384);errors=child.stderr.read(2048)
    print(output.strip(),flush=True)
    print(json.dumps({'event':'usb_collector_stopped','done':done,'error':error,'elapsed_s':round(time.monotonic()-started,3),'counts':counts,'receiver_exit':child.returncode}),flush=True)
    if child.returncode and errors:print(errors,file=sys.stderr)
    return 0 if done and not error and child.returncode==0 else 2

if __name__=='__main__':sys.exit(main())
