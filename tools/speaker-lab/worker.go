//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux/v2"
)

const requestTimeout = 2 * time.Second
const startupTimeout = 10 * time.Second
const maxModelBytes = 100 * 1024 * 1024

var errModel = errors.New("invalid_model")
var errWorker = errors.New("worker_failed")

type extractor interface {
	Dimension() int
	Extract([]byte) ([]float32, error)
	Close()
}
type nativeExtractor struct {
	native *sherpa.SpeakerEmbeddingExtractor
	dim    int
}

func (n *nativeExtractor) Dimension() int { return n.dim }
func (n *nativeExtractor) Close()         { sherpa.DeleteSpeakerEmbeddingExtractor(n.native) }
func (n *nativeExtractor) Extract(pcm []byte) ([]float32, error) {
	samples := make([]float32, len(pcm)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
	}
	stream := n.native.CreateStream()
	if stream == nil {
		return nil, errUnavailable
	}
	defer sherpa.DeleteOnlineStream(stream)
	stream.AcceptWaveform(sampleRate, samples)
	stream.InputFinished()
	if !n.native.IsReady(stream) {
		return nil, errUnavailable
	}
	v := n.native.Compute(stream)
	if !validVector(v, n.dim) {
		return nil, errUnavailable
	}
	return v, nil
}

// Refuse non-canonical paths and symlink components, including ancestors.
func noSymlinks(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	part := string(filepath.Separator)
	for _, name := range strings.Split(strings.TrimPrefix(path, part), part) {
		if name == "" {
			continue
		}
		part = filepath.Join(part, name)
		st, e := os.Lstat(part)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func openModel(path, sha string) (*os.File, error) {
	if !shaPattern.MatchString(sha) || !noSymlinks(path) {
		return nil, errModel
	}
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > maxModelBytes {
		return nil, errModel
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, errModel
	}
	f := os.NewFile(uintptr(fd), "model")
	current, e := f.Stat()
	if e != nil || !os.SameFile(st, current) {
		f.Close()
		return nil, errModel
	}
	digest := sha256.New()
	n, e := io.Copy(digest, io.LimitReader(f, maxModelBytes+1))
	if e != nil || n != st.Size() || n > maxModelBytes || hex.EncodeToString(digest.Sum(nil)) != sha {
		f.Close()
		return nil, errModel
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		f.Close()
		return nil, errModel
	}
	return f, nil
}
func validateModel(path, sha string) error {
	f, e := openModel(path, sha)
	if e == nil {
		f.Close()
	}
	return e
}
func loadExtractor(path, sha string) (extractor, error) {
	f, e := openModel(path, sha)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	// Load the verified open inode, never reopen the caller's mutable path.
	config := sherpa.SpeakerEmbeddingExtractorConfig{Model: fmt.Sprintf("/proc/self/fd/%d", f.Fd()), NumThreads: 1, Debug: 0, Provider: "cpu"}
	native := sherpa.NewSpeakerEmbeddingExtractor(&config)
	if native == nil {
		return nil, errModel
	}
	dim := native.Dim()
	if dim < 1 || dim > maxDimension {
		sherpa.DeleteSpeakerEmbeddingExtractor(native)
		return nil, errModel
	}
	return &nativeExtractor{native: native, dim: dim}, nil
}

type privateListener struct {
	*net.UnixListener
	path     string
	identity os.FileInfo
	once     sync.Once
}

func (l *privateListener) Close() error {
	var result error
	l.once.Do(func() {
		result = l.UnixListener.Close()
		st, e := os.Lstat(l.path)
		if e == nil && os.SameFile(st, l.identity) {
			_ = os.Remove(l.path)
		}
	})
	return result
}
func listenPrivate(path string) (*privateListener, error) {
	parent := filepath.Dir(path)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !noSymlinks(parent) || len(path) > 100 {
		return nil, errWorker
	}
	st, e := os.Stat(parent)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, errWorker
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errWorker
	}
	if _, e = os.Lstat(path); !os.IsNotExist(e) {
		return nil, errWorker
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, errWorker
	}
	l.SetUnlinkOnClose(false)
	identity, e := os.Lstat(path)
	if e != nil {
		l.Close()
		return nil, errWorker
	}
	managed := &privateListener{UnixListener: l, path: path, identity: identity}
	if os.Chmod(path, 0600) != nil {
		managed.Close()
		return nil, errWorker
	}
	return managed, nil
}
func serveConnection(ctx context.Context, c net.Conn, ex extractor, sha string) {
	serveConnectionSequence(ctx, c, ex, sha, &requestSequence{})
}
func serveConnectionSequence(ctx context.Context, c net.Conn, ex extractor, sha string, sequence *requestSequence) {
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if c.SetDeadline(time.Now().Add(requestTimeout)) != nil || writeReady(c, sha, ex.Dimension()) != nil {
		return
	}
	for {
		if ctx.Err() != nil || c.SetDeadline(time.Now().Add(requestTimeout)) != nil {
			return
		}
		h, pcm, e := readRequest(c, sha)
		if e != nil || !sequence.accept(h) {
			return
		}
		started := time.Now()
		v, e := ex.Extract(pcm)
		us := time.Since(started).Microseconds()
		code := "ok"
		if e != nil || !validVector(v, ex.Dimension()) {
			code = "unavailable"
			v = nil
		}
		// The absolute deadline set before the read also bounds response transmission.
		if ctx.Err() != nil || writeResponse(c, responseFor(h, ex.Dimension(), us, code), v) != nil {
			return
		}
	}
}
func silenceNativeOutput() error {
	f, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if e != nil {
		return errWorker
	}
	defer f.Close()
	for _, fd := range []int{1, 2} {
		if syscall.Dup2(int(f.Fd()), fd) != nil {
			return errWorker
		}
	}
	return nil
}
func runWorker(ctx context.Context, socket, model, sha string) error {
	// Zero core limit and private umask apply before native initialization/bind.
	if syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0}) != nil {
		return errWorker
	}
	syscall.Umask(0077)
	if validateModel(model, sha) != nil {
		return errModel
	}
	// Native libraries can emit diagnostics outside Go's log writers.
	if silenceNativeOutput() != nil {
		return errWorker
	}
	ex, e := loadExtractor(model, sha)
	if e != nil {
		return e
	}
	defer ex.Close()
	l, e := listenPrivate(socket)
	if e != nil {
		return e
	}
	defer l.Close()
	stop := context.AfterFunc(ctx, func() { _ = l.Close() })
	defer stop()
	sequence := &requestSequence{}
	for {
		c, e := l.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errWorker
		}
		serveConnectionSequence(ctx, c, ex, sha, sequence)
		_ = c.Close()
	}
}

