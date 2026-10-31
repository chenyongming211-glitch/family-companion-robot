#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include "esp_aec.h"
#include "esp_heap_caps.h"
#include "esp_system.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

// Deliberately never touches NVS, network, codec, OTA validity or eFuses.
// The pending image restarts so the installed bootloader rolls back to ota1.
enum { BLOCKS = 128, MAX_FRAME = 1024 };
static int16_t mic[MAX_FRAME] __attribute__((aligned(16)));
static int16_t ref[MAX_FRAME] __attribute__((aligned(16)));
static int16_t out[MAX_FRAME] __attribute__((aligned(16)));
static int64_t times[BLOCKS];
static uint32_t random_state = 0x12345678;
static int16_t signal_sample(void) {
    random_state = random_state * 1664525u + 1013904223u;
    return (int16_t)((int16_t)(random_state >> 16) / 4);
}
static int order_time(const void *a, const void *b) {
    const int64_t x=*(const int64_t*)a, y=*(const int64_t*)b;
    return (x>y)-(x<y);
}
void app_main(void) {
    vTaskDelay(pdMS_TO_TICKS(1500));
    const uint32_t caps=MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT;
    const size_t before=heap_caps_get_free_size(caps);
    printf("C3_AEC_PROBE begin version=1 cpu_mhz=160 free=%u largest=%u\n",
           (unsigned)before,(unsigned)heap_caps_get_largest_free_block(caps));
    aec_handle_t *aec=aec_create(16000,2,1,AEC_MODE_SR_LOW_COST);
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
    aec_destroy(aec);
    printf("C3_AEC_PROBE done free=%u restarting_for_rollback\n",(unsigned)heap_caps_get_free_size(caps));
    fflush(stdout);vTaskDelay(pdMS_TO_TICKS(1000));esp_restart();
}
