// Included by the existing display owner; all mutations use its LVGL lock.
private:
    family_robot::ChickFaceView chick_face_;
    bool face_notice_=false;
    bool face_charging_=false,face_low_battery_=false;
    int64_t face_battery_checked_us_=-1000000;
    static constexpr lv_state_t kFaceChromeState=LV_STATE_USER_4;
    bool face_chrome_=false;
    void SetupFaceUI() {
        DisplayLockGuard lock(this);if(!lock)return;
        auto font=current_theme_->GetTextFont();
        chick_face_.Initialize(font->font(),LV_HOR_RES,LV_VER_RES,[](void* p) {
            static_cast<decltype(this)>(p)->RefreshFace();
        },this);
        const auto selector=static_cast<lv_style_selector_t>(LV_PART_MAIN)|
            static_cast<lv_style_selector_t>(kFaceChromeState);
        if(top_bar_)lv_obj_set_style_bg_opa(top_bar_,LV_OPA_TRANSP,selector);
        for(auto* icon:{network_label_,mute_label_,battery_label_,notification_label_})if(icon)
            lv_obj_set_style_text_color(icon,lv_color_hex(0xffd8a8),selector);
    }
    static family_robot::FaceState FaceStateFor(DeviceState state) {
        using S=family_robot::FaceState;
        switch(state) {
        case kDeviceStateUnknown:case kDeviceStateStarting:case kDeviceStateActivating:return S::Boot;
        case kDeviceStateWifiConfiguring:return S::WifiConfig;
        case kDeviceStateConnecting:return S::Connecting;
        case kDeviceStateListening:case kDeviceStateAudioTesting:return S::Listening;
        case kDeviceStateSpeaking:return S::Speaking;
        case kDeviceStateNotifying:return S::Notifying;
        case kDeviceStateUpgrading:return S::Upgrading;
        case kDeviceStateFatalError:return S::Error;
        default:return S::Idle;
        }
    }
    void RefreshFace() {
        if(!IsSetupUICalled() || !chick_face_.Ready())return;
        auto& app=Application::GetInstance();
        DisplayLockGuard lock(this);if(!lock)return;
        family_robot::FaceFacts facts{};
        facts.state=FaceStateFor(app.GetDeviceState());
        // Serialize observation and consumption with the existing display guard,
        // so main/timer cannot feed coherent snapshots in reverse order. Codec
        // getters only copy under portMUX; the input queue read is nonblocking.
        // Output facts remain independent of raw state, hidden pages and Idle.
        facts.audio=app.GetAudioService().GetOrbAudioActivity();
        facts.now_us=esp_timer_get_time();
        // Channel closure/error can enter Idle while preserving an alert.
        // Retire the operational hint independently of alert dismissal/text.
        if(facts.state!=family_robot::FaceState::Listening && facts.state!=family_robot::FaceState::Speaking)
            chick_face_.SetThinking(false);
        // PMIC I2C is not animation work. Sample at most once per second.
        if(facts.now_us-face_battery_checked_us_>=1000000) {
            int battery=0;bool charging=false,discharging=false;
            if(Board::GetInstance().GetBatteryLevel(battery,charging,discharging)) {
                face_charging_=charging;face_low_battery_=discharging && battery>=0 && battery<15;
            }
            face_battery_checked_us_=facts.now_us;
        }
        facts.charging=face_charging_;facts.low_battery=face_low_battery_;
        facts.system_page=preview_image_ && !lv_obj_has_flag(preview_image_,LV_OBJ_FLAG_HIDDEN);
        if(face_notice_ && facts.state==family_robot::FaceState::Idle)
            facts.state=family_robot::FaceState::Notifying;
        // A completion may occur while observing audio; PMIC reads can also
        // take time. Evaluate at actual current time after all observations.
        facts.now_us=esp_timer_get_time();
        const bool shown=chick_face_.Update(facts);
        // Never return to the stock smile/sleep glyph while this UI is selected.
        if(emoji_box_)lv_obj_add_flag(emoji_box_,LV_OBJ_FLAG_HIDDEN);
        if(bottom_bar_)lv_obj_add_flag(bottom_bar_,LV_OBJ_FLAG_HIDDEN);
        if(status_label_)lv_obj_add_flag(status_label_,LV_OBJ_FLAG_HIDDEN);
        if(face_chrome_!=shown) {
            face_chrome_=shown;
            for(auto* object:{top_bar_,network_label_,mute_label_,battery_label_,notification_label_})if(object) {
                if(shown)lv_obj_add_state(object,kFaceChromeState);
                else lv_obj_remove_state(object,kFaceChromeState);
            }
        }
    }
