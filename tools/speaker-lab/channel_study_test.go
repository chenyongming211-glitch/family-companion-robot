//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Numerical fixtures contain no audio and make no human-performance claim.
func channelFixture() (channelManifest, map[string]auditCase) {
	m := channelManifest{Schema: 1, SourceKind: "public_human", Condition: "device06", Usage: "seen_development_regression", DiagnosticOnly: true, Status: "qualified", SourceID: "3dspeaker_device06_channel_20261105", SourceURL: "https://3dspeaker.github.io/", LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/", Basis: "source_annotations", SampleRate: 16000, SelectionSHA: channelSelectionSHA}
	m.Audits = []backendAuditRef{}
	a := map[string]auditCase{}
	for gi, g := range []string{"development_known", "development_strangers", "diagnostic_known", "diagnostic_strangers"} {
		n := 5
		if gi == 3 {
			n = 20
		}
		for p := 1; p <= n; p++ {
			person := channelPerson{Speaker: fmt.Sprintf("%s_%d", g, p), Queries: 5}
			if gi == 0 {
				person.EnrollmentSegments = []int{8, 8, 8, 7, 7}[p-1]
				person.Queries = []int{7, 7, 5, 2, 4}[p-1]
			}
			if gi == 2 {
				person.EnrollmentSegments = []int{8, 7, 7, 7, 7}[p-1]
				person.Queries = []int{4, 8, 4, 5, 4}[p-1]
			}
			for j := 0; j < person.EnrollmentSegments+person.Queries; j++ {
				role := g + "_query"
				samples := 40000
				speech := 20000
				if j < person.EnrollmentSegments {
					role = g + "_enrollment"
					samples = 320000 / person.EnrollmentSegments
					if j == person.EnrollmentSegments-1 {
						samples = 320000 - (person.EnrollmentSegments-1)*samples
					}
					person.EnrollmentSamples += samples
					person.EnrollmentSpeechSamples += speech
				}
				id := fmt.Sprintf("channel_%04d", len(m.Cases)+1)
				c := backendCase{ID: id, Speaker: person.Speaker, Role: role, Path: "/private/" + id + ".wav", Samples: samples, SHA: channelDigest([]byte(id + "wav")), RecordingSHA: channelDigest([]byte(id + "record")), SourceGroupSHA: channelDigest([]byte(id + "group")), TextSHA: channelDigest([]byte(id + "text"))}
				m.Cases = append(m.Cases, c)
				a[c.SHA] = auditCase{Samples: samples, SpeechSamples: speech, RMS: .1}
			}
			m.People = append(m.People, person)
		}
	}
	return m, a
}
func TestChannelStrictMetadata(t *testing.T) {
	for _, name := range []string{"valid", "flag", "source", "condition", "id", "speaker", "role", "samples", "wave_duplicate", "record_duplicate", "group_duplicate", "person_text_duplicate", "cross_phase_text", "enroll_query_text", "audit_missing", "audit_samples", "low_enroll", "short_speech", "summary", "prefix", "missing_person"} {
		t.Run(name, func(t *testing.T) {
			m, a := channelFixture()
			switch name {
			case "flag":
				m.FamilyAcceptance = true
			case "source":
				m.SourceKind = "synthetic_tts"
			case "condition":
				m.Condition = "device03"
			case "id":
				m.Cases[0].ID = "channel_9999"
			case "speaker":
				m.Cases[0].Speaker = "development_known_6"
			case "role":
				m.Cases[0].Role = "diagnostic_known_enrollment"
			case "samples":
				m.Cases[0].Samples = 31999
			case "wave_duplicate":
				m.Cases[1].SHA = m.Cases[0].SHA
			case "record_duplicate":
				m.Cases[1].RecordingSHA = m.Cases[0].RecordingSHA
			case "group_duplicate":
				m.Cases[1].SourceGroupSHA = m.Cases[0].SourceGroupSHA
			case "person_text_duplicate":
				m.Cases[1].TextSHA = m.Cases[0].TextSHA
			case "cross_phase_text":
				m.Cases[88].TextSHA = m.Cases[0].TextSHA
			case "enroll_query_text":
				m.Cases[8].TextSHA = m.Cases[0].TextSHA
			case "audit_missing":
				delete(a, m.Cases[0].SHA)
			case "audit_samples":
				q := a[m.Cases[0].SHA]
				q.Samples--
				a[m.Cases[0].SHA] = q
			case "low_enroll":
				q := a[m.Cases[0].SHA]
				q.RMS = .001
				a[m.Cases[0].SHA] = q
			case "short_speech":
				q := a[m.Cases[0].SHA]
				q.SpeechSamples = 15999
				a[m.Cases[0].SHA] = q
			case "summary":
				m.People[0].Queries++
			case "prefix":
				m.Cases[0].Samples = 128000
				q := a[m.Cases[0].SHA]
				q.Samples = 128000
				a[m.Cases[0].SHA] = q
			case "missing_person":
				m.People = m.People[1:]
			}
			e := validateChannel(m, a, func(backendCase) (string, error) { return "qualified", nil })
			if (e == nil) != (name == "valid") {
				t.Fatal("strict metadata validation result")
			}
		})
	}
}
func TestChannelStrictJSON(t *testing.T) {
	m, _ := channelFixture()
	b, _ := json.Marshal(m)
	if _, e := decodeChannel(b); e != nil {
		t.Fatal("round trip")
	}
	for _, bad := range [][]byte{bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":false`), 1), bytes.Replace(b, []byte(`"samples":40000`), []byte(`"samples":40000,"extra":false`), 1), bytes.Replace(b, []byte(`"queries":7`), []byte(`"queries":7,"queries":7`), 1), bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":null`), 1)} {
		if _, e := decodeChannel(bad); e == nil {
			t.Fatal("non strict JSON accepted")
		}
	}
}
func TestChannelCheckpointStrictJSON(t *testing.T) {
	// Engineering metadata only; no scoring, extraction or measured resources.
	cp := newChannelCheckpoint(backendPolicy{Status: "calibrated", Threshold: .5, Target: .01, Validation: backendCounts{Total: 25, Rejected: 25, UnknownTotal: 25, UnknownRejected: 25}})
	cp.Resources = channelResources{Status: "complete", Cases: 88, NativeVectors: 82, RejectedSilence: 6, Dimension: 512, WorkerReaped: true, SocketRemoved: true, OwnedCleared: true, Native: timingSummary{WarmCount: 81}, Wall: timingSummary{WarmCount: 81}}
	b, e := json.Marshal(cp)
	if e != nil {
		t.Fatal("marshal checkpoint metadata")
	}
	t.Run("valid_zero", func(t *testing.T) {
		if restored, e := readChannelCheckpointBytes(b, channelDigest(b)); e != nil || !reflect.DeepEqual(restored, cp) {
			t.Fatal("numeric zero grid roundtrip")
		}
	})
	for _, first := range []string{"null", " null ", `"0"`, "false", "{}", "[]", "1e999"} {
		t.Run(first, func(t *testing.T) {
			bad := bytes.Replace(b, []byte(`"margin_grid":[0,`), []byte(`"margin_grid":[`+first+`,`), 1)
			if bytes.Equal(bad, b) {
				t.Fatal("margin grid mutation missing")
			}
			// Bind the mutated bytes so a hash mismatch cannot mask schema acceptance.
			if _, e := readChannelCheckpointBytes(bad, channelDigest(bad)); e == nil {
				t.Fatal("invalid primitive margin accepted")
			}
		})
	}
}
func channelNumeric(t *testing.T, phase string) (channelManifest, []embeddedCase) {
	t.Helper()
	m, _ := channelFixture()
	m = channelPhase(m, phase)
	var cs []embeddedCase
	for _, c := range m.Cases {
		_, n := channelSpeaker(c.Speaker)
		v := []float32{.11, .17, .23, .29, .31}
		v[(n-1)%5] += 1.3
		if strings.HasSuffix(c.Role, "_enrollment") {
			v[(n)%5] += .2
		}
		cs = append(cs, embeddedCase{fixtureCase: fixtureCase{ID: c.ID, Speaker: c.Speaker}, quality: "qualified", vector: v})
	}
	return m, cs
}
func TestChannelBMathOwnership(t *testing.T) {
	m, cs := channelNumeric(t, "calibration")
	before := make([][]uint32, len(cs))
	for i, c := range cs {
		for _, v := range c.vector {
			before[i] = append(before[i], math.Float32bits(v))
		}
	}
	k, u, clearScores, e := channelQueries(m, cs, 5, "calibration")
	if e != nil {
		t.Fatal("queries")
	}
	defer clearScores()
	bm := channelBackend(m, "calibration")
	scores := map[int][]float64{}
	templates := map[string][]float32{}
	defer func() {
		for _, v := range templates {
			clear(v)
		}
	}()
	for p := 1; p <= 5; p++ {
		s := fmt.Sprintf("validation_%d", p)
		var vs [][]float32
		for i, c := range bm.Cases {
			if c.Speaker == s && c.Role == "validation_enrollment" {
				vs = append(vs, cs[i].vector)
			}
		}
		templates[s], e = fuseEnrollment(vs, 5)
		if e != nil {
			t.Fatal("fusion")
		}
	}
	for i, c := range bm.Cases {
		if strings.HasSuffix(c.Role, "_enrollment") {
			continue
		}
		for p := 1; p <= 5; p++ {
			x, e := prototypeScore(cs[i].vector, [][]float32{templates[fmt.Sprintf("validation_%d", p)]}, 5)
			if e != nil {
				t.Fatal("score")
			}
			scores[i] = append(scores[i], x)
		}
	}
	wantK, wantU, _ := backendPoolQueries(bm, cs, scores, "validation", 5, 5)
	if !reflect.DeepEqual(k, wantK) || !reflect.DeepEqual(u, wantU) {
		t.Fatal("B scores diverged")
	}
	got := channelCalibrate(k, u)
	want := backendCalibrate(wantK, wantU, channelMargins(), .01)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("policy differs")
	}
	clearScores()
	for _, v := range templates {
		clear(v)
		for _, x := range v {
			if x != 0 {
				t.Fatal("fusion cleanup")
			}
		}
	}
	for _, q := range append(k, u...) {
		for _, x := range q.Scores {
			if x != 0 {
				t.Fatal("owned scores retained")
			}
		}
	}
	for i, c := range cs {
		for j, x := range c.vector {
			if math.Float32bits(x) != before[i][j] {
				t.Fatal("borrowed vector mutated")
			}
		}
	}
	for _, v := range [][]float32{{0, 0, 0, 0, 0}, {float32(math.NaN()), 0, 0, 0, 1}, {float32(math.Inf(1)), 0, 0, 0, 1}} {
		bad := append([]embeddedCase(nil), cs...)
		bad[0].vector = v
		if _, _, cleanup, e := channelQueries(m, bad, 5, "calibration"); e == nil {
			cleanup()
			t.Fatal("invalid vector accepted")
		}
	}
}
func TestChannelCountsAndPolicyBinding(t *testing.T) {
	m, cs := channelNumeric(t, "calibration")
	k, u, cleanup, e := channelQueries(m, cs, 5, "calibration")
	if e != nil {
		t.Fatal("queries")
	}
	defer cleanup()
	k[0].Qualified = false
	u[0].Qualified = false
	p := channelCalibrate(k, u)
	cp := newChannelCheckpoint(p)
	// Engineering metadata fixture only; no native extraction is claimed.
	cp.Resources = channelResources{Status: "complete", Cases: 88, NativeVectors: 82, RejectedSilence: 6, Dimension: 512, WorkerReaped: true, SocketRemoved: true, OwnedCleared: true, Native: timingSummary{WarmCount: 81}, Wall: timingSummary{WarmCount: 81}}
	b, _ := json.Marshal(cp)
	if restored, e := readChannelCheckpointBytes(b, channelDigest(b)); e != nil || !reflect.DeepEqual(restored, cp) {
		t.Fatal("checkpoint roundtrip")
	}
	for _, name := range []string{"hash", "base", "model", "manifest", "selection", "status", "margin", "target", "budget", "count", "flag", "resource_count", "resource_status", "resource_ownership", "resource_negative"} {
		q := cp
		digest := ""
		switch name {
		case "hash":
			digest = strings.Repeat("0", 64)
		case "base":
			q.Base = "wrong"
		case "model":
			q.Model = "wrong"
		case "manifest":
			q.Manifest = "wrong"
		case "selection":
			q.Selection = "wrong"
		case "status":
			q.Policy.Status = "not_run"
		case "margin":
			q.Policy.Margin = .02
		case "target":
			q.Policy.Target = .1
		case "budget":
			q.Policy.Budget = 1
		case "count":
			q.Policy.Validation.UnknownTotal--
		case "flag":
			q.DiagnosticOnly = false
		case "resource_count":
			q.Resources.NativeVectors--
		case "resource_status":
			q.Resources.Status = "partial_failure"
		case "resource_ownership":
			q.Resources.OwnedCleared = false
		case "resource_negative":
			q.Resources.TotalMS = -1
		}
		v, _ := json.Marshal(q)
		if digest == "" {
			digest = channelDigest(v)
		}
		if _, e := readChannelCheckpointBytes(v, digest); e == nil {
			t.Fatal("checkpoint accepted", name)
		}
	}
	r := channelRule(p, k, u)
	expected := backendRuleResult("B", p, k, u, 1, true)
	if r.Counts != expected.Counts || len(r.People) != 10 || r.Counts.Total != 25 || r.Counts.UnknownTotal != 25 {
		t.Fatal("denominators")
	}
	allK := append([]backendQuery(nil), k...)
	allU := append([]backendQuery(nil), u...)
	for i := range allK {
		allK[i].Qualified = false
	}
	for i := range allU {
		allU[i].Qualified = false
	}
	all := channelRule(p, allK, allU)
	if all.Counts.Total != 25 || all.Counts.UnknownTotal != 25 || all.Counts.Rejected != 25 || all.Counts.UnknownRejected != 25 || all.Counts.Qualified != 0 || all.Counts.UnknownQualified != 0 {
		t.Fatal("all-quality-rejected denominators")
	}
	p.Status = "not_run"
	r = channelRule(p, k, u)
	if r.Status != "not_run" || r.Counts.Correct != 0 || r.Counts.Rejected != 0 || r.Counts.UnknownRejected != 0 {
		t.Fatal("fabricated decisions")
	}
	s, _ := json.Marshal(r)
	if bytes.Contains(s, []byte("top1")) || bytes.Contains(s, []byte("score")) {
		t.Fatal("private or unused score output")
	}
	// Diagnostic inputs cannot enter the calibration API: phase split contains development only.
	full, _ := channelFixture()
	dev := channelPhase(full, "calibration")
	full.Cases[88].Speaker = "tampered"
	if !reflect.DeepEqual(dev, channelPhase(full, "calibration")) {
		t.Fatal("heldout label influenced development")
	}
}
func TestChannelConfigAndExclusiveFiles(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if _, enabled, e := channelConfigFrom(get); e != nil || enabled {
		t.Fatal("default skip")
	}
	env["SPEAKER_LAB_TEST_CHANNEL_PHASE"] = "calibration"
	if _, _, e := channelConfigFrom(get); e == nil {
		t.Fatal("partial config accepted")
	}

	// All eight phase fields are required; the diagnostic hash is an additional
	// requirement and is forbidden during calibration.
	for key, value := range map[string]string{"PHASE": "calibration", "MODEL": "/private/model.onnx", "SHA256": channelModelSHA, "BINARY": "/private/native-cli", "MANIFEST": "/private/channel-manifest.json", "MANIFEST_SHA256": channelManifestSHA, "POLICY": "/private/policy.json", "OUTPUT": "/private/output.json"} {
		env["SPEAKER_LAB_TEST_CHANNEL_"+key] = value
	}
	if _, enabled, e := channelConfigFrom(get); e != nil || !enabled {
		t.Fatal("complete calibration configuration")
	}
	for _, name := range []string{"bad_model", "bad_manifest", "bad_phase", "calibration_hash", "diagnostic_missing_hash", "relative", "same_output"} {
		copyEnv := map[string]string{}
		for k, v := range env {
			copyEnv[k] = v
		}
		switch name {
		case "bad_model":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_SHA256"] = strings.Repeat("0", 64)
		case "bad_manifest":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_MANIFEST_SHA256"] = strings.Repeat("0", 64)
		case "bad_phase":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_PHASE"] = "all"
		case "calibration_hash":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_POLICY_SHA256"] = channelModelSHA
		case "diagnostic_missing_hash":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_PHASE"] = "diagnostic"
		case "relative":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_MODEL"] = "relative"
		case "same_output":
			copyEnv["SPEAKER_LAB_TEST_CHANNEL_OUTPUT"] = copyEnv["SPEAKER_LAB_TEST_CHANNEL_POLICY"]
		}
		if _, _, e := channelConfigFrom(func(k string) string { return copyEnv[k] }); e == nil {
			t.Fatal("invalid configuration accepted", name)
		}
	}
	env["SPEAKER_LAB_TEST_CHANNEL_PHASE"] = "diagnostic"
	env["SPEAKER_LAB_TEST_CHANNEL_POLICY_SHA256"] = channelModelSHA
	if _, _, e := channelConfigFrom(get); e != nil {
		t.Fatal("complete diagnostic configuration")
	}
	d := t.TempDir()
	p := filepath.Join(d, "result.json")
	f, e := reserveChannel(p)
	if e != nil {
		t.Fatal("reserve")
	}
	defer f.Close()
	if _, e := reserveChannel(p); e == nil {
		t.Fatal("overwritten output")
	}
	st, _ := f.Stat()
	if st.Mode().Perm() != 0600 {
		t.Fatal("output mode")
	}
	sy := filepath.Join(d, "link")
	if e := os.Symlink(p, sy); e != nil {
		t.Fatal(e)
	}
	if _, e := reserveChannel(sy); e == nil {
		t.Fatal("symlink accepted")
	}
	if e := writeChannelJSON(f, map[string]string{"status": "not_run"}); e != nil {
		t.Fatal("write")
	}
}
func TestChannelStartFailure(t *testing.T) {
	starts := 0
	_, e := extractEvaluation(context.Background(), evaluationManifest{}, "unread", "unread", func(context.Context, string, string) (*workerProcess, error) { starts++; return nil, errWorker })
	if e == nil || starts != 1 {
		t.Fatal("starter error propagation")
	}
}

func TestChannelDiagnosticPolicyCannotRefit(t *testing.T) {
	m, cs := channelNumeric(t, "calibration")
	k, u, cleanup, e := channelQueries(m, cs, 5, "calibration")
	if e != nil {
		t.Fatal("development queries")
	}
	defer cleanup()
	cp := newChannelCheckpoint(channelCalibrate(k, u))
	first, e := channelPhasePolicy("diagnostic", cp, k, u)
	if e != nil || first != cp.Policy {
		t.Fatal("frozen policy changed")
	}
	for i := range k {
		k[i].Own = -1
		for j := range k[i].Scores {
			k[i].Scores[j] = -1
		}
	}
	for i := range u {
		for j := range u[i].Scores {
			u[i].Scores[j] = 1
		}
	}
	second, e := channelPhasePolicy("diagnostic", cp, k, u)
	if e != nil || second != first {
		t.Fatal("diagnostic vectors/labels changed policy")
	}
	cp.Policy.Status = "not_run"
	if _, e := channelPhasePolicy("diagnostic", cp, k, u); e == nil {
		t.Fatal("uncalibrated policy accepted")
	}
}
func TestChannelCancellationReapsInjectedChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var child *workerProcess
	_, e := extractEvaluation(ctx, evaluationManifest{Cases: []evaluationCase{{ID: "unread", Path: "/must-not-be-read"}}}, "unread", "unread", func(context.Context, string, string) (*workerProcess, error) {
		cmd := exec.Command("/bin/sleep", "30")
		if cmd.Start() != nil {
			return nil, errWorker
		}
		child = &workerProcess{cmd: cmd, done: make(chan struct{}), dimension: 5}
		go func() { _ = cmd.Wait(); close(child.done) }()
		cancel()
		return child, nil
	})
	if e == nil || child == nil || child.cmd.ProcessState == nil {
		t.Fatal("canceled extraction failed to reap child")
	}
	select {
	case <-child.done:
	default:
		t.Fatal("child remained live")
	}
}

func TestChannelBoundInputs(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "proof.json")
	data := []byte(`{"proof":"numeric-metadata"}`)
	if os.WriteFile(p, data, 0600) != nil {
		t.Fatal("write metadata")
	}
	b, e := channelBound(p, channelDigest(data), 1024)
	if e != nil || !bytes.Equal(b, data) {
		t.Fatal("bound bytes")
	}
	clear(b)
	if _, e := channelBound(p, strings.Repeat("0", 64), 1024); e == nil {
		t.Fatal("wrong hash accepted")
	}
	link := filepath.Join(d, "linked.json")
	if os.Symlink(p, link) != nil {
		t.Fatal("link")
	}
	if _, e := channelBound(link, channelDigest(data), 1024); e == nil {
		t.Fatal("symlink accepted")
	}
	if _, e := channelBound(p, channelDigest(data), 3); e == nil {
		t.Fatal("size bound accepted")
	}
}

// Source/audio qualification only. This opt-in never starts a worker or extracts
// any identity vector; the public score runner remains separately gated.
func TestChannelFrozenInputsOptIn(t *testing.T) {
	path := os.Getenv("SPEAKER_LAB_TEST_CHANNEL_INPUT_PREFLIGHT")
	if path == "" {
		t.Skip("source-only preflight not set")
	}
	m, _, e := loadChannel(path)
	if e != nil {
		t.Fatal("private frozen channel inputs invalid")
	}
	if len(m.Cases) != 249 || len(channelPhase(m, "calibration").Cases) != 88 || len(channelPhase(m, "diagnostic").Cases) != 161 {
		t.Fatal("phase isolation")
	}
	t.Log("249 frozen source cases validated; development 88 / diagnostic 161; no identity extraction")
}

const (
	channelBase          = "915dea8b242d95f56504a86b27979863a6dacecb"
	channelModelSHA      = "1a331345f04805badbb495c775a6ddffcdd1a732567d5ec8b3d5749e3c7a5e4b"
	channelManifestSHA   = "1961568f935f22e59fff415e95433bf89648922ddb272d5e37dfa02db35d5805"
	channelSelectionSHA  = "7ec62cf16657af2ad4beb53f0b0fb6c8385ebb2b9649375a0b52da753b8337ae"
	channelProvenanceSHA = "3c30c41e72766e345e9317802f996aca664cf4c67c76f33a25c227c921623775"
	channelBinarySHA     = "a68112ef4b0a183a9cfda22658a92a335460e7d027b86c40daeaddddeef17845"
)

func channelDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func channelMargins() []float64     { return []float64{0, .01, .030229, .05, .1} }

type channelPerson struct {
	Speaker                 string `json:"speaker"`
	EnrollmentSegments      int    `json:"enrollment_segments"`
	EnrollmentSamples       int    `json:"enrollment_samples"`
	EnrollmentSpeechSamples int    `json:"enrollment_speech_samples"`
	Queries                 int    `json:"queries"`
	QueryQualityFailures    int    `json:"query_quality_failures"`
}
type channelManifest struct {
	Schema               int               `json:"schema_version"`
	SourceKind           string            `json:"source_kind"`
	Condition            string            `json:"condition"`
	Usage                string            `json:"dataset_usage"`
	DiagnosticOnly       bool              `json:"diagnostic_only"`
	NewBlind             bool              `json:"new_blind_test"`
	HumanAcceptance      bool              `json:"human_acceptance"`
	FamilyAcceptance     bool              `json:"family_acceptance"`
	ProductionAcceptance bool              `json:"production_acceptance"`
	FeatureEnabled       bool              `json:"feature_enabled"`
	SourceScores         bool              `json:"source_identity_scores_consulted"`
	SDKLoaded            bool              `json:"speaker_sdk_loaded"`
	NewQuality           bool              `json:"new_quality_computation"`
	Status               string            `json:"status"`
	SourceID             string            `json:"source_id"`
	SourceURL            string            `json:"source_url"`
	LicenseURL           string            `json:"license_url"`
	Basis                string            `json:"single_speaker_basis"`
	SampleRate           int               `json:"sample_rate"`
	SelectionSHA         string            `json:"selection_sha256"`
	Audits               []backendAuditRef `json:"audits"`
	Cases                []backendCase     `json:"cases"`
	People               []channelPerson   `json:"people"`
}

// Apply the established exact-key/type/null/duplicate check recursively as well
// as at the root; ordinary encoding/json alone would accept nested duplicates.
func channelStrict(b []byte, dst any) error {
	if strictHeader(b, dst) != nil {
		return errProtocol
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return errProtocol
	}
	typ := reflect.TypeOf(dst).Elem()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		r := raw[f.Tag.Get("json")]
		if f.Type.Kind() == reflect.Struct {
			if channelStrict(r, reflect.New(f.Type).Interface()) != nil {
				return errProtocol
			}
		} else if f.Type.Kind() == reflect.Slice {
			var rs []json.RawMessage
			if json.Unmarshal(r, &rs) != nil {
				return errProtocol
			}
			for _, v := range rs {
				if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
					return errProtocol
				}
				if f.Type.Elem().Kind() == reflect.Struct && channelStrict(v, reflect.New(f.Type.Elem()).Interface()) != nil {
					return errProtocol
				}
			}
		}
	}
	return nil
}
func decodeChannel(b []byte) (channelManifest, error) {
	var m channelManifest
	e := channelStrict(b, &m)
	return m, e
}
func channelSpeaker(s string) (string, int) {
	for _, g := range []string{"development_known", "development_strangers", "diagnostic_known", "diagnostic_strangers"} {
		n, e := strconv.Atoi(strings.TrimPrefix(s, g+"_"))
		maxN := 5
		if g == "diagnostic_strangers" {
			maxN = 20
		}
		if e == nil && n >= 1 && n <= maxN && s == fmt.Sprintf("%s_%d", g, n) {
			return g, n
		}
	}
	return "", 0
}
func validateChannel(m channelManifest, a map[string]auditCase, fixture func(backendCase) (string, error)) error {
	if m.Schema != 1 || m.SourceKind != "public_human" || m.Condition != "device06" || m.Usage != "seen_development_regression" || !m.DiagnosticOnly || m.NewBlind || m.HumanAcceptance || m.FamilyAcceptance || m.ProductionAcceptance || m.FeatureEnabled || m.SourceScores || m.SDKLoaded || m.NewQuality || m.Status != "qualified" || m.SourceID != "3dspeaker_device06_channel_20261105" || m.SourceURL != "https://3dspeaker.github.io/" || m.LicenseURL != "https://creativecommons.org/licenses/by-sa/4.0/" || m.Basis != "source_annotations" || m.SampleRate != 16000 || m.SelectionSHA != channelSelectionSHA || len(m.Cases) != 249 || len(m.People) != 35 {
		return errProtocol
	}
	seen := map[string]bool{}
	textPhase, textRole := map[string]string{}, map[string]string{}
	totals := map[string]int{}
	last := map[string]int{}
	people := map[string]channelPerson{}
	for i, c := range m.Cases {
		g, _ := channelSpeaker(c.Speaker)
		enroll := strings.HasSuffix(c.Role, "_enrollment")
		if g == "" || c.ID != fmt.Sprintf("channel_%04d", i+1) || (c.Role != g+"_query" && c.Role != g+"_enrollment") || strings.Contains(g, "strangers") && enroll || !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path || c.Samples < 32000 || c.Samples > 128000 {
			return errProtocol
		}
		for _, v := range []struct{ k, v string }{{"path", c.Path}, {"wave", c.SHA}, {"record", c.RecordingSHA}, {"group", c.SourceGroupSHA}} {
			if (v.k != "path" && !shaPattern.MatchString(v.v)) || seen[v.k+v.v] {
				return errProtocol
			}
			seen[v.k+v.v] = true
		}
		if !shaPattern.MatchString(c.TextSHA) || seen["persontext"+c.Speaker+c.TextSHA] {
			return errProtocol
		}
		seen["persontext"+c.Speaker+c.TextSHA] = true
		phase := "development"
		if strings.HasPrefix(g, "diagnostic") {
			phase = "diagnostic"
		}
		role := "query"
		if enroll {
			role = "enrollment"
		}
		if old, ok := textPhase[c.TextSHA]; ok && old != phase {
			return errProtocol
		}
		if old, ok := textRole[c.TextSHA]; ok && old != role {
			return errProtocol
		}
		textPhase[c.TextSHA], textRole[c.TextSHA] = phase, role
		ac, ok := a[c.SHA]
		if !ok || ac.Samples != c.Samples || !backendFinite(ac.RMS) || ac.RMS < 0 || ac.RMS > 1 || ac.SpeechSamples < 0 || ac.SpeechSamples > c.Samples {
			return errProtocol
		}
		p := people[c.Speaker]
		p.Speaker = c.Speaker
		if enroll {
			if ac.RMS < minRMS || ac.SpeechSamples < 16000 || p.EnrollmentSamples >= 320000 || p.Queries > 0 {
				return errProtocol
			}
			p.EnrollmentSegments++
			p.EnrollmentSamples += c.Samples
			p.EnrollmentSpeechSamples += ac.SpeechSamples
			last[c.Speaker] = c.Samples
		} else {
			p.Queries++
		}
		q, e := fixture(c)
		if e != nil || (q != "qualified" && q != "short" && q != "silence") || (q == "qualified") != (ac.RMS >= minRMS) {
			return errProtocol
		}
		if !enroll && q != "qualified" {
			p.QueryQualityFailures++
		}
		people[c.Speaker] = p
		totals[c.Role]++
	}
	expected := map[string]int{"development_known_enrollment": 38, "development_known_query": 25, "development_strangers_query": 25, "diagnostic_known_enrollment": 36, "diagnostic_known_query": 25, "diagnostic_strangers_query": 100}
	if !reflect.DeepEqual(totals, expected) || len(people) != 35 {
		return errProtocol
	}
	summary := map[string]bool{}
	for _, p := range m.People {
		g, _ := channelSpeaker(p.Speaker)
		if g == "" || summary[p.Speaker] || p != people[p.Speaker] || p.Queries < 2 {
			return errProtocol
		}
		summary[p.Speaker] = true
		if strings.Contains(g, "strangers") {
			if p.EnrollmentSegments != 0 || p.Queries != 5 {
				return errProtocol
			}
		} else if p.EnrollmentSegments < 2 || p.EnrollmentSamples < 320000 || p.EnrollmentSamples > 448000 || p.EnrollmentSamples-last[p.Speaker] >= 320000 {
			return errProtocol
		}
	}
	return nil
}
func channelFixtureQuality(c backendCase) (string, error) {
	pcm, e := loadFixture(fixtureCase{ID: c.ID, Path: c.Path, Samples: c.Samples, SHA: c.SHA})
	if e != nil {
		return "", errProtocol
	}
	defer clear(pcm)
	return qualityReason(pcm), nil
}
func channelBound(path, sha string, limit int64) ([]byte, error) {
	b, e := boundedFile(path, limit)
	if e != nil {
		return nil, errProtocol
	}
	if channelDigest(b) != sha {
		clear(b)
		return nil, errProtocol
	}
	return b, nil
}
func loadChannel(path string) (channelManifest, map[string]auditCase, error) {
	var m channelManifest
	b, e := channelBound(path, channelManifestSHA, backendMaxBytes)
	if e != nil {
		return m, nil, e
	}
	defer clear(b)
	m, e = decodeChannel(b)
	if e != nil {
		return m, nil, e
	}
	// These exact frozen source/selection bytes are independently audited by the
	// controller. They are input proofs, never trusted from a WAV filename.
	for _, ref := range []backendAuditRef{{filepath.Join(filepath.Dir(path), "channel-provenance.json"), channelProvenanceSHA}, {filepath.Join(filepath.Dir(filepath.Dir(path)), "evidence", "channel-selection.json"), channelSelectionSHA}} {
		v, e := channelBound(ref.Path, ref.SHA, 16*1024*1024)
		if e != nil {
			return m, nil, e
		}
		clear(v)
	}
	if len(m.Audits) != 4 {
		return m, nil, errProtocol
	}
	a := map[string]auditCase{}
	seen := map[string]bool{}
	for _, ref := range m.Audits {
		if seen[ref.Path] || seen[ref.SHA] || !shaPattern.MatchString(ref.SHA) {
			return m, nil, errProtocol
		}
		seen[ref.Path], seen[ref.SHA] = true, true
		qa, e := loadBoundAudit(ref.Path, ref.SHA)
		if e != nil {
			return m, nil, errProtocol
		}
		for _, c := range qa.Cases {
			if _, ok := a[c.SHA]; ok {
				return m, nil, errProtocol
			}
			a[c.SHA] = c
		}
	}
	if e := validateChannel(m, a, channelFixtureQuality); e != nil {
		return m, nil, e
	}
	return m, a, nil
}
func channelPhase(m channelManifest, phase string) channelManifest {
	r := m
	r.Cases = nil
	r.People = nil
	prefix := "development_"
	if phase == "diagnostic" {
		prefix = "diagnostic_"
	}
	for _, c := range m.Cases {
		if strings.HasPrefix(c.Role, prefix) {
			r.Cases = append(r.Cases, c)
		}
	}
	for _, p := range m.People {
		if strings.HasPrefix(p.Speaker, prefix) {
			r.People = append(r.People, p)
		}
	}
	return r
}
func channelBackend(m channelManifest, phase string) backendManifest {
	r := backendManifest{SourceKind: m.SourceKind, Condition: m.Condition, SampleRate: 16000}
	split := "validation"
	if phase == "diagnostic" {
		split = "test"
	}
	for _, c := range m.Cases {
		g, n := channelSpeaker(c.Speaker)
		s := split
		if strings.Contains(g, "strangers") {
			s += "_unknown"
			c.Role = s
		} else if strings.HasSuffix(c.Role, "_enrollment") {
			c.Role = s + "_enrollment"
		} else {
			c.Role = s + "_query"
		}
		c.Speaker = fmt.Sprintf("%s_%d", s, n)
		r.Cases = append(r.Cases, c)
	}
	return r
}

// Input embeddings are borrowed; independent B templates and score copies are
// owned here and erased on every failure or by the returned cleanup function.
func channelQueries(m channelManifest, cs []embeddedCase, dimension int, phase string) (known, unknown []backendQuery, cleanup func(), err error) {
	scores := map[int][]float64{}
	var templates [][]float32
	cleanup = func() {
		for _, v := range templates {
			clear(v)
		}
		for _, v := range scores {
			clear(v)
		}
		for _, q := range known {
			clear(q.Scores)
		}
		for _, q := range unknown {
			clear(q.Scores)
		}
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	bm := channelBackend(m, phase)
	split := "validation"
	if phase == "diagnostic" {
		split = "test"
	}
	if (phase != "calibration" && phase != "diagnostic") || len(cs) != len(bm.Cases) || dimension < 1 {
		return nil, nil, cleanup, errProtocol
	}
	for i, c := range cs {
		g, _ := channelSpeaker(m.Cases[i].Speaker)
		prefix := "development_"
		if phase == "diagnostic" {
			prefix = "diagnostic_"
		}
		if !strings.HasPrefix(g, prefix) {
			return nil, nil, cleanup, errProtocol
		}
		if c.ID != m.Cases[i].ID || c.Speaker != m.Cases[i].Speaker || (c.quality != "qualified" && c.quality != "short" && c.quality != "silence") || c.quality == "qualified" && !validVector(c.vector, dimension) {
			return nil, nil, cleanup, errProtocol
		}
	}
	for p := 1; p <= 5; p++ {
		var vs [][]float32
		for i, c := range bm.Cases {
			if c.Speaker == fmt.Sprintf("%s_%d", split, p) && c.Role == split+"_enrollment" {
				if cs[i].quality != "qualified" {
					return nil, nil, cleanup, errProtocol
				}
				vs = append(vs, cs[i].vector)
			}
		}
		v, e := fuseEnrollment(vs, dimension)
		if e != nil {
			return nil, nil, cleanup, e
		}
		templates = append(templates, v)
	}
	for i, c := range bm.Cases {
		if strings.HasSuffix(c.Role, "_enrollment") || cs[i].quality != "qualified" {
			continue
		}
		scores[i] = make([]float64, 5)
		for j, v := range templates {
			score, e := prototypeScore(cs[i].vector, [][]float32{v}, dimension)
			if e != nil {
				return nil, nil, cleanup, e
			}
			scores[i][j] = score
		}
	}
	known, unknown, _ = backendPoolQueries(bm, cs, scores, split, 5, 5)
	return known, unknown, cleanup, nil
}
func channelPhasePolicy(phase string, cp channelCheckpoint, k, u []backendQuery) (backendPolicy, error) {
	if phase == "calibration" {
		return channelCalibrate(k, u), nil
	}
	if phase != "diagnostic" || !validChannelPolicy(cp.Policy) {
		return backendPolicy{}, errProtocol
	}
	return cp.Policy, nil
}
func validChannelPolicy(p backendPolicy) bool {
	v := p.Validation
	return p.Status == "calibrated" && backendFinite(p.Threshold) && backendFinite(p.Margin) && p.Target == .01 && p.Budget == 0 && slices.Contains(channelMargins(), p.Margin) && v.Total == 25 && v.UnknownTotal == 25 && v.Wrong == 0 && v.UnknownAccepted == 0 && v.Correct >= 0 && v.Qualified >= v.Correct && v.Qualified <= 25 && v.Rejected >= 0 && v.Correct+v.Rejected == 25 && v.UnknownQualified >= 0 && v.UnknownQualified <= 25 && v.UnknownRejected == 25
}
func channelCalibrate(k, u []backendQuery) backendPolicy {
	return backendCalibrate(k, u, channelMargins(), .01)
}

type channelAnonymousPerson struct {
	Speaker string        `json:"anonymous_speaker"`
	Counts  backendCounts `json:"counts"`
}
type channelMacro struct {
	KnownPeople           int     `json:"known_people"`
	QualifiedKnownPeople  int     `json:"qualified_known_people"`
	UnknownPeople         int     `json:"unknown_people"`
	Correct               float64 `json:"equal_person_correct_accept_all"`
	CorrectQualified      float64 `json:"equal_person_correct_accept_qualified"`
	UnknownPeopleAccepted int     `json:"unknown_people_with_any_false_accept"`
}
type channelRuleReport struct {
	Status string                   `json:"status"`
	Counts backendCounts            `json:"counts"`
	People []channelAnonymousPerson `json:"people"`
	Macro  channelMacro             `json:"macro"`
}

func channelRule(p backendPolicy, k, u []backendQuery) channelRuleReport {
	r := backendRuleResult("B", p, k, u, 1, true)
	out := channelRuleReport{Status: r.Status, Counts: r.Counts, Macro: channelMacro{r.Macro.KnownPeople, r.Macro.QualifiedKnownPeople, r.Macro.UnknownPeople, r.Macro.CorrectAccept, r.Macro.CorrectAcceptQualified, r.Macro.UnknownPeopleWithFalseAccept}}
	for _, p := range r.People {
		out.People = append(out.People, channelAnonymousPerson{p.Speaker, p.Counts})
	}
	return out
}

type channelCheckpoint struct {
	Schema               int              `json:"schema_version"`
	Base                 string           `json:"base_revision"`
	Phase                string           `json:"phase"`
	Model                string           `json:"model_sha256"`
	Manifest             string           `json:"manifest_sha256"`
	Selection            string           `json:"selection_sha256"`
	Provenance           string           `json:"provenance_sha256"`
	SourceKind           string           `json:"source_kind"`
	Usage                string           `json:"dataset_usage"`
	DiagnosticOnly       bool             `json:"diagnostic_only"`
	NewBlind             bool             `json:"new_blind_test"`
	HumanAcceptance      bool             `json:"human_acceptance"`
	FamilyAcceptance     bool             `json:"family_acceptance"`
	ProductionAcceptance bool             `json:"production_acceptance"`
	FeatureEnabled       bool             `json:"feature_enabled"`
	Candidates           int              `json:"candidates"`
	DevelopmentCases     int              `json:"development_cases"`
	Margins              []float64        `json:"margin_grid"`
	Policy               backendPolicy    `json:"policy"`
	Resources            channelResources `json:"resources"`
}
type channelResources struct {
	Status          string        `json:"status"`
	Cases           int           `json:"selected_cases"`
	NativeVectors   int           `json:"actual_native_vectors"`
	RejectedShort   int           `json:"quality_rejected_short"`
	RejectedSilence int           `json:"quality_rejected_silence"`
	Dimension       int           `json:"dimension"`
	ColdMS          float64       `json:"cold_start_ms"`
	CPUms           float64       `json:"worker_cpu_ms"`
	RSS             int64         `json:"worker_max_rss_kib"`
	TotalMS         float64       `json:"phase_wall_ms"`
	Native          timingSummary `json:"native_inference"`
	Wall            timingSummary `json:"wall_ipc_inference"`
	WorkerReaped    bool          `json:"worker_reaped"`
	SocketRemoved   bool          `json:"socket_removed"`
	OwnedCleared    bool          `json:"owned_memory_cleared"`
}

func newChannelCheckpoint(p backendPolicy) channelCheckpoint {
	return channelCheckpoint{Schema: 1, Base: channelBase, Phase: "calibration", Model: channelModelSHA, Manifest: channelManifestSHA, Selection: channelSelectionSHA, Provenance: channelProvenanceSHA, SourceKind: "public_human", Usage: "seen_development_regression", DiagnosticOnly: true, Candidates: 5, DevelopmentCases: 88, Margins: channelMargins(), Policy: p, Resources: channelResources{Status: "not_run"}}
}
func channelCalibrationResources(r channelResources) bool {
	if r.Status != "complete" || r.Cases != 88 || r.NativeVectors != 82 || r.RejectedShort != 0 || r.RejectedSilence != 6 || r.Dimension != 512 || r.RSS < 0 || !r.WorkerReaped || !r.SocketRemoved || !r.OwnedCleared || r.Native.WarmCount != 81 || r.Wall.WarmCount != 81 {
		return false
	}
	for _, x := range []float64{r.ColdMS, r.CPUms, r.TotalMS, r.Native.FirstMS, r.Native.WarmMedianMS, r.Native.WarmP95MS, r.Wall.FirstMS, r.Wall.WarmMedianMS, r.Wall.WarmP95MS} {
		if !backendFinite(x) || x < 0 {
			return false
		}
	}
	return true
}
func readChannelCheckpointBytes(b []byte, sha string) (channelCheckpoint, error) {
	var p channelCheckpoint
	if !shaPattern.MatchString(sha) || channelDigest(b) != sha || channelStrict(b, &p) != nil {
		return p, errProtocol
	}
	want := newChannelCheckpoint(p.Policy)
	want.Resources = p.Resources
	if !reflect.DeepEqual(p, want) || !validChannelPolicy(p.Policy) || !channelCalibrationResources(p.Resources) {
		return p, errProtocol
	}
	return p, nil
}

type channelConfig struct{ Phase, Model, SHA, Binary, Manifest, ManifestSHA, Policy, Output, PolicySHA string }

func channelConfigFrom(get func(string) string) (channelConfig, bool, error) {
	var c channelConfig
	names := []string{"PHASE", "MODEL", "SHA256", "BINARY", "MANIFEST", "MANIFEST_SHA256", "POLICY", "OUTPUT", "POLICY_SHA256"}
	values := []*string{&c.Phase, &c.Model, &c.SHA, &c.Binary, &c.Manifest, &c.ManifestSHA, &c.Policy, &c.Output, &c.PolicySHA}
	any := false
	for i, n := range names {
		*values[i] = get("SPEAKER_LAB_TEST_CHANNEL_" + n)
		any = any || *values[i] != ""
	}
	if !any {
		return c, false, nil
	}
	for i := 0; i < 8; i++ {
		if *values[i] == "" {
			return c, true, errProtocol
		}
	}
	if c.Phase != "calibration" && c.Phase != "diagnostic" || c.SHA != channelModelSHA || c.ManifestSHA != channelManifestSHA || c.Phase == "calibration" && c.PolicySHA != "" || c.Phase == "diagnostic" && !shaPattern.MatchString(c.PolicySHA) {
		return c, true, errProtocol
	}
	for _, p := range []string{c.Model, c.Binary, c.Manifest, c.Policy, c.Output} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, true, errProtocol
		}
	}
	if c.Output == c.Policy {
		return c, true, errProtocol
	}
	return c, true, nil
}
func reserveChannel(path string) (*os.File, error) {
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
func writeChannelJSON(f *os.File, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return errProtocol
	}
	defer clear(b)
	b = append(b, '\n')
	if _, e = f.Write(b); e != nil {
		return errProtocol
	}
	if f.Sync() != nil {
		return errProtocol
	}
	return nil
}

type channelReport struct {
	Schema               int                `json:"schema_version"`
	Phase                string             `json:"phase"`
	Status               string             `json:"status"`
	SourceKind           string             `json:"source_kind"`
	Usage                string             `json:"dataset_usage"`
	DiagnosticOnly       bool               `json:"diagnostic_only"`
	NewBlind             bool               `json:"new_blind_test"`
	HumanAcceptance      bool               `json:"human_acceptance"`
	FamilyAcceptance     bool               `json:"family_acceptance"`
	ProductionAcceptance bool               `json:"production_acceptance"`
	FeatureEnabled       bool               `json:"feature_enabled"`
	Household            string             `json:"household_assessment"`
	Model                string             `json:"model_sha256"`
	Manifest             string             `json:"manifest_sha256"`
	Selection            string             `json:"selection_sha256"`
	PolicySHA            string             `json:"frozen_policy_sha256"`
	Calibration          backendPolicy      `json:"calibration"`
	People               []channelPerson    `json:"original_role_person_counts"`
	Roles                []backendRoleCount `json:"role_quality_counts"`
	KnownQuality         map[string]int     `json:"known_query_quality_rejection_reasons"`
	Resources            channelResources   `json:"resources"`
	Rule                 channelRuleReport  `json:"B_rule"`
}

func runChannel(ctx context.Context, c channelConfig) (err error) {
	// All failures are private; a reserved output survives as not_run and cannot
	// be silently overwritten for another attempt.
	f, e := reserveChannel(c.Output)
	if e != nil {
		return errProtocol
	}
	defer f.Close()
	r := channelReport{Schema: 1, Phase: c.Phase, Status: "not_run", SourceKind: "public_human", Usage: "seen_development_regression", DiagnosticOnly: true, Household: "awaiting_recordings", Model: channelModelSHA, Manifest: channelManifestSHA, Selection: channelSelectionSHA, PolicySHA: c.PolicySHA, KnownQuality: map[string]int{}, Resources: channelResources{Status: "not_run"}}
	defer func() {
		if writeChannelJSON(f, r) != nil {
			err = errProtocol
		}
	}()
	var policyFile *os.File
	if c.Phase == "calibration" {
		policyFile, e = reserveChannel(c.Policy)
		if e != nil {
			return errProtocol
		}
		defer policyFile.Close()
	}
	started := time.Now()
	defer func() { r.Resources.TotalMS = backendElapsed(started) }()
	if validateModel(c.Model, c.SHA) != nil {
		return errProtocol
	}
	binary, e := channelBound(c.Binary, channelBinarySHA, 128*1024*1024)
	if e != nil {
		return errProtocol
	}
	clear(binary)
	self, e := os.Executable()
	if e != nil {
		return errProtocol
	}
	own, e := os.Stat(self)
	if e != nil {
		return errProtocol
	}
	cli, e := os.Stat(c.Binary)
	if e != nil || os.SameFile(own, cli) || cli.Mode().Perm()&0111 == 0 {
		return errProtocol
	}
	tmp := os.TempDir()
	st, e := os.Stat(tmp)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 || !noSymlinks(tmp) || len(filepath.Join(tmp, "speaker-lab-4294967295", "worker.sock")) > 100 {
		return errProtocol
	}
	m, a, e := loadChannel(c.Manifest)
	if e != nil {
		return errProtocol
	}
	var cp channelCheckpoint
	if c.Phase == "diagnostic" {
		b, e := boundedFile(c.Policy, backendMaxBytes)
		if e != nil {
			return errProtocol
		}
		cp, e = readChannelCheckpointBytes(b, c.PolicySHA)
		clear(b)
		if e != nil || cp.Resources.Status != "complete" || cp.Resources.Cases != 88 || cp.Resources.NativeVectors+cp.Resources.RejectedShort+cp.Resources.RejectedSilence != 88 || cp.Resources.Dimension != 512 || !cp.Resources.WorkerReaped || !cp.Resources.SocketRemoved || !cp.Resources.OwnedCleared {
			return errProtocol
		}
		r.Calibration = cp.Policy
	}
	m = channelPhase(m, c.Phase)
	r.People = append([]channelPerson(nil), m.People...)
	r.Resources.Cases = len(m.Cases)
	em := evaluationManifest{SourceKind: "public_human", SampleRate: 16000}
	roles := map[string]*backendRoleCount{}
	for _, v := range m.Cases {
		em.Cases = append(em.Cases, evaluationCase{ID: v.ID, Speaker: v.Speaker, Split: "evaluation", Path: v.Path, Samples: v.Samples, SHA: v.SHA, RecordingSHA: v.RecordingSHA, TextSHA: v.TextSHA})
		q := a[v.SHA]
		if roles[v.Role] == nil {
			roles[v.Role] = &backendRoleCount{Role: v.Role}
		}
		rc := roles[v.Role]
		rc.Total++
		if q.RMS >= minRMS {
			rc.EnergyQualified++
		}
		if q.SpeechSamples >= 16000 {
			rc.VADQualified++
		}
		if q.RMS >= minRMS && q.SpeechSamples >= 16000 {
			rc.BothQualified++
		}
	}
	var worker *workerProcess
	socket := ""
	ex, e := extractEvaluation(ctx, em, c.Model, c.SHA, func(ctx context.Context, model, sha string) (*workerProcess, error) {
		w, e := startWorkerBinary(ctx, c.Binary, model, sha)
		if e == nil {
			worker = w
			socket = w.conn.RemoteAddr().String()
		}
		return w, e
	})
	r.Resources.NativeVectors = ex.qualified
	r.Resources.RejectedShort = ex.short
	r.Resources.RejectedSilence = ex.silence
	r.Resources.Dimension = ex.dimension
	r.Resources.ColdMS = ex.coldMS
	r.Resources.CPUms = ex.cpuMS
	r.Resources.RSS = ex.workerRSS
	r.Resources.Native = ex.native
	r.Resources.Wall = ex.wall
	defer func() { ex.clearVectors(); r.Resources.OwnedCleared = true }()
	if worker != nil {
		r.Resources.WorkerReaped = worker.cmd.ProcessState != nil
		_, se := os.Lstat(socket)
		r.Resources.SocketRemoved = os.IsNotExist(se)
	}
	if e != nil {
		r.Resources.Status = "partial_failure"
		return errProtocol
	}
	if ex.dimension != 512 || len(ex.cases) != len(m.Cases) || ex.qualified+ex.short+ex.silence != len(m.Cases) || !r.Resources.WorkerReaped || !r.Resources.SocketRemoved {
		return errProtocol
	}
	k, u, cleanup, e := channelQueries(m, ex.cases, ex.dimension, c.Phase)
	if e != nil {
		return errProtocol
	}
	defer cleanup()
	p, e := channelPhasePolicy(c.Phase, cp, k, u)
	if e != nil {
		return errProtocol
	}
	r.Calibration = p
	r.Rule = channelRule(p, k, u)
	for _, v := range roles {
		r.Roles = append(r.Roles, *v)
	}
	sort.Slice(r.Roles, func(i, j int) bool { return r.Roles[i].Role < r.Roles[j].Role })
	for i, v := range m.Cases {
		if strings.Contains(v.Role, "known_query") && ex.cases[i].quality != "qualified" {
			r.KnownQuality[ex.cases[i].quality]++
		}
	}
	// Clear all owned scores/templates/embeddings before asserting the ownership
	// evidence and persisting the frozen checkpoint.
	cleanup()
	ex.clearVectors()
	r.Resources.OwnedCleared = true
	r.Resources.TotalMS = backendElapsed(started)
	r.Resources.Status = "complete"
	if p.Status != "calibrated" {
		return errProtocol
	}
	if c.Phase == "calibration" {
		cp = newChannelCheckpoint(p)
		cp.Resources = r.Resources
		if writeChannelJSON(policyFile, cp) != nil {
			return errProtocol
		}
	}
	r.Status = c.Phase + "_complete"
	return nil
}
func TestRealChannelStudyOptIn(t *testing.T) {
	c, enabled, e := channelConfigFrom(os.Getenv)
	if !enabled {
		t.Skip("explicit two-phase channel study opt-in not set")
	}
	if e != nil {
		t.Fatal("private channel configuration invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3600*time.Second)
	defer cancel()
	if runChannel(ctx, c) != nil {
		t.Fatal("private channel phase failed; reserved evidence retained")
	}
}
