"""Production PCM task, span, cue mapping, updater and lifecycle interleavings."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]

class C3PcmMediaTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)
        cls.generated=Path(os.environ.get('FIRMWARE_PCM_GENERATED',cls.work/'generated'))
        if not (cls.generated/'family_c3_pcm_cues.pack').exists():
            result=subprocess.run(['python3',str(FIRMWARE/'overlays/scripts/family_c3_pcm_cues.py'),
                '--source-root',str(cls.source),'--output-dir',str(cls.generated)],capture_output=True,text=True)
            if result.returncode:raise AssertionError(result.stdout+result.stderr)
        table=(cls.source/'main/assets.cc').read_text()
        methods='\n'.join(defaults.method(table,signature) for signature in [
            'uint32_t Assets::LvglStrategy::CalculateChecksum(',
            'bool Assets::LvglStrategy::InitializePartition(', 'bool Assets::EmoteStrategy::InitializePartition('])
        assets=(FIRMWARE/'tests/c3_pcm_assets_host.cc').read_text().replace('// PRODUCTION_TABLE_METHODS',methods)
        ws_source=(cls.source/'main/protocols/websocket_protocol.cc').read_text()
        ws=(FIRMWARE/'tests/c3_pcm_ws_open_host.cc').read_text().replace('// PRODUCTION_WS_METHODS',
            '\n'.join(defaults.method(ws_source,signature) for signature in [
                'bool WebsocketProtocol::OpenAudioChannel(', 'bool WebsocketProtocol::IsAudioChannelOpened(',
                'bool WebsocketProtocol::SendAudio(', 'std::string WebsocketProtocol::GetHelloMessage(']))
        app_source=(cls.source/'main/application.cc').read_text()
        unsupported_methods=[defaults.method(app_source,signature) for signature in [
            'void Application::HandleToggleChatEvent(', 'void Application::HandleStartListeningEvent(',
            'void Application::StartNotification(']]
        unsupported_methods.append(defaults.method((cls.source/'main/audio/audio_service.cc').read_text(),'void AudioService::EnableAudioTesting('))
        unsupported_methods.append(defaults.method((cls.source/'main/notify/notify_player.cc').read_text(),'bool NotifyPlayer::Start('))
        unsupported=(FIRMWARE/'tests/c3_pcm_unsupported_host.cc').read_text().replace('// PRODUCTION_UNSUPPORTED_METHODS','\n'.join(unsupported_methods))
        media=(FIRMWARE/'tests/c3_pcm_media_host.cc').read_text().replace('// PRODUCTION_RESOURCES_METHOD',
            defaults.method((cls.source/'main/audio/audio_service.cc').read_text(),'void AudioService::PrintC3Resources() const')).replace('// PRODUCTION_OPEN_METHOD',defaults.method((cls.source/'main/audio/audio_service.cc').read_text(),'bool AudioService::OpenC3Uplink('))
        initialize=(FIRMWARE/'tests/c3_pcm_initialize_host.cc').read_text().replace('// PRODUCTION_INITIALIZE_METHOD',
            defaults.method((cls.source/'main/audio/audio_service.cc').read_text(),'void AudioService::Initialize('))
        disconnect=(FIRMWARE/'tests/c3_pcm_disconnect_host.cc').read_text()
        playback=(FIRMWARE/'overlays/main/audio/playback_websocket.inc').read_text()
        pcm=(FIRMWARE/'overlays/main/audio/c3_pcm_protocol.inc').read_text()
        ws+='\n'+'\n'.join(defaults.method(pcm,signature) for signature in ['void WebsocketProtocol::RequestC3PcmDisconnect(', 'void WebsocketProtocol::PollC3PcmDisconnect('])
        disconnect_methods=[defaults.method(ws_source,signature) for signature in [
            'WebsocketProtocol::WebsocketProtocol()', 'WebsocketProtocol::~WebsocketProtocol()',
            'bool WebsocketProtocol::OpenAudioChannel(', 'bool WebsocketProtocol::SendAudio(',
            'std::string WebsocketProtocol::GetHelloMessage(']]
        disconnect_methods += [defaults.method(playback,signature) for signature in [
            'bool WebsocketProtocol::ResetPlaybackConnection(', 'void WebsocketProtocol::NotifyPlaybackClosed(',
            'void WebsocketProtocol::CloseAudioChannel(']]
        for signature in ['void WebsocketProtocol::RequestC3PcmDisconnect(', 'void WebsocketProtocol::PollC3PcmDisconnect(']:
            disconnect_methods.append(defaults.method(pcm,signature))
        disconnect_methods.append(defaults.method((FIRMWARE/'overlays/main/audio/c3_pcm_audio.inc').read_text(),'void AudioService::WakeC3PcmOwner('))
        disconnect=disconnect.replace('// PRODUCTION_DISCONNECT_METHODS','\n'.join(disconnect_methods))
        cls.binaries={}
        for name,program,libs in [('media',media,['-lcrypto']),
                ('assets',assets,[]),('disconnect',disconnect,[]),('initialize',initialize,[]),('ws',ws,[]),('unsupported',unsupported,[]),('revoke',(FIRMWARE/'tests/c3_pcm_revocation_host.cc').read_text(),[]),
                ('lite',(FIRMWARE/'tests/c3_pcm_lite_host.cc').read_text(),[]),
                ('protocol',(FIRMWARE/'tests/c3_pcm_protocol_host.cc').read_text(),[])]:
            source=cls.work/(name+'.cc');source.write_text(program)
            for mode,flags in [('optimized',['-O2']),('sanitized',['-O2','-g','-fsanitize=address,undefined','-fno-sanitize-recover=all'])]:
                binary=cls.work/(name+'-'+mode)
                result=subprocess.run(['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                    '-Wno-unused-function',*flags,'-DCONFIG_XMINI_C3_BUTTON_ENTRY=1','-DCONFIG_XMINI_C3_VOICE_BARGE=1',
                    '-DCONFIG_XMINI_C3_PCM=1','-I',str(FIRMWARE/'overlays/main/audio'),
                    '-I',str(FIRMWARE/'tests/c3_sdk_stubs'),'-I',str(cls.generated),str(source),*libs,'-o',str(binary)],capture_output=True,text=True)
                if result.returncode:raise AssertionError(name+' '+mode+'\n'+result.stderr)
                cls.binaries[name,mode]=binary

    def run_case(self,name,case=None):
        for mode in ('optimized','sanitized'):
            with self.subTest(mode=mode):
                env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode=='sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                args=[str(self.binaries[name,mode])]+([case] if case else [])
                if name=='media':args += [str(self.generated/'family_c3_pcm_cues.pack'),str(self.source)]
                result=subprocess.run(args,env=env,capture_output=True,text=True,timeout=15)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_nonblocking_token_revocation_while_transport_owns_dispatch(self):self.run_case('revoke')
    def test_actual_registered_disconnect_revokes_without_dispatch_wait_and_owner_retires_exact_socket(self):
        self.run_case('disconnect')
        owner=defaults.method((self.source/'main/application.cc').read_text(),'void Application::Run(')
        self.assertLess(owner.index('protocol_->PollC3PcmDisconnect()'),owner.index('FinishC3PcmFault()'))
    def test_actual_initialize_uses_pcm_callback_and_input_src_with_no_opus_or_output_src_platform_api(self):self.run_case('initialize')
    def test_pcm_flag_is_opt_in_and_enabled_only_for_family_c3(self):
        config=json.loads((self.source/'main/boards/xmini/c3/config.json').read_text())
        for variant in config['builds']:
            self.assertEqual('CONFIG_XMINI_C3_PCM=y' in variant['sdkconfig_append'],variant['name']=='xmini-c3-family')
        block=(self.source/'main/Kconfig.projbuild').read_text().split('config XMINI_C3_PCM',1)[1].split('\nconfig ',1)[0]
        self.assertIn('default n',block);self.assertIn('depends on XMINI_C3_BUTTON_ENTRY && XMINI_C3_VOICE_BARGE',block)
    def test_actual_lite_engine_bypasses_vectors_and_stops_after_callback_close(self):self.run_case('lite')
    def test_diagnostics_query_only_live_input_output_tasks_and_resume_before_heap_dsp_or_logs(self):self.run_case('media','resources')
    def test_input_span_epoch_adoption_and_original_packet_epoch(self):self.run_case('media','input')
    def test_input_full_revokes_without_waiting_for_inflight_send(self):self.run_case('media','busy-full')
    def test_pack_mapping_verifies_sha_and_location_before_admission(self):self.run_case('media','mapping')
    def test_pcm_hash_uses_public_pinned_sdk_header_only_when_pcm_is_enabled(self):
        source=(self.source/'main/audio/audio_service.cc').read_text()
        includes=source.split('#if CONFIG_XMINI_C3_PCM',1)[1].split('#endif',1)[0]
        self.assertIn('#include <psa/crypto.h>',includes)
        self.assertNotIn('mbedtls/sha256.h',includes)
    def test_pcm_sha256_matches_real_digest_for_empty_text_and_binary_inputs(self):self.run_case('media','hash')
    def test_pcm_sha256_rejects_init_failure_without_hashing_then_can_retry(self):self.run_case('media','hash-init-error')
    def test_pcm_sha256_rejects_hash_failure(self):self.run_case('media','hash-error')
    def test_pcm_sha256_rejects_success_with_non_sha256_output_lengths(self):self.run_case('media','hash-length')
    def test_cue_pack_hash_init_failure_unmaps_without_admission(self):self.run_case('media','hash-mapping-init-error')
    def test_cue_pack_hash_failure_unmaps_without_admission(self):self.run_case('media','hash-mapping-error')
    def test_cue_pack_hash_short_length_unmaps_without_admission(self):self.run_case('media','hash-mapping-length')
    def test_cue_hash_failure_revokes_without_admission(self):self.run_case('media','hash-cue-error')
    def test_cue_hash_short_length_revokes_without_admission(self):self.run_case('media','hash-cue-length')
    def test_inflight_output_counts_toward_five_and_survives_reset(self):self.run_case('media','output-reset')
    def test_cues_order_wait_for_server_drain_and_cancel_entire_old_tail(self):self.run_case('media','cues')
    def test_start_stop_and_task_creation_failure_unwind(self):self.run_case('media','lifecycle')
    def test_stop_waits_for_actual_output_completion(self):self.run_case('media','barrier')
    def test_stop_timeout_permanently_blocks_reusing_worker_state(self):self.run_case('media','timeout')
    def test_assets_validate_header_and_every_read_before_trailer_write(self):self.run_case('assets','download')
    def test_actual_lvgl_method_rejects_bad_length_count_name_and_entry_ranges(self):self.run_case('assets','table')
    def test_unsupported_recording_and_notification_methods_return_before_state_or_power_changes(self):self.run_case('unsupported')
    def test_actual_open_rejects_wss_and_version_before_connection_and_emits_pcm_hello(self):self.run_case('ws')
    def test_current_invalid_stop_revokes_without_follow_on_admission_and_stale_or_valid_controls_are_safe(self):self.run_case('protocol','invalid-stop')
    def test_strict_pcm_hello_and_borrowed_ingress_keep_original_identity(self):self.run_case('protocol')

if __name__=='__main__':unittest.main()
