package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"familyrobot/internal/gateway"
	"familyrobot/internal/qwen"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := gateway.Config{
		DeviceToken: os.Getenv("ROBOT_DEVICE_TOKEN"), BootstrapSecret: os.Getenv("ROBOT_OTA_SECRET"), PublicWSURL: os.Getenv("ROBOT_PUBLIC_WS_URL"), AllowedDevice: os.Getenv("ROBOT_ALLOWED_DEVICE"),
		RecordDirectory: os.Getenv("ROBOT_RECORD_DIR"), RecordRetention: duration("ROBOT_RECORD_RETENTION", 168*time.Hour),
		MaxSessions: integer("ROBOT_MAX_SESSIONS", 1), IdleTimeout: duration("ROBOT_IDLE_TIMEOUT", 2*time.Minute), SessionTimeout: duration("ROBOT_SESSION_TIMEOUT", 30*time.Minute),
		Qwen: qwen.Config{URL: os.Getenv("QWEN_REALTIME_URL"), APIKey: os.Getenv("DASHSCOPE_API_KEY"), Voice: env("QWEN_VOICE", "Tina"), VAD: env("QWEN_VAD", "semantic_vad"), Instructions: env("ROBOT_INSTRUCTIONS", "你是友善、耐心的中文家庭语音助手。自然简洁地交流，每次先用一到三句话回答。用户插话时立即让出话轮，根据新的问题继续交流。不要声称完成了你没有执行的操作。")},
	}
	if cfg.RecordRetention <= 0 {
		slog.Error("ROBOT_RECORD_RETENTION must be a positive duration")
		os.Exit(1)
	}
	if cfg.Qwen.VAD != "semantic_vad" && cfg.Qwen.VAD != "server_vad" {
		slog.Error("QWEN_VAD must be semantic_vad or server_vad")
		os.Exit(1)
	}
	g, err := gateway.New(cfg)
	if err != nil {
		slog.Error("invalid configuration", "reason", err.Error())
		os.Exit(1)
	}
	defer g.Close()
	server := &http.Server{Addr: env("ROBOT_LISTEN_ADDR", "127.0.0.1:8000"), Handler: g.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		g.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	slog.Info("gateway_starting", "version", version, "listen", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server failed")
		os.Exit(1)
	}
}
func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func integer(k string, fallback int) int {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
func duration(k string, fallback time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}
