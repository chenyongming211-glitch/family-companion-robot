// C3 generic assets updates own only the original 0x50000-byte prefix.
// This updater remains in-place: a partial download can invalidate the prefix.
bool Assets::DownloadC3Pcm(std::string url,std::function<void(int,size_t)> progress) {
    constexpr size_t prefix=0x50000,header_size=12;
    if(!partition_ || partition_->size<prefix)return false;
    auto http=Board::GetInstance().GetNetwork()->CreateHttp(0);
    if(!http->Open("GET",url))return false;
    const auto status=http->GetStatusCode();if(!status || *status!=200)return false;
    const size_t announced=http->GetBodyLength();
    if(announced<header_size || announced>prefix)return false;
    const size_t sector=esp_partition_get_main_flash_sector_size();
    if(!sector || sector>prefix || announced>prefix-(sector-1)) {
        // Exact prefix-sized input is allowed when the prefix is sector aligned.
        if(!sector || prefix%sector || announced>prefix)return false;
    }
    const size_t erase=((announced-1)/sector+1)*sector;
    if(erase>prefix)return false;
    using Buffer=std::unique_ptr<char,decltype(&heap_caps_free)>;
    Buffer buffer(static_cast<char*>(heap_caps_malloc(sector,MALLOC_CAP_INTERNAL)),&heap_caps_free);
    if(!buffer)return false;
    uint8_t header[header_size]{};size_t collected=0,received=0,written=0;
    bool prepared=false;
    for(;;) {
        const auto read=http->Read(buffer.get(),sector);if(!read)return false;
        const int n=*read;
        // Validate EVERY read before copy, unmap, erase or write. Some HTTP
        // implementations deliver bytes past Content-Length until the next EOF.
        if(n<0 || static_cast<size_t>(n)>sector || received>announced || static_cast<size_t>(n)>announced-received)return false;
        if(!n)break;
        received+=static_cast<size_t>(n);size_t pos=0;
        if(collected<header_size) {
            const auto take=std::min(static_cast<size_t>(n),header_size-collected);
            std::memcpy(header+collected,buffer.get(),take);collected+=take;pos=take;
            if(collected==header_size) {
                uint32_t stored=0;std::memcpy(&stored,header+8,sizeof(stored));
                if(stored!=announced-header_size)return false;
                // Only a complete consistent header may invalidate the old mapping.
                UnApplyPartition();
                if(esp_partition_erase_range(partition_,0,erase)!=ESP_OK)return false;
                prepared=true;
            }
        }
        const auto count=static_cast<size_t>(n)-pos;
        if(count) {
            if(!prepared || written>announced-header_size || count>announced-header_size-written ||
               written>prefix-header_size || count>prefix-header_size-written)return false;
            if(esp_partition_write(partition_,header_size+written,buffer.get()+pos,count)!=ESP_OK)return false;
            written+=count;
        }
        if(progress)progress(static_cast<int>(received*100/announced),count);
    }
    if(!prepared || received!=announced || collected!=header_size || written!=announced-header_size)return false;
    if(esp_partition_write(partition_,0,header,header_size)!=ESP_OK)return false;
    return InitializePartition();
}
