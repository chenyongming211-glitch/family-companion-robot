//go:build linux

// speaker-lab is an isolated, Linux-only experiment. It never runs by default.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0}) != nil {
		_ = json.NewEncoder(os.Stderr).Encode(struct {
			Error string `json:"error"`
		}{"core_limit_failed"})
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(runCLI(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
func runCLI(ctx context.Context, args []string, out, errOut io.Writer) int {
	return runCLIWithInput(ctx, args, os.Stdin, out, errOut)
}
func runCLIWithInput(ctx context.Context, args []string, input io.ReadCloser, out, errOut io.Writer) int {
	fail := func(code string) int {
		_ = json.NewEncoder(errOut).Encode(struct {
			Error string `json:"error"`
		}{code})
		return 1
	}
	if len(args) == 0 {
		return fail("usage")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	model := f.String("model", "", "")
	sha := f.String("sha256", "", "")
	var socket, wav, manifest *string
	var declared, realtime *bool
	switch args[0] {
	case "session":
		declared = f.Bool("declared-single-speaker", false, "")
		realtime = f.Bool("realtime-test", false, "")
	case "worker":
		socket = f.String("socket", "", "")
	case "benchmark", "evaluate", "evaluate-large":
		manifest = f.String("manifest", "", "")
	case "probe":
		wav = f.String("wav", "", "")
	default:
		return fail("usage")
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *model == "" || !shaPattern.MatchString(*sha) {
		return fail("usage")
	}
	if declared != nil {
		if runSession(ctx, input, out, *model, *sha, *declared, sessionOptions{realtime: *realtime}) != nil {
			return fail("session_failed")
		}
		return 0
	}
	if socket != nil {
		if *socket == "" {
			return fail("usage")
		}
		if runWorker(ctx, *socket, *model, *sha) != nil {
			return fail("worker_failed")
		}
		return 0
	}
	if manifest != nil {
		if *manifest == "" {
			return fail("usage")
		}
		if args[0] == "evaluate-large" {
			result, e := runLargeEvaluation(ctx, *model, *sha, *manifest)
			if e != nil {
				return fail("evaluate_large_failed")
			}
			if json.NewEncoder(out).Encode(result) != nil {
				return 1
			}
			return 0
		}
		if args[0] == "evaluate" {
			result, e := runEvaluation(ctx, *model, *sha, *manifest)
			if e != nil {
				return fail("evaluate_failed")
			}
			if json.NewEncoder(out).Encode(result) != nil {
				return 1
			}
			return 0
		}
		result, e := runBenchmark(ctx, *model, *sha, *manifest)
		if e != nil {
			return fail("benchmark_failed")
		}
		if json.NewEncoder(out).Encode(result) != nil {
			return 1
		}
		return 0
	}
	if *wav == "" {
		return fail("usage")
	}
	result, e := runProbe(ctx, *model, *sha, *wav)
	if e != nil {
		return fail("probe_failed")
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}