// workerProcess has one serial owner. Every timeout/protocol failure retires the
// actual child before returning; no old result can be consumed by a later request.
type workerProcess struct {
	cmd            *exec.Cmd
	conn           net.Conn
	dimension      int
	sha            string
	done           chan struct{}
	closeOnce      sync.Once
	cleanup        func()
	coldStart      time.Duration
	socketIdentity os.FileInfo
}

func (w *workerProcess) Close() {
	w.closeOnce.Do(func() {
		if w.conn != nil {
			_ = w.conn.Close()
		}
		if w.cmd != nil && w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
			<-w.done
		}
		if w.cleanup != nil {
			w.cleanup()
		}
	})
}
func launchWorker(ctx context.Context, cmd *exec.Cmd, socket, sha string, coldLimit time.Duration) (*workerProcess, error) {
	if !shaPattern.MatchString(sha) || coldLimit <= 0 || coldLimit > startupTimeout {
		return nil, errWorker
	}
	w := &workerProcess{cmd: cmd, sha: sha, done: make(chan struct{})}
	started := time.Now()
	// Discard all child output, including native stderr; never inherit a debug sink.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if cmd.Start() != nil {
		return nil, errWorker
	}
	go func() { _ = cmd.Wait(); close(w.done) }()
	startup, cancel := context.WithTimeout(ctx, coldLimit)
	defer cancel()
	for {
		select {
		case <-w.done:
			w.Close()
			return nil, errWorker
		default:
		}
		if startup.Err() != nil {
			w.Close()
			return nil, errWorker
		}
		// Capture the path inode before connecting and verify it again after ready.
		// Never infer ownership from a socket found only during later cleanup.
		identity, identityErr := os.Lstat(socket)
		var c net.Conn
		e := errWorker
		if identityErr == nil && identity.Mode()&os.ModeSocket != 0 {
			c, e = (&net.Dialer{}).DialContext(startup, "unix", socket)
		}
		if e == nil {
			w.conn = c
			deadline, _ := startup.Deadline()
			_ = c.SetDeadline(deadline)
			cancelRead := context.AfterFunc(startup, func() { _ = c.Close() })
			w.dimension, e = readReady(c, sha)
			cancelRead()
			current, statErr := os.Lstat(socket)
			if e != nil || startup.Err() != nil || statErr != nil || !os.SameFile(identity, current) {
				w.Close()
				return nil, errWorker
			}
			w.socketIdentity = identity
			_ = c.SetDeadline(time.Time{})
			w.coldStart = time.Since(started)
			return w, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-startup.Done():
			timer.Stop()
			w.Close()
			return nil, errWorker
		case <-w.done:
			timer.Stop()
			w.Close()
			return nil, errWorker
		case <-timer.C:
		}
	}
}
func startWorker(ctx context.Context, model, sha string) (*workerProcess, error) {
	executable, e := os.Executable()
	if e != nil {
		return nil, errWorker
	}
	return startWorkerBinary(ctx, executable, model, sha)
}
func startWorkerBinary(ctx context.Context, executable, model, sha string) (*workerProcess, error) {
	d, e := os.MkdirTemp("", "speaker-lab-")
	if e != nil {
		return nil, errWorker
	}
	if os.Chmod(d, 0700) != nil {
		os.Remove(d)
		return nil, errWorker
	}
	socket := filepath.Join(d, "worker.sock")
	cmd := exec.Command(executable, "worker", "--socket", socket, "--model", model, "--sha256", sha)
	// The model process receives no service tokens, database keys, or caller configuration.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	w, e := launchWorker(ctx, cmd, socket, sha, startupTimeout)
	if e != nil {
		// No verified inode is available: remove only an empty directory.
		cleanupWorkerDirectory(d, socket, nil)
		return nil, e
	}
	w.cleanup = func() { cleanupWorkerDirectory(d, socket, w.socketIdentity) }
	return w, nil
}

