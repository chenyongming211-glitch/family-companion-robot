package realtimelive

import "strings"

// Search is on for every turn. Measured 2026-11-22: a search turn injects about
// five thousand tokens and the first packet lands at 6.1s, against 1.1s on a
// turn that does not search. Most turns do not need anything newer than the
// model's own knowledge: it was searching to tell a folk story, to say hello and
// to answer whether it remembered where the user was going.
//
// The rule is written backwards on purpose. A positive rule ("search only when
// these words appear") fails by answering from old data while sounding certain,
// which is worse than being slow. This one fails by searching when it did not
// have to, which costs a second and nothing else. So: strong signals for fresh
// information win outright, clearly-no-new-information turns turn it off, and
// everything else keeps today's behaviour.

// Words that mean the answer depends on something newer than the model knows.
// Bare "today" and "tomorrow" are deliberately absent: "I'm tired today" is a
// feeling, and the weather, price and schedule words below already carry the
// turns that really do need the world.
var searchFreshWords = []string{
	"天气", "气温", "温度", "下雨", "下雪", "刮风", "冷不冷", "热不热",
	"新闻", "最近", "最新", "现在几点",
	"价格", "多少钱", "汇率", "股价", "票价", "油价", "房价",
	"哪家", "推荐", "附近", "营业", "开门", "路线", "怎么走", "怎么去", "堵不堵",
	"几点", "航班", "机票", "火车", "高铁", "限行", "上映", "比分", "赛程",
	"政策", "规定", "发布", "上市",
}

// Explicit memory-verification questions ask about past conversation turns
// themselves ("do you remember which city you checked the weather for just
// now?"). They must not trigger a web search merely because the recalled topic
// mentions a noun like "weather" or "movie". By contrast, bare time references
// ("我刚才说的那个城市明天天气怎么样") still fall through to searchFreshWords.
var searchMetaRecallWords = []string{
	"记得", "记不记得", "还记得",
	"刚才我让你", "刚才让你", "我刚才让你",
	"我刚才问", "刚才问你", "刚才我问",
	"刚才说的什么", "刚才讲的什么", "刚才查的什么", "刚才查的是",
}

// Turns that plainly do not need anything from the network. Each entry is a
// reason string plus the words that prove it; the first hit wins.
var noSearchGroups = []struct {
	reason string
	words  []string
}{
	{"continuation", []string{"接着讲", "接着", "继续", "往下讲", "后来呢", "然后呢", "再讲一个", "再讲讲", "还有吗", "还有呢"}},
	{"story", []string{"讲个故事", "讲故事", "讲个笑话", "说个故事", "唱首歌", "唱一个"}},
	{"recall", []string{"记得", "我刚才说", "我说过", "上次说", "之前说", "刚才说到", "我刚说"}},
	{"company", []string{"你好", "在吗", "谢谢", "陪我", "好累", "有点累", "难过", "不开心", "开心", "无聊", "睡觉", "晚安", "早安"}},
	{"opinion", []string{"你觉得", "你喜欢", "你怎么看", "你说呢", "你想不想"}},
	{"device", []string{"大点声", "小点声", "大声点", "小声点", "停一下", "别说了", "再说一遍", "重说"}},
}

// searchDecision reports the per-turn search setting and why it was chosen. A nil
// result means the config says nothing about search and must stay untouched.
func searchDecision(user string, configured *bool) (*bool, string) {
	if configured == nil {
		return nil, "unset"
	}
	if !*configured {
		// A deployment that switched search off must not have it switched back on
		// by a turn that happens to mention the weather.
		return configured, "disabled_by_config"
	}
	text := strings.TrimSpace(user)
	if text == "" {
		return configured, "no_transcript"
	}
	// A long question is doing work that a keyword cannot classify, so it keeps
	// the default rather than risking a wrong guess.
	if len([]rune(text)) > 40 {
		return configured, "long_input"
	}
	for _, word := range searchMetaRecallWords {
		if strings.Contains(text, word) {
			off := false
			return &off, "recall"
		}
	}
	for _, word := range searchFreshWords {
		if strings.Contains(text, word) {
			// "谢谢，明天天气怎么样" contains a company word, but the fresh signal
			// is what the turn is actually about.
			return configured, "fresh_information"
		}
	}
	for _, group := range noSearchGroups {
		for _, word := range group.words {
			if strings.Contains(text, word) {
				off := false
				return &off, group.reason
			}
		}
	}
	return configured, "default_on"
}
