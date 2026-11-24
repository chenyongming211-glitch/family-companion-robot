package realtimelive

import "testing"

func searchAllowedFor(t *testing.T, user string) (bool, string) {
	t.Helper()
	on := true
	got, reason := searchDecision(user, &on)
	if got == nil {
		t.Fatalf("%q: a configured setting must never come back nil", user)
	}
	return *got, reason
}

// The turns that actually cost five seconds were mostly asking for things the
// model already knew: it searched to tell a folk story and to say hello.
func TestSearchGateClosesTurnsThatNeedNothingNew(t *testing.T) {
	for _, tc := range []struct{ user, reason string }{
		{"讲个故事吧", "story"},
		{"再讲一个跟长江有关的", "continuation"},
		{"接着说", "continuation"},
		{"还有吗", "continuation"},
		{"你好", "company"},
		{"我有点累了", "company"},
		{"你还记得我刚才说要去哪吗", "recall"},
		{"嗯，你还记得刚才我让你查哪个城市的天气吗？", "recall"},
		{"记不记得上次推荐的电影叫什么", "recall"},
		{"你觉得哪个好", "opinion"},
		{"大点声", "device"},
	} {
		allowed, reason := searchAllowedFor(t, tc.user)
		if allowed {
			t.Errorf("%q must not search", tc.user)
		}
		if reason != tc.reason {
			t.Errorf("%q: expected reason %q, got %q", tc.user, tc.reason, reason)
		}
	}
}

// A wrong close answers from old data while sounding certain, which is worse
// than being slow, so anything asking about the world keeps the default.
func TestSearchGateKeepsTurnsThatNeedTheWorld(t *testing.T) {
	for _, user := range []string{
		"明天杭州天气怎么样",
		"帮我推荐杭州吃饭的地方",
		"最近有什么新闻",
		"现在金价多少钱",
		"高铁几点发车",
		"这附近哪家店还开着",
	} {
		allowed, reason := searchAllowedFor(t, user)
		if !allowed {
			t.Errorf("%q must keep search, got reason %q", user, reason)
		}
	}
}

// A fresh-information word wins over a company word or a bare past reference
// ("我刚才说的那个城市明天天气怎么样" needs tomorrow's weather), while explicit
// memory-verification questions ("你还记得刚才我让你查哪个城市的天气吗") close search.
func TestSearchGateLetsFreshInformationOverride(t *testing.T) {
	for _, user := range []string{
		"谢谢，明天天气怎么样",
		"我刚才说的那个城市明天天气怎么样",
		"上次说的那部电影什么时候上映",
	} {
		allowed, reason := searchAllowedFor(t, user)
		if !allowed || reason != "fresh_information" {
			t.Fatalf("%q: expected a search with reason fresh_information, got %v %q", user, allowed, reason)
		}
	}
}

// A long question is doing something a keyword cannot classify: this one ends in
// an opinion word, which would otherwise close search on a turn that is really
// asking for a considered answer.
func TestSearchGateKeepsLongInput(t *testing.T) {
	allowed, reason := searchAllowedFor(t, "我在想咱们家里这台机器人的反应有时候慢了一点，说话也偶尔重复，不过总的来说还挺好的，你自己觉得呢")
	if !allowed || reason != "long_input" {
		t.Fatalf("a long turn must keep the default, got %v %q", allowed, reason)
	}
}

// A deployment that turned search off must not get it turned back on.
func TestSearchGateNeverEnablesSearchByItself(t *testing.T) {
	off := false
	got, reason := searchDecision("明天杭州天气怎么样", &off)
	if got == nil || *got || reason != "disabled_by_config" {
		t.Fatalf("a disabled setting must stay disabled, got %v %q", got, reason)
	}
	if got, _ := searchDecision("明天杭州天气怎么样", nil); got != nil {
		t.Fatal("an unset setting must stay unset")
	}
}

// No transcript yet is not evidence that the turn needed nothing.
func TestSearchGateKeepsTheDefaultWithoutTranscript(t *testing.T) {
	allowed, reason := searchAllowedFor(t, "   ")
	if !allowed || reason != "no_transcript" {
		t.Fatalf("an empty transcript must keep the default, got %v %q", allowed, reason)
	}
	allowed, reason = searchAllowedFor(t, "随便聊聊")
	if !allowed || reason != "default_on" {
		t.Fatalf("an unclassified turn must keep the default, got %v %q", allowed, reason)
	}
}
