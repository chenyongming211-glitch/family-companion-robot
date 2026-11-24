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

// When search is enabled in configuration, every turn with a transcript keeps
// search enabled so the model's built-in search agent decides whether to query
// the web.
func TestSearchGateDelegatesToModelWhenConfiguredOn(t *testing.T) {
	for _, user := range []string{
		"明天杭州天气怎么样",
		"帮我推荐杭州吃饭的地方",
		"讲个故事吧",
		"你还记得刚才我让你查哪个城市的天气吗？",
		"随便聊聊",
	} {
		allowed, reason := searchAllowedFor(t, user)
		if !allowed || reason != "default_on" {
			t.Errorf("%q: expected search allowed with default_on, got %v %q", user, allowed, reason)
		}
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

// No transcript yet still keeps the configured default.
func TestSearchGateKeepsTheDefaultWithoutTranscript(t *testing.T) {
	allowed, reason := searchAllowedFor(t, "   ")
	if !allowed || reason != "no_transcript" {
		t.Fatalf("an empty transcript must keep the default, got %v %q", allowed, reason)
	}
}
