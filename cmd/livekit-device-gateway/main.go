// DEPRECATED_PRODUCT_PATH (2026-10-25): legacy device bridge for the LiveKit cascade.
// The default voice core is cmd/realtime-output-live; see docs/CODE_LIFECYCLE.md.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"familyrobot/internal/devicebridge"
)

func main() {
	addr := env("VOICE_DEVICE_LISTEN", "127.0.0.1:18020")
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		slog.Error("device gateway requires loopback listen address")
		os.Exit(1)
	}
	bridge, err := devicebridge.New(devicebridge.Config{BackendURL: env("VOICE_DEVICE_BACKEND", "ws://127.0.0.1:18010/v1/audio?mode=drain"), BackendToken: os.Getenv("VOICE_CLIENT_TOKEN"), KeepOpen: os.Getenv("VOICE_TEST_KEEP_OPEN") == "1"})
	if err != nil {
		slog.Error("invalid device gateway configuration")
		os.Exit(1)
	}
	defer bridge.Close()
	server := &http.Server{Addr: addr, Handler: bridge.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		bridge.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	slog.Info("livekit_device_gateway_starting", "listen", addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("device gateway listen failed")
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
