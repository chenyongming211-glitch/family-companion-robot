//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These adapters exist only in the test binary. No normal CLI loads an SDK.
type productSDKReport struct {
	prototypeReport
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Profile            string `json:"profile"`
	Planned            int    `json:"planned_cases"`
	Processed          int    `json:"processed_cases"`
	Attempts           int    `json:"sdk_attempts"`
	Success            int    `json:"sdk_vectors"`
	NoVector           int    `json:"sdk_no_vector"`
	EnrollmentNoVector int    `json:"enrollment_sdk_no_vector"`
	NativeNoVector     int    `json:"native_sdk_no_vector"`
	SDK                string `json:"sdk"`
	SDKVersion         string `json:"sdk_version"`
	SourceRevision     string `json:"source_revision"`
}

func productAnalyze(ctx context.Context, m backendManifest, a map[string]auditCase, cs []embeddedCase, dim int) (productSDKReport, error) {
	started := time.Now()
	r := productSDKReport{Status: "not_run", Planned: len(m.Cases)}
	r.evaluationMetadata = newEvaluationReport("").evaluationMetadata
	r.SourceKind, r.SingleSpeakerBasis, r.Condition = m.SourceKind, m.SingleSpeakerBasis, m.Condition
	r.CaseCount, r.Dimension, r.SampleRate = len(m.Cases), dim, sampleRate
	r.Population, r.Frontend, r.OfficialFeatureEquivalence = "seen_development_regression", "official_sdk_bound_bundle", "upstream_frontend_control_not_product_equivalence"
	r.HouseholdAssessment, r.EndToEndLatencyAssessment = "unavailable", "unavailable"
	r.FusionBasis = "qualified_segment_equal_weight_normalized_vectors"
	if m.SourceKind == "synthetic_tts" {
		r.Population = "synthetic_engineering_fixture"
	} else if m.SourceKind == "public_human" {
		r.OpenSetAssessment = "limited_public_unknown_cohort"
	} else {
		return r, errProtocol
	}
	if len(cs) != len(m.Cases) || dim < 1 || dim > maxDimension || m.SampleRate != sampleRate || (m.Condition != "device03" && m.Condition != "device06") {
		return r, errProtocol
	}
	bySpeaker := map[string][]evidenceLabeledVector{}
	summaries := map[string]backendEnrollment{}
	roles := map[string]backendRoleCount{}
	ids := map[string]bool{}
	testN := 0
	for i, c := range m.Cases {
		if e := ctx.Err(); e != nil {
			return r, e
		}
		ac, ok := a[c.SHA]
		group, person := backendSpeaker(c.Speaker)
		limit := map[string]int{"train": 10, "validation": 50, "test": 5, "validation_unknown": 5, "test_unknown": 20, "cohort": 30}[group]
		roleOK := c.Role == group+"_enrollment" || c.Role == group+"_query"
		if group == "validation_unknown" || group == "test_unknown" || group == "cohort" {
			roleOK = c.Role == group
		}
		if !ok || ac.Samples != c.Samples || c.Samples < 1 || c.Samples > maxSamples || ac.SpeechSamples < 0 || ac.SpeechSamples > c.Samples || person < 1 || person > limit || !roleOK || c.ID == "" || ids[c.ID] || cs[i].ID != c.ID || cs[i].Speaker != c.Speaker {
			return r, errProtocol
		}
		ids[c.ID] = true
		if group == "test" {
			testN = max(testN, person)
		}
		energy := cs[i].quality == "qualified" || cs[i].quality == "sdk_no_vector"
		switch cs[i].quality {
		case "qualified":
			if !validVector(cs[i].vector, dim) {
				return r, errProtocol
			}
			r.QualifiedCases++
		case "sdk_no_vector":
			if len(cs[i].vector) != 0 {
				return r, errProtocol
			}
			r.NoVector++
		case "short":
			if len(cs[i].vector) != 0 {
				return r, errProtocol
			}
			r.RejectedShort++
		case "silence":
			if len(cs[i].vector) != 0 {
				return r, errProtocol
			}
			r.RejectedSilence++
		default:
			return r, errProtocol
		}
		r.AudioSeconds += float64(c.Samples) / sampleRate
		role := roles[c.Role]
		role.Role = c.Role
		role.Total++
		vad := ac.SpeechSamples >= 16000
		if energy {
			role.EnergyQualified++
		}
		if vad {
			role.VADQualified++
		}
		if energy && vad {
			role.BothQualified++
		}
		roles[c.Role] = role
		if strings.HasSuffix(c.Role, "_enrollment") {
			if !energy || !vad {
				return r, errProtocol
			}
			if cs[i].quality == "sdk_no_vector" {
				r.EnrollmentNoVector++
			} else {
				bySpeaker[c.Speaker] = append(bySpeaker[c.Speaker], evidenceLabeledVector{Speaker: c.Speaker, ID: c.ID, Vector: cs[i].vector})
			}
			s := summaries[c.Speaker]
			s.Speaker = c.Speaker
			s.Cases++
			s.RawSamples += c.Samples
			s.SpeechSamples += ac.SpeechSamples
			summaries[c.Speaker] = s
		}
	}
	r.Processed = len(cs)
	for _, role := range roles {
		r.Roles = append(r.Roles, role)
	}
	sort.Slice(r.Roles, func(i, j int) bool { return r.Roles[i].Role < r.Roles[j].Role })
	if r.EnrollmentNoVector > 0 {
		r.Reason = "enrollment_sdk_no_vector"
		return r, nil
	}
	if testN < 1 || testN > 5 {
		return r, errProtocol
	}
	for group, n := range map[string]int{"validation": 50, "test": testN} {
		for i := 1; i <= n; i++ {
			if len(bySpeaker[fmt.Sprintf("%s_%d", group, i)]) == 0 {
				return r, errProtocol
			}
		}
	}
	var speakers []string
	for s := range bySpeaker {
		speakers = append(speakers, s)
	}
	sort.Slice(speakers, func(i, j int) bool { return backendSpeakerLess(speakers[i], speakers[j]) })
	for _, s := range speakers {
		r.Enrollment = append(r.Enrollment, summaries[s])
	}
	method, e := analyzePrototypeMethod(ctx, m, cs, dim, speakers, bySpeaker, testN, "B_single_template")
	if e != nil {
		return r, e
	}
	r.Methods = []prototypeMethod{method}
	r.Status = "evaluated"
	r.TotalMS = backendElapsed(started)
	return r, nil
}
func productDecoder(noVector *bool) workerResponseDecoder {
	return func(rd io.Reader, h requestHeader, dim int) ([]float32, int64, error) {
		v, us, e := readResponse(rd, h, dim)
		if e == errUnavailable {
			*noVector = true
			return nil, 0, nil
		}
		return v, us, e
	}
}

