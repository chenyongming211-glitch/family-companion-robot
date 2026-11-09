/* Bounded transport framing; encrypted protocomm payloads remain unchanged. */
#ifndef FAMILY_PROTOCOMM_BLE_FRAMING_H
#define FAMILY_PROTOCOMM_BLE_FRAMING_H
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

enum { FAMILY_FRAME_ERROR = -1, FAMILY_FRAME_PASSTHROUGH = 0,
       FAMILY_FRAME_PARTIAL = 1, FAMILY_FRAME_REQUEST = 2, FAMILY_FRAME_PULL = 3 };
enum { FAMILY_FRAME_RAW = 1, FAMILY_FRAME_FRAMED = 2 };
#define FAMILY_FRAME_MAX 481u
typedef struct {
    uint8_t mode, phase;
    uint16_t conn, handle, id, total, received, response_len, next_offset, last_offset;
    uint64_t started_ms, touched_ms;
    uint8_t data[FAMILY_FRAME_MAX];
} family_ble_frame_state_t;

static inline void family_ble_frame_reset(family_ble_frame_state_t *s) { memset(s, 0, sizeof(*s)); }
static inline int family_ble_frame_fail(family_ble_frame_state_t *s)
{
    family_ble_frame_reset(s);
    s->mode = FAMILY_FRAME_FRAMED; /* No raw downgrade until disconnection. */
    return FAMILY_FRAME_ERROR;
}
static inline uint16_t family_ble_frame_u16(const uint8_t *p)
{
    return (uint16_t) (p[0] | ((uint16_t) p[1] << 8));
}
static inline int family_ble_frame_accept(family_ble_frame_state_t *s, uint16_t conn,
    uint16_t handle, uint64_t now, const uint8_t *frame, size_t len,
    const uint8_t **request, size_t *request_len, uint8_t *reply, size_t *reply_len)
{
    if (!s || !request || !request_len || !reply || !reply_len || (!frame && len)) {
        return FAMILY_FRAME_ERROR;
    }
    *request = NULL;
    *request_len = 0;
    *reply_len = 0;
    if (!s->mode) {
        s->mode = (len >= 2 && frame[0] == 0x46 && frame[1] == 0x52)
            ? FAMILY_FRAME_FRAMED : FAMILY_FRAME_RAW;
    }
    if (s->mode == FAMILY_FRAME_RAW) return FAMILY_FRAME_PASSTHROUGH;
    if (len < 10 || len > 20 || frame[0] != 0x46 || frame[1] != 0x52 || frame[2] != 1) {
        return family_ble_frame_fail(s);
    }
    uint16_t id = family_ble_frame_u16(frame + 4);
    uint16_t offset = family_ble_frame_u16(frame + 6);
    uint16_t total = family_ble_frame_u16(frame + 8);
    size_t count = len - 10;
    bool response_read = s->phase == 2 && s->last_offset != UINT16_MAX && s->next_offset == s->response_len;
    bool next_request = response_read && frame[3] == 1 && offset == 0 && id != s->id;
    if (s->phase && (conn != s->conn || (!next_request &&
        (now - s->touched_ms > 10000 || now - s->started_ms > 30000)))) {
        return family_ble_frame_fail(s);
    }
    if (!id) return family_ble_frame_fail(s);
    if (frame[3] == 1) {
        if (!total || total > FAMILY_FRAME_MAX || !count || offset > total || count > (size_t) (total - offset)) {
            return family_ble_frame_fail(s);
        }
        if (!s->phase || s->phase == 2) {
            if (offset || (s->phase == 2 && (!response_read || s->id == id))) return family_ble_frame_fail(s);
            memset(s->data, 0, sizeof(s->data));
            s->phase = 1;
            s->conn = conn; s->handle = handle; s->id = id; s->total = total;
            s->received = 0; s->response_len = 0;
            s->started_ms = now;
        }
        if (s->phase != 1 || conn != s->conn || handle != s->handle || id != s->id ||
            total != s->total || offset != s->received) return family_ble_frame_fail(s);
        memcpy(s->data + offset, frame + 10, count);
        s->received = (uint16_t) (offset + count);
        s->touched_ms = now;
        if (s->received != s->total) return FAMILY_FRAME_PARTIAL;
        s->phase = 3; /* Dispatch exactly once before accepting response pulls. */
        *request = s->data;
        *request_len = s->total;
        return FAMILY_FRAME_REQUEST;
    }
    if (frame[3] != 2 || count || total || s->phase != 2 || conn != s->conn ||
        handle != s->handle || id != s->id || offset > s->response_len ||
        (s->response_len && offset == s->response_len) ||
        (offset != s->next_offset && offset != s->last_offset)) {
        return family_ble_frame_fail(s);
    }
    count = s->response_len - offset;
    if (count > 10) count = 10;
    memcpy(reply, frame, 10);
    reply[3] = 3;
    reply[8] = (uint8_t) s->response_len;
    reply[9] = (uint8_t) (s->response_len >> 8);
    if (count) memcpy(reply + 10, s->data + offset, count);
    *reply_len = 10 + count;
    s->last_offset = offset;
    s->next_offset = (uint16_t) (offset + count);
    s->touched_ms = now;
    return FAMILY_FRAME_PULL;
}
static inline bool family_ble_frame_reply(family_ble_frame_state_t *s,
    const uint8_t *data, size_t len, uint64_t now)
{
    if (!s || s->mode != FAMILY_FRAME_FRAMED || s->phase != 3 || len > FAMILY_FRAME_MAX ||
        (len && !data) || now - s->touched_ms > 10000 || now - s->started_ms > 30000) {
        if (s) family_ble_frame_fail(s);
        return false;
    }
    if (len) memmove(s->data, data, len);
    memset(s->data + len, 0, sizeof(s->data) - len);
    s->response_len = (uint16_t) len;
    s->next_offset = 0;
    s->last_offset = UINT16_MAX;
    s->phase = 2;
    s->touched_ms = now;
    return true;
}
#endif
