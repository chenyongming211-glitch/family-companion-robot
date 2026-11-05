//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPrototypeCLIExplicitPrivateErrors(t *testing.T) {
	sha := fmt.Sprintf("%064d", 1)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"prototype-study"}, "usage"},
		{[]string{"prototype-study", "--model", "relative-secret", "--sha256", sha, "--manifest", "/missing-secret"}, "usage"},
		{[]string{"prototype-study", "--model", "/missing-secret", "--sha256", sha, "--manifest", "relative-secret"}, "usage"},
		{[]string{"prototype-study", "--model", "/missing-secret", "--sha256", sha, "--manifest", "/missing-secret", "--source-kind", "synthetic_tts"}, "usage"},
		{[]string{"prototype-study", "--model", "/missing-secret", "--sha256", sha, "--manifest", "/missing-secret"}, "prototype_study_failed"},
	} {
		var out, err bytes.Buffer
		code := runCLI(context.Background(), tc.args, &out, &err)
		if code != 1 || out.Len() != 0 || err.String() != "{\"error\":\""+tc.want+"\"}\n" {
			t.Fatalf("private CLI contract: code=%d stdout=%q stderr=%q want=%s", code, out.String(), err.String(), tc.want)
		}
	}
}

func prototypeNumericFixture() (backendManifest, map[string]auditCase, []embeddedCase) {
	m, a, cs := evidenceStudyFixture()
	original := len(cs)
	for i := 0; i < original; i++ {
		v := make([]float32, 256)
		copy(v, cs[i].vector)
		cs[i].vector = v
	}
	for i := 0; i < original; i++ {
		c := m.Cases[i]
		if !strings.HasSuffix(c.Role, "_enrollment") {
			continue
		}
		c.ID += "_second_mode"
		c.SHA = c.ID
		v := make([]float32, 256)
		for j, x := range cs[i].vector {
			if x != 0 {
				v[j+128] = x
			}
		}
		m.Cases = append(m.Cases, c)
		a[c.SHA] = a[m.Cases[i].SHA]
		cs = append(cs, embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, vector: v, quality: "qualified"})
	}
	return m, a, cs
}
func TestPrototypeStudyTwoMethodsInclusivePoolsAndPrivacy(t *testing.T) {
	m, a, cs := prototypeNumericFixture()
	for i, c := range m.Cases {
		if c.Role == "validation_query" || c.Role == "test_unknown" {
			cs[i].quality = "silence"
			cs[i].vector = nil
			break
		}
	}
	for i, c := range m.Cases {
		if c.Role == "test_unknown" {
			cs[i].quality = "short"
			cs[i].vector = nil
			break
		}
	}
	r, e := analyzePrototypeStudy(context.Background(), m, a, cs, 256)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Methods) != 2 || r.Methods[0].Name != "B_single_template" || r.Methods[1].Name != "P2_two_prototypes" || len(r.Enrollment) != 57 {
		t.Fatal("method/enrollment layout")
	}
	if r.SourceKind != "synthetic_tts" || r.Population != "synthetic_engineering_fixture" || r.OpenSetAssessment != "unavailable" || r.QueryAccumulation || r.NewBlindTest || r.FeatureEnabled || r.HumanAcceptance || r.ProductionAcceptance || r.Frontend != "sherpa_native" || r.OfficialFeatureEquivalence != "unverified" {
		t.Fatal("synthetic metadata")
	}
	if r.RejectedShort != 1 || r.RejectedSilence != 1 || r.CaseCount != len(cs) || r.QualifiedCases != len(cs)-2 {
		t.Fatal("shared extraction counts")
	}
	for mi, method := range r.Methods {
		if len(method.Pools) != 3 || len(method.Prototypes) != 57 {
			t.Fatal("pools/coverage")
		}
		for _, s := range method.Prototypes {
			if mi == 1 && (s.Status != "two_prototypes" || !reflect.DeepEqual(s.ClusterSizes, []int{2, 2})) {
				t.Fatal("two mode pipeline not covered", s)
			}
		}
		for pi, p := range method.Pools {
			if len(p.Rules) != 1 || p.Rules[0].Name != "unknown_target_0.01" {
				t.Fatal("unregistered rule")
			}
			c := p.Rules[0].Counts
			if pi < 2 && (p.Top1.Total != 100 || p.Top1.Qualified != 99 || c.Total != 100 || c.UnknownTotal != 10 || c.Total-c.Qualified != 1) {
				t.Fatal("validation denominator", c)
			}
			if pi == 2 && (p.Candidates != 4 || c.Total != 8 || c.UnknownTotal != 40 || c.UnknownTotal-c.UnknownQualified != 1 || c.UnknownRejected+c.UnknownAccepted != 40) {
				t.Fatal("test unknown denominator", c)
			}
		}
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"/private/", "secret.wav", `"vector"`, "recording_sha", "text_sha", "source_group"} {
		if bytes.Contains(b, []byte(s)) {
			t.Fatal("private data exposed", s)
		}
	}
	if cs[0].vector[0] != 1 {
		t.Fatal("borrowed vector mutated")
	}
	// Public metadata is distinct, without changing input source kind via a CLI flag.
	m.SourceKind = "public_human"
	r, e = analyzePrototypeStudy(context.Background(), m, a, cs, 256)
	if e != nil || r.Population != "seen_development_regression" || r.OpenSetAssessment != "limited_public_unknown_cohort" {
		t.Fatal("public metadata", e)
	}
}
func TestPrototypeStudyOriginalBackendR0Parity(t *testing.T) {
	m, qa := backendFixture(t)
	cs := backendSynthetic(m)
	a := map[string]auditCase{}
	for _, c := range qa.Cases {
		a[c.SHA] = c
	}
	// Three non-unit enrollment vectors in deliberately unsorted manifest ID order.
	for i, c := range m.Cases {
		if strings.HasSuffix(c.Role, "_enrollment") {
			cs[i].vector[(i+1)%128] += .1234567
			cs[i].vector[(i+2)%128] += .7654321
		}
	}
	for i, c := range m.Cases {
		if c.Role == "validation_query" || c.Role == "validation_unknown" || c.Role == "test_query" || c.Role == "test_unknown" {
			cs[i].quality = "silence"
			cs[i].vector = nil
			if i%2 == 0 {
				continue
			}
			break
		}
	}
	old, _, e := analyzeBackend(m, a, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	r, e := analyzePrototypeStudy(context.Background(), m, a, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	for i, p := range old[0].Pools {
		now := r.Methods[0].Pools[i]
		if !reflect.DeepEqual(p.Top1, now.Top1) || !reflect.DeepEqual(p.PairDiagnostic, now.PairDiagnostic) || !reflect.DeepEqual(p.Rules[0], now.Rules[0]) || p.GroupCount != now.GroupCount {
			t.Fatal("original R0 changed", i)
		}
		if !reflect.DeepEqual(now, r.Methods[1].Pools[i]) {
			t.Fatal("fallback did not preserve exact B", i)
		}
	}
	if len(r.Enrollment) != 64 || len(r.Methods[1].Prototypes) != 64 {
		t.Fatal("cohort leaked into enrollment summaries")
	}
}
func TestPrototypeStudyCalibrationIndependentOfTrainingAndTest(t *testing.T) {
	m, a, cs := prototypeNumericFixture()
	r, e := analyzePrototypeStudy(context.Background(), m, a, cs, 256)
	if e != nil {
		t.Fatal(e)
	}
	for i, c := range m.Cases {
		if c.Role == "train_query" || c.Role == "train_enrollment" || c.Role == "test_query" || c.Role == "test_unknown" {
			v := make([]float32, 256)
			v[255] = 1
			cs[i].vector = v
		}
	}
	changed, e := analyzePrototypeStudy(context.Background(), m, a, cs, 256)
	if e != nil {
		t.Fatal(e)
	}
	for mi, method := range r.Methods {
		for pi, p := range method.Pools {
			if !reflect.DeepEqual(p.Rules[0].Calibration, changed.Methods[mi].Pools[pi].Rules[0].Calibration) {
				t.Fatal("test/train changed validation calibration")
			}
		}
		for j, s := range method.Prototypes {
			if !strings.HasPrefix(s.Speaker, "train_") && !reflect.DeepEqual(s, changed.Methods[mi].Prototypes[j]) {
				t.Fatal("query/train data changed evaluation prototypes")
			}
		}
	}
}
func TestPrototypeStudyValidationCancellationAndPublicLoader(t *testing.T) {
	m, a, cs := prototypeNumericFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := analyzePrototypeStudy(ctx, m, a, cs, 256); e != context.Canceled {
		t.Fatal("cancel ignored", e)
	}
	for _, mutate := range []func(*backendManifest, map[string]auditCase, []embeddedCase){
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) { m.SourceKind = "unknown" },
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) { m.Cases[0].Role = "test_unknown" },
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) {
			m.Cases[0].Speaker = "validation_999"
		},
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) {
			cs[0].quality = "silence"
			cs[0].vector = nil
		},
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) { cs[0].vector = []float32{1} },
		func(m *backendManifest, a map[string]auditCase, cs []embeddedCase) { delete(a, m.Cases[0].SHA) },
	} {
		mm, aa, cc := prototypeNumericFixture()
		mutate(&mm, aa, cc)
		if _, e := analyzePrototypeStudy(context.Background(), mm, aa, cc, 256); e != errProtocol {
			t.Fatal("bad input accepted", e)
		}
	}
	if cs[0].vector[0] != 1 {
		t.Fatal("error cleared borrowed vector")
	}
	path := filepath.Join(t.TempDir(), "synthetic-secret.json")
	b, _ := json.Marshal(m)
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	calls := 0
	_, e := runPrototypeStudyWithStart(context.Background(), "/unused", testSHA, path, func(context.Context, string, string) (*workerProcess, error) { calls++; return nil, errWorker })
	if e != errProtocol || calls != 0 {
		t.Fatal("strict public loader bypassed")
	}
}
func TestPrototypeStudyExtractionOnceAndWorkerCleanup(t *testing.T) {
	m, qa := backendFixture(t)
	for i := range m.Cases {
		c := &m.Cases[i]
		b := wavBytes(c.Samples)
		copy(b[44:], sessionPCM(c.Samples))
		binary.LittleEndian.PutUint16(b[44:], uint16(i+1))
		if e := os.WriteFile(c.Path, b, 0600); e != nil {
			t.Fatal(e)
		}
		c.SHA = backendSHA(b)
		c.RecordingSHA = c.SHA
		c.SourceGroupSHA = c.SHA
		c.TextSHA = c.SHA
		if c.Role == "cohort" {
			c.TextSHA = ""
		}
		qa.Cases[i].SHA = c.SHA
	}
	path := writeBackend(t, m, qa)
	for _, mode := range []string{"success", "hang"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			socketDir := filepath.Join(dir, "worker")
			if e := os.Mkdir(socketDir, 0700); e != nil {
				t.Fatal(e)
			}
			socket := filepath.Join(socketDir, "worker.sock")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			starts := 0
			reached := make(chan struct{})
			watchDone := make(chan struct{})
			marker := filepath.Join(dir, "second-request-started")
			var worker *workerProcess
			start := func(ctx context.Context, _, _ string) (*workerProcess, error) {
				starts++
				helper := "^TestLargeExtractionHelper$"
				if mode == "hang" {
					helper = "^TestPrototypeExtractionHelper$"
				}
				cmd := exec.Command(os.Args[0], "-test.run="+helper)
				cmd.Env = append(os.Environ(), "SPEAKER_LAB_LARGE_HELPER="+mode, "SPEAKER_LAB_HELPER_SOCKET="+socket, "SPEAKER_LAB_PROTOTYPE_HELPER="+mode, "SPEAKER_LAB_PROTOTYPE_MARKER="+marker)
				w, e := launchWorker(ctx, cmd, socket, testSHA, time.Second)
				if e == nil {
					worker = w
					if mode == "hang" {
						go func() {
							defer close(watchDone)
							ticker := time.NewTicker(5 * time.Millisecond)
							defer ticker.Stop()
							for {
								select {
								case <-ctx.Done():
									return
								case <-ticker.C:
									if _, e := os.Lstat(marker); e == nil {
										close(reached)
										cancel()
										return
									}
								}
							}
						}()
					}
					w.cleanup = func() { cleanupWorkerDirectory(socketDir, socket, w.socketIdentity) }
				}
				return w, e
			}
			r, e := runPrototypeStudyWithStart(ctx, "/unused", testSHA, path, start)
			if mode == "success" && (e != nil || starts != 1 || r.QualifiedCases != len(m.Cases) || r.Native.WarmCount+1 != len(m.Cases) || len(r.Methods) != 2) {
				t.Fatal("extraction not shared", e, starts, r.QualifiedCases, r.Native.WarmCount)
			}
			if mode == "hang" {
				cancel()
				if worker != nil {
					<-watchDone
				}
				select {
				case <-reached:
				default:
					t.Fatal("cancellation did not reach second extraction")
				}
				if e == nil || len(r.Methods) != 0 || ctx.Err() != context.Canceled {
					t.Fatal("cancelled extraction leaked result")
				}
			}
			if starts != 1 || worker == nil || worker.cmd.ProcessState == nil {
				t.Fatalf("worker not reaped: starts=%d ready=%t err=%v context=%v", starts, worker != nil, e, ctx.Err())
			}
			if _, e := os.Lstat(socketDir); !os.IsNotExist(e) {
				t.Fatal("worker files retained")
			}
		})
	}
}

