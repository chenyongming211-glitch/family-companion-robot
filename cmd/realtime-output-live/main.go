// Standalone Go parity entry; Python and device gateways are independent.
package main

import (
	"context"
	"familyrobot/internal/realtimelive"
	"familyrobot/internal/realtimestate"
	"flag"
	"log"
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
	flag.Parse()
	if *port != "127.0.0.1:18040" {
		log.Fatal("unsupported_listen_address")
	}
	cfg, e := realtimelive.LoadConfig(*config)
	if e != nil {
		log.Fatal("protected_config_invalid")
	}
	product, e := realtimestate.LoadProduct(*identity)
	if e != nil {
		log.Fatal("identity_invalid")
	}
	if e = os.MkdirAll(*root, 0700); e != nil {
		log.Fatal("record_directory_failed")
	}
	app := realtimelive.NewServer(cfg, product, *root, build)
	srv := &http.Server{Addr: *port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		app.CloseActive(shutdown)
		srv.Shutdown(shutdown)
	}()
	log.Printf("realtime_go.listening port=18040 build=%s", build)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal("listen_failed")
	}
	if ctx.Err() != nil {
		<-stopped
	}
}
