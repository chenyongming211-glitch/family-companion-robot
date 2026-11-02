//go:build !linux

package speakerlive

import "os/exec"

// The ONNX helper and its process-tree guarantees are Linux-only.
func configureProcess(*exec.Cmd) error { return errUnavailable }
func killProcess(*exec.Cmd)            {}
