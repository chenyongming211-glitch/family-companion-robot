#ifndef FAMILY_ECHO_CORRELATION_H
#define FAMILY_ECHO_CORRELATION_H
#include <stdint.h>
#include <stddef.h>

typedef struct FCEchoProbe FCEchoProbe;
enum {
    FC_ECHO_MEASURED = 0, FC_ECHO_LOW_REFERENCE = 1, FC_ECHO_LOW_CAPTURE = 2,
    FC_ECHO_REFERENCE_GAP = 3, FC_ECHO_CAPTURE_GAP = 4, FC_ECHO_CLOCK_MISSING = 5,
    FC_ECHO_CLOCK_UNSTABLE = 6, FC_ECHO_INCOMPLETE = 7, FC_ECHO_SHORT_WINDOW = 8
};
typedef struct {
    int status;
    int64_t playback_id, generation;
    int window_start_ms, samples, tested_lags, boundary;
    double reference_origin_host_s, window_host_s, anchor_spread_ms;
    int anchors;
    double correlation, second_peak, lag_ms, reference_rms, capture_rms;
    double projection_gain, projected_capture_rms;
    double converter_offset_spread_ms;
    double capture_mapping_spread_ms;
    double source_clock_spread_ms;
} FCEchoResult;
typedef struct {
    uint64_t blocks, source_frames, converted_samples, source_gaps, source_backwards;
    double source_duration_s, pcm_duration_s, source_delta_ms, source_delta_min_ms, source_delta_max_ms;
    double first_source_host_s, last_source_end_s, converter_offset_ms;
    double source_pcm_offset_ms;
    double source_clock_error_ms;
} FCEchoCaptureStats;

FCEchoProbe *fc_echo_create(void);
void fc_echo_destroy(FCEchoProbe *probe);
void fc_echo_begin(FCEchoProbe *probe, int64_t playback_id, int64_t generation);
void fc_echo_reference(FCEchoProbe *probe, int64_t generation, const int16_t *pcm, size_t count, int64_t player_frame);
void fc_echo_anchor(FCEchoProbe *probe, int64_t generation, double origin_host_s);
void fc_echo_capture(FCEchoProbe *probe, const int16_t *pcm, size_t count, double source_host_s);
void fc_echo_capture_source(FCEchoProbe *probe, const int16_t *pcm, size_t count, double source_host_s, size_t source_frames, double source_rate);
void fc_echo_capture_stats(const FCEchoProbe *probe, FCEchoCaptureStats *stats);
void fc_echo_end(FCEchoProbe *probe, int64_t generation);
void fc_echo_close(FCEchoProbe *probe);
int fc_echo_poll(FCEchoProbe *probe, FCEchoResult *result);
uint64_t fc_echo_dropped_playbacks(const FCEchoProbe *probe);
#endif
