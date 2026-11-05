//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWeSpeakerModelBound(t *testing.T) {
	if maxModelBytes != 128*1024*1024 || requestTimeout != 2*time.Second || startupTimeout != 10*time.Second {
		t.Fatal("fixed bounded model/request/startup contract changed")
	}
	path := filepath.Join(t.TempDir(), "synthetic-model")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Match the fixed large candidate's size without generating model weights.
	if err = f.Truncate(114336285); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	sha := hex.EncodeToString(h.Sum(nil))
	verified, err := openModel(path, sha)
	if err != nil {
		t.Fatal("fixed large model size refused", err)
	}
	verified.Close()
	if validateModel(path, testSHA) == nil {
		t.Fatal("wrong hash accepted")
	}
	link := path + "-link"
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if validateModel(link, sha) == nil {
		t.Fatal("symlink accepted")
	}
	if err = f.Truncate(maxModelBytes + 1); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if validateModel(path, sha) == nil {
		t.Fatal("oversize model accepted")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("oversize model was read before rejection")
	}
}

func TestWeSpeakerNativeSyntheticOptIn(t *testing.T) {
	model, sha, wav, binary := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_WAV"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if os.Getenv("SPEAKER_LAB_TEST_WESPEAKER") != "1" {
		t.Skip("explicit fixed-model synthetic preflight opt-in not set")
	}
	if model == "" || sha == "" || wav == "" || binary == "" {
		t.Fatal("explicit verified model, built binary and synthetic WAV required")
	}
	pcm, err := readWAVFileForModelTest(wav)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(pcm)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	w, err := startWorkerBinary(ctx, binary, model, sha)
	if err != nil {
		t.Fatal("fixed-budget native startup failed", err)
	}
	defer w.Close()
	socket := w.conn.RemoteAddr().String()
	h := requestHeader{Version: 1, ID: 1, Session: "preflight", Scope: "synthetic", Generation: 1, Start: 0, End: uint64(len(pcm) / 2), ModelSHA: sha, SampleRate: sampleRate, PCMBytes: len(pcm)}
	v, us, err := w.request(ctx, h, pcm)
	if err != nil {
		t.Fatal("fixed-budget native extraction failed", err)
	}
	if w.dimension != 256 || !validVector(v, 256) {
		clear(v)
		t.Fatal("expected finite 256-dimensional vector")
	}
	clear(v)
	for _, value := range v {
		if value != 0 {
			t.Fatal("owned vector not cleared")
		}
	}
	w.Close()
	if w.cmd.ProcessState == nil {
		t.Fatal("native worker not reaped")
	}
	select {
	case <-w.done:
	default:
		t.Fatal("native worker still running")
	}
	if _, err = os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatal("native socket retained")
	}
	t.Logf("population=synthetic_engineering_fixture open_set_assessment=unavailable dimension=256 samples=%d cold_ms=%.3f native_ms=%.3f cleanup=reaped_socket_removed_vector_cleared", len(pcm)/2, float64(w.coldStart.Microseconds())/1000, float64(us)/1000)
}

func readWAVFileForModelTest(path string) ([]byte, error) {
	if !noSymlinks(path) {
		return nil, fmt.Errorf("invalid synthetic fixture")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readWAV(f)
}