// Remove only the verified socket inode. An unknown or replaced entry keeps
// the private directory non-empty and is deliberately left for its owner.
func cleanupWorkerDirectory(dir, socket string, identity os.FileInfo) {
	if identity != nil {
		if current, e := os.Lstat(socket); e == nil && os.SameFile(identity, current) {
			_ = os.Remove(socket)
		}
	}
	_ = os.Remove(dir)
}
func (w *workerProcess) request(ctx context.Context, h requestHeader, pcm []byte) ([]float32, int64, error) {
	if !validRequest(h, w.sha) || len(pcm) != h.PCMBytes {
		w.Close()
		return nil, 0, errProtocol
	}
	deadline := time.Now().Add(requestTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if ctx.Err() != nil {
		w.Close()
		return nil, 0, errWorker
	}
	if w.conn.SetDeadline(deadline) != nil {
		w.Close()
		return nil, 0, errWorker
	}
	cancelIO := context.AfterFunc(ctx, func() { _ = w.conn.Close(); _ = w.cmd.Process.Kill() })
	defer cancelIO()
	if writeRequest(w.conn, h, pcm) != nil {
		w.Close()
		return nil, 0, errWorker
	}
	v, us, e := readResponse(w.conn, h, w.dimension)
	if e != nil || ctx.Err() != nil {
		w.Close()
		if e == errUnavailable {
			return nil, 0, errUnavailable
		}
		return nil, 0, errWorker
	}
	return v, us, nil
}

type probeResult struct {
	ModelSHA          string  `json:"model_sha256"`
	Dimension         int     `json:"dimension"`
	Samples           int     `json:"samples"`
	InferenceMS       float64 `json:"inference_ms"`
	ColdStartMS       float64 `json:"cold_start_ms"`
	OverlapAssessment string  `json:"overlap_assessment"`
	QualityEvidence   string  `json:"quality_evidence"`
}

func runProbe(ctx context.Context, model, sha, wav string) (probeResult, error) {
	var result probeResult
	// WAV parsing is bounded Go code, never Native ReadWave.
	if !noSymlinks(wav) {
		return result, errProtocol
	}
	before, e := os.Lstat(wav)
	if e != nil || !before.Mode().IsRegular() {
		return result, errProtocol
	}
	fd, e := syscall.Open(wav, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return result, errProtocol
	}
	f := os.NewFile(uintptr(fd), "fixture")
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !os.SameFile(before, st) {
		f.Close()
		return result, errProtocol
	}
	pcm, e := readWAV(f)
	f.Close()
	if e != nil {
		return result, e
	}
	w, e := startWorker(ctx, model, sha)
	if e != nil {
		return result, e
	}
	defer w.Close()
	h := requestHeader{Version: 1, ID: 1, Session: "probe", Scope: "fixture", Generation: 1, Start: 0, End: uint64(len(pcm) / 2), ModelSHA: sha, SampleRate: sampleRate, PCMBytes: len(pcm)}
	_, us, e := w.request(ctx, h, pcm)
	if e != nil {
		return result, e
	}
	return probeResult{ModelSHA: sha, Dimension: w.dimension, Samples: len(pcm) / 2, InferenceMS: float64(us) / 1000, ColdStartMS: float64(w.coldStart.Microseconds()) / 1000, OverlapAssessment: "unavailable", QualityEvidence: "fixture-only"}, nil
}
