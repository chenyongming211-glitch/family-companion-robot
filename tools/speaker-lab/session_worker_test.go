//go:build linux

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sessionFakeWorker returns a workerProcess backed by an in-process
// serveConnection + fakeExtractor, so no model binary or ONNX load is needed.
func sessionFakeWorker(t *testing.T) *workerProcess {
	t.Helper()
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		serveConnection(ctx, server, &fakeExtractor{}, testSHA)
		_ = server.Close()
	}()
	dim, err := readReady(client, testSHA)
	if err != nil {
		_ = client.Close()
		cancel()
		<-serveDone
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w := &workerProcess{conn: client, sha: testSHA, dimension: dim, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(w.done) }()
	t.Cleanup(func() {
		w.Close()
		cancel()
		<-serveDone
	})
	return w
}

// sessionDeadWorker returns a workerProcess whose peer is already closed, so
// the first request fails and the session must rebuild the worker.
func sessionDeadWorker(t *testing.T) *workerProcess {
	t.Helper()
	client, server := net.Pipe()
	_ = server.Close()
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	w := &workerProcess{conn: client, sha: testSHA, dimension: 2, cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(w.done) }()
	t.Cleanup(w.Close)
	return w
}

func TestSessionReusesOneWorkerForManyClips(t *testing.T) {
	var starts int
	start := func(context.Context, string, string) (*workerProcess, error) {
		starts++
		if starts > 1 {
			t.Fatal("started a second worker")
		}
		return sessionFakeWorker(t), nil
	}
	var in, out bytes.Buffer
	for i := 0; i < 3; i++ {
		h := sessionFrame(uint64(i+1), uint64(i*32000), 32000)
		if err := writeRequest(&in, h, sessionPCM(32000)); err != nil {
			t.Fatal(err)
		}
	}
	opt := sessionOptions{validate: func(string, string) error { return nil }, start: start}
	if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("worker starts=%d want=1", starts)
	}
	ev := sessionEvents(t, out.Bytes())
	if len(ev) != 5 || ev[0].Event != "ready" || ev[4].Event != "closed" {
		t.Fatalf("events=%+v", ev)
	}
	for i := 1; i <= 3; i++ {
		if ev[i].Label != "guest_1" || ev[i].ID != uint64(i) {
			t.Fatalf("clip %d event=%+v", i, ev[i])
		}
	}
}

func TestSessionDegradesOneClipAfterWorkerFailure(t *testing.T) {
	var starts int
	start := func(context.Context, string, string) (*workerProcess, error) {
		starts++
		if starts == 1 {
			return sessionDeadWorker(t), nil
		}
		return sessionFakeWorker(t), nil
	}
	var in, out bytes.Buffer
	for i := 0; i < 2; i++ {
		h := sessionFrame(uint64(i+1), uint64(i*32000), 32000)
		if err := writeRequest(&in, h, sessionPCM(32000)); err != nil {
			t.Fatal(err)
		}
	}
	opt := sessionOptions{validate: func(string, string) error { return nil }, start: start}
	if err := runSession(context.Background(), io.NopCloser(&in), &out, "model", strings.Repeat("a", 64), true, opt); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("worker starts=%d want=2 (warm + one restart)", starts)
	}
	ev := sessionEvents(t, out.Bytes())
	if len(ev) != 4 || ev[0].Event != "ready" || ev[3].Event != "closed" {
		t.Fatalf("events=%+v", ev)
	}
	if ev[1].Label != "uncertain" {
		t.Fatalf("failed clip must degrade to uncertain: %+v", ev[1])
	}
	if ev[2].Label != "guest_1" || ev[2].ID != 2 {
		t.Fatalf("next clip must recover on a fresh worker: %+v", ev[2])
	}
}

// TestWorkerConnectionIdleReuse proves an explicitly configured idle deadline
// stays separate from the two-second read/extract/write budget. Production
// leaves the idle deadline at zero (no idle close); this test sets four seconds
// and sleeps past the two-second request budget to prove the two are distinct.
func TestWorkerConnectionIdleReuse(t *testing.T) {
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveConnectionSequence(ctx, server, &fakeExtractor{}, testSHA, &requestSequence{}, 4*time.Second)
		_ = server.Close()
	}()
	dim, err := readReady(client, testSHA)
	if err != nil {
		t.Fatal(err)
	}
	req := sampleRequest()
	if err := writeRequest(client, req, make([]byte, req.PCMBytes)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readResponse(client, req, dim); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2500 * time.Millisecond)
	req.ID++
	if err := writeRequest(client, req, make([]byte, req.PCMBytes)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readResponse(client, req, dim); err != nil {
		t.Fatal("idle connection was not reused", err)
	}
	_ = client.Close()
	<-done
}
