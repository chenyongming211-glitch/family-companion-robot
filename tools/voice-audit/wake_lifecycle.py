"""Synthetic host audit; compile actual supplied source, do not interact with hardware."""

import hashlib, json, pathlib, subprocess
import argparse

parser = argparse.ArgumentParser(
    description="Compile the supplied firmware wake method verbatim with a failure-injecting host transport; no device access."
)
parser.add_argument("--source-root", type=pathlib.Path, required=True)
parser.add_argument("--out", type=pathlib.Path, required=True)
args = parser.parse_args()
src = args.source_root / "main/application.cc"
out = args.out
art = args.out
out.mkdir(parents=True, exist_ok=False)
text = src.read_text()
start = text.index("void Application::ContinueWakeWordInvoke(")
end = text.index("\nvoid Application::HandleStateChangedEvent()", start)
method = text[start:end]
preamble = r"""
#include <cassert>
#include <cstdio>
#include <iostream>
#include <memory>
#include <string>
#include <vector>
#define CONFIG_STOPWATCH_SOFTWARE_AEC 1
#define CONFIG_SEND_WAKE_WORD_DATA 1
enum DeviceState {kDeviceStateIdle,kDeviceStateConnecting,kDeviceStateListening};
enum PowerSaveLevel {PERFORMANCE};
enum AecMode {kAecOff};
struct Board {static Board& GetInstance(){static Board b;return b;} void SetPowerSaveLevel(PowerSaveLevel){}};
struct Packet {int n;};
struct Protocol {bool opened=true,open_result=true;int open_calls=0,send_calls=0,fail_at=-1,detect=0;bool IsAudioChannelOpened(){return opened;} bool OpenAudioChannel(){++open_calls;return open_result;} bool SendAudio(std::unique_ptr<Packet>){return send_calls++!=fail_at;} void SendWakeWordDetected(const std::string&){++detect;}};
struct AudioService {int available=3,popped=0;bool valid=true;std::unique_ptr<Packet> PopWakeWordPacket(){if(popped==available)return nullptr;return std::make_unique<Packet>(Packet{popped++});}bool WakeAudioBatchValid(){return valid;}};
struct Application {DeviceState state=kDeviceStateConnecting;Protocol p;Protocol* protocol_=&p;AudioService audio_service_;int listening=0,aec_off=0;DeviceState GetDeviceState(){return state;}void SetDeviceState(DeviceState s){state=s;}void SetAecMode(AecMode){++aec_off;}int GetDefaultListeningMode(){return 0;}void SetListeningMode(int){++listening;state=kDeviceStateListening;}void ContinueWakeWordInvoke(const std::string&);};
"""
tail = r"""
int main(){
 for(int i=0;i<8;++i){Application a;const char* name="";
 switch(i){case 0:name="healthy";break;case 1:name="wrong_state";a.state=kDeviceStateIdle;break;case 2:name="open_failure";a.p.opened=false;a.p.open_result=false;break;case 3:name="invalid_batch";a.audio_service_.valid=false;break;case 4:name="first_audio_send_failure";a.p.fail_at=0;break;case 5:name="middle_audio_send_failure";a.p.fail_at=1;break;case 6:name="last_audio_send_failure";a.p.fail_at=2;break;case 7:name="empty_valid_batch";a.audio_service_.available=0;break;}
 a.ContinueWakeWordInvoke("synthetic-wake");
 std::cout<<"{\"case\":\""<<name<<"\",\"send_attempts\":"<<a.p.send_calls<<",\"detect_sent\":"<<a.p.detect<<",\"listening_started\":"<<a.listening<<",\"state\":"<<a.state<<",\"aec_off\":"<<a.aec_off<<"}\n";
 if(i==0)assert(a.listening==1&&a.p.detect==1&&a.p.send_calls==3);
 if(i==1)assert(a.listening==0&&a.p.detect==0&&a.p.send_calls==0);
 if(i==2)assert(a.state==kDeviceStateIdle&&a.listening==0&&a.p.detect==0);
 if(i==3)assert(a.aec_off==1&&a.listening==0&&a.p.detect==0);
 }
}
"""
(out / "wake_lifecycle.cc").write_text(preamble + method + tail)
cmd = [
    "c++",
    "-std=c++17",
    "-Wall",
    "-Wextra",
    "-Werror",
    "-pedantic",
    "-fsanitize=address,undefined",
    "-fno-sanitize-recover=all",
    str(out / "wake_lifecycle.cc"),
    "-o",
    str(out / "wake_lifecycle"),
]
r = subprocess.run(cmd, capture_output=True, text=True)
(art / "wake-compile.log").write_text(r.stdout + r.stderr)
r.check_returncode()
r = subprocess.run([str(out / "wake_lifecycle")], capture_output=True, text=True)
(art / "wake-runtime.log").write_text(r.stdout + r.stderr)
r.check_returncode()
cases = [json.loads(x) for x in r.stdout.splitlines()]
result = {
    "source_file_sha256": hashlib.sha256(src.read_bytes()).hexdigest(),
    "method_sha256": hashlib.sha256(method.encode()).hexdigest(),
    "method_extracted_verbatim": True,
    "asan_ubsan": True,
    "cases": cases,
    "failure_send_not_rejected": [
        c["case"]
        for c in cases
        if "audio_send_failure" in c["case"] and c["listening_started"]
    ],
}
(art / "wake-lifecycle.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result, indent=2))

# Exit2 means the injected failure was not rejected, not a compiler failure.
raise SystemExit(2 if result["failure_send_not_rejected"] else 0)