func TestProductSDKCommonBParityDenominators(t *testing.T) {
	for _, dim := range []int{128, 192, 256} {
		m, a, cs := evidenceStudyFixture()
		for i := range cs {
			v := make([]float32, dim)
			copy(v, cs[i].vector)
			clear(cs[i].vector)
			cs[i].vector = v
		}
		t.Cleanup(func() {
			for _, c := range cs {
				clear(c.vector)
			}
		})
		old, e := productReferenceB(context.Background(), m, cs, dim)
		if e != nil {
			t.Fatal(e)
		}
		snapshot := snapshotNativeStudyVectors(t, cs)
		r, e := productAnalyze(context.Background(), m, a, cs, dim)
		if e != nil || r.Status != "evaluated" || len(r.Methods) != 1 || r.Methods[0].Name != "B_single_template" || !reflect.DeepEqual(old.Pools, r.Methods[0].Pools) {
			t.Fatal("complete B pool/ROC/calibration parity", e)
		}
		if !backendFinite(r.TotalMS) || r.TotalMS <= 0 || r.TotalMS < r.Methods[0].TotalMS {
			t.Fatal("complete analysis duration is finite and contains method duration")
		}
		assertNativeStudyVectors(t, "common B borrows", cs, snapshot)
		for i, c := range m.Cases {
			if c.Role == "validation_query" || c.Role == "test_unknown" {
				clear(cs[i].vector)
				cs[i].vector = nil
				cs[i].quality = "sdk_no_vector"
			}
		}
		r, e = productAnalyze(context.Background(), m, a, cs, dim)
		if e != nil || r.Status != "evaluated" || r.NoVector != 140 {
			t.Fatal("no-vector coverage", e, r.NoVector)
		}
		for pi, p := range r.Methods[0].Pools {
			c := p.Rules[0].Counts
			if pi < 2 && (c.Total != 100 || c.Qualified != 0 || c.Rejected != 100 || c.UnknownTotal != 10) {
				t.Fatal("known no-vector denominator", c)
			}
			if pi == 2 && (c.Total != 8 || c.UnknownTotal != 40 || c.UnknownQualified != 0 || c.UnknownRejected != 40) {
				t.Fatal("unknown no-vector denominator", c)
			}
		}
	}
}

func TestProductSDKEnrollmentNotRunAndNumeric(t *testing.T) {
	for _, quality := range []string{"sdk_no_vector", "nan", "zero", "dimension", "short", "silence", "bad-role"} {
		m, a, cs := evidenceStudyFixture()
		t.Cleanup(func() {
			for _, c := range cs {
				clear(c.vector)
			}
		})
		switch quality {
		case "sdk_no_vector", "short", "silence":
			clear(cs[0].vector)
			cs[0].vector = nil
			cs[0].quality = quality
		case "nan":
			cs[0].vector[0] = float32(math.NaN())
		case "zero":
			clear(cs[0].vector)
		case "dimension":
			cs[0].vector = cs[0].vector[:2]
		case "bad-role":
			m.Cases[0].Role = "test_query"
		}
		r, e := productAnalyze(context.Background(), m, a, cs, 128)
		if quality == "sdk_no_vector" {
			if e != nil || r.Status != "not_run" || r.Reason != "enrollment_sdk_no_vector" || r.EnrollmentNoVector != 1 || len(r.Methods) != 0 || r.CaseCount != len(cs) {
				t.Fatal("missing enrollment must invalidate whole condition", e)
			}
		} else if e == nil {
			t.Fatal("invalid input accepted", quality)
		}
	}
}

func TestProductSDKValidationOnlyAndPrivacy(t *testing.T) {
	m, a, cs := evidenceStudyFixture()
	defer func() {
		for _, c := range cs {
			clear(c.vector)
		}
	}()
	r, e := productAnalyze(context.Background(), m, a, cs, 128)
	if e != nil || len(r.Methods) != 1 {
		t.Fatal("missing B")
	}
	for i, c := range m.Cases {
		if strings.HasPrefix(c.Role, "train_") || c.Role == "test_query" || c.Role == "test_unknown" {
			clear(cs[i].vector)
			cs[i].vector[127] = 1
		}
	}
	x, e := productAnalyze(context.Background(), m, a, cs, 128)
	if e != nil {
		t.Fatal(e)
	}
	for i, p := range r.Methods[0].Pools {
		if !reflect.DeepEqual(p.Rules[0].Calibration, x.Methods[0].Pools[i].Rules[0].Calibration) {
			t.Fatal("non-validation changed calibration")
		}
	}
	if r.SourceKind != "synthetic_tts" || r.Population != "synthetic_engineering_fixture" || r.OpenSetAssessment != "unavailable" || r.Frontend == "sherpa_native" || r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled || r.NewBlindTest || r.QueryAccumulation {
		t.Fatal("metadata overclaim")
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(b)
	for _, secret := range []string{"/private/", `"vector"`, `"embedding"`, `"path"`, `"text_sha256"`, `"recording_sha256"`, `P2_two_prototypes`} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("private data in aggregate")
		}
	}
}

