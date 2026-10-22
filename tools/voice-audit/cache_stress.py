"""Synthetic host audit; compile actual supplied source, do not interact with hardware."""

import hashlib, json, pathlib, subprocess
import argparse

parser = argparse.ArgumentParser(
    description="Exercise the supplied firmware ring cache and epoch guard under host ASAN/UBSAN; no device access."
)
parser.add_argument("--source-root", type=pathlib.Path, required=True)
parser.add_argument("--out", type=pathlib.Path, required=True)
args = parser.parse_args()
source = args.source_root / "main"
out = args.out
art = args.out
out.mkdir(parents=True, exist_ok=False)
stub = out / "stubs"
stub.mkdir()
(stub / "esp_heap_caps.h").write_text(
    "#pragma once\n#include <cstdlib>\n#define MALLOC_CAP_SPIRAM 1\n#define MALLOC_CAP_8BIT 2\nextern bool fail_allocation;\ninline void* heap_caps_malloc(size_t n,int){return fail_allocation?nullptr:std::malloc(n);}\ninline void heap_caps_free(void* p){std::free(p);}\n"
)
(stub / "esp_log.h").write_text(
    "#pragma once\n#define ESP_LOGE(...) do{}while(0)\n#define ESP_LOGI(...) do{}while(0)\n"
)
code = r"""
#include <algorithm>
#include <atomic>
#include <cassert>
#include <deque>
#include <iostream>
#include <random>
#include <thread>
#include <vector>
#include "wake_word_audio_cache.h"
#include "experiment_gate.h"
bool fail_allocation=false;
int main(){
 WakeWordAudioCache fail;assert(!fail.Initialize(0));fail_allocation=true;assert(!fail.Initialize(32000));fail_allocation=false;assert(fail.Initialize(32000));assert(fail.Initialize(32000));assert(!fail.Initialize(64));
 std::mt19937 rng(20261023);size_t comparisons=0;
 for(size_t cap:{size_t(1),size_t(17),size_t(32000)}){WakeWordAudioCache cache;assert(cache.Initialize(cap));std::deque<int16_t> expected;
 for(int op=0;op<12000;++op){unsigned choice=rng()%8;
 if(choice==0){cache.Clear();expected.clear();}
 else if(choice<6){size_t n=op%97==0?cap+201:rng()%201;std::vector<int16_t> in(n);for(auto& s:in)s=static_cast<int16_t>(rng());cache.Store(in.data(),in.size());for(auto s:in){expected.push_back(s);if(expected.size()>cap)expected.pop_front();}}
 else{size_t offset=rng()%(cap+3),n=rng()%(cap+3);std::vector<int16_t> result(n+1,123);size_t got=cache.Read(offset,result.data(),n);size_t want=offset<expected.size()?std::min(n,expected.size()-offset):0;assert(got==want);for(size_t i=0;i<got;++i)assert(result[i]==expected[offset+i]);assert(result[n]==123);++comparisons;}
 assert(cache.Size()==expected.size());assert(cache.Read(0,nullptr,100)==0);cache.Store(nullptr,100);
 }
 }
 WakeWordAudioCache concurrent;assert(concurrent.Initialize(32000));std::thread a([&]{int16_t pcm[480]{};for(int i=0;i<10000;++i)concurrent.Store(pcm,480);});std::thread b([&]{int16_t pcm[480]{};for(int i=0;i<10000;++i){assert(concurrent.Read(0,pcm,480)<=480);assert(concurrent.Size()<=32000);}});std::thread c([&]{for(int i=0;i<10000;++i)concurrent.Clear();});a.join();b.join();c.join();
 for(uint32_t batch:{0u,1u,100u,0xffffffffu})for(uint32_t gen:{0u,1u,100u,0xffffffffu}){assert(family_robot::WakeBatchValid(batch,batch,gen,gen));assert(!family_robot::WakeBatchValid(batch,batch+1,gen,gen));assert(!family_robot::WakeBatchValid(batch,batch,gen,gen+1));}
 std::cout<<"{\"seed\":20261023,\"cache_operations\":36000,\"value_comparisons\":"<<comparisons<<",\"concurrent_calls\":40000,\"wake_batch_checks\":48,\"status\":\"passed\"}\n";
}
"""
(out / "cache_stress.cc").write_text(code)
cmd = [
    "c++",
    "-std=c++17",
    "-pthread",
    "-Wall",
    "-Wextra",
    "-Werror",
    "-pedantic",
    "-fsanitize=address,undefined",
    "-fno-sanitize-recover=all",
    "-I" + str(stub),
    "-I" + str(source / "audio/wake_words"),
    "-I" + str(source / "audio"),
    str(source / "audio/wake_words/wake_word_audio_cache.cc"),
    str(out / "cache_stress.cc"),
    "-o",
    str(out / "cache_stress"),
]
r = subprocess.run(cmd, capture_output=True, text=True)
(art / "cache-compile.log").write_text(r.stdout + r.stderr)
r.check_returncode()
r = subprocess.run(
    [str(out / "cache_stress")], capture_output=True, text=True, timeout=30
)
(art / "cache-runtime.log").write_text(r.stdout + r.stderr)
r.check_returncode()
print(r.stdout)
result = json.loads(r.stdout)
result["source_sha256"] = {
    str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest()
    for p in [
        source / "audio/wake_words/wake_word_audio_cache.cc",
        source / "audio/wake_words/wake_word_audio_cache.h",
        source / "audio/experiment_gate.h",
    ]
}
result["asan_ubsan"] = True
result["tsan"] = False
(art / "cache-stress.json").write_text(json.dumps(result, indent=2) + "\n")
