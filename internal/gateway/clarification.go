package gateway

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"familyrobot/internal/qwen"
)

const clarificationText = "我还不能确认刚才停在哪儿。请告诉我从哪里继续，或从头再来。"
const clarificationInstructions = "当前用户请求恢复原任务，但网关无法确定已播文字位置，用户也没有指定起点或允许从头重读。这轮只原样说下面一句话，不加称呼、解释、承诺或其他任何内容，不调用工具，不执行原任务：" + clarificationText
const maxClarificationPCM = 24000 * 2 * 15
const clarificationTimeout = 8 * time.Second

type clarificationGuard struct {
	active, verified                                              bool
	generation                                                    uint64
	input                                                         requestOrigin
	taskID, responseID, itemID                                    string
	deadline                                                      time.Time
	bytes, events                                                 int
	transcriptDelta, transcript                                   string
	transcriptDone, audioDone                                     bool
	itemAdded, itemDone, partAdded, partDone, conversationCreated bool
}

func normalizeClarification(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, text)
}
func isClarification(text string) bool {
	return len(text) <= 2048 && normalizeClarification(text) == normalizeClarification(clarificationText)
}
func (s *session) clearClarification() {
	if !s.clarification.active {
		return
	}
	s.retired.add(s.clarification.responseID)
	s.retired.add(s.responseID)
	s.pending.Clear()
	s.responseID = ""
	s.responseDone = false
	s.responseAudioSent = false
	s.transcript = ""
	s.draftTranscript = ""
	s.awaitingResponse = false
	s.clarification = clarificationGuard{}
}
func (s *session) rejectClarification() error {
	s.traceVoice("clarification_rejected")
	s.clearClarification()
	return errors.New("native clarification rejected")
}
func (s *session) checkClarificationDeadline(now time.Time) error {
	if s.clarification.active && !s.clarification.verified && !now.Before(s.clarification.deadline) {
		return s.rejectClarification()
	}
	return nil
}