func TestProductSDKDecoderNoVectorVersusFatal(t *testing.T) {
	h := requestHeader{Version: 1, ID: 1, Session: "sdk_study", Scope: "fixture", Generation: 1, Start: 0, End: 32000, ModelSHA: testSHA, SampleRate: sampleRate, PCMBytes: 64000}
	for _, code := range []string{"unavailable", "ok", "broken"} {
		var b bytes.Buffer
		v := []float32{1, 0}
		if code != "ok" {
			v = nil
		}
		rh := responseFor(h, 2, 100, code)
		if code == "unavailable" {
			rh.VectorBytes = 0
		}
		if e := writeFrame(&b, rh, nil); e != nil {
			t.Fatal(e)
		}
		if code == "ok" {
			b.Reset()
			if e := writeResponse(&b, rh, v); e != nil {
				t.Fatal(e)
			}
		}
		missing := false
		got, _, e := productDecoder(&missing)(&b, h, 2)
		defer clear(got)
		if code == "unavailable" {
			if e != nil || !missing || got != nil {
				t.Fatal("legitimate unavailable must be out-of-band, no fabricated vector")
			}
		} else if code == "ok" {
			if e != nil || missing || !validVector(got, 2) {
				t.Fatal("valid vector")
			}
		} else if e == nil || missing {
			t.Fatal("protocol failure became rejection")
		}
	}
	missing := false
	if _, _, e := productDecoder(&missing)(io.LimitReader(strings.NewReader("broken"), 6), h, 2); e == nil || missing {
		t.Fatal("truncation became rejection")
	}
}

