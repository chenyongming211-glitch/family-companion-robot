//go:build linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"time"

	"familyrobot/internal/speaker"
)

const sessionMaxSamples = 64000
const sessionMaxClips = 32
const sessionLifetime = 5 * time.Minute

// sessionEvent deliberately excludes audio, features, paths and identity fields.
type sessionEvent struct {
	Event      string `json:"event"`
	ID         uint64 `json:"id,omitempty"`
	Label      string `json:"label,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Evidence   string `json:"evidence"`
	Reason     string `json:"reason,omitempty"`
}

type sessionOptions struct {
	realtime bool
	validate func(string, string) error
	extract  func(context.Context, requestHeader, []byte) ([]float32, error)
	lifetime time.Duration
}

// A fresh child is intentional: capture and source gaps can exceed the private
// worker's two-second idle deadline. Only the Tracker survives a clip.
func extractSessionClip(ctx context.Context, model, sha string, h requestHeader, pcm []byte) ([]float32, error) {
	w, err := startWorker(ctx, model, sha)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	v, _, err := w.request(ctx, h, pcm)
	return v, err
}

// runSession owns input. Cancellation closes it to interrupt a partial frame or
// idle read; no goroutine is left blocked on a caller-owned global stdin.
func runSession(parent context.Context, input io.ReadCloser, out io.Writer, model, sha string, declared bool, opt sessionOptions) error {
	defer input.Close()
	if !declared || !shaPattern.MatchString(sha) {
		return errProtocol
	}
	if opt.validate == nil {
		opt.validate = validateModel
	}
	if opt.validate(model, sha) != nil {
		return errModel
	}
	if opt.lifetime <= 0 || opt.lifetime > sessionLifetime {
		opt.lifetime = sessionLifetime
	}
	ctx, cancel := context.WithTimeout(parent, opt.lifetime)
	defer cancel()
	interrupt := context.AfterFunc(ctx, func() { _ = input.Close() })
	defer interrupt()
	if ctx.Err() != nil {
		return errWorker
	}
	scope, evidence, maxClipSamples := "manual-test", "user_declared_test", uint64(sessionMaxSamples)
	if opt.realtime {
		scope, evidence, maxClipSamples = "realtime-test", "turn_taking_test", 8*sampleRate
	}
	encode := func(event string, id uint64, label string, duration int64, reason string) error {
		fields := map[string]any{"event": event, "evidence": evidence}
		if event == "observation" {
			fields["id"] = id
			fields["label"] = label
			fields["duration_ms"] = duration
			if opt.realtime {
				fields["reason"] = reason
			}
		}
		return json.NewEncoder(out).Encode(fields)
	}
	if err := encode("ready", 0, "", 0, ""); err != nil {
		return err
	}
	var tracker *speaker.Tracker
	defer func() {
		if tracker != nil {
			tracker.Close()
		}
		_ = encode("closed", 0, "", 0, "")
	}()
	if opt.extract == nil {
		opt.extract = func(ctx context.Context, h requestHeader, p []byte) ([]float32, error) {
			return extractSessionClip(ctx, model, sha, h, p)
		}
	}
	reader := bufio.NewReaderSize(input, maxHeaderBytes+4)
	var first requestHeader
	var lastID, end uint64
	for count := 0; count < sessionMaxClips; count++ {
		if _, err := reader.Peek(1); err != nil {
			if err == io.EOF && ctx.Err() == nil {
				return nil
			}
			return errProtocol
		}
		h, pcm, err := readRequest(reader, sha)
		if err != nil {
			clear(pcm)
			return errProtocol
		}
		invalidStart := h.Start != end
		if opt.realtime {
			invalidStart = count > 0 && h.Start < end
		}
		if ctx.Err() != nil || h.Scope != scope || h.End-h.Start > maxClipSamples || h.ID <= lastID || invalidStart || count > 0 && (h.Session != first.Session || h.Generation != first.Generation) {
			clear(pcm)
			return errProtocol
		}
		if count == 0 {
			first = h
		}
		lastID, end = h.ID, h.End
		label, reason := "uncertain", "match_uncertain"
		quality := qualityReason(pcm)
		if quality == "qualified" {
			v, err := opt.extract(ctx, h, pcm)
			clear(pcm)
			if err != nil || ctx.Err() != nil {
				clear(v)
				return errWorker
			}
			if !validVector(v, len(v)) {
				clear(v)
				return errProtocol
			}
			scope := speaker.Scope{SessionID: first.Session, StreamGeneration: first.Generation, ModelRevision: sha}
			if tracker == nil {
				tracker, err = speaker.NewTracker(speaker.Config{Scope: scope, Dimension: len(v), MinSamples: minQualitySamples, MatchThreshold: .682987, NewSpeakerThreshold: .622530, Margin: .030229, MaxSpeakers: 8})
				if err != nil {
					clear(v)
					return errProtocol
				}
			}
			result, err := tracker.Observe(speaker.Observation{Scope: scope, StartSample: int64(h.Start), EndSample: int64(h.End), Embedding: v, SingleSpeaker: true, Complete: true})
			clear(v)
			if err != nil {
				return errProtocol
			}
			if result.State == speaker.StateTemporary {
				label = result.Label
				reason = "candidate"
			} else {
				label = string(result.State)
				if result.State == speaker.StateUnknown {
					reason = "capacity"
				}
			}
		} else {
			clear(pcm)
			reason = "low_energy"
			if quality == "short" {
				reason = "too_short"
			}
		}
		if ctx.Err() != nil {
			return errWorker
		}
		if err := encode("observation", h.ID, label, int64((h.End-h.Start)*1000/sampleRate), reason); err != nil {
			return err
		}
	}
	return nil
}
