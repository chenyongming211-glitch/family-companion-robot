// realtime-speaker-test serves an isolated, memory-only normal voice conversation.
package main

import (
	"context"
	"familyrobot/internal/realtimelive"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/speakerlive"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var build = "development"

func main() {
	syscall.Umask(0077)
	listen := flag.String("listen", "127.0.0.1:18052", "isolated loopback endpoint")
	config := flag.String("config", "/etc/family-robot/realtime-test.env", "server-only config")
	identity := flag.String("identity", "", "product facts path")
	records := flag.String("records", "", "private, fresh pilot metadata directory")
	binary := flag.String("speaker-binary", "", "cloud-only lab executable")
	model := flag.String("speaker-model", "", "cloud-only model")
	sha := flag.String("speaker-sha256", "", "model digest")
	enabled := flag.Bool("speaker", true, "enable candidate observation")
	flag.Parse()
	if (*listen != "127.0.0.1:18052" && *listen != "127.0.0.1:18053") || *identity == "" || !filepath.IsAbs(*records) || !strings.HasPrefix(filepath.Clean(*records), "/opt/family-robot-validation/") {
		log.Fatal("invalid_pilot_configuration")
	}
	cfg, err := realtimelive.LoadConfig(*config)
	if err != nil {
		log.Fatal("protected_config_invalid")
	}
	cfg.MemoryOnly = true
	cfg.MemoryEnabled = false
	cfg.MaxSessions = 1
	if *enabled {
		cfg.Speaker = &speakerlive.Config{Binary: *binary, Model: *model, SHA256: *sha}
	}
	product, err := realtimestate.LoadProduct(*identity)
	if err != nil {
		log.Fatal("identity_invalid")
	}
	if os.MkdirAll(*records, 0700) != nil {
		log.Fatal("metadata_directory_failed")
	}
	app := realtimelive.NewServer(cfg, product, *records, build)
	if app.InitializationError() != nil {
		log.Fatal("pilot_configuration_invalid")
	}
	server := &http.Server{Addr: *listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		closeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		app.CloseActive(closeCtx)
		server.Shutdown(closeCtx)
	}()
	log.Print("speaker_pilot.listening")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("pilot_listener_failed")
	}
}