// Only this explicitly configured entry point can load an actual SDK.
func TestRealProductSDKStudyOptIn(t *testing.T) {
	c, on, e := productConfig(os.Getenv)
	if e != nil {
		t.Fatal("invalid private SDK study configuration")
	}
	if !on {
		t.Skip("explicit SDK study opt-in not set")
	}
	// Reserve evidence before model loading; repeated invocation cannot replace it.
	out, e := productOutput(c.Output)
	if e != nil {
		t.Fatal("private aggregate output unavailable")
	}
	defer out.Close()
	r := productSDKReport{Status: "not_run", Reason: "configuration_failed"}
	defer func() {
		if json.NewEncoder(out).Encode(r) != nil || out.Sync() != nil {
			t.Error("private aggregate write failed")
		}
	}()
	bundle, e := productBundle(c.Bundle, c.SHA, c.Worker)
	if e != nil {
		t.Fatal("invalid authenticated SDK bundle")
	}
	var m backendManifest
	var a map[string]auditCase
	var digest string
	if c.Mode == "public" {
		m, a, digest, e = loadBackendManifest(c.Manifest)
	} else {
		var f fixtureManifest
		f, digest, e = loadManifest(c.Manifest)
		if e == nil {
			m = backendManifest{SourceKind: "synthetic_tts", SampleRate: sampleRate, Condition: "device03", SingleSpeakerBasis: "synthetic_fixture_declared_single_speaker"}
			for _, v := range f.Cases {
				m.Cases = append(m.Cases, backendCase{ID: v.ID, Speaker: v.Speaker, Path: v.Path, Samples: v.Samples, SHA: v.SHA, TextSHA: v.TextSHA})
			}
		}
	}
	if e != nil {
		t.Fatal("invalid private study manifest")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3600*time.Second)
	defer cancel()
	var worker *workerProcess
	socket := ""
	x, extractErr := productExtract(ctx, m, c.SHA, func(ctx context.Context, _, _ string) (*workerProcess, error) {
		var e error
		worker, e = productStart(ctx, c)
		if e == nil && worker.dimension != bundle.Metadata.Dimension {
			worker.Close()
			return nil, errWorker
		}
		if worker != nil {
			socket = worker.conn.LocalAddr().String()
		}
		return worker, e
	}, nil)
	defer func() {
		for _, v := range x.cases {
			clear(v.vector)
		}
	}()
	r = x.report
	if extractErr == nil {
		if c.Mode == "synthetic" {
			if len(x.cases) != len(m.Cases) || x.report.Processed != len(m.Cases) {
				t.Fatal("incomplete synthetic extraction")
			}
			var unused []embeddedCase
			m, a, unused = evidenceStudyFixture()
			for _, v := range unused {
				clear(v.vector)
			}
			logical := make([]embeddedCase, len(m.Cases))
			defer func() {
				for _, v := range logical {
					clear(v.vector)
				}
			}()
			m.SingleSpeakerBasis = "synthetic_fixture_declared_single_speaker"
			for i, v := range m.Cases {
				src := x.cases[i%len(x.cases)]
				logical[i] = embeddedCase{fixtureCase: fixtureCase{ID: v.ID, Speaker: v.Speaker}, quality: src.quality, vector: append([]float32(nil), src.vector...)}
				m.Cases[i].Samples = src.Samples
				a[v.SHA] = auditCase{Samples: src.Samples, SpeechSamples: min(src.Samples, 16000)}
			}
			if x.report.NoVector == 0 {
				known, unknown := false, false
				for i, v := range m.Cases {
					if !known && v.Role == "validation_query" {
						clear(logical[i].vector)
						logical[i].vector = nil
						logical[i].quality = "silence"
						known = true
					}
					if !unknown && v.Role == "test_unknown" {
						clear(logical[i].vector)
						logical[i].vector = nil
						logical[i].quality = "short"
						unknown = true
					}
				}
			}
			borrowed := snapshotNativeStudyVectors(t, logical)
			r, e = productAnalyze(ctx, m, a, logical, x.report.Dimension)
			assertNativeStudyVectors(t, "SDK B analysis", logical, borrowed)
			if e != nil {
				t.Fatal("synthetic SDK common policy failed")
			}
			if x.report.NoVector == 0 {
				want, e := productReferenceB(ctx, m, logical, x.report.Dimension)
				if e != nil || r.Status != "evaluated" || len(r.Methods) != 1 || !reflect.DeepEqual(want.Pools, r.Methods[0].Pools) {
					t.Fatal("native complete B pool parity")
				}
				if r.RejectedShort != 1 || r.RejectedSilence != 1 {
					t.Fatal("native inclusive quality coverage")
				}
			} else if r.Status != "not_run" || r.EnrollmentNoVector == 0 || len(r.Methods) != 0 {
				t.Fatal("native missing enrollment was filtered")
			}
		} else {
			r, e = productAnalyze(ctx, m, a, x.cases, x.report.Dimension)
			if e != nil {
				r.Status = "not_run"
				r.Reason = "analysis_failed"
				r.Methods = nil
			}
		}
	}
	// Actual native work and reused logical rows are deliberately separate.
	r.Planned = x.report.Planned
	r.Processed = x.report.Processed
	r.Attempts = x.report.Attempts
	r.Success = x.report.Success
	r.NativeNoVector = x.report.NoVector
	r.Native = x.report.Native
	r.Wall = x.report.Wall
	r.ColdStartMS = x.report.ColdStartMS
	r.WorkerCPUms = x.report.WorkerCPUms
	r.WorkerMaxRSSKiB = x.report.WorkerMaxRSSKiB
	r.Profile = bundle.Profile
	r.SDK = bundle.Metadata.SDK
	r.SDKVersion = bundle.Metadata.Version
	r.SourceRevision = bundle.Metadata.Revision
	r.Frontend = bundle.Metadata.Frontend
	r.ModelSHA = c.SHA
	r.ManifestSHA = digest
	r.Provider = "cpu"
	r.Threads = 1
	if c.Mode == "public" {
		r.VADModelSHA = sileroSHA
		r.VADConfig = fixedVADConfig()
		for _, v := range m.Audits {
			r.AuditSHAs = append(r.AuditSHAs, v.SHA)
		}
	}
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) != nil {
		t.Fatal("private usage unavailable")
	}
	r.ParentMaxRSSKiB = usage.Maxrss
	if worker != nil {
		if worker.cmd.ProcessState == nil {
			t.Fatal("SDK child not reaped")
		}
		select {
		case <-worker.done:
		default:
			t.Fatal("SDK child alive")
		}
		if _, e := os.Lstat(socket); socket != "" && !os.IsNotExist(e) {
			t.Fatal("SDK socket retained")
		}
	}
	if c.Mode == "synthetic" {
		if bundle.Profile != "vosk" && (extractErr != nil || r.Status != "evaluated" || r.Success != r.Attempts || r.Success == 0) {
			t.Fatal("positive SDK synthetic coverage missing")
		}
		if bundle.Profile == "vosk" && (r.Status != "not_run" || len(r.Methods) != 0 || (extractErr == nil && x.report.NoVector == 0)) {
			t.Fatal("Vosk expected genuine missing enrollment or bounded failure")
		}
		if r.HumanAcceptance || r.ProductionAcceptance || r.FeatureEnabled || r.NewBlindTest || r.QueryAccumulation || r.OpenSetAssessment != "unavailable" {
			t.Fatal("synthetic acceptance overclaim")
		}
		if worker != nil && (r.ColdStartMS <= 0 || r.WorkerCPUms <= 0 || r.WorkerMaxRSSKiB <= 0 || r.ParentMaxRSSKiB <= 0) {
			t.Fatal("native usage evidence missing")
		}
		// Exercise independent B output ownership with actual native vectors.
		if x.report.Success > 0 {
			before := snapshotNativeStudyVectors(t, x.cases)
			for _, v := range x.cases {
				if v.quality != "qualified" {
					continue
				}
				owned, e := fuseEnrollment([][]float32{v.vector, v.vector}, x.report.Dimension)
				if e != nil {
					clear(owned)
					t.Fatal("native B ownership")
				}
				clear(owned)
				assertNativeStudyVectors(t, "cleared owned B output", x.cases, before)
				break
			}
		}
	}
	for _, v := range x.cases {
		clear(v.vector)
		for _, f := range v.vector {
			if f != 0 {
				t.Fatal("native owned vector retained")
			}
		}
	}
	t.Logf("profile=%s status=%s native_attempts=%d vectors=%d native_no_vector=%d processed=%d planned=%d logical_rows=%d (research only)", r.Profile, r.Status, r.Attempts, r.Success, x.report.NoVector, r.Processed, r.Planned, r.CaseCount)
}

func productReferenceB(ctx context.Context, m backendManifest, cs []embeddedCase, dim int) (prototypeMethod, error) {
	by := map[string][]evidenceLabeledVector{}
	n := 0
	for i, c := range m.Cases {
		g, p := backendSpeaker(c.Speaker)
		if g == "test" {
			n = max(n, p)
		}
		if strings.HasSuffix(c.Role, "_enrollment") {
			by[c.Speaker] = append(by[c.Speaker], evidenceLabeledVector{ID: c.ID, Speaker: c.Speaker, Vector: cs[i].vector})
		}
	}
	var speakers []string
	for s := range by {
		speakers = append(speakers, s)
	}
	sort.Slice(speakers, func(i, j int) bool { return backendSpeakerLess(speakers[i], speakers[j]) })
	return analyzePrototypeMethod(ctx, m, cs, dim, speakers, by, n, "B_single_template")
}

