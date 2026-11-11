package realtimelive

import (
	"errors"
	"familyrobot/internal/deviceauth"
	"familyrobot/internal/protocol"
	"familyrobot/internal/voiceidentity"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type DeviceConfig struct {
	Token, AllowedDevice string
	RegistryPath         string
	OnboardingAuthURL    string
	registry             *deviceauth.Registry
}

// LoadDeviceConfig reads only the device-auth keys and optional registry path. In particular,
// OTA bootstrap URLs and provider credentials never enter this adapter.
func LoadDeviceConfig(path string) (DeviceConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return DeviceConfig{}, errors.New("device_config_unavailable")
	}
	c := DeviceConfig{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "ROBOT_DEVICE_TOKEN" && key != "ROBOT_ALLOWED_DEVICE" && key != "ROBOT_DEVICE_REGISTRY" && key != "ROBOT_ONBOARDING_AUTH_URL") {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			if len(value) < 2 || value[len(value)-1] != value[0] {
				return DeviceConfig{}, errors.New("device_config_invalid")
			}
			value = value[1 : len(value)-1]
		}
		if strings.ContainsAny(value, "\x00\r\n`$\\ \t") {
			return DeviceConfig{}, errors.New("device_config_invalid")
		}
		if key == "ROBOT_DEVICE_TOKEN" {
			c.Token = value
		} else if key == "ROBOT_ALLOWED_DEVICE" {
			c.AllowedDevice = value
		} else if key == "ROBOT_DEVICE_REGISTRY" {
			c.RegistryPath = value
		} else {
			c.OnboardingAuthURL = value
		}
	}
	return validateDeviceConfig(c)
}
func validateDeviceConfig(c DeviceConfig) (DeviceConfig, error) {
	mac, err := net.ParseMAC(c.AllowedDevice)
	if err != nil || len(mac) != 6 || len(c.Token) < 32 || len(c.Token) > 512 || strings.ContainsAny(c.Token, "\x00\r\n \t") {
		return DeviceConfig{}, errors.New("device_config_invalid")
	}
	c.AllowedDevice = mac.String()
	if c.OnboardingAuthURL != "" && !validOnboardingAuthURL(c.OnboardingAuthURL) {
		return DeviceConfig{}, errors.New("device_config_invalid")
	}
	if c.registry == nil {
		var err error
		if c.RegistryPath != "" {
			c.registry, err = deviceauth.Load(c.RegistryPath, c.AllowedDevice, c.Token)
		} else {
			c.registry, err = deviceauth.Single(c.AllowedDevice, c.Token)
		}
		if err != nil {
			return DeviceConfig{}, errors.New("device_config_invalid")
		}
	}
	return c, nil
}

