#ifndef NATIVE_OPUS_BRIDGE_H
#define NATIVE_OPUS_BRIDGE_H
#include <stdint.h>
#include "EchoCorrelation.h"
void *nv_encoder_create(int rate);
void nv_encoder_destroy(void *encoder);
int nv_encoder_lookahead(void *encoder);
int nv_encode(void *encoder, const int16_t *pcm, int samples, unsigned char *packet, int capacity);
void *nv_decoder_create(int rate);
void nv_decoder_destroy(void *decoder);
int nv_decode(void *decoder, const unsigned char *packet, int length, int16_t *pcm, int capacity);
#endif