type productSDKBundle struct {
	Schema  int    `json:"schema_version"`
	Profile string `json:"profile"`
	Assets  []struct {
		Role string `json:"role"`
		Path string `json:"path"`
		SHA  string `json:"sha256"`
	} `json:"assets"`
	Metadata productSDKMetadata `json:"metadata"`
}
type productSDKMetadata struct {
	SDK       string `json:"sdk"`
	Version   string `json:"sdk_version"`
	Dimension int    `json:"dimension"`
	Revision  string `json:"source_revision"`
	Frontend  string `json:"frontend"`
}

func productBundle(path, digest, worker string) (productSDKBundle, error) {
	var r productSDKBundle
	b, e := boundedFile(path, 65536)
	if e != nil {
		return r, errProtocol
	}
	defer clear(b)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != digest || strictHeader(b, &r) != nil || r.Schema != 1 {
		return r, errProtocol
	}
	// Resolve after decode, and authenticate report metadata against the same
	// source lock as Python. An unsuccessful child must not publish arbitrary
	// caller-supplied frontend text. Python additionally authenticates all assets.
	dim, known := map[string]int{"speechbrain_ecapa": 192, "wespeaker_cn34_ref": 256, "vosk": 128}[r.Profile]
	if !known || r.Metadata.Dimension != dim {
		return r, errProtocol
	}
	lb, e := boundedFile(filepath.Join(filepath.Dir(worker), "products.lock.json"), 1<<20)
	if e != nil {
		return r, errProtocol
	}
	defer clear(lb)
	var lock struct {
		Profiles map[string]struct {
			Metadata productSDKMetadata `json:"metadata"`
		} `json:"profiles"`
	}
	if json.Unmarshal(lb, &lock) != nil || lock.Profiles[r.Profile].Metadata != r.Metadata {
		return r, errProtocol
	}
	return r, nil
}

func TestProductSDKBundleMetadataPrivacy(t *testing.T) {
	lock, e := os.ReadFile("products.lock.json")
	if e != nil {
		t.Fatal("source lock unavailable")
	}
	defer clear(lock)
	var pinned struct {
		Profiles map[string]struct {
			Metadata json.RawMessage `json:"metadata"`
		} `json:"profiles"`
	}
	if json.Unmarshal(lock, &pinned) != nil {
		t.Fatal("lock schema")
	}
	var metadata map[string]any
	if json.Unmarshal(pinned.Profiles["speechbrain_ecapa"].Metadata, &metadata) != nil {
		t.Fatal("metadata")
	}
	base := map[string]any{"schema_version": 1, "profile": "speechbrain_ecapa", "assets": []any{}, "metadata": metadata}
	d := t.TempDir()
	path := filepath.Join(d, "bundle.json")
	wd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	worker := filepath.Join(wd, "product_sdk_worker.py")
	check := func(want bool) {
		b, e := json.Marshal(base)
		if e != nil {
			t.Fatal(e)
		}
		defer clear(b)
		if os.WriteFile(path, b, 0600) != nil {
			t.Fatal("fixture write")
		}
		_, e = productBundle(path, backendSHA(b), worker)
		if (e == nil) != want {
			t.Fatal("untrusted metadata accepted or pinned bundle rejected")
		}
	}
	check(true)
	metadata["frontend"] = "/private/secret"
	check(false)
	metadata["frontend"] = "SpeakerRecognition.encode_batch(normalize=False)"
	base["profile"] = "unknown"
	metadata["dimension"] = 0
	check(false)
}

type productSDKConfig struct{ Mode, Python, Worker, Bundle, SHA, Manifest, Output string }

func productConfig(get func(string) string) (productSDKConfig, bool, error) {
	values := make([]string, 7)
	set := 0
	for i, k := range []string{"MODE", "PYTHON", "WORKER", "BUNDLE", "SHA256", "MANIFEST", "OUTPUT"} {
		values[i] = get("SPEAKER_LAB_TEST_PRODUCT_" + k)
		if values[i] != "" {
			set++
		}
	}
	if set == 0 {
		return productSDKConfig{}, false, nil
	}
	c := productSDKConfig{values[0], values[1], values[2], values[3], values[4], values[5], values[6]}
	if set != 7 || (c.Mode != "synthetic" && c.Mode != "public") || !shaPattern.MatchString(c.SHA) {
		return c, true, errProtocol
	}
	for _, p := range []string{c.Python, c.Worker, c.Bundle, c.Manifest, c.Output} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, true, errProtocol
		}
	}
	return c, true, nil
}
func productWrite(path string, r productSDKReport) error {
	f, e := productOutput(path)
	if e != nil {
		return errProtocol
	}
	defer f.Close()
	if json.NewEncoder(f).Encode(r) != nil {
		return errProtocol
	}
	return f.Sync()
}
func productOutput(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !noSymlinks(filepath.Dir(path)) {
		return nil, errProtocol
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, errProtocol
	}
	if f.Chmod(0600) != nil {
		f.Close()
		return nil, errProtocol
	}
	return f, nil
}

type productSDKExtraction struct {
	cases  []embeddedCase
	report productSDKReport
}

