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
	var socket, wav *string
	switch args[0] {
	case "worker":
		socket = f.String("socket", "", "")
	case "probe":
		wav = f.String("wav", "", "")
	default:
		return fail("usage")
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *model == "" || !shaPattern.MatchString(*sha) {
		return fail("usage")
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
