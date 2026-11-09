/* Included after handle_to_handler() in the pinned official BLE transport. */
#include "protocomm_ble_framing.h"

static family_ble_frame_state_t family_frame;

/* Log only a fixed endpoint vocabulary, never a runtime string or payload. */
static const char *family_ble_endpoint_label(const char *endpoint)
{
    static const char *const names[] = {"prov-session", "prov-config", "prov-scan",
        "prov-ctrl", "proto-ver", "family-claim"};
    for (size_t i = 0; i < sizeof(names) / sizeof(names[0]); ++i) {
        if (strcmp(endpoint, names[i]) == 0) return names[i];
    }
    return "other";
}

static bool transport_family_framed_write(esp_gatt_if_t gatts_if,
                                          esp_ble_gatts_cb_param_t *param)
{
    uint8_t reply[20];
    const uint8_t *request = NULL;
    size_t request_len = 0, reply_len = 0;
    uint8_t *outbuf = NULL;
    ssize_t outlen = 0;
    esp_err_t status = ESP_OK;

    if (param->write.is_prep) {
        if (family_frame.mode == FAMILY_FRAME_FRAMED) goto fail;
        family_frame.mode = FAMILY_FRAME_RAW;
        return false;
    }
    int kind = family_ble_frame_accept(&family_frame, param->write.conn_id,
        param->write.handle, esp_log_timestamp(), param->write.value,
        param->write.len, &request, &request_len, reply, &reply_len);
    if (kind == FAMILY_FRAME_PASSTHROUGH) return false;
    if (kind == FAMILY_FRAME_ERROR) goto fail;
    if (kind == FAMILY_FRAME_REQUEST) {
        const char *endpoint = handle_to_handler(param->write.handle);
        if (!endpoint) goto fail;
        const char *label = family_ble_endpoint_label(endpoint);
        const uint32_t started = esp_log_timestamp();
        ESP_LOGI(TAG, "framed request endpoint=%s len=%zu", label, request_len);
        status = protocomm_req_handle(protoble_internal->pc_ble, endpoint,
            param->write.conn_id, request, request_len, &outbuf, &outlen);
        ESP_LOGI(TAG, "framed complete endpoint=%s status=%d outlen=%ld elapsed_ms=%lu",
            label, (int) status, (long) outlen,
            (unsigned long) (esp_log_timestamp() - started));
        if (status != ESP_OK || outlen < 0 ||
            !family_ble_frame_reply(&family_frame, outbuf, (size_t) outlen, esp_log_timestamp())) {
            goto fail;
        }
        /* Only a framed pull may read a response; never expose a partial one. */
        status = esp_ble_gatts_set_attr_value(param->write.handle, 0, NULL);
    } else if (kind == FAMILY_FRAME_PULL) {
        status = esp_ble_gatts_set_attr_value(param->write.handle, reply_len, reply);
    } else {
        status = esp_ble_gatts_set_attr_value(param->write.handle, 0, NULL);
    }
    free(outbuf);
    if (status != ESP_OK) {
        outbuf = NULL;
        goto fail;
    }
    if (param->write.need_rsp) {
        if (esp_ble_gatts_send_response(gatts_if, param->write.conn_id,
                param->write.trans_id, ESP_GATT_OK, NULL) != ESP_OK) {
            outbuf = NULL;
            goto fail;
        }
    }
    return true;

fail:
    free(outbuf);
    family_ble_frame_fail(&family_frame);
    simple_ble_gatts_clear_char_values();
    if (param->write.need_rsp) {
        esp_ble_gatts_send_response(gatts_if, param->write.conn_id,
            param->write.trans_id, ESP_GATT_ERROR, NULL);
    }
    esp_ble_gatts_close(gatts_if, param->write.conn_id);
    return true;
}