type prototypeCancelAfterChecks struct {
	context.Context
	checks, after int
}

func (c *prototypeCancelAfterChecks) Err() error {
	c.checks++
	if c.checks >= c.after {
		return context.Canceled
	}
	return nil
}
func TestPrototypeStudyCancellationThroughoutAnalysis(t *testing.T) {
	m, a, cs := prototypeNumericFixture()
	for _, after := range []int{len(cs) + 3, len(cs) + 57 + 5, len(cs) + 57 + len(cs) + 3} {
		ctx := &prototypeCancelAfterChecks{Context: context.Background(), after: after}
		if _, e := analyzePrototypeStudy(ctx, m, a, cs, 256); e != context.Canceled {
			t.Fatal("analysis phase ignored cancellation", after, e)
		}
		if cs[0].vector[0] != 1 {
			t.Fatal("cancellation cleared borrowed input")
		}
	}
}

// Only the test child writes this state marker, immediately before blocking in
// the second extraction. It contains no PCM/vector and proves the hang was hit.
type prototypeHangingExtractor struct {
	calls  int
	marker string
}

func (x *prototypeHangingExtractor) Dimension() int { return 2 }
func (x *prototypeHangingExtractor) Close()         {}
func (x *prototypeHangingExtractor) Extract([]byte) ([]float32, error) {
	x.calls++
	if x.calls == 2 {
		if e := os.WriteFile(x.marker, []byte("second_request_started"), 0600); e != nil {
			return nil, errWorker
		}
		time.Sleep(10 * time.Second)
	}
	return []float32{1, 0}, nil
}
func TestPrototypeExtractionHelper(t *testing.T) {
	if os.Getenv("SPEAKER_LAB_PROTOTYPE_HELPER") != "hang" {
		return
	}
	l, e := listenPrivate(os.Getenv("SPEAKER_LAB_HELPER_SOCKET"))
	if e != nil {
		os.Exit(2)
	}
	defer l.Close()
	c, e := l.Accept()
	if e != nil {
		os.Exit(3)
	}
	defer c.Close()
	serveConnection(context.Background(), c, &prototypeHangingExtractor{marker: os.Getenv("SPEAKER_LAB_PROTOTYPE_MARKER")}, testSHA)
}