// Inspect the native voice response before ordinary handlers can mutate tools,
// history, playback, or transcript state. Audio stays in the existing bounded
// pending buffer; play cannot pop it until the full response is verified.
func (s *session) guardClarification(ev qwen.Event) error {
	g := &s.clarification
	if !g.active {
		return nil
	}
	if err := s.checkClarificationDeadline(time.Now()); err != nil {
		return err
	}
	if g.generation != s.generation || g.input != s.currentInput || s.paused == nil || g.taskID != s.paused.id {
		return s.rejectClarification()
	}
	if ev.Type == "conversation.item.created" {
		var item struct{ ID, Role string }
		if json.Unmarshal(ev.Item, &item) == nil && item.Role == "user" && item.ID == g.input.itemID {
			return nil
		}
	} else if !strings.HasPrefix(ev.Type, "response.") {
		return nil
	}
	if g.verified {
		return s.rejectClarification()
	}
	g.events++
	if g.events > 2048 {
		return s.rejectClarification()
	}
	if ev.Type == "response.created" {
		if g.responseID != "" || ev.Response.ID == "" || len(ev.Response.Output) != 0 || ev.Response.Status != "" && ev.Response.Status != "in_progress" {
			return s.rejectClarification()
		}
		for _, modality := range ev.Response.Modalities {
			if modality != "text" && modality != "audio" {
				return s.rejectClarification()
			}
		}
		g.responseID = ev.Response.ID
		return nil
	}
	if g.responseID == "" {
		return s.rejectClarification()
	}
	if ev.Type != "response.done" && ev.Type != "conversation.item.created" {
		if ev.ResponseID != g.responseID || ev.OutputIndex != nil && *ev.OutputIndex != 0 || ev.ContentIndex != nil && *ev.ContentIndex != 0 {
			return s.rejectClarification()
		}
		if ev.ItemID != "" {
			if len(ev.ItemID) > 256 || g.itemID != "" && g.itemID != ev.ItemID {
				return s.rejectClarification()
			}
			g.itemID = ev.ItemID
		}
	}
	switch ev.Type {
	case "response.audio.delta":
		if g.audioDone || len(ev.Delta) > base64.StdEncoding.EncodedLen(maxClarificationPCM-g.bytes) {
			return s.rejectClarification()
		}
		pcm, err := base64.StdEncoding.DecodeString(ev.Delta)
		if err != nil || g.bytes+len(pcm) > maxClarificationPCM {
			return s.rejectClarification()
		}
		g.bytes += len(pcm)
	case "response.audio.done":
		if g.audioDone {
			return s.rejectClarification()
		}
		g.audioDone = true
	case "response.audio_transcript.delta":
		if g.transcriptDone || len(g.transcriptDelta)+len(ev.Delta) > 2048 {
			return s.rejectClarification()
		}
		g.transcriptDelta += ev.Delta
	case "response.audio_transcript.done":
		if g.transcriptDone || !isClarification(ev.Transcript) || g.transcriptDelta != "" && normalizeClarification(g.transcriptDelta) != normalizeClarification(ev.Transcript) {
			return s.rejectClarification()
		}
		g.transcriptDone = true
		g.transcript = ev.Transcript
	case "response.output_item.added", "response.output_item.done", "conversation.item.created":
		complete := ev.Type == "response.output_item.done"
		if ev.Type == "response.output_item.added" && (g.itemAdded || g.itemDone) || complete && g.itemDone || ev.Type == "conversation.item.created" && g.conversationCreated || !g.validateItem(ev.Item, complete, false) {
			return s.rejectClarification()
		}
		switch ev.Type {
		case "response.output_item.added":
			g.itemAdded = true
		case "response.output_item.done":
			g.itemDone = true
		case "conversation.item.created":
			g.conversationCreated = true
		}
	case "response.content_part.added", "response.content_part.done":
		complete := ev.Type == "response.content_part.done"
		if !g.validatePart(ev.Part, false) || !complete && (g.partAdded || g.partDone) || complete && (g.partDone || !g.transcriptDone || !g.audioDone) {
			return s.rejectClarification()
		}
		if complete {
			g.partDone = true
		} else {
			g.partAdded = true
		}
	case "response.done":
		if ev.Response.ID != g.responseID || ev.Response.Status != "completed" || !g.transcriptDone || !g.audioDone || g.bytes == 0 || g.bytes%2 != 0 || s.pending.Len() != g.bytes || len(ev.Response.Output) > 1 || g.itemAdded && !g.itemDone || g.partAdded && !g.partDone {
			return s.rejectClarification()
		}
		if len(ev.Response.Output) == 1 && !g.validateItem(ev.Response.Output[0], true, true) {
			return s.rejectClarification()
		}
		g.verified = true
		s.traceVoice("clarification_verified")
	default:
		return s.rejectClarification()
	}
	return nil
}
func (g *clarificationGuard) validatePart(raw json.RawMessage, final bool) bool {
	var part struct {
		Type             string
		Text, Transcript *string
		Audio            json.RawMessage
	}
	if json.Unmarshal(raw, &part) != nil || part.Type != "audio" || len(part.Audio) != 0 {
		return false
	}
	if part.Text != nil && *part.Text != "" && !isClarification(*part.Text) {
		return false
	}
	if part.Transcript != nil && (!g.transcriptDone || !isClarification(*part.Transcript)) {
		return false
	}
	return !final || part.Transcript != nil
}
func (g *clarificationGuard) validateItem(raw json.RawMessage, complete, final bool) bool {
	var item struct {
		ID, Type, Role, Status string
		Content                []json.RawMessage
	}
	if json.Unmarshal(raw, &item) != nil || item.ID == "" || len(item.ID) > 256 || item.Type != "message" || item.Role != "assistant" || len(item.Content) > 1 || g.itemID != "" && g.itemID != item.ID {
		return false
	}
	if complete && item.Status != "" && item.Status != "completed" {
		return false
	}
	g.itemID = item.ID
	if len(item.Content) == 1 && !g.validatePart(item.Content[0], final) {
		return false
	}
	return !complete || len(item.Content) == 1
}
