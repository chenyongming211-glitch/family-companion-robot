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

	"familyrobot/internal/qwen"
	"familyrobot/internal/realtimeentry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	addr := env("REALTIME_TEST_LISTEN", "127.0.0.1:18030")
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		slog.Error("loopback listen required")
		os.Exit(1)
	}
	vad := env("QWEN_VAD", "semantic_vad")
	if vad != "semantic_vad" && vad != "server_vad" {
		slog.Error("invalid VAD")
		os.Exit(1)
	}
	entry, err := realtimeentry.New(qwen.Config{URL: os.Getenv("QWEN_REALTIME_URL"), APIKey: os.Getenv("DASHSCOPE_API_KEY"), Voice: env("QWEN_VOICE", "Tina"), VAD: vad,
		Instructions: env("ROBOT_INSTRUCTIONS", "你是友善、耐心的中文家庭语音助手。自然简洁地交流，每次先用一到三句话回答。用户插话时立即让出话轮，根据新的问题继续交流。不要声称完成了你没有执行的操作。")})
	if err != nil {
		slog.Error("invalid realtime test configuration")
		os.Exit(1)
	}
	defer entry.Close()
	server := &http.Server{Addr: addr, Handler: entry.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		entry.Close()
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(deadline)
	}()
	slog.Info("realtime_mac_test_starting", "listen", addr, "voice_core", "existing_xiaozhi_gateway", "recording", false)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("test listener failed")
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
