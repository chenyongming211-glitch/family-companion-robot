// Standalone Go parity entry; Python and device gateways are independent.
package main

import (
	"context"
	"familyrobot/internal/realtimelive"
	"familyrobot/internal/realtimestate"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var build = "development"

func main() {
	syscall.Umask(0077)
	port := flag.String("listen", "127.0.0.1:18040", "loopback listen address")
	config := flag.String("config", "/etc/family-robot/realtime-test.env", "protected server-only configuration")
	identity := flag.String("identity", "config/product-identity.json", "read-only product facts")
	root := flag.String("records", "/var/lib/family-robot-realtime-test/verification/output-live-go", "private per-session text directory")
	deviceListen := flag.String("device-listen", "", "optional device-only listener: 0.0.0.0:18041")
	deviceConfig := flag.String("device-config", "/etc/family-robot/server.env", "protected server-only device authentication configuration")
	maxSessions := flag.Int("max-sessions", 1, "maximum simultaneous terminal sessions (1-8)")
	memory := flag.Bool("memory", true, "restore bounded recent conversation memory per authenticated device")
	diagnosticDevice := flag.String("diagnostics-device", "", "optional authenticated device ID for one diagnostic session")
	diagnosticDuration := flag.Duration("diagnostics-duration", 5*time.Minute, "diagnostic window, maximum 5m")
	flag.Parse()
	if *maxSessions < 1 || *maxSessions > 8 {
		log.Fatal("invalid_max_sessions")
	}
	if *port != "127.0.0.1:18040" {
		log.Fatal("unsupported_listen_address")
	}
	if *deviceListen != "" && *deviceListen != "0.0.0.0:18041" {
		log.Fatal("unsupported_device_listen_address")
	}
	cfg, e := realtimelive.LoadConfig(*config)
	if e != nil {
		log.Fatal("protected_config_invalid")
	}
	cfg.MaxSessions = *maxSessions
	cfg.MemoryEnabled = *memory
	cfg.DiagnosticsDevice = *diagnosticDevice
	cfg.DiagnosticsDuration = *diagnosticDuration
	product, e := realtimestate.LoadProduct(*identity)
	if e != nil {
		log.Fatal("identity_invalid")
	}
	if e = os.MkdirAll(*root, 0700); e != nil {
		log.Fatal("record_directory_failed")
	}
	app := realtimelive.NewServer(cfg, product, *root, build)
	if app.InitializationError() != nil {
		log.Fatal("device_registry_invalid")
	}
	servers := []*http.Server{{Addr: *port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}}
	if *deviceListen != "" {
		dc, err := realtimelive.LoadDeviceConfig(*deviceConfig)
		if err != nil {
			log.Fatal("protected_device_config_invalid")
		}
		servers = append(servers, &http.Server{Addr: *deviceListen, Handler: app.DeviceHandler(dc), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384})
	}
	// Bind every requested listener before accepting either client kind. A
	// failed device bind must not leave a partially configured running process.
	listeners := make([]net.Listener, 0, len(servers))
	for _, srv := range servers {
		listener, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			log.Fatal("listen_failed")
		}
		listeners = append(listeners, listener)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		app.CloseActive(shutdown)
		for _, srv := range servers {
			srv.Shutdown(shutdown)
		}
	}()
	log.Printf("realtime_go.listening port=18040 build=%s", build)
	if *deviceListen != "" {
		log.Printf("realtime_go.device_listening port=18041")
	}
	results := make(chan error, len(servers))
	for i, srv := range servers {
		go func() { results <- srv.Serve(listeners[i]) }()
	}
	e = <-results
	stop()
	<-stopped
	if e != nil && e != http.ErrServerClosed {
		log.Fatal("serve_failed")
	}
}
