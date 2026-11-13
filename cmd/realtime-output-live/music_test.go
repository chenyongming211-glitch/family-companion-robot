package main

import (
	"bytes"
	"context"
	"encoding/json"
	"familyrobot/internal/management"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimelive"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const testMusicDevice = "dev_11111111111111111111111111111111"

func TestDeviceMusicBindingComposition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "management.json")
	root := filepath.Join(dir, "library")
	cfg := management.Config{SchemaVersion: 1, Families: []management.FamilyConfig{{ID: "family-a", Name: "A"}, {ID: "family-b", Name: "B"}}, Devices: []management.DeviceConfig{{ID: testMusicDevice, Model: "微雪 ESP32-S3"}}, Bindings: []management.Binding{{ID: "binding-a", Version: 1, FamilyID: "family-a", DeviceID: testMusicDevice, From: time.Now().Add(-time.Hour)}}}
	write := func() {
		t.Helper()
		b, _ := json.Marshal(cfg)
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	var voice realtimelive.Config
	if configureMusic(&voice, false, root, path, "") != nil || voice.MusicForDevice == nil {
		t.Fatal("library-only rejected")
	}
	if voice.MusicForDevice("dev_22222222222222222222222222222222") != nil {
		t.Fatal("unbound device allowed")
	}
	a := voice.MusicForDevice(testMusicDevice)
	if a == nil {
		t.Fatal("bound device denied")
	}
	media := filepath.Join(dir, "fixture.flac")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-y", media).CombinedOutput(); e != nil {
		t.Fatalf("fixture %v %s", e, b)
	}
	data, e := os.ReadFile(media)
	if e != nil {
		t.Fatal(e)
	}
	for _, family := range []string{"family-a", "family-b"} {
		l, e := music.NewLibrary(root, family)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = l.WithAccessCheck(func() bool { return true }).Import(context.Background(), family, "Artist", "flac", bytes.NewReader(data)); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := a.Search(context.Background(), music.Query{Title: "family-a", Artist: "Artist"})
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	r, e := a.Resolve(context.Background(), rows[0])
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chunks := 0
	n, e := a.Stream(ctx, r, func([]byte) error {
		chunks++
		cfg.Bindings[0].FamilyID = "family-b"
		cfg.Bindings[0].ID = "binding-b"
		cfg.Bindings[0].Version++
		write()
		return nil
	})
	if e == nil || n == 0 || chunks != 1 || a.CanAccessTrack(rows[0]) {
		t.Fatal("rebind failed to revoke PCM", n, e, chunks)
	}
	b := voice.MusicForDevice(testMusicDevice)
	if b == nil {
		t.Fatal("fresh source denied")
	}
	if _, e = b.Resolve(context.Background(), rows[0]); e == nil {
		t.Fatal("family B resolved A")
	}
	br, e := b.Search(context.Background(), music.Query{Title: "family-b", Artist: "Artist"})
	if e != nil || len(br) != 1 {
		t.Fatal(br, e)
	}
	cfg.Bindings = nil
	write()
	if voice.MusicForDevice(testMusicDevice) != nil {
		t.Fatal("revoked device allowed")
	}
	os.Remove(path)
	if voice.MusicForDevice(testMusicDevice) != nil {
		t.Fatal("unreadable config allowed")
	}
}
func TestMusicConfigRequirements(t *testing.T) {
	for _, tc := range []struct {
		root, path, socket string
		fail               bool
	}{{"private", "", "", true}, {"", "", "socket", true}, {"", "", "", false}} {
		var cfg realtimelive.Config
		if e := configureMusic(&cfg, false, tc.root, tc.path, tc.socket); (e != nil) != tc.fail {
			t.Fatal(tc, e)
		}
	}
}