func productExtract(ctx context.Context, m backendManifest, sha string, start evaluationWorkerStart, observe func([]byte, []float32)) (productSDKExtraction, error) {
	x := productSDKExtraction{report: productSDKReport{Status: "not_run", Reason: "worker_failed", Planned: len(m.Cases)}}
	x.report.evaluationMetadata = newEvaluationReport(sha).evaluationMetadata
	x.report.SourceKind = m.SourceKind
	x.report.SingleSpeakerBasis = m.SingleSpeakerBasis
	x.report.Condition = m.Condition
	x.report.CaseCount = len(m.Cases)
	x.report.Population = "seen_development_regression"
	if m.SourceKind == "synthetic_tts" {
		x.report.Population = "synthetic_engineering_fixture"
	} else if m.SourceKind == "public_human" {
		x.report.OpenSetAssessment = "limited_public_unknown_cohort"
	}
	x.report.HouseholdAssessment = "unavailable"
	x.report.EndToEndLatencyAssessment = "unavailable"
	w, e := start(ctx, "", sha)
	if e != nil {
		return x, errWorker
	}
	x.report.Dimension = w.dimension
	x.report.ColdStartMS = float64(w.coldStart.Microseconds()) / 1000
	var native, wall []float64
	finish := func() {
		w.Close()
		x.report.Native = summarizeTimes(native)
		x.report.Wall = summarizeTimes(wall)
		if w.cmd.ProcessState != nil {
			if u, ok := w.cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
				x.report.WorkerCPUms = float64(u.Utime.Sec+u.Stime.Sec)*1000 + float64(u.Utime.Usec+u.Stime.Usec)/1000
				x.report.WorkerMaxRSSKiB = u.Maxrss
			}
		}
	}
	defer finish()
	fail := func() (productSDKExtraction, error) {
		for _, c := range x.cases {
			clear(c.vector)
		}
		x.cases = nil
		finish()
		return x, errWorker
	}
	var end uint64
	for _, c := range m.Cases {
		if ctx.Err() != nil {
			return fail()
		}
		fc := fixtureCase{ID: c.ID, Speaker: c.Speaker, Path: c.Path, Samples: c.Samples, SHA: c.SHA, TextSHA: c.TextSHA}
		pcm, e := loadFixture(fc)
		if e != nil {
			return fail()
		}
		if observe != nil {
			observe(pcm, nil)
		}
		item := embeddedCase{fixtureCase: fc, quality: qualityReason(pcm)}
		if item.quality == "short" {
			x.report.RejectedShort++
		} else if item.quality == "silence" {
			x.report.RejectedSilence++
		} else {
			x.report.Attempts++
			h := requestHeader{Version: 1, ID: uint64(x.report.Attempts), Session: "sdk_study", Scope: "fixture", Generation: 1, Start: end, End: end + uint64(c.Samples), ModelSHA: sha, SampleRate: sampleRate, PCMBytes: len(pcm)}
			end = h.End
			missing := false
			began := time.Now()
			v, us, e := w.requestWithDecoder(ctx, h, pcm, productDecoder(&missing))
			wall = append(wall, backendElapsed(began))
			clear(pcm)
			if observe != nil && v != nil {
				observe(nil, v)
			}
			if e != nil {
				clear(v)
				return fail()
			}
			if missing {
				item.quality = "sdk_no_vector"
				x.report.NoVector++
			} else if !validVector(v, w.dimension) {
				clear(v)
				return fail()
			} else {
				item.vector = v
				x.report.Success++
				native = append(native, float64(us)/1000)
			}
		}
		clear(pcm)
		x.cases = append(x.cases, item)
		x.report.Processed++
	}
	x.report.Reason = ""
	finish()
	return x, nil
}

// The Python SDK worker reverse-connects; launchWorker is a dialer. This
// test-only starter reuses listenPrivate/workerProcess and the same budgets.
func productStartCommand(ctx context.Context, cmd *exec.Cmd, sha string) (*workerProcess, string, error) {
	if !shaPattern.MatchString(sha) {
		return nil, "", errWorker
	}
	d, e := os.MkdirTemp("", "speaker-product-")
	if e != nil {
		return nil, "", errWorker
	}
	if os.Chmod(d, 0700) != nil {
		os.Remove(d)
		return nil, "", errWorker
	}
	socket := filepath.Join(d, "worker.sock")
	l, e := listenPrivate(socket)
	if e != nil {
		os.Remove(d)
		return nil, socket, errWorker
	}
	cmd.Args = append(cmd.Args, "--socket", socket)
	w := &workerProcess{cmd: cmd, done: make(chan struct{}), socketIdentity: l.identity, sha: sha}
	w.cleanup = func() { l.Close(); cleanupWorkerDirectory(d, socket, l.identity) }
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	started := time.Now()
	if cmd.Start() != nil {
		w.cleanup()
		return nil, socket, errWorker
	}
	go func() { _ = cmd.Wait(); close(w.done) }()
	startup, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	_ = l.SetDeadline(started.Add(startupTimeout))
	stop := context.AfterFunc(startup, func() { l.Close(); _ = cmd.Process.Kill() })
	defer stop()
	// Child exit must also unblock Accept, without leaking a waiter on success.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-w.done:
			l.Close()
		case <-watchDone:
		}
	}()
	c, e := l.AcceptUnix()
	if e != nil {
		w.Close()
		return nil, socket, errWorker
	}
	w.conn = c
	validPeer := false
	raw, e := c.SyscallConn()
	if e == nil {
		_ = raw.Control(func(fd uintptr) {
			u, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
			validPeer = e == nil && u.Pid == int32(cmd.Process.Pid) && u.Uid == uint32(os.Geteuid())
		})
	}
	if !validPeer {
		w.Close()
		return nil, socket, errWorker
	}
	deadline, _ := startup.Deadline()
	_ = c.SetDeadline(deadline)
	stopRead := context.AfterFunc(startup, func() { c.Close() })
	w.dimension, e = readReady(c, w.sha)
	stopRead()
	if e != nil || startup.Err() != nil {
		w.Close()
		return nil, socket, errWorker
	}
	l.Close()
	_ = c.SetDeadline(time.Time{})
	w.coldStart = time.Since(started)
	return w, socket, nil
}

