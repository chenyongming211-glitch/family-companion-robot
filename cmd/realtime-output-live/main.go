// Standalone Go parity entry; Python and device gateways are independent.
package main

import (
	"context"
	"errors"
	"familyrobot/internal/management"
	"familyrobot/internal/realtimelive"
	"familyrobot/internal/realtimestate"
	"familyrobot/internal/speakerlive"
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
	onboardingAuth := flag.String("onboarding-auth", "", "optional literal loopback factory admission authority")
	maxSessions := flag.Int("max-sessions", 1, "maximum simultaneous terminal sessions (1-8)")
	deviceProfiles := flag.String("device-profiles", "", "optional private device presentation profile JSON")
	musicLibrary := flag.String("music-library", "", "optional private family music library root")
	musicOnline := flag.Bool("music-online", false, "enable complete publicly playable online songs")
	memory := flag.Bool("memory", true, "restore bounded recent conversation memory per authenticated device")
	parallelPlanning := flag.Bool("parallel-planning", false, "overlap silent music and device control planning (candidate)")
	expressions := flag.Bool("expressions", false, "enable silent expression selection for negotiated face_expression_v1 devices")
	diagnosticDevice := flag.String("diagnostics-device", "", "optional authenticated device ID for one diagnostic session")
	diagnosticDuration := flag.Duration("diagnostics-duration", 5*time.Minute, "diagnostic window, maximum 5m")
	memorySocket := flag.String("memory-control-socket", "", "optional root-private memory control Unix socket")
	managementConfig := flag.String("management-config", "", "private management config for voice routes, memory control or family music")
	devVoiceRoute := flag.String("dev-voice-route", "", "development terminal only: force A or B for a device that has no route row")
	speakerBinary := flag.String("speaker-binary", "", "optional Linux speaker observer helper executable")
	speakerModel := flag.String("speaker-model", "", "optional speaker embedding ONNX model path")
	speakerSHA := flag.String("speaker-sha256", "", "optional speaker embedding ONNX model SHA-256")
	flag.Parse()
	if (*speakerBinary != "" || *speakerModel != "" || *speakerSHA != "") && (*speakerBinary == "" || *speakerModel == "" || *speakerSHA == "") {
		log.Fatal("speaker_config_invalid")
	}
	if (*memorySocket != "" || *musicLibrary != "") && *managementConfig == "" {
		log.Fatal("memory_control_config_invalid")
	}
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
	if e = configureMusic(&cfg, *musicOnline, *musicLibrary, *managementConfig, *memorySocket); e != nil {
		log.Fatal("music_config_invalid")
	}
	if e = configureVoiceRoutes(&cfg, *managementConfig, *devVoiceRoute); e != nil {
		log.Fatal("voice_route_config_invalid")
	}
	cfg.MaxSessions = *maxSessions
	cfg.MemoryEnabled = *memory
	cfg.ExpressionsEnabled = *expressions
	cfg.ParallelPlanning = *parallelPlanning
	cfg.DeviceProfilesPath = *deviceProfiles
	cfg.DiagnosticsDevice = *diagnosticDevice
	cfg.DiagnosticsDuration = *diagnosticDuration
	if *speakerBinary != "" {
		cfg.Speaker = &speakerlive.Config{Binary: *speakerBinary, Model: *speakerModel, SHA256: *speakerSHA}
	}
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
		if *onboardingAuth != "" {
			dc, err = dc.WithOnboardingAuthURL(*onboardingAuth)
			if err != nil {
				log.Fatal("onboarding_authority_invalid")
			}
		}
		servers = append(servers, &http.Server{Addr: *deviceListen, Handler: app.DeviceHandler(dc), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384})
	}
	var memoryListener net.Listener
	if *memorySocket != "" {
		if _, err := management.LoadConfig(*managementConfig); err != nil {
			log.Fatal("memory_control_config_invalid")
		}
		var err error
		memoryListener, err = realtimelive.ListenMemoryControl(*memorySocket)
		if err != nil {
			log.Fatal("memory_control_listen_failed")
		}
		defer memoryListener.Close()
		servers = append(servers, &http.Server{Handler: privateControlHandler(app, *managementConfig), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192})
	}
	// Bind every requested listener before accepting either client kind. A
	// failed device bind must not leave a partially configured running process.
	listeners := make([]net.Listener, 0, len(servers))
	for i, srv := range servers {
		if memoryListener != nil && i == len(servers)-1 {
			listeners = append(listeners, memoryListener)
			continue
		}
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

// The existing Unix listener is the sole transport for both private controls.
// Exact dispatch avoids adding any private operation to the public handlers.
func privateControlHandler(app *realtimelive.Server, configPath string) http.Handler {
	memory := app.MemoryControlHandler(management.NewMemoryClearGuard(configPath))
	routes := app.VoiceRouteControlHandler(management.NewVoiceRouteGuard(configPath))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/memory/clear":
			memory.ServeHTTP(w, r)
		case "/voice-route/status", "/voice-route/disconnect":
			routes.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
func configureVoiceRoutes(cfg *realtimelive.Config, configPath, devRoute string) error {
	if configPath == "" {
		return nil
	}
	if _, err := management.LoadConfig(configPath); err != nil {
		return err
	}
	resolve, err := withDevVoiceRoute(management.NewVoiceRouteResolver(configPath), devRoute)
	if err != nil {
		return err
	}
	cfg.VoiceRouteForDevice = resolve
	if devRoute != "" {
		log.Printf("realtime_go.dev_voice_route_forced route=%s", devRoute)
	}
	return nil
}

// withDevVoiceRoute answers for a device that has no route row, so the
// development terminal can reach A and B at all. Those two routes exist because
// some conversation paths were smoother than others, and until now only a
// physical device could exercise them: the terminal is not a managed family
// device, so the resolver reports it unknown and voice_routes.go falls back to C.
//
// The override answers only that case. Every provisioned device keeps its own
// persisted choice, so this cannot move a family's device off its route.
func withDevVoiceRoute(resolve func(string) (string, error), devRoute string) (func(string) (string, error), error) {
	if devRoute == "" {
		return resolve, nil
	}
	if devRoute != "A" && devRoute != "B" && devRoute != "C" {
		return nil, errors.New("invalid_dev_voice_route")
	}
	return func(id string) (string, error) {
		route, err := resolve(id)
		if errors.Is(err, realtimelive.ErrVoiceRouteUnknown) {
			return devRoute, nil
		}
		return route, err
	}, nil
}