// DeviceHandler intentionally contains no registration, pairing, legacy ticket
// or Mac routes. It may be bound publicly while Handler remains loopback-only.
func (s *Server) DeviceHandler(config DeviceConfig) http.Handler {
	cfg, configErr := validateDeviceConfig(config)
	authority := newOnboardingAuthority(cfg.OnboardingAuthURL)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /xiaozhi/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xiaozhi/v1/" {
			http.NotFound(w, r)
			return
		}
		trace := s.traceConnection(r)
		trace.stage(connectionEntry, 0)
		reject := func(stage connectionStage, status int) {
			trace.stage(stage, status)
			w.WriteHeader(status)
		}
		if configErr != nil || s.initErr != nil {
			reject(connectionAuth, 503)
			return
		}
		if r.Header.Get("Origin") != "" {
			reject(connectionAuth, 403)
			return
		}
		mac, err := net.ParseMAC(r.Header.Get("Device-Id"))
		if err != nil || len(mac) != 6 {
			reject(connectionAuth, 403)
			return
		}
		entry, registered := cfg.registry.Lookup(mac.String())
		if !registered && authority == nil {
			reject(connectionAuth, 403)
			return
		}
		var device voiceidentity.Device
		if registered {
			if _, ok := cfg.registry.Authenticate(mac.String(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); !ok || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				reject(connectionAuth, 401)
				return
			}
		} else {
			token, ok := validFactoryBearer(r.Header.Get("Authorization"))
			if !ok {
				reject(connectionAuth, 401)
				return
			}
			var status int
			device, status = authority.authenticate(r.Context(), mac.String(), token)
			if status != 0 {
				reject(connectionAuth, status)
				return
			}
		}
		trace.stage(connectionAuth, http.StatusOK)
		version := r.Header.Get("Protocol-Version")
		if version == "" {
			version = "1"
		}
		if version != "1" && version != "2" && version != "3" {
			reject(connectionVersion, 400)
			return
		}
		trace.stage(connectionVersion, http.StatusOK)
		if registered {
			var err error
			device, err = s.devices.BindXiaozhi(mac.String(), entry.Token)
			if err != nil {
				reject(connectionAdmission, 503)
				return
			}
		}
		s.mu.Lock()
		lease := s.reserveLocked(device.ID)
		s.mu.Unlock()
		if lease == nil {
			reject(connectionAdmission, 409)
			return
		}
		trace.stage(connectionAdmission, http.StatusOK)
		defer s.releaseLease(lease)
		upgradeStatus := 0
		upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 8192, WriteBufferSize: 8192, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
		if trace != nil {
			// Preserve gorilla v1.5.3's default rejection headers/body exactly.
			upgrader.Error = func(w http.ResponseWriter, r *http.Request, status int, _ error) {
				upgradeStatus = status
				w.Header().Set("Sec-Websocket-Version", "13")
				http.Error(w, http.StatusText(status), status)
			}
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			trace.stage(connectionUpgrade, upgradeStatus)
			return
		}
		trace.stage(connectionUpgrade, http.StatusSwitchingProtocols)
		defer ws.Close()
		if !s.attachLease(lease, ws) {
			trace.stage(connectionDisconnect, websocket.CloseGoingAway)
			return
		}
		ws.SetReadLimit(16384)
		ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		kind, b, err := ws.ReadMessage()
		if err != nil {
			trace.stage(connectionHello, connectionReadStatus(err))
			return
		}
		if kind != websocket.TextMessage {
			trace.stage(connectionHello, websocket.CloseUnsupportedData)
			return
		}
		hello, err := protocol.ParseRealtimeHello(b, int(version[0]-'0'))
		if err != nil {
			trace.stage(connectionHello, websocket.ClosePolicyViolation)
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, err.Error()), time.Now().Add(time.Second))
			return
		}
		if !hello.Features["playback_progress_v1"] || !hello.Features["playback_drain_v1"] {
			trace.stage(connectionHello, websocket.ClosePolicyViolation)
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "playback_drain_required"), time.Now().Add(time.Second))
			return
		}
		trace.stage(connectionHello, http.StatusOK)
		owner := Ownership{DeviceID: device.ID, ClientType: "xiaozhi"}
		// Firmware version is advisory. The authenticated binding, never
		// Client-Id or a hello-supplied identity, determines the record owner.
		if v := r.Header.Get("Client-Version"); voiceidentity.VersionPattern.MatchString(v) {
			owner.ClientVersion = v
		}
		dir, err := s.sessionDirectory(owner)
		if err != nil {
			trace.stage(connectionSession, http.StatusInternalServerError)
			return
		}
		session := s.newSession(ws, dir)
		session.inputFrame = hello.AudioParams.FrameDuration * 16
		session.wire = newXiaozhiWire(ws, hello.Version, hello.AudioParams.FrameDuration, session.rec.owner.SessionID)
		session.wire.(*xiaozhiWire).mcp = hello.Features["mcp"]
		session.wire.(*xiaozhiWire).pcm = hello.AudioParams.Format == "pcm_s16le"
		session.wire.(*xiaozhiWire).face = hello.Features["face_state_v1"]
		session.wire.(*xiaozhiWire).expression = session.cfg.ExpressionsEnabled && hello.Features["face_expression_v1"]
		s.attachDiagnostics(session)
		session.enableDeviceControls()
		session.serverStopping = func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
		trace.session(session.rec.owner.SessionID)
		trace.stage(connectionSession, http.StatusOK)
		s.runDevice(session)
		trace.stage(connectionDisconnect, 0)
	})
	return http.MaxBytesHandler(mux, 16384)
}
