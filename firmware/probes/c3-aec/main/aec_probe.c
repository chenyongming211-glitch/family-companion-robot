#include <inttypes.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include "esp_aec.h"
#include "esp_audio_enc.h"
#include "esp_audio_types.h"
#include "esp_opus_dec.h"
#include "esp_opus_enc.h"
#include "esp_heap_caps.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

// Deliberately never touches NVS, network, codec, OTA validity or eFuses.
// The pending image restarts so the installed bootloader rolls back to ota1.
enum { BLOCKS = 128, LOADED_BLOCKS = 240, MAX_FRAME = 1024 };
static int16_t mic[MAX_FRAME] __attribute__((aligned(16)));
static int16_t ref[MAX_FRAME] __attribute__((aligned(16)));
static int16_t out[MAX_FRAME] __attribute__((aligned(16)));
static int64_t times[BLOCKS];
static int16_t encode_pcm[960] __attribute__((aligned(16)));
static int16_t decode_pcm[1440] __attribute__((aligned(16)));
static uint32_t random_state = 0x12345678;
static int16_t signal_sample(void) {
    random_state = random_state * 1664525u + 1013904223u;
    return (int16_t)((int16_t)(random_state >> 16) / 4);
}
static int order_time(const void *a, const void *b) {
    const int64_t x=*(const int64_t*)a, y=*(const int64_t*)b;
    return (x>y)-(x<y);
}
static bool codec_load(aec_handle_t *aec,int n) {
    void *encoder=NULL,*decoder=NULL;
    uint8_t *packet=NULL;
    bool good=false;
    esp_opus_enc_config_t ec={
        .sample_rate=ESP_AUDIO_SAMPLE_RATE_16K,.channel=ESP_AUDIO_MONO,
        .bits_per_sample=ESP_AUDIO_BIT16,.bitrate=ESP_OPUS_BITRATE_AUTO,
        .frame_duration=ESP_OPUS_ENC_FRAME_DURATION_60_MS,
        .application_mode=ESP_OPUS_ENC_APPLICATION_AUDIO,.complexity=0,
        .enable_fec=false,.enable_dtx=true,.enable_vbr=true,
    };
    esp_opus_dec_cfg_t dc={.sample_rate=24000,.channel=ESP_AUDIO_MONO,
        .frame_duration=(esp_opus_dec_frame_duration_t)ESP_OPUS_ENC_FRAME_DURATION_60_MS,
        .self_delimited=false};
    if(esp_opus_enc_open(&ec,sizeof(ec),&encoder)!=ESP_AUDIO_ERR_OK || !encoder)goto end;
    if(esp_opus_dec_open(&dc,sizeof(dc),&decoder)!=ESP_AUDIO_ERR_OK || !decoder)goto end;
    int frame_bytes=0,packet_bytes=0;
    if(esp_opus_enc_get_frame_size(encoder,&frame_bytes,&packet_bytes)!=ESP_AUDIO_ERR_OK ||
       frame_bytes!=(int)sizeof(encode_pcm) || packet_bytes<=0 || packet_bytes>65536)goto end;
    packet=malloc(packet_bytes);if(!packet)goto end;
    size_t used=0;unsigned packets=0;
    int64_t aec_us=0,encode_us=0,decode_us=0,max_combo_us=0;
    uint64_t decoded_energy=0;
    for(int block=0;block<LOADED_BLOCKS;++block) {
        for(int i=0;i<n;++i) {ref[i]=signal_sample();mic[i]=(int16_t)(ref[i]/2+signal_sample()/2);}
        int64_t started=esp_timer_get_time();aec_process(aec,mic,ref,out);
        int64_t elapsed=esp_timer_get_time()-started;aec_us+=elapsed;
        for(int i=0;i<n;++i) {
            encode_pcm[used++]=out[i];
            if(used!=960)continue;
            esp_audio_enc_in_frame_t in={.buffer=(uint8_t*)encode_pcm,.len=sizeof(encode_pcm)};
            esp_audio_enc_out_frame_t encoded={.buffer=packet,.len=packet_bytes,.encoded_bytes=0};
            started=esp_timer_get_time();
            int result=esp_opus_enc_process(encoder,&in,&encoded);
            int64_t encoded_time=esp_timer_get_time()-started;encode_us+=encoded_time;elapsed+=encoded_time;
            if(result!=ESP_AUDIO_ERR_OK || !encoded.encoded_bytes || encoded.encoded_bytes>(unsigned)packet_bytes)goto end;
            esp_audio_dec_in_raw_t raw={.buffer=packet,.len=encoded.encoded_bytes,.consumed=0,
                .frame_recover=ESP_AUDIO_DEC_RECOVERY_NONE};
            esp_audio_dec_out_frame_t decoded={.buffer=(uint8_t*)decode_pcm,.len=sizeof(decode_pcm),.decoded_size=0};
            esp_audio_dec_info_t info={0};
            started=esp_timer_get_time();result=esp_opus_dec_decode(decoder,&raw,&decoded,&info);
            int64_t decoded_time=esp_timer_get_time()-started;decode_us+=decoded_time;elapsed+=decoded_time;
            if(result!=ESP_AUDIO_ERR_OK || decoded.decoded_size!=sizeof(decode_pcm))goto end;
            for(unsigned j=0;j<1440;++j)decoded_energy+=(int64_t)decode_pcm[j]*decode_pcm[j];
            used=0;++packets;
        }
        if(elapsed>max_combo_us)max_combo_us=elapsed;
        vTaskDelay(1);
    }
    if(used!=0)goto end; // whole encoder/decoder frames share the measured window
    printf("C3_AEC_PROBE loaded blocks=%d samples=%d packets=%u aec_us=%"PRId64
           " encode_us=%"PRId64" decode_us=%"PRId64" total_cpu_us=%"PRId64
           " audio_budget_us=%"PRId64" max_combo_us=%"PRId64" decoded_energy=%"PRIu64" free=%u stack_min_free=%u\n",
           LOADED_BLOCKS,n,packets,aec_us,encode_us,decode_us,aec_us+encode_us+decode_us,
           (int64_t)LOADED_BLOCKS*n*1000000/16000,max_combo_us,decoded_energy,
           (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT),
           (unsigned)uxTaskGetStackHighWaterMark(NULL));
    good=true;
end:
    if(!good)printf("C3_AEC_PROBE loaded_failed\n");
    free(packet);if(decoder)esp_opus_dec_close(decoder);if(encoder)esp_opus_enc_close(encoder);
    return good;
}
void app_main(void) {
    vTaskDelay(pdMS_TO_TICKS(1500));
    const uint32_t caps=MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT;
    const size_t before=heap_caps_get_free_size(caps);
#if CONFIG_C3_AEC_PROBE_FULL_DUPLEX
    const aec_mode_t mode=AEC_MODE_FD_LOW_COST;
#else
    const aec_mode_t mode=AEC_MODE_SR_LOW_COST;
#endif
    printf("C3_AEC_PROBE begin version=3 mode=%d cpu_mhz=160 free=%u largest=%u\n",
           (int)mode,(unsigned)before,(unsigned)heap_caps_get_largest_free_block(caps));
    aec_handle_t *aec=aec_create(16000,2,1,mode);
    if (!aec) {
        printf("C3_AEC_PROBE create_failed\n");
        fflush(stdout);vTaskDelay(pdMS_TO_TICKS(200));esp_restart();return;
    }
    const int n=aec_get_chunksize(aec);
    printf("C3_AEC_PROBE initialized samples=%d free=%u used=%u\n",n,
           (unsigned)heap_caps_get_free_size(caps),
           (unsigned)(before-heap_caps_get_free_size(caps)));
    if (n<=0 || n>MAX_FRAME) {
        printf("C3_AEC_PROBE bad_chunksize\n");
    } else for (int scenario=0;scenario<3;++scenario) {
        uint64_t in_energy=0,ref_energy=0,out_energy=0;
        int64_t sum=0;
        for (int b=0;b<BLOCKS;++b) {
            for(int i=0;i<n;++i) {
                ref[i]=scenario==1?0:signal_sample();
                mic[i]=(int16_t)(ref[i]/2+(scenario==0?0:signal_sample()/2));
                in_energy+=(int64_t)mic[i]*mic[i];ref_energy+=(int64_t)ref[i]*ref[i];
            }
            const int64_t start=esp_timer_get_time();
            aec_process(aec,mic,ref,out);
            times[b]=esp_timer_get_time()-start;sum+=times[b];
            for(int i=0;i<n;++i)out_energy+=(int64_t)out[i]*out[i];
            vTaskDelay(1); // permit watchdog/USB tasks; excluded from process time
        }
        qsort(times,BLOCKS,sizeof(times[0]),order_time);
        printf("C3_AEC_PROBE case=%d blocks=%d mean_us=%"PRId64" p95_us=%"PRId64
               " p99_us=%"PRId64" max_us=%"PRId64" budget_us=%d in_energy=%"PRIu64
               " ref_energy=%"PRIu64" out_energy=%"PRIu64" min_free=%u\n",
               scenario,BLOCKS,sum/BLOCKS,times[121],times[126],times[127],
               n*1000000/16000,in_energy,ref_energy,out_energy,
               (unsigned)heap_caps_get_minimum_free_size(caps));
        fflush(stdout);
    }
    if(n>0 && n<=MAX_FRAME)codec_load(aec,n);
    aec_destroy(aec);
    printf("C3_AEC_PROBE done free=%u restarting_for_rollback\n",(unsigned)heap_caps_get_free_size(caps));
    fflush(stdout);vTaskDelay(pdMS_TO_TICKS(1000));esp_restart();
}
