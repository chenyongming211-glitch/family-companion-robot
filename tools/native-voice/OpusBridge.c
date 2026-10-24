#include "OpusBridge.h"
#include <opus.h>
void *nv_encoder_create(int rate) {
    int error = OPUS_OK;
    if (rate != 16000 && rate != 24000) return 0;
    OpusEncoder *encoder = opus_encoder_create(rate, 1, OPUS_APPLICATION_VOIP, &error);
    if (error != OPUS_OK) return 0;
    opus_encoder_ctl(encoder, OPUS_SET_BITRATE(32000));
    opus_encoder_ctl(encoder, OPUS_SET_VBR(1));
    return encoder;
}
void nv_encoder_destroy(void *encoder) { if (encoder) opus_encoder_destroy(encoder); }
int nv_encoder_lookahead(void *encoder) {
    int samples = 0;
    if (!encoder || opus_encoder_ctl(encoder, OPUS_GET_LOOKAHEAD(&samples)) != OPUS_OK) return -1;
    return samples;
}
int nv_encode(void *encoder, const int16_t *pcm, int samples, unsigned char *packet, int capacity) {
    return opus_encode(encoder, pcm, samples, packet, capacity);
}
void *nv_decoder_create(int rate) {
    int error = OPUS_OK;
    OpusDecoder *decoder = opus_decoder_create(rate, 1, &error);
    return error == OPUS_OK ? decoder : 0;
}
void nv_decoder_destroy(void *decoder) { if (decoder) opus_decoder_destroy(decoder); }
int nv_decode(void *decoder, const unsigned char *packet, int length, int16_t *pcm, int capacity) {
    return opus_decode(decoder, packet, length, pcm, capacity, 0);
}
