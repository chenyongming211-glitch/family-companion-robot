#pragma once
#include "../audio/orb_activity.h"
#include "face_emotion.h"
#include <array>

namespace family_robot {
// UI-task-only bounded lifecycle. No PCM, receipt adapter, locks or heap state.
class FaceExpressionController {
public:
    using Identity=OrbOutputActivity::Identity;
    using Fact=OrbOutputActivity::PlaybackFact;
    static constexpr size_t kSlots=OrbOutputActivity::kPlaybackFacts;
    static constexpr int64_t kLateUs=2000000,kPostUs=2000000,kFreshUs=350000;
    enum class Stage : uint8_t { Pending,Active,PostSpeech,Retired };
    struct Selection {FaceEmotion emotion=FaceEmotion::Neutral;int64_t applied_us=0;};
    bool Set(Identity id,FaceEmotion emotion,int64_t now) {
        if(!id || !enabled_ || now<=0 || now<now_ ||
           static_cast<unsigned>(emotion)>=15 ||
           (connection_ && id.connection!=connection_) || id.playback<=retired_through_)return false;
        if(!connection_)connection_=id.connection;
        auto* e=Get(id);
        if(!e || e->chosen || e->stage==Stage::Retired || e->draining)return false;
        if(e->first_us && (!playing_ || visible_!=id || now-e->first_us>kLateUs)) {
            Retire(*e);return false;
        }
        e->emotion=emotion;e->arrival_us=now;e->chosen=true;
        if(e->stage==Stage::Active)e->applied_us=now;
        return true;
    }
    void Clear() {
        for(auto& e:entries_)if(e.id)Retire(e);
        visible_={};playing_=false;selection_={};
    }
    Selection Observe(const OrbOutputActivity& o,int64_t now,bool enabled=true) {
        selection_={};playing_=false;
        if(now<=0 || now<now_) {Clear();return selection_;}
        now_=now;
        // Connection generations never wrap in the protocol. An older snapshot
        // is not a reconnect and must not reopen a stale session's mouth.
        if(o.connection<connection_) {Clear();enabled_=false;return selection_;}
        if(o.connection!=connection_) {
            Clear();entries_={};retired_through_=0;connection_=o.connection;
        }
        enabled_=enabled;
        // Consume terminal facts even when the display is off. A fixed page or
        // replacement L0 owns the face and retires all choices immediately.
        bool bad_physical=false;
        for(const auto& f:o.playbacks) {
            if(!f.identity || f.identity.connection!=connection_)continue;
            auto* e=Get(f.identity);if(!e)continue;
            if(!ValidTimes(f,now) || !f.valid ||
               (e->first_us && (f.first_us!=e->first_us || f.last_us<e->last_us))) {
                if(f.identity==o.identity)bad_physical=true;
                Retire(*e);continue;
            }
            if(e->chosen && (e->arrival_us>now || (e->stage==Stage::Pending &&
                f.first_us && e->arrival_us>f.first_us && e->arrival_us-f.first_us>kLateUs))) {
                Retire(*e);continue;
            }
            e->draining=f.draining || f.canceled;
            e->first_us=f.first_us;e->last_us=f.last_us;
            if(e->stage==Stage::Retired)continue;
            if(e->draining && !(e->stage==Stage::Active && e->chosen && e->shown && visible_==e->id)) {
                Retire(*e);continue;
            }
            if(f.terminal) {
                // The end can be learned after a late Set. PostSpeech only
                // continues a choice already shown while this audio played.
                if(e->draining || !e->chosen || !e->shown || e->arrival_us>f.last_us ||
                   e->stage==Stage::Pending || !f.last_us || now-f.last_us>kPostUs)Retire(*e);
                else e->stage=Stage::PostSpeech;
            }
        }
        if(!enabled) {Clear();return selection_;}
        const Fact* physical=bad_physical?nullptr:Physical(o,now);
        if(physical) {
            // A new actual reply always takes ownership, including neutral and
            // a reply with no label. Pending labels for other IDs stay pending.
            if(visible_!=physical->identity) {
                if(auto* previous=Find(visible_))Retire(*previous);
                visible_=physical->identity;
            }
            playing_=true;
            if(auto* e=Find(visible_)) {
                if(e->stage==Stage::Pending && !e->draining) {
                    e->stage=Stage::Active;
                    e->applied_us=e->arrival_us>e->first_us?e->arrival_us:e->first_us;
                }
                if(e->stage==Stage::Active && e->chosen)selection_={e->emotion,e->applied_us};
            }
        } else if(o.active) {
            // Missing, stale or contradictory physical evidence must not revive
            // a previously visible mood when a later snapshot becomes valid.
            if(auto* e=Find(visible_))Retire(*e);
            if(auto* e=Find(o.identity))Retire(*e);
        } else if(auto* e=Find(visible_)) {
            if(e->stage==Stage::PostSpeech && now>=e->last_us && now-e->last_us<=kPostUs)
                selection_={e->emotion,e->applied_us};
            else if(e->stage==Stage::Active)Retire(*e);
        }
        return selection_;
    }
    // Physical Active and an accepted choice do not prove the face displayed
    // that choice. Only the model can confirm an eligible visible frame.
    void ConfirmShown(Identity id,FaceEmotion emotion) {
        if(auto* e=Find(id);playing_ && e && visible_==id && e->stage==Stage::Active &&
           e->chosen && e->emotion==emotion)e->shown=true;
    }
    bool bound() const {
        for(const auto& e:entries_)if(e.chosen && e.stage!=Stage::Retired)return true;
        return false;
    }
    bool playing() const {return playing_;}
    static bool ValidTimes(const Fact& f,int64_t now) {
        return (f.first_us==0 && f.last_us==0) ||
            (f.first_us>0 && f.first_us<=f.last_us && f.last_us<=now);
    }
    static const Fact* Physical(const OrbOutputActivity& o,int64_t now) {
        if(!o.active || !o.valid || !o.identity || o.connection!=o.identity.connection ||
           o.stamp_us<=0 || o.stamp_us>now || now-o.stamp_us>kFreshUs)return nullptr;
        const Fact* result=nullptr;
        for(const auto& f:o.playbacks)if(f.identity==o.identity) {
            // Duplicate identities are a malformed snapshot, not extra evidence.
            if(result || !f.valid || f.terminal || !f.first_us ||
               !ValidTimes(f,now) || f.last_us!=o.stamp_us)return nullptr;
            result=&f;
        }
        return result;
    }
private:
    struct Entry {
        Identity id{};
        int64_t arrival_us=0,applied_us=0,first_us=0,last_us=0;
        FaceEmotion emotion=FaceEmotion::Neutral;
        Stage stage=Stage::Pending;
        bool chosen=false,draining=false,shown=false;
    };
    Entry* Find(Identity id) {
        if(id)for(auto& e:entries_)if(e.id==id)return &e;
        return nullptr;
    }
    Entry* Get(Identity id) {
        if(auto* e=Find(id))return e;
        // IDs are strictly increasing within one connection. Tombstones can be
        // reused without losing the rejection fence for old/duplicate labels.
        for(auto& e:entries_)if(!e.id || e.stage==Stage::Retired) {
            e={};e.id=id;
            if(id.playback<=retired_through_)e.stage=Stage::Retired;
            return &e;
        }
        return nullptr;
    }
    void Retire(Entry& e) {
        e.stage=Stage::Retired;
        if(e.id.playback>retired_through_)retired_through_=e.id.playback;
    }
    std::array<Entry,kSlots> entries_{};
    Identity visible_{};
    Selection selection_{};
    int64_t now_=0;
    uint32_t connection_=0,retired_through_=0;
    bool enabled_=true,playing_=false;
};
} // namespace family_robot