func productStart(ctx context.Context, c productSDKConfig) (*workerProcess, error) {
	// A venv's interpreter symlink is legitimate. Bundle/worker/assets retain
	// their canonical no-symlink requirements; the worker authenticates assets.
	if st, e := os.Stat(c.Python); e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		return nil, errWorker
	}
	if !noSymlinks(c.Worker) || !noSymlinks(c.Bundle) {
		return nil, errWorker
	}
	cmd := exec.Command(c.Python, c.Worker, "--bundle", c.Bundle, "--sha256", c.SHA)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "PYTHONNOUSERSITE=1", "PYTHONDONTWRITEBYTECODE=1", "OMP_NUM_THREADS=1", "MKL_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1", "HF_HUB_OFFLINE=1", "HF_DATASETS_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "HF_HUB_DISABLE_TELEMETRY=1", "DO_NOT_TRACK=1"}
	w, _, e := productStartCommand(ctx, cmd, c.SHA)
	return w, e
}

func TestProductSDKExplicitConfigAndExclusiveOutput(t *testing.T) {
	keys := []string{"MODE", "PYTHON", "WORKER", "BUNDLE", "SHA256", "MANIFEST", "OUTPUT"}
	values := []string{"synthetic", "/private/python", "/private/worker.py", "/private/bundle.json", testSHA, "/private/manifest.json", filepath.Join(t.TempDir(), "aggregate.json")}
	get := func(k string) string {
		for i, s := range keys {
			if k == "SPEAKER_LAB_TEST_PRODUCT_"+s {
				return values[i]
			}
		}
		return ""
	}
	if _, enabled, e := productConfig(func(string) string { return "" }); enabled || e != nil {
		t.Fatal("default must skip")
	}
	for i := range keys {
		saved := values[i]
		values[i] = ""
		if _, _, e := productConfig(get); e == nil {
			t.Fatal("partial opt-in accepted", keys[i])
		}
		values[i] = saved
	}
	c, on, e := productConfig(get)
	if e != nil || !on || c.Mode != "synthetic" {
		t.Fatal("complete explicit config")
	}
	for _, bad := range []string{"human", "public_human", ""} {
		values[0] = bad
		if _, _, e := productConfig(get); e == nil {
			t.Fatal("bad mode")
		}
	}
	values[0] = "synthetic"
	values[1] = "relative-secret"
	if _, _, e := productConfig(get); e == nil {
		t.Fatal("relative interpreter")
	}
	values[1] = "/private/python"
	r := productSDKReport{Status: "not_run", Reason: "worker_failed"}
	if e := productWrite(c.Output, r); e != nil {
		t.Fatal("aggregate output")
	}
	b, e := os.ReadFile(c.Output)
	if e != nil || !bytes.Contains(b, []byte(`"not_run"`)) {
		t.Fatal("missing aggregate")
	}
	defer clear(b)
	st, e := os.Stat(c.Output)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private permissions")
	}
	if e := productWrite(c.Output, r); e == nil {
		t.Fatal("overwrote prior evidence")
	}
}

func TestProductSDKExtractionIPCAccountingCleanup(t *testing.T) {
	for _, mode := range []string{"success", "no-vector", "fatal", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			dir, e := os.MkdirTemp("", "product-ipc-")
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "w.sock")
			marker := filepath.Join(dir, "entered")
			m := backendManifest{SampleRate: sampleRate}
			for i := 0; i < 4; i++ {
				samples := 32000
				b := wavBytes(samples)
				if i != 3 {
					copy(b[44:], sessionPCM(samples))
				}
				path := filepath.Join(dir, fmt.Sprintf("%d.wav", i))
				if e := os.WriteFile(path, b, 0600); e != nil {
					t.Fatal(e)
				}
				m.Cases = append(m.Cases, backendCase{ID: fmt.Sprintf("c%d", i), Speaker: "test_1", Samples: samples, Path: path, SHA: backendSHA(b), Role: "test_query"})
				clear(b)
			}
			var w *workerProcess
			start := func(ctx context.Context, _, sha string) (*workerProcess, error) {
				cmd := exec.Command(os.Args[0], "-test.run=^TestProductSDKIPCHelper$")
				cmd.Env = []string{"SPEAKER_PRODUCT_HELPER=" + mode, "SPEAKER_PRODUCT_SOCKET=" + socket, "SPEAKER_PRODUCT_MARKER=" + marker}
				var e error
				w, e = launchWorker(ctx, cmd, socket, sha, time.Second)
				if e == nil {
					w.cleanup = func() { cleanupWorkerDirectory(dir, socket, w.socketIdentity) }
				}
				return w, e
			}
			watch := make(chan struct{})
			if mode == "cancel" {
				go func() {
					defer close(watch)
					ticker := time.NewTicker(time.Millisecond * 5)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							if _, e := os.Stat(marker); e == nil {
								cancel()
								return
							}
						}
					}
				}()
			}
			var pcms [][]byte
			var vectors [][]float32
			x, e := productExtract(ctx, m, testSHA, start, func(p []byte, v []float32) {
				if p != nil {
					pcms = append(pcms, p)
				}
				if v != nil {
					vectors = append(vectors, v)
				}
			})
			if mode == "cancel" {
				cancel()
				<-watch
			}
			if mode == "success" || mode == "no-vector" {
				if e != nil || x.report.Processed != 4 || x.report.Attempts != 3 || len(x.cases) != 4 || x.report.RejectedSilence != 1 {
					t.Fatal("inclusive extraction", e, x.report.Processed, x.report.Attempts)
				}
				want := 3
				if mode == "no-vector" {
					want = 2
					if x.report.NoVector != 1 || x.cases[1].quality != "sdk_no_vector" {
						t.Fatal("no-vector accounting")
					}
				}
				if x.report.Success != want || x.report.Native.WarmCount != want-1 || x.report.Wall.WarmCount != 2 {
					t.Fatal("timing population", x.report.Success)
				}
			} else if e == nil || x.report.Status != "not_run" || len(x.cases) != 0 || x.report.Processed != 1 || x.report.Attempts != 2 || x.report.NoVector != 0 || x.report.Success != 1 || len(x.report.Methods) != 0 {
				t.Fatal("infra failure became rejection or complete pool", e, x.report.Processed, x.report.Attempts)
			}
			for _, p := range pcms {
				for _, b := range p {
					if b != 0 {
						t.Fatal("owned PCM retained")
					}
				}
			}
			for _, c := range x.cases {
				clear(c.vector)
			}
			for _, v := range vectors {
				for _, f := range v {
					if f != 0 {
						t.Fatal("owned vector retained")
					}
				}
			}
			if w == nil || w.cmd.ProcessState == nil {
				t.Fatal("child not reaped")
			}
			select {
			case <-w.done:
			default:
				t.Fatal("child alive")
			}
			if _, e := os.Stat(socket); !os.IsNotExist(e) {
				t.Fatal("socket retained")
			}
		})
	}
}

