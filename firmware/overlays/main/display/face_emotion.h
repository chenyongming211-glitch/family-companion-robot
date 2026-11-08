#pragma once
#include <cstdint>
#include <cstring>
namespace family_robot {
enum class FaceEmotion : uint8_t { Neutral, Happy, Loving, Embarrassed, Confident, Delicious,
    Sad, Crying, Sleepy, Silly, Angry, Surprised, Winking, Relaxed, Confused, Thinking };
inline FaceEmotion ParseEmotion(const char* text) {
    if(!text)return FaceEmotion::Neutral;
    struct Mapping {const char* name;FaceEmotion emotion;};
    static constexpr Mapping map[]={
        {"neutral",FaceEmotion::Neutral},{"happy",FaceEmotion::Happy},
        {"laughing",FaceEmotion::Happy},{"funny",FaceEmotion::Happy},
        {"loving",FaceEmotion::Loving},{"embarrassed",FaceEmotion::Embarrassed},
        {"confident",FaceEmotion::Confident},{"delicious",FaceEmotion::Delicious},
        {"sad",FaceEmotion::Sad},{"crying",FaceEmotion::Crying},{"sleepy",FaceEmotion::Sleepy},
        {"silly",FaceEmotion::Silly},{"angry",FaceEmotion::Angry},{"surprised",FaceEmotion::Surprised},
        {"shocked",FaceEmotion::Surprised},{"thinking",FaceEmotion::Thinking},
        {"winking",FaceEmotion::Winking},{"relaxed",FaceEmotion::Relaxed},{"confused",FaceEmotion::Confused}};
    for(const auto& item:map)if(!std::strcmp(text,item.name))return item.emotion;
    return FaceEmotion::Neutral;
}
// Bound model output is deliberately narrower than the legacy wire aliases.
inline bool ParsePlaybackEmotion(const char* text,FaceEmotion& emotion) {
    static constexpr const char* names[]={"neutral","happy","loving","embarrassed","confident",
        "delicious","sad","crying","sleepy","silly","angry","surprised","winking","relaxed","confused"};
    if(!text)return false;
    for(unsigned i=0;i<15;++i)if(!std::strcmp(text,names[i])) {
        emotion=static_cast<FaceEmotion>(i);return true;
    }
    return false;
}
} // namespace family_robot
