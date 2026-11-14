namespace FamilyOta {
namespace {
std::atomic<bool> download_disabled{false};
struct IoDeadline {
    const int fd;std::atomic<bool> expired{false};esp_timer_handle_t timer=nullptr;
    explicit IoDeadline(int socket):fd(socket){}
    static void Expire(void* arg){auto self=static_cast<IoDeadline*>(arg);self->expired=true;shutdown(self->fd,SHUT_RDWR);}
};
}
bool DownloadAvailable(){return !download_disabled.load();}
// The only caller owns the checked hardware pause for this entire transaction.
// A handle is relinquished by esp_ota_end even when validation fails.
const char* Download(const Command& command) {
    if(!ValidUrl(command))return "invalid_url";
    const auto partition=esp_ota_get_next_update_partition(nullptr);
    if(!partition||command.size>MaxImage||command.size>partition->size||partition->size!=SlotBytes)return "slot_too_small";
    esp_http_client_config_t config{};config.url=command.url.c_str();config.crt_bundle_attach=esp_crt_bundle_attach;
    config.timeout_ms=5000;config.is_async=true;config.disable_auto_redirect=true;config.buffer_size=4096;
    struct Transaction {
        esp_http_client_handle_t http=nullptr;esp_ota_handle_t handle=0;psa_hash_operation_t sha=PSA_HASH_OPERATION_INIT;
        IoDeadline* io=nullptr;bool quarantined=false;uint8_t* buffer=nullptr;
        bool Stop() {
            if(quarantined)return false;
            if(io && io->timer && esp_timer_stop_blocking(io->timer,pdMS_TO_TICKS(200))!=ESP_OK){
                // A callback may still hold fd. Retain the client, fd, timer and
                // heap-only context for this boot, preventing descriptor reuse.
                quarantined=true;download_disabled=true;return false;
            }
            return true;
        }
        bool Arm(uint64_t deadline) {
            if(!io){io=new(std::nothrow) IoDeadline(esp_http_client_get_socket(http));if(!io||io->fd<0)return false;
                esp_timer_create_args_t args{};args.callback=IoDeadline::Expire;args.arg=io;args.name="ota_read";
                if(esp_timer_create(&args,&io->timer)!=ESP_OK)return false;}
            io->expired=false;const auto now=Now();
            return deadline>now && esp_timer_start_once(io->timer,std::min(uint64_t(5000),deadline-now)*1000)==ESP_OK;
        }
        ~Transaction(){
            if(handle)esp_ota_abort(handle);
            if(Stop()){
                if(io){if(io->timer)esp_timer_delete(io->timer);delete io;}
                if(http){esp_http_client_close(http);esp_http_client_cleanup(http);}
            }
            if(buffer)heap_caps_free(buffer);
            psa_hash_abort(&sha);
        }
    } transaction;
    auto& http=transaction.http;http=esp_http_client_init(&config);if(!http)return "http_error";
    const auto authorization="Bearer "+FamilyOnboarding::Key();
    esp_http_client_set_header(http,"Authorization",authorization.c_str());
    esp_http_client_set_header(http,"Device-Id",SystemInfo::GetMacAddress().c_str());
    esp_http_client_set_method(http,HTTP_METHOD_GET);
    const auto deadline=Now()+120000;
    const auto connect_deadline=Now()+5000;
    while(true) {
        const auto opened=esp_http_client_open(http,0);
        if(opened==ESP_OK)break;
        if(opened!=ESP_ERR_HTTP_EAGAIN)return "http_error";
        if(Now()>=connect_deadline || Now()>=deadline)return "timeout";
        vTaskDelay(1);
    }
    if(Now()>=connect_deadline || Now()>=deadline)return "timeout";
    int64_t length=-1;const auto headers_deadline=std::min(deadline,Now()+5000);
    do {
        if(!transaction.Arm(headers_deadline))return "download_failed";
        length=esp_http_client_fetch_headers(http);
        if(!transaction.Stop())return "timeout";
        if(transaction.io->expired || Now()>=headers_deadline)return "timeout";
        if(length==-ESP_ERR_HTTP_EAGAIN)vTaskDelay(1);
    } while(length==-ESP_ERR_HTTP_EAGAIN);
    if(esp_http_client_get_status_code(http)!=200||esp_http_client_is_chunked_response(http))return "http_error";
    if(length!=command.size)return "length_mismatch";
    if(psa_crypto_init()!=PSA_SUCCESS || psa_hash_setup(&transaction.sha,PSA_ALG_SHA_256)!=PSA_SUCCESS)return "download_failed";
    transaction.buffer=static_cast<uint8_t*>(heap_caps_malloc(4096,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
    if(!transaction.buffer)return "download_failed";
    auto buffer=transaction.buffer;size_t received=0,header=0;
    while(received<command.size) {
        if(Now()>=deadline)return "timeout";
        const auto remaining=deadline-Now();esp_http_client_set_timeout_ms(http,static_cast<int>(std::min(uint64_t(5000),remaining)));
        const auto read_deadline=std::min(deadline,Now()+5000);
        // The first full header is collected before the first flash mutation.
        const size_t capacity=transaction.handle?4096:4096-header;
        int count=0;
        do {
            if(!transaction.Arm(read_deadline))return "download_failed";
            count=esp_http_client_read(http,reinterpret_cast<char*>(buffer+(transaction.handle?0:header)),
                static_cast<int>(std::min(capacity,size_t(command.size-received))));
            if(!transaction.Stop())return "timeout";
            if(transaction.io->expired || Now()>=read_deadline)return "timeout";
            if(count==-ESP_ERR_HTTP_EAGAIN)vTaskDelay(1);
        } while(count==-ESP_ERR_HTTP_EAGAIN);
        if(count<0)return "download_failed";
        if(!count||size_t(count)>command.size-received)return "length_mismatch";
        received+=count;
        size_t writing=count;
        if(!transaction.handle) {
            header+=count;if(header<288)continue;
            if(!ImageIdentity(buffer,command))return "image_mismatch";
            if(esp_ota_begin(partition,command.size,&transaction.handle)!=ESP_OK)return "write_failed";
            writing=header;
        }
        if(psa_hash_update(&transaction.sha,buffer,writing)!=PSA_SUCCESS)return "download_failed";
        if(esp_ota_write(transaction.handle,buffer,writing)!=ESP_OK)return "write_failed";
        Progress(static_cast<int>(received*100/command.size));
    }
    uint8_t extra=0;const auto began=Now();
    if(!transaction.Arm(deadline))return "download_failed";
    const int tail=esp_http_client_read(http,reinterpret_cast<char*>(&extra),1);
    if(!transaction.Stop())return "timeout";
    if(transaction.io->expired || Now()>=deadline||Now()-began>5000)return "timeout";
    if(tail<0)return "download_failed";
    if(tail||!esp_http_client_is_complete_data_received(http))return "length_mismatch";
    uint8_t digest[32];size_t digest_size=0;
    if(psa_hash_finish(&transaction.sha,digest,sizeof(digest),&digest_size)!=PSA_SUCCESS || digest_size!=sizeof(digest))return "download_failed";
    if(Hex(digest,32)!=command.sha256)return "hash_mismatch";
    if(Now()>=deadline)return "timeout";
    auto handle=transaction.handle;transaction.handle=0;
    if(esp_ota_end(handle)!=ESP_OK)return "image_mismatch";
    if(Now()>=deadline)return "timeout";
    if(esp_ota_set_boot_partition(partition)!=ESP_OK)return "write_failed";
    return nullptr;
}
}
