"""Inject bounded USB wake diagnostics into a fresh locked product tree."""
import argparse
import hashlib
import json
import pathlib
import re
import textwrap


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
    edit('main/audio/engines/afe_audio_engine.cc',
         '        EventBits_t bits = xEventGroupGetBits(event_group_);\n        if (bits & kWakeWordEnabled)',
         '        EventBits_t bits = xEventGroupGetBits(event_group_);\n        wave_wake_lab::Count((bits & kWakeWordEnabled) ? wave_wake_lab::Step::WakeEnabled : wave_wake_lab::Step::WakeDisabled);\n        if (bits & kWakeWordEnabled)')
    edit('main/audio/engines/afe_audio_engine.cc',
         '        if (emitted_wake_generation_ != codec_->WakeAudioGeneration())\n            return;',
         '        if (emitted_wake_generation_ != codec_->WakeAudioGeneration()) {\n            wave_wake_lab::Count(wave_wake_lab::Step::CacheRejected);\n            return;\n        }')
    edit('main/audio/wake_words/custom_wake_word.cc', '#include "assets.h"',
         '#include "assets.h"\n#include "wave_wake_runtime.h"\n#include <cstdio>')
    edit('main/audio/wake_words/custom_wake_word.cc',
         'void CustomWakeWord::ResetCapture() {',
         'void CustomWakeWord::ResetCapture() {\n    wave_wake_lab::Count(wave_wake_lab::Step::Reset);')
    edit('main/audio/wake_words/custom_wake_word.cc',
         '    // Check running state inside lock to avoid TOCTOU race with Stop()',
         '    wave_wake_lab::Count(wave_wake_lab::Step::Feed);\n    // Check running state inside lock to avoid TOCTOU race with Stop()')
    edit('main/audio/wake_words/custom_wake_word.cc',
         '    if (!running_) {\n        return;\n    }',
         '    if (!running_) {\n        wave_wake_lab::Count(wave_wake_lab::Step::Stopped);\n        return;\n    }')
    edit('main/audio/wake_words/custom_wake_word.cc',
         '            return true;\n        input_buffer_.clear();',
         '            return true;\n        wave_wake_lab::Count(wave_wake_lab::Step::GenerationRejected);\n        input_buffer_.clear();')
    edit('main/audio/wake_words/custom_wake_word.h', '    float threshold_ = 0.2;',
         '    float threshold_ = 0.2;\n    unsigned wave_lab_threshold_applied_ = 0;')
    custom_name = 'main/audio/wake_words/custom_wake_word.cc'
    custom_text = (tree/custom_name).read_text()
    detect_line = 'esp_mn_state_t mn_state = multinet_->detect(multinet_model_data_, input_buffer_.data());'
    indent = re.search(r'^([ \t]*)'+re.escape(detect_line), custom_text, re.M).group(1)
    diagnostic = '''        // This runs under input_buffer_mutex_, alongside detect/clean.
        const auto requested = wave_wake_lab::threshold_percent.load();
        if (requested != wave_lab_threshold_applied_) {
            multinet_->set_det_threshold(multinet_model_data_, requested / 100.0f);
            wave_lab_threshold_applied_ = requested;
            printf("WWD_THRESHOLD applied=%u\\n", requested);
            fflush(stdout);
        }
        esp_mn_state_t mn_state = multinet_->detect(multinet_model_data_, input_buffer_.data());
        wave_wake_lab::Count(wave_wake_lab::Step::Infer);
        if (mn_state == ESP_MN_STATE_DETECTED) wave_wake_lab::Count(wave_wake_lab::Step::Detected);
        if (mn_state == ESP_MN_STATE_TIMEOUT) wave_wake_lab::Count(wave_wake_lab::Step::Timeout);'''
    edit(custom_name, indent+detect_line, textwrap.indent(textwrap.dedent(diagnostic), indent))
    speech_alignment = '// WAVE_SPEECH_WINDOW_RESET' in custom_text
    if speech_alignment:
        edit(custom_name, '            // WAVE_SPEECH_WINDOW_RESET',
             '            // WAVE_SPEECH_WINDOW_RESET\n            wave_wake_lab::Count(wave_wake_lab::Step::Reset);')
    edit('main/application.cc', '#include "websocket_protocol.h"',
         '#include "websocket_protocol.h"\n#include "wave_wake_console.inc"')
    edit('main/application.cc', '    audio_service_.Start();\n    ESP_LOGI(TAG, "After board/audio init");',
         '    audio_service_.Start();\n    wave_wake_lab::Start(this);\n    ESP_LOGI(TAG, "After board/audio init");')
    for name, entry in changes.items():
        entry['after'] = digest(tree/name)
    data = {'kind': 'waveshare-wake-lab-only', 'source_sha': source,
            'dma_descriptors': 6, 'probe_max_seconds': 10,
            'threshold_percent_choices': [15, 20], 'changes': changes,
            'model_flow_counters': True,
            'speech_window_alignment': speech_alignment,
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
