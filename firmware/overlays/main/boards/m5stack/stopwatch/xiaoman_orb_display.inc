// Included inside RoundLcdDisplay. Every LVGL mutation holds the existing lock.
private:
    family_robot::XiaomanOrbView orb_;
    bool orb_system_text_=false;
    void SetupOrbUI() {
        auto font=current_theme_->GetTextFont();
        orb_.Initialize(font->font(),[](void* context) {
            static_cast<RoundLcdDisplay*>(context)->RefreshOrb();
        },this);
    }
    void RefreshOrb() {
        // RestoreBrightness runs during board construction, before SetupUI.
        // Never touch LVGL objects or audio service until both are initialized.
        if(!IsSetupUICalled() || !orb_.Ready()) return;
        auto& app=Application::GetInstance();
        family_robot::OrbFacts facts{};
        const auto state=app.GetDeviceState();
        facts.now_us=esp_timer_get_time();
        facts.session=state==kDeviceStateConnecting || state==kDeviceStateListening || state==kDeviceStateSpeaking;
        facts.connecting=state==kDeviceStateConnecting;
        // This getter uses try_lock: display refresh never stalls an audio task.
        facts.audio=app.GetAudioService().GetOrbAudioActivity();
        DisplayLockGuard lock(this);
        if(!lock) return;
        auto visible=[](lv_obj_t* object) {return object && !lv_obj_has_flag(object,LV_OBJ_FLAG_HIDDEN);};
        facts.system_page=orb_system_text_ || visible(preview_image_) || visible(low_battery_popup_) || visible(notification_label_);
        const bool shown=orb_.Update(facts);
        if(shown) {
            if(emoji_box_) lv_obj_add_flag(emoji_box_,LV_OBJ_FLAG_HIDDEN);
            if(bottom_bar_) lv_obj_add_flag(bottom_bar_,LV_OBJ_FLAG_HIDDEN);
            if(status_label_) lv_obj_add_flag(status_label_,LV_OBJ_FLAG_HIDDEN);
        } else {
            // LcdDisplay hides the emoji while a preview owns the centre.
            // Yielding the orb must not reverse that existing ownership.
            if(emoji_box_ && !visible(preview_image_)) lv_obj_remove_flag(emoji_box_,LV_OBJ_FLAG_HIDDEN);
            if(status_label_ && !visible(notification_label_)) lv_obj_remove_flag(status_label_,LV_OBJ_FLAG_HIDDEN);
        }
    }
public:
    void SetOrbScreenOn(bool on) {
        {DisplayLockGuard lock(this);orb_.SetScreenOn(on);}
        RefreshOrb();
    }
    void SetStatus(const char* status) override {
        SpiLcdDisplay::SetStatus(status); RefreshOrb();
    }
    void SetEmotion(const char* emotion) override {
        SpiLcdDisplay::SetEmotion(emotion); RefreshOrb();
    }
    void SetChatMessage(const char* role,const char* content) override {
        const bool assistant=role && std::strcmp(role,"assistant")==0;
        const bool conversation=role && (std::strcmp(role,"user")==0 || assistant);
        // Existing notification pages use the assistant role for their text.
        // Ordinary/stale chat transcripts stay hidden outside that page too.
        if(conversation && !(assistant && Application::GetInstance().GetDeviceState()==kDeviceStateNotifying)) return;
        {DisplayLockGuard lock(this);
            orb_system_text_=content && content[0];
            SpiLcdDisplay::SetChatMessage(role,content);
        }
        RefreshOrb();
    }
    void ClearChatMessages() override {
        {DisplayLockGuard lock(this);orb_system_text_=false;SpiLcdDisplay::ClearChatMessages();}
        RefreshOrb();
    }
    void SetPowerSaveMode(bool on) override {
        {DisplayLockGuard lock(this);orb_.SetPowerSave(on);}
        SpiLcdDisplay::SetPowerSaveMode(on);RefreshOrb();
    }
    using SpiLcdDisplay::ShowNotification;
    void ShowNotification(const char* text,int duration_ms=3000) override {
        SpiLcdDisplay::ShowNotification(text,duration_ms);RefreshOrb();
    }
    void SetPreviewImage(std::unique_ptr<LvglImage> image) override {
        SpiLcdDisplay::SetPreviewImage(std::move(image));RefreshOrb();
    }
    void SetTheme(Theme* theme) override {
        SpiLcdDisplay::SetTheme(theme);
        DisplayLockGuard lock(this);
        if(orb_.Ready()) orb_.SetFont(current_theme_->GetTextFont()->font());
    }
