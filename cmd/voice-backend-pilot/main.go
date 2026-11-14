// voice-backend-pilot is a cloud-only, file-in/file-out comparison tool.
// It has no device, microphone, playback, HTTP listener or service controls.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"familyrobot/internal/realtimelive"
	"familyrobot/internal/voicecompare"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		// Do not expose provider error bodies, input text or protected config.
		fmt.Fprintln(os.Stderr, "voice_backend_pilot_failed:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	f := flag.NewFlagSet("voice-backend-pilot", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	allow := f.Bool("allow-provider-calls", false, "explicitly allow paid cloud calls")
	backend := f.String("backend", "tts", "tts, native or cascade")
	config := f.String("config", "/etc/family-robot/server.env", "protected cloud config")
	input := f.String("input", "", "cloud PCM16/16k/mono input, at most 10 seconds")
	text := f.String("text", "", "public synthetic text for TTS-only check")
	out := f.String("out", "", "new private result directory")
	ledger := f.String("ledger", "", "existing shared protocol/primary attempt ledger")
	budgetFile := f.String("budget-file", "", "shared private API reservation ledger")
	budgetFen := f.Int("budget-fen", 100, "whole quiet phase ceiling, at most 100 fen")
	attempt := f.String("attempt", "", "unique quiet-voice-20261114/... attempt; never reused")
	model := f.String("tts-model", "cosyvoice-v3-flash", "explicit actual TTS model")
	voice := f.String("voice", "longanyang", "timestamp-capable voice belonging to this TTS model")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return errors.New("invalid_arguments")
	}
	if !*allow {
		return errors.New("provider_calls_disabled")
	}
	if runtime.GOOS != "linux" {
		return errors.New("cloud_linux_required")
	}
	if (*backend != "tts" && *backend != "native" && *backend != "cascade") || *budgetFen < 1 || *budgetFen > 100 ||
		!validPaths(*out, *ledger, *budgetFile) || !validAttempt(*attempt) ||
		(*model != "cosyvoice-v3-flash" && *model != "cosyvoice-v3.5-flash") ||
		*voice == "" || len(*voice) > 160 {
		return errors.New("invalid_options")
	}
	if (*backend == "tts" && (*input != "" || utf8.RuneCountInString(*text) < 1 || utf8.RuneCountInString(*text) > 120)) ||
		(*backend != "tts" && (*input == "" || *text != "")) {
		return errors.New("invalid_input_options")
	}
	// 3.5 has no confirmed system-voice pairing in this task. Require an
	// explicit created voice instead of silently borrowing a v3 system voice.
	if *model == "cosyvoice-v3.5-flash" && !strings.HasPrefix(*voice, *model+"-") {
		return errors.New("unverified_model_voice_pair")
	}
	var pcm []byte
	var err error
	if *input != "" {
		pcm, err = readInput(*input)
		if err != nil {
			return err
		}
	}
	cfg, err := realtimelive.LoadConfig(*config)
	if err != nil {
		return errors.New("protected_config_invalid")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme != "wss" || u.Query().Get("model") != "qwen3.8-omni-flash-realtime" ||
		!strings.HasSuffix(u.Hostname(), ".cn-beijing.maas.aliyuncs.com") {
		return errors.New("unverified_region_or_native_model")
	}
	if err = os.Mkdir(*out, 0700); err != nil {
		return errors.New("output_must_be_new")
	}
	if err = reserveGlobal(*ledger, *backend, *attempt); err != nil {
		return err
	}
	u.Path, u.RawQuery = "/compatible-mode/v1/chat/completions", ""
	u.Scheme = "https"
	client := voicecompare.Client{
		Key: cfg.Key, RealtimeURL: cfg.URL, InferenceURL: cfg.ASRURL, TextURL: u.String(), TTSModel: *model, Voice: *voice,
		BeforeCall: func(stage string, reserve float64) error {
			return reserveBudget(*budgetFile, stage, reserve, *budgetFen)
		},
		AfterCall: func(stage string, usage map[string]any, callErr error) error {
			if err := recordOutcome(*budgetFile, stage, usage, callErr != nil); err != nil {
				return err
			}
			if len(usage) == 0 {
				return errors.New("api_usage_unknown")
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	started := time.Now()
	result, callErr := client.Run(ctx, *backend,
		"你是中文语音助手小满。优先回答当前问题，简短自然地交流；最多120个汉字，不使用Markdown，不调用工具，不声称未发生的设备操作。", pcm, *text)
	report := map[string]any{
		"backend": *backend, "attempt": *attempt, "tts_model": *model, "voice": *voice,
		"state": "completed", "device_connected": false, "audio_played": false,
		"elapsed_ms": time.Since(started).Milliseconds(), "sample_rate": 24000,
		"samples": len(result.PCM) / 2, "text": result.Text, "words": result.Words, "calls": result.Calls,
		"cost_status": "usage_requires_billing_reconciliation", "budget_fen": *budgetFen,
		"timestamp_limit": "provider timestamps; no physical playback or acoustic precision verified",
	}
	if callErr != nil {
		report["state"] = "failed_or_unknown"
		report["error"] = callErr.Error() // Private file only; never print provider details.
	}
	if len(result.PCM) > 0 {
		if err = os.WriteFile(filepath.Join(*out, "output.pcm"), result.PCM, 0600); err != nil {
			report["state"] = "output_write_failed"
			callErr = errors.New("output_write_failed")
		}
	}
	if err = writeJSON(filepath.Join(*out, "result.json"), report); err != nil {
		return err
	}
	if callErr != nil {
		return errors.New("provider_run_failed_or_unknown")
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"state": "completed", "backend": *backend, "samples": len(result.PCM) / 2, "word_count": len(result.Words), "audio_played": false, "result_path": filepath.Join(*out, "result.json")})
}

func validPaths(values ...string) bool {
	for _, p := range values {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return false
		}
	}
	return true
}

func validAttempt(s string) bool {
	if !strings.HasPrefix(s, "quiet-voice-20261114/") || len(s) > 120 || len(s) < 23 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("/-_", r)) {
			return false
		}
	}
	return true
}

func readInput(path string) ([]byte, error) {
	info, statErr := os.Lstat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input_regular_file_required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("input_unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 2 || st.Size() > 320000 || st.Size()%2 != 0 {
		return nil, errors.New("input_bounds")
	}
	b, err := io.ReadAll(io.LimitReader(f, 320001))
	if err != nil || len(b) == 0 || len(b) > 320000 || len(b)%2 != 0 {
		return nil, errors.New("input_bounds")
	}
	return b, nil
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("result_encoding")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("result_create_failed")
	}
	defer f.Close()
	if _, err = f.Write(append(b, '\n')); err != nil {
		return errors.New("result_write_failed")
	}
	if f.Sync() != nil {
		return errors.New("result_sync_failed")
	}
	return nil
}
