#include <assert.h>
#include <stdio.h>
#include <string.h>
#include "../overlays/onboarding/protocomm_ble_framing.h"

static size_t frame(uint8_t *out, unsigned op, unsigned id, unsigned offset,
                    unsigned total, const uint8_t *data, size_t count)
{
    const uint8_t header[] = {0x46, 0x52, 1, (uint8_t) op,
        (uint8_t) id, (uint8_t) (id >> 8), (uint8_t) offset,
        (uint8_t) (offset >> 8), (uint8_t) total, (uint8_t) (total >> 8)};
    memcpy(out, header, sizeof(header));
    if (count) memcpy(out + sizeof(header), data, count);
    return sizeof(header) + count;
}

static void roundtrip(void)
{
    family_ble_frame_state_t state = {0};
    uint8_t original[430], packet[20], answer[20], assembled[430];
    for (size_t i = 0; i < sizeof(original); ++i) original[i] = (uint8_t) i;
    const uint8_t *request = NULL;
    size_t request_len = 0, answer_len = 0;
    for (size_t off = 0; off < sizeof(original); off += 10) {
        size_t n = frame(packet, 1, 0x1234, off, sizeof(original), original + off, 10);
        int result = family_ble_frame_accept(&state, 7, 19, 100 + off, packet, n,
                                             &request, &request_len, answer, &answer_len);
        assert(result == (off + 10 == sizeof(original) ? FAMILY_FRAME_REQUEST : FAMILY_FRAME_PARTIAL));
    }
    assert(request_len == sizeof(original));
    assert(memcmp(original, request, sizeof(original)) == 0);
    assert(family_ble_frame_reply(&state, original, sizeof(original), 600));
    for (size_t off = 0; off < sizeof(original); off += 10) {
        size_t n = frame(packet, 2, 0x1234, off, 0, NULL, 0);
        assert(family_ble_frame_accept(&state, 7, 19, 700 + off, packet, n,
               &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PULL);
        assert(answer_len == 20 && answer[0] == 0x46 && answer[1] == 0x52 && answer[2] == 1 && answer[3] == 3);
        assert(answer[4] == 0x34 && answer[5] == 0x12);
        assert((size_t) (answer[6] | answer[7] << 8) == off);
        assert((answer[8] | answer[9] << 8) == sizeof(original));
        memcpy(assembled + off, answer + 10, answer_len - 10);
        /* Retrying a pull returns the identical ciphertext fragment. */
        uint8_t repeated[20]; size_t repeated_len = 0;
        assert(family_ble_frame_accept(&state, 7, 19, 701 + off, packet, n,
               &request, &request_len, repeated, &repeated_len) == FAMILY_FRAME_PULL);
        assert(repeated_len == answer_len && memcmp(repeated, answer, answer_len) == 0);
    }
    assert(memcmp(original, assembled, sizeof(original)) == 0);
    family_ble_frame_reset(&state);
    for (size_t i = 0; i < sizeof(state); ++i) assert(((uint8_t *) &state)[i] == 0);
}

static void rejects(void)
{
    family_ble_frame_state_t state = {0};
    uint8_t packet[20], answer[20], bytes[10] = {0};
    const uint8_t *request = NULL; size_t request_len = 0, answer_len = 0;
    size_t n = frame(packet, 1, 1, 0, 20, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PARTIAL);
    /* Never merge data from another connection, handle or request. */
    assert(family_ble_frame_accept(&state, 9, 2, 1, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
    family_ble_frame_reset(&state);
    n = frame(packet, 1, 1, 0, 482, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
    family_ble_frame_reset(&state);
    n = frame(packet, 1, 0, 0, 10, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
    family_ble_frame_reset(&state);
    n = frame(packet, 1, 2, 0, 20, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PARTIAL);
    n = frame(packet, 1, 2, 11, 20, bytes, 9);
    assert(family_ble_frame_accept(&state, 1, 2, 1, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
    family_ble_frame_reset(&state);
    n = frame(packet, 1, 2, 0, 20, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PARTIAL);
    n = frame(packet, 1, 2, 10, 20, bytes, 10);
    assert(family_ble_frame_accept(&state, 1, 2, 10001, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
}

static void raw_and_empty(void)
{
    family_ble_frame_state_t state = {0};
    uint8_t packet[20], answer[20], byte = 1;
    const uint8_t *request = NULL; size_t request_len = 0, answer_len = 0;
    assert(family_ble_frame_accept(&state, 1, 2, 0, &byte, 1,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PASSTHROUGH);
    /* Ciphertext can coincidentally begin with FR without changing raw mode. */
    size_t n = frame(packet, 1, 1, 0, 1, &byte, 1);
    assert(family_ble_frame_accept(&state, 1, 2, 1, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PASSTHROUGH);
    family_ble_frame_reset(&state);
    assert(family_ble_frame_accept(&state, 1, 2, 2, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_REQUEST);
    assert(family_ble_frame_reply(&state, NULL, 0, 3));
    n = frame(packet, 2, 1, 0, 0, NULL, 0);
    assert(family_ble_frame_accept(&state, 1, 2, 4, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PULL);
    assert(answer_len == 10 && answer[8] == 0 && answer[9] == 0);
    assert(family_ble_frame_accept(&state, 1, 2, 5, &byte, 1,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_ERROR);
}

static void pause_between_requests(void)
{
    family_ble_frame_state_t state = {0};
    uint8_t packet[20], answer[20], byte = 1;
    const uint8_t *request = NULL; size_t request_len = 0, answer_len = 0;
    size_t n = frame(packet, 1, 1, 0, 1, &byte, 1);
    assert(family_ble_frame_accept(&state, 1, 2, 0, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_REQUEST);
    assert(family_ble_frame_reply(&state, &byte, 1, 1));
    n = frame(packet, 2, 1, 0, 0, NULL, 0);
    assert(family_ble_frame_accept(&state, 1, 2, 2, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_PULL);
    /* User can spend a minute typing Wi-Fi password after scanning networks. */
    n = frame(packet, 1, 2, 0, 1, &byte, 1);
    assert(family_ble_frame_accept(&state, 1, 4, 60000, packet, n,
           &request, &request_len, answer, &answer_len) == FAMILY_FRAME_REQUEST);
}

int main(int argc, char **argv)
{
    assert(argc == 2);
    if (strcmp(argv[1], "roundtrip") == 0) roundtrip();
    else if (strcmp(argv[1], "rejects") == 0) rejects();
    else if (strcmp(argv[1], "raw") == 0) raw_and_empty();
    else if (strcmp(argv[1], "pause") == 0) pause_between_requests();
    else return 2;
    puts("ok");
    return 0;
}
