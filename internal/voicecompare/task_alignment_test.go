package voicecompare

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func taskAlignmentFixture(t *testing.T, c *websocket.Conn, failure string) {
	run := readJSON(t, c)
	id := run["header"].(map[string]any)["task_id"].(string)
	taskEvent(c, id, "task-started", nil)
	cont := readJSON(t, c)
	text := cont["payload"].(map[string]any)["input"].(map[string]any)["text"].(string)
	readJSON(t, c)
	pieces := []string{}
	for _, r := range text {
		pieces = append(pieces, string(r))
	}
	if strings.Contains(text, "100") {
		if strings.Contains(text, "……") {
			pieces = []string{"第", "一", "百", "颗", "……", "结", "束", "。"}
		} else {
			pieces = []string{"第", "一", "百", "颗", "、", "、", "结", "束", "。"}
		}
	}
	words := []map[string]any{}
	for i, part := range pieces {
		words = append(words, map[string]any{"text": part, "begin_index": i, "end_index": i + 1, "begin_time": i * 20, "end_time": (i + 1) * 20})
	}
	switch failure {
	case "lexical":
		words[len(words)-2]["text"] = "错"
	case "index":
		words[len(words)-1]["end_index"] = 200
	case "time":
		words[len(words)-1]["end_time"] = 201
	case "nonmonotonic":
		words[len(words)-1]["begin_time"] = 0
	case "missing":
		delete(words[0], "begin_index")
	}
	original := text
	if failure == "original" {
		original = "其他原稿。"
	}
	out := func(kind string, value any) map[string]any {
		return map[string]any{"type": kind, "original_text": original, "normalized_text": original, "sentence": map[string]any{"index": 0, "words": value}}
	}
	taskEvent(c, id, "result-generated", out("sentence-begin", nil))
	taskEvent(c, id, "result-generated", out("sentence-synthesis", nil))
	_ = c.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
	taskEvent(c, id, "result-generated", out("sentence-end", words))
	if failure != "terminal" {
		taskEvent(c, id, "task-finished", nil)
	}
}

func TestTTSSpokenCoordinatesUseExplicitTaskBoundaries(t *testing.T) {
	for _, text := range []string{"第100颗……结束。", "第100颗——结束。", "开头。第100颗……结束。完毕。"} {
		t.Run(text, func(t *testing.T) {
			client := fixtureClient(wsFixture(t, func(c *websocket.Conn) { taskAlignmentFixture(t, c, "") }))
			result, err := client.Run(context.Background(), "tts", "", nil, text)
			if err != nil {
				t.Fatalf("completed normalized speech became silent: %v", err)
			}
			var stored struct {
				TaskBoundaries []struct {
					Text, Precision string
					EndSamples      int64
				}
			}
			raw, _ := json.Marshal(result)
			_ = json.Unmarshal(raw, &stored)
			clauses := splitClauses(text)
			if len(stored.TaskBoundaries) != len(clauses) || len(result.PCM) != len(clauses)*9600 || result.Text != text {
				t.Fatalf("lost completed PCM or explicit task boundaries: %s", raw)
			}
			for i, boundary := range stored.TaskBoundaries {
				precision := "word"
				if strings.Contains(clauses[i], "100") {
					precision = "task_boundary"
				}
				if boundary.Text != clauses[i] || boundary.Precision != precision || boundary.EndSamples != int64((i+1)*4800) {
					t.Fatal("wrong frozen source, precision or PCM boundary", boundary)
				}
			}
			prefix := ""
			for _, w := range result.Words {
				prefix += w.Text
			}
			want := ""
			if strings.HasPrefix(text, "开头。") {
				want = "开头。"
			}
			if prefix != want {
				t.Fatalf("invented words or crossed a coordinate gap: %q", prefix)
			}
		})
	}
}

func TestTTSTaskBoundaryStillRejectsUntrustedMetadata(t *testing.T) {
	for _, failure := range []string{"lexical", "index", "time", "nonmonotonic", "missing", "original", "terminal"} {
		t.Run(failure, func(t *testing.T) {
			client := fixtureClient(wsFixture(t, func(c *websocket.Conn) { taskAlignmentFixture(t, c, failure) }))
			result, err := client.Run(context.Background(), "tts", "", nil, "第100颗……结束。")
			if err == nil || len(result.PCM) != 0 {
				t.Fatalf("untrusted %s became playable: %v", failure, err)
			}
		})
	}
}
