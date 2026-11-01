#include "sdkconfig.h"
#if CONFIG_XMINI_C3_PCM
#include <stddef.h>
#include <stdint.h>
#include "i2s_private.h"
#include "gdma_priv.h"
#include "hal/systimer_hal.h"
// Native-compiler ABI evidence, retained by --undefined. Never read by an ISR.
// The independent ELF auditor reads these words and binds their owning sources.
const uint32_t family_c3_sdk_layout[] = {
    1, sizeof(void *),
    offsetof(struct i2s_channel_obj_t,callbacks.on_recv),
    offsetof(struct i2s_channel_obj_t,callbacks.on_recv_q_ovf),
    offsetof(struct i2s_channel_obj_t,callbacks.on_sent),
    offsetof(struct i2s_channel_obj_t,callbacks.on_send_q_ovf),
    offsetof(gdma_tx_channel_t,cbs.on_trans_eof),
    offsetof(gdma_tx_channel_t,cbs.on_descr_err),
    offsetof(gdma_rx_channel_t,cbs.on_recv_eof),
    offsetof(gdma_rx_channel_t,cbs.on_descr_err),
    offsetof(gdma_rx_channel_t,cbs.on_descr_empty),
    offsetof(gdma_rx_channel_t,cbs.on_recv_done),
    offsetof(systimer_hal_context_t,ticks_to_us),
};
#endif
