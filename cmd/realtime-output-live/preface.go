package main

import (
	"context"
	"familyrobot/internal/realtimelive"
	"log"
	"os"
	"path/filepath"
	"time"
)

// configurePreface attaches the acknowledgement store and makes sure the
// configured voice has clips.
//
// The clips are generated at runtime rather than shipped, because the voice is
// configuration: a set of files baked for "Tina" becomes the wrong speaker the
// moment QWEN_VOICE changes, and the robot would answer in one voice and clear
// its throat in another. Clips live in one directory per voice, so a later
// voice change finds or regenerates its own set.
//
// Nothing here is fatal. An acknowledgement only fills the silence before an
// answer; refusing to start the voice service because a directory of clips is
// missing would turn a cosmetic regression into an outage. The log line is the
// signal -- if it says synthesizing, turns wait in silence until it finishes.
func configurePreface(cfg *realtimelive.Config, root, linesPath string) {
	lines, err := realtimelive.LoadPrefaceLines(linesPath)
	if err != nil {
		log.Printf("preface_lines_unavailable: %v", err)
		return
	}
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("preface_pool_unavailable: %v", err)
			return
		}
		// Beside the binary, so the pool deploys and rolls back with the release
		// it belongs to and no drop-in has to learn about it. That matters: the
		// playback credit window drop-in is load-bearing, and every new ExecStart
		// is a chance to lose it.
		root = filepath.Join(filepath.Dir(exe), "preface")
	}
	store := realtimelive.NewPrefaceStore()
	cfg.Preface = store
	// Snapshot the provider settings: the goroutine below outlives this call and
	// main keeps assigning to cfg, so reading it later would be a data race.
	synthCfg := realtimelive.Config{URL: cfg.URL, Key: cfg.Key, Voice: cfg.Voice, Model: cfg.Model}

	dir := filepath.Join(root, cfg.Voice)
	if pool, err := realtimelive.LoadPrefacePool(dir, cfg.Voice); err == nil {
		store.Put(pool)
		log.Printf("preface_pool_loaded dir=%s voice=%s lines=%d", dir, cfg.Voice, pool.Lines())
		return
	} else {
		log.Printf("preface_pool_absent dir=%s voice=%s: synthesizing", dir, cfg.Voice)
	}
	// Synthesis takes about a minute for a full set and touches the same provider
	// the conversations use, so it runs behind the listener rather than in front
	// of it.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		pool, err := store.Ensure(ctx, synthCfg, root, lines)
		if err != nil {
			log.Printf("preface_pool_synthesis_failed voice=%s: %v", cfg.Voice, err)
			return
		}
		log.Printf("preface_pool_synthesized dir=%s voice=%s lines=%d", dir, cfg.Voice, pool.Lines())
	}()
}