func TestProductSDKIPCHelper(t *testing.T) {
	mode := os.Getenv("SPEAKER_PRODUCT_HELPER")
	if mode == "" {
		t.Skip("private helper")
	}
	l, e := listenPrivate(os.Getenv("SPEAKER_PRODUCT_SOCKET"))
	if e != nil {
		os.Exit(2)
	}
	defer l.Close()
	c, e := l.Accept()
	if e != nil {
		os.Exit(3)
	}
	defer c.Close()
	if writeReady(c, testSHA, 2) != nil {
		os.Exit(4)
	}
	for n := 0; ; n++ {
		h, p, e := readRequest(c, testSHA)
		if e != nil {
			return
		}
		clear(p)
		if n == 1 {
			if mode == "fatal" {
				return
			}
			if mode == "timeout" || mode == "cancel" {
				if os.WriteFile(os.Getenv("SPEAKER_PRODUCT_MARKER"), []byte("entered"), 0600) != nil {
					os.Exit(5)
				}
				select {}
			}
			if mode == "no-vector" {
				rh := responseFor(h, 2, 100, "unavailable")
				if writeResponse(c, rh, nil) != nil {
					return
				}
				continue
			}
		}
		if writeResponse(c, responseFor(h, 2, 100, "ok"), []float32{1, 0}) != nil {
			return
		}
	}
}

func TestProductSDKReverseStarter(t *testing.T) {
	for _, mode := range []string{"ready", "bad-ready", "exit", "cancel-accept", "cancel-ready"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			d, e := os.MkdirTemp("", "product-reverse-test-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(d)
			marker := filepath.Join(d, "entered")
			cmd := exec.Command(os.Args[0], "-test.run=^TestProductSDKReverseHelper$", "--")
			cmd.Env = []string{"PRODUCT_REVERSE_MODE=" + mode, "PRODUCT_REVERSE_MARKER=" + marker}
			done := make(chan struct{})
			if strings.HasPrefix(mode, "cancel-") {
				go func() {
					defer close(done)
					ticker := time.NewTicker(time.Millisecond * 5)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							if _, e := os.Stat(marker); e == nil {
								cancel()
								return
							}
						}
					}
				}()
			}
			w, socket, e := productStartCommand(ctx, cmd, testSHA)
			if strings.HasPrefix(mode, "cancel-") {
				cancel()
				<-done
			}
			if mode == "ready" {
				if e != nil || w == nil || w.dimension != 2 {
					t.Fatal("reverse startup", e)
				}
				w.Close()
				if w.cmd.ProcessState == nil {
					t.Fatal("success child not reaped")
				}
			} else if e == nil || w != nil {
				if w != nil {
					w.Close()
				}
				t.Fatal("invalid reverse startup accepted")
			}
			if cmd.ProcessState == nil {
				t.Fatal("failed startup child not reaped")
			}
			if _, e := os.Lstat(filepath.Dir(socket)); !os.IsNotExist(e) {
				t.Fatal("private socket directory retained")
			}
		})
	}
}

func TestProductSDKReverseHelper(t *testing.T) {
	mode := os.Getenv("PRODUCT_REVERSE_MODE")
	if mode == "" {
		t.Skip("private reverse helper")
	}
	if mode == "exit" {
		return
	}
	var socket string
	for i, v := range os.Args {
		if v == "--socket" && i+1 < len(os.Args) {
			socket = os.Args[i+1]
		}
	}
	if mode == "cancel-accept" {
		if os.WriteFile(os.Getenv("PRODUCT_REVERSE_MARKER"), []byte("entered"), 0600) != nil {
			os.Exit(2)
		}
		select {}
	}
	c, e := net.Dial("unix", socket)
	if e != nil {
		os.Exit(3)
	}
	defer c.Close()
	if mode == "cancel-ready" {
		if os.WriteFile(os.Getenv("PRODUCT_REVERSE_MARKER"), []byte("entered"), 0600) != nil {
			os.Exit(4)
		}
		select {}
	}
	if mode == "bad-ready" {
		_, _ = c.Write([]byte("broken"))
		return
	}
	if writeReady(c, testSHA, 2) != nil {
		os.Exit(5)
	}
	_, _ = io.Copy(io.Discard, c)
}
