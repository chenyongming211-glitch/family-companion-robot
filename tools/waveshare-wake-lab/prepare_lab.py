"""Inject bounded USB wake diagnostics into a fresh locked product tree."""
import argparse
import hashlib
import json
import pathlib
import re


def digest(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def prepare(tree, source):
    here = pathlib.Path(__file__).resolve().parent
    tree = pathlib.Path(tree)
    if not re.fullmatch('[0-9a-f]{40}', source):
        raise ValueError('source commit')
    manifest = tree/'wake-lab-manifest.json'
    if manifest.exists():
        raise ValueError('fresh tree required')
    changes = {}

    def edit(name, old, new):
        p = tree/name
        text = p.read_text()
        if text.count(old) != 1:
            raise ValueError('nonunique anchor: '+name)
        changes.setdefault(name, {'before': digest(p)})
        p.write_text(text.replace(old, new, 1))

    for src, dst in [('probe.h', 'wave_wake_probe.h'),
                     ('runtime.h', 'wave_wake_runtime.h'),
                     ('console.inc', 'wave_wake_console.inc')]:
        (tree/'main/audio'/dst).write_bytes((here/src).read_bytes())
    board = 'main/boards/waveshare/esp32-s3-touch-amoled-2.16/'
    edit(board+'xiaoman_box_codec.h', '#include "experiment_gate.h"',
         '#include "experiment_gate.h"\n#include "wave_wake_runtime.h"')
    edit(board+'xiaoman_box_lifecycle.inc', '    const bool read=BoxAudioCodec::InputData(data);',
         '    const bool read=BoxAudioCodec::InputData(data);\n    if(read)wave_wake_lab::Raw(data.data(),data.size());')
    edit('main/audio/engines/afe_audio_engine.cc', '#include "audio_service.h"',
         '#include "audio_service.h"\n#include "wave_wake_runtime.h"')
    edit('main/audio/engines/afe_audio_engine.cc', '        NotifyProcessedCaptureReady(capture);',
         '        wave_wake_lab::Processed(result->data,result->data_size/sizeof(int16_t));\n        NotifyProcessedCaptureReady(capture);')
    edit('main/application.cc', '#include "websocket_protocol.h"',
         '#include "websocket_protocol.h"\n#include "wave_wake_console.inc"')
    edit('main/application.cc', '    audio_service_.Start();\n    ESP_LOGI(TAG, "After board/audio init");',
         '    audio_service_.Start();\n    wave_wake_lab::Start(this);\n    ESP_LOGI(TAG, "After board/audio init");')
    for name, entry in changes.items():
        entry['after'] = digest(tree/name)
    data = {'kind': 'waveshare-wake-lab-only', 'source_sha': source,
            'dma_descriptors': 6, 'probe_max_seconds': 10, 'changes': changes,
            'added': {p.relative_to(tree).as_posix(): digest(p)
                      for p in (tree/'main/audio').glob('wave_wake_*')}}
    manifest.write_text(json.dumps(data, indent=2)+'\n')


def finalize(tree):
    tree = pathlib.Path(tree)
    p = tree/'wake-lab-manifest.json'
    data = json.loads(p.read_text())
    for name, entry in data['changes'].items():
        if digest(tree/name) != entry['after']:
            raise ValueError('source drift')
    for name, sha in data['added'].items():
        if digest(tree/name) != sha:
            raise ValueError('diagnostic drift')
    data['build_sha256'] = {name: digest(tree/name) for name in (
        'build/xiaozhi.bin', 'build/xiaozhi.elf', 'build/generated_assets.bin',
        'build/partition_table/partition-table.bin', 'sdkconfig')}
    p.write_text(json.dumps(data, indent=2)+'\n')


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    p.add_argument('--tree', required=True)
    p.add_argument('--source-sha')
    p.add_argument('--finalize', action='store_true')
    a = p.parse_args()
    if a.finalize:
        finalize(a.tree)
    else:
        prepare(a.tree, a.source_sha or '')