public:
    void SetFaceScreenOn(bool on) {
        {DisplayLockGuard lock(this);if(lock)chick_face_.SetScreenOn(on);}
        RefreshFace();
    }
    void SetStatus(const char* status) override {SpiLcdDisplay::SetStatus(status);RefreshFace();}
    void SetThinking(bool on) override {
        {DisplayLockGuard lock(this);if(lock)chick_face_.SetThinking(on);}
        RefreshFace();
    }
    void SetEmotion(const char* emotion) override {
        {DisplayLockGuard lock(this);if(lock)chick_face_.SetEmotion(emotion,esp_timer_get_time());}
        RefreshFace();
    }
    void SetLegacyEmotion(uint32_t connection,const char* emotion) override {
        // Retire an old observed connection before applying its successor's mood.
        RefreshFace();
        {DisplayLockGuard lock(this);if(lock)
            chick_face_.SetLegacyEmotion(connection,emotion,esp_timer_get_time());}
        RefreshFace();
    }
    void SetPlaybackEmotion(uint32_t connection,uint32_t playback,const char* emotion,int64_t arrival_us) override {
        // Main's logical start can precede its deferred state-change refresh.
        // Observe current lifecycle/page facts before admitting the queued label.
        RefreshFace();
        {DisplayLockGuard lock(this);if(lock)
            chick_face_.SetPlaybackEmotion({connection,playback},emotion,arrival_us);}
        RefreshFace();
    }
    void ClearPlaybackEmotions() override {
        {DisplayLockGuard lock(this);if(lock)chick_face_.ClearPlaybackEmotions();}
        RefreshFace();
    }
    void SetChatMessage(const char* role,const char* content) override {
        const bool conversation=role && (!std::strcmp(role,"user") || !std::strcmp(role,"assistant"));
        if(conversation && Application::GetInstance().GetDeviceState()!=kDeviceStateNotifying)return;
        {DisplayLockGuard lock(this);if(!lock)return;
            face_notice_=content && content[0];chick_face_.SetMessage(content);
        }
        RefreshFace();
    }
    void ClearChatMessages() override {
        {DisplayLockGuard lock(this);if(!lock)return;face_notice_=false;chick_face_.SetMessage("");}
        SpiLcdDisplay::ClearChatMessages();RefreshFace();
    }
    void SetPowerSaveMode(bool on) override {
        {DisplayLockGuard lock(this);if(lock)chick_face_.SetQuiet(on);}
        // The pinned base implementation only changes system text and the
        // stock sleepy/neutral glyph. Panel brightness remains board-owned.
        RefreshFace();
    }
    using SpiLcdDisplay::ShowNotification;
    void ShowNotification(const char* text,int duration_ms=3000) override {
        SpiLcdDisplay::ShowNotification(text,duration_ms);RefreshFace();
    }
    void SetPreviewImage(std::unique_ptr<LvglImage> image) override {
        SpiLcdDisplay::SetPreviewImage(std::move(image));RefreshFace();
    }
    void SetTheme(Theme* theme) override {
        SpiLcdDisplay::SetTheme(theme);
        {DisplayLockGuard lock(this);if(lock && chick_face_.Ready())
            chick_face_.SetFont(current_theme_->GetTextFont()->font());}
        RefreshFace();
    }
