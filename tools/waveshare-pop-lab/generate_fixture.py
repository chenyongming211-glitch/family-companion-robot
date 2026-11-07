"""Cloud-only deterministic PCM/Opus fixture; no user audio or provider keys."""
import argparse
import ctypes
import hashlib
import json
import math
import pathlib
import struct

FRAMES=200

def generate(out, url):
    opus=ctypes.CDLL('libopus.so.0')
    opus.opus_encoder_create.argtypes=[ctypes.c_int,ctypes.c_int,ctypes.c_int,ctypes.POINTER(ctypes.c_int)]
    opus.opus_encoder_create.restype=ctypes.c_void_p
    opus.opus_encode.argtypes=[ctypes.c_void_p,ctypes.POINTER(ctypes.c_int16),ctypes.c_int,ctypes.POINTER(ctypes.c_ubyte),ctypes.c_int]
    opus.opus_encoder_destroy.argtypes=[ctypes.c_void_p]
    opus.opus_packet_get_nb_samples.argtypes=[ctypes.POINTER(ctypes.c_ubyte),ctypes.c_int,ctypes.c_int]
    error=ctypes.c_int()
    encoder=opus.opus_encoder_create(24000,1,2049,ctypes.byref(error))
    if not encoder or error.value: raise RuntimeError('encoder_create_failed')
    # Explicit fixed bitrate, low complexity, no DTX. ctypes varargs require the
    # opaque pointer and int arguments to be explicitly typed on 64-bit hosts.
    for request,value in [(4002,24000),(4010,0),(4016,0)]:
        if opus.opus_encoder_ctl(ctypes.c_void_p(encoder),ctypes.c_int(request),ctypes.c_int(value)):
            raise RuntimeError('encoder_configuration_failed')
    pcm=(ctypes.c_int16*1440)(); buffer=(ctypes.c_ubyte*4096)()
    packets=[]
    try:
        for frame in range(FRAMES):
            for j in range(1440):
                sample=frame*1440+j
                fade=min(1.0,sample/240,(FRAMES*1440-1-sample)/240)
                pcm[j]=round(3000*fade*math.sin(2*math.pi*400*sample/24000))
            n=opus.opus_encode(encoder,pcm,1440,buffer,4096)
            if n<=0 or opus.opus_packet_get_nb_samples(buffer,n,24000)!=1440:
                raise RuntimeError('fixture_packet_failed')
            packets.append(bytes(buffer[:n]))
    finally: opus.opus_encoder_destroy(encoder)
    data=b''.join(packets); offsets=[0]
    for p in packets: offsets.append(offsets[-1]+len(p))
    out=pathlib.Path(out);out.mkdir(parents=True,exist_ok=True)
    wire=b''.join(struct.pack('<H',len(p))+p for p in packets)
    (out/'fixture.bin').write_bytes(wire)
    header='#pragma once\nnamespace wave_pop_lab {\n'
    header+='inline constexpr unsigned char kOpus[]={'+','.join(map(str,data))+'};\n'
    header+='inline constexpr unsigned kOffsets[]={'+','.join(map(str,offsets))+'};\n'
    header+='inline constexpr int kFrames=200;\ninline constexpr char kFixtureUrl[]='+json.dumps(url)+';\n}\n'
    (out/'fixture.h').write_text(header)
    (out/'fixture.json').write_text(json.dumps({'sample_rate':24000,'frames':FRAMES,'samples_per_frame':1440,'frequency_hz':400,'pcm_amplitude':3000,'wire_sha256':hashlib.sha256(wire).hexdigest()},indent=2)+'\n')

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--output',required=True);p.add_argument('--fixture-url',default='');a=p.parse_args()
    if a.fixture_url and not a.fixture_url.startswith('http://'):p.error('fixture must use explicit HTTP lab URL')
    generate(a.output,a.fixture_url)
