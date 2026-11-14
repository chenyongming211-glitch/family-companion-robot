package main

import (
	"bytes"
	"testing"
)

func TestPublishFirmwareCLIRejectsMissingAndArbitraryInput(t *testing.T) {
	for _, args := range [][]string{{"publish-firmware"}, {"publish-firmware", "--root", "/tmp/private", "--url", "https://evil.example/app.bin"}, {"publish-firmware", "--root", "/tmp/private", "--manifest", "/no/file", "--app", "/no/app", "--partition", "/no/partition"}} {
		var out bytes.Buffer
		if run(args, &out) == nil || out.Len() != 0 {
			t.Fatal("accepted invalid publication")
		}
	}
}
