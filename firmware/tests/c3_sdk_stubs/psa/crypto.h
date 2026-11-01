#pragma once
#include <stddef.h>
#include <stdint.h>

// The public one-shot PSA Crypto contract used by the pinned Mbed TLS 4 SDK.
typedef int32_t psa_status_t;
typedef uint32_t psa_algorithm_t;
#define PSA_SUCCESS ((psa_status_t)0)
#define PSA_ERROR_HARDWARE_FAILURE ((psa_status_t)-147)
#define PSA_ERROR_BAD_STATE ((psa_status_t)-137)
#define PSA_ERROR_BUFFER_TOO_SMALL ((psa_status_t)-138)
#define PSA_ALG_SHA_256 ((psa_algorithm_t)0x02000009)

extern "C" {
psa_status_t psa_crypto_init(void);
psa_status_t psa_hash_compute(psa_algorithm_t alg, const uint8_t* input,
                              size_t input_length, uint8_t* hash,
                              size_t hash_size, size_t* hash_length);
}
