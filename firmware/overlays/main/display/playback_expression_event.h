#pragma once
#include "../audio/orb_activity.h"
#include "face_emotion.h"

namespace family_robot {
// Value-only receive event. No cJSON, protocol pointer or mutable string escapes
// the receive callback; arrival_us is sampled before parsing/queuing on main.
struct PlaybackExpressionEvent {
    OrbOutputActivity::Identity identity{};
    uint32_t lease=0;
    FaceEmotion emotion=FaceEmotion::Neutral;
    int64_t arrival_us=0;
    const char* EmotionName() const {
        static constexpr const char* names[]={"neutral","happy","loving","embarrassed","confident",
            "delicious","sad","crying","sleepy","silly","angry","surprised","winking","relaxed","confused"};
        const auto index=static_cast<unsigned>(emotion);
        return index<15?names[index]:nullptr;
    }
};
} // namespace family_robot
