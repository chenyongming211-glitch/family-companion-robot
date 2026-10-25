package realtimestate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func baselineTurn(data map[string]any) *Turn {
	id, _ := strictInt(data["id"])
	turn := NewTurn(id)
	turn.User, _ = data["user"].(string)
	turn.Generated, _ = data["generated"].(string)
	turn.Sent, _ = strictInt(data["sent"])
	turn.Completed, _ = strictInt(data["completed"])
	turn.Accepted, _ = strictInt(data["accepted"])
	turn.Cut, _ = data["cut"].(bool)
	turn.Begun, _ = data["begun"].(bool)
	turn.Ended, _ = data["ended"].(bool)
	for _, raw := range list(data["sentences"]) {
		turn.Tracker.Add(object(raw), 1000)
	}
	return turn
}
func evaluateBaselineCase(caseData map[string]any, product *Product) (any, error) {
	switch caseData["action"] {
	case "number":
		return NumberKey(caseData["value"].(string)), nil
	case "equivalent":
		return EquivalentPrefix(caseData["generated"].(string), caseData["observed"].(string)), nil
	case "context", "instructions":
		turns := []*Turn{}
		for _, raw := range list(caseData["turns"]) {
			turns = append(turns, baselineTurn(object(raw)))
		}
		context := ContextFor(turns)
		if caseData["action"] == "context" {
			var result any
			err := json.Unmarshal([]byte(context), &result)
			return result, err
		}
		return product.InstructionsFor(object(caseData["cfg"]), context, caseData["connected"].(bool), time.Date(2026, 10, 25, 5, 14, 15, 0, time.UTC))
	case "tracker":
		total, _ := numeric(caseData["total"])
		played, _ := numeric(caseData["played"])
		tracker := NewPrefixTracker(caseData["generated"].(string), total)
		for _, raw := range list(caseData["sentences"]) {
			entry := object(raw)
			at, _ := numeric(entry["received"])
			tracker.Add(object(entry["sentence"]), at)
		}
		return map[string]any{"candidate": tracker.Candidate(played), "covers": tracker.Covers(played), "first": tracker.FirstUsableMS(played)}, nil
	case "receipt":
		turn := baselineTurn(object(caseData["turn"]))
		if caseData["seal"] == true {
			turn.SealForDrain()
		}
		accepted := []bool{}
		for _, raw := range list(caseData["events"]) {
			accepted = append(accepted, turn.Receipt(object(raw)))
		}
		done := false
		select {
		case <-turn.DrainDone:
			done = true
		default:
		}
		return map[string]any{"accepted": accepted, "completed": turn.Completed, "received": turn.Accepted, "cut": turn.Cut, "drain_target": turn.DrainTarget, "drain_done": done, "can_send": turn.CanSend()}, nil
	}
	return nil, fmt.Errorf("unknown action")
}
func TestPythonDifferential(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if override := os.Getenv("REALTIME_PYTHON_BASELINE"); override != "" {
		root = override
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python baseline unavailable")
	}
	product, err := LoadProduct(filepath.Join(root, "config/product-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []map[string]any{}
	for _, value := range []string{"0", "0012", "０１２", "零", "〇", "两", "一二", "十二", "十七", "一百零五", "二百五", "二百〇五", "一千零一", "一千一", "一百一十", "一十百", "12三", "", "九千九百九十九"} {
		cases = append(cases, map[string]any{"action": "number", "value": value})
	}
	for _, pair := range [][2]string{{"刚才念到十七。接下来十八。", "刚才念到17"}, {"一、二。", "12"}, {"二百五。", "205"}, {"ＡＢＣ：Straße。后面", "abcstrasse"}, {"İstanbul。后面", "i̇stanbul"}, {"一千零一！后面", "1001"}, {"\tＡ ①。后面", "\ta1"}, {"ǅ😀foo。后面", "džfoo"}, {"你好\u2028世界。未播", "你好世界"}} {
		cases = append(cases, map[string]any{"action": "equivalent", "generated": pair[0], "observed": pair[1]})
	}
	sentence := func(id int, text string, start, end any) map[string]any {
		return map[string]any{"sentence_id": id, "sentence_end": true, "words": []any{map[string]any{"text": text, "begin_time": start, "end_time": end}}}
	}
	rng := rand.New(rand.NewSource(20261025))
	for i := 0; i < 180; i++ {
		generated := []string{"刚才念到十七。接下来十八。", "小兔走进森林。后面有狐狸。", "ＡＢＣ Straße。后面", "一、二、三。四。"}[i%4]
		observed := []string{"刚才念到17", "小鹿走进森林", "abcstrasse", "123"}[i%4]
		sent := int64(1440 * (rng.Intn(80) + 1))
		completed := int64(1440 * rng.Intn(int(sent/1440)+1))
		words := []any{sentence(1, observed, 0, 500), sentence(2, "尾", 500, 1000)}
		switch i % 11 {
		case 0:
			words = nil
		case 1:
			words = []any{sentence(2, observed, 0, 500)}
		case 2:
			words = []any{sentence(1, observed, 0, true)}
		case 3:
			words = []any{sentence(1, observed, 0, 500), sentence(3, "尾", 500, 1000)}
		case 4:
			words = []any{sentence(1, strings.Repeat("字", 2049), 0, 500)}
		case 5:
			words = []any{sentence(1, observed, 500, 100)}
		case 6:
			words = []any{sentence(1, observed, 0, 500), sentence(2, "尾", 400, 1000)}
		}
		turn := map[string]any{"id": 1, "user": "继续讲", "generated": generated, "sent": sent, "completed": completed, "accepted": completed, "cut": i%2 == 0, "ended": i%3 == 0, "sentences": words}
		cases = append(cases, map[string]any{"action": "context", "turns": []any{turn}})
		entries := []any{}
		for j, s := range words {
			entries = append(entries, map[string]any{"sentence": s, "received": j*137 + 100})
		}
		cases = append(cases, map[string]any{"action": "tracker", "generated": generated, "total": float64(sent) / 24, "played": float64(completed) / 24, "sentences": entries})
	}
	turns := []any{}
	for i := 0; i < 20; i++ {
		turns = append(turns, map[string]any{"id": i, "user": strings.Repeat("🙂", 2100)})
	}
	cases = append(cases, map[string]any{"action": "context", "turns": turns})
	cases = append(cases, map[string]any{"action": "context", "turns": []any{map[string]any{"id": 1, "generated": "小兔。未播", "sent": 48000, "completed": 28800, "sentences": []any{sentence(1, "\x1c小兔\x1f", 100, 500)}}}})
	cases = append(cases, map[string]any{"action": "context", "turns": []any{map[string]any{"id": 1, "user": "literal \\u2028 and actual \u2028 separator"}}})
	for _, cfg := range []map[string]any{{"model": "qwen3.8-omni-flash-realtime"}, {"QWEN_REALTIME_URL": "wss://secret.invalid/?model=other-model"}, {"model": "QWEN-test", "enable_search": true}, {"model": "qwen", "enable_search": 1}, {"model": "", "QWEN_REALTIME_URL": "wss://example/?model=&model=qwen+test"}, {}} {
		for _, connected := range []bool{true, false} {
			cases = append(cases, map[string]any{"action": "instructions", "cfg": cfg, "connected": connected, "turns": turns[:2]})
		}
	}
	cases = append(cases, map[string]any{"action": "instructions", "cfg": map[string]any{"model": "qwen-test", "enable_search": true}, "connected": true, "turns": []any{
		map[string]any{"id": 1, "user": "刚才到哪里了？literal \\u2029", "generated": "刚才念到十七。未播。", "sent": 48000, "completed": 28800, "cut": true, "sentences": []any{sentence(1, "刚才念到17", json.Number("100.0"), json.Number("500.5"))}},
		map[string]any{"id": 2, "user": "继续", "generated": "继续讲故事。", "sent": 57600, "completed": 57600, "ended": true},
	}})
	for i := 0; i < 90; i++ {
		events := []any{}
		for j := 0; j < 8; j++ {
			completed := rng.Intn(9) * 1440
			accepted := rng.Intn(9) * 1440
			terminal := []string{"progress", "played", "drained", "unknown"}[rng.Intn(4)]
			events = append(events, map[string]any{"id": 1, "basis": "CoreAudio_dataPlayedBack", "completed": completed, "accepted": accepted, "terminal": terminal})
		}
		cases = append(cases, map[string]any{"action": "receipt", "turn": map[string]any{"id": 1, "sent": 7200, "ended": i%2 == 0}, "seal": i%3 == 0, "events": events})
	}
	raw, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	// Decode the shared transport with UseNumber so integer/float lexical types survive.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "testdata/oracle.py", root)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python baseline: %v: %s", err, stderr.String())
	}
	var expected []any
	if err := json.Unmarshal(output, &expected); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		actual, err := evaluateBaselineCase(c, product)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		encoded, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		var comparable any
		if err := json.Unmarshal(encoded, &comparable); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(comparable, expected[i]) {
			t.Errorf("case %d (%v):\nGo: %s\nPython: %s", i, c["action"], encoded, mustJSON(expected[i]))
		}
	}
	t.Logf("%d deterministic Python differential cases", len(cases))
}
func mustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
