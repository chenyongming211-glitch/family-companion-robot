"""Bounded lab HTTP fixture: fixed generated Opus only, no production routes."""
import argparse
import http.server
import pathlib
import socket
import struct
import time

def main():
    p=argparse.ArgumentParser();p.add_argument('--fixture',required=True);p.add_argument('--port',type=int,default=18055);a=p.parse_args()
    wire=pathlib.Path(a.fixture).read_bytes();frames=[];pos=0
    while pos<len(wire):
        n=struct.unpack_from('<H',wire,pos)[0]
        if not 0<n<=4096 or pos+2+n>len(wire):raise ValueError('invalid_fixture')
        frames.append(wire[pos:pos+2+n]);pos+=2+n
    if len(frames)!=200:raise ValueError('invalid_fixture_count')
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self,*args):pass
        def do_GET(self):
            if self.path not in ('/burst','/paced'):self.send_error(404);return
            self.connection.setsockopt(socket.IPPROTO_TCP,socket.TCP_NODELAY,1)
            self.send_response(200);self.send_header('Content-Type','application/octet-stream');self.send_header('Content-Length',str(len(wire)));self.end_headers()
            start=time.monotonic()
            try:
                for i,f in enumerate(frames):
                    if self.path=='/paced':time.sleep(max(0,start+i*.060-time.monotonic()))
                    self.wfile.write(f);self.wfile.flush()
                print('fixture_sent',self.path,len(frames),round(time.monotonic()-start,6),flush=True)
            except (BrokenPipeError,ConnectionResetError):print('fixture_disconnected',flush=True)
    server=http.server.ThreadingHTTPServer(('0.0.0.0',a.port),Handler)
    print('fixture_ready',a.port,flush=True);server.serve_forever()

if __name__=='__main__':main()
