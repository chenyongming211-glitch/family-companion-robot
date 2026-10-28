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
		if !ok || (key != "ROBOT_DEVICE_TOKEN" && key != "ROBOT_ALLOWED_DEVICE" && key != "ROBOT_DEVICE_REGISTRY") {
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
		} else {
			c.RegistryPath = value
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /xiaozhi/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xiaozhi/v1/" {
			http.NotFound(w, r)
			return
		}
		if configErr != nil || s.initErr != nil {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(403)
			return
		}
		mac, err := net.ParseMAC(r.Header.Get("Device-Id"))
		if err != nil || len(mac) != 6 {
			w.WriteHeader(403)
			return
		}
		entry, registered := cfg.registry.Lookup(mac.String())
		if !registered {
			w.WriteHeader(403)
			return
		}
		if _, ok := cfg.registry.Authenticate(mac.String(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); !ok || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(401)
			return
		}
		version := r.Header.Get("Protocol-Version")
		if version == "" {
			version = "1"
		}
		if version != "1" && version != "2" && version != "3" {
			w.WriteHeader(400)
			return
		}
		device, err := s.devices.BindXiaozhi(mac.String(), entry.Token)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		s.mu.Lock()
		lease := s.reserveLocked(device.ID)
		s.mu.Unlock()
		if lease == nil {
			w.WriteHeader(409)
			return
		}
		defer s.releaseLease(lease)
		ws, err := (&websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 8192, WriteBufferSize: 8192, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if !s.attachLease(lease, ws) {
			return
		}
		ws.SetReadLimit(16384)
		ws.SetReadDeadline(time.Now().Add(8 * time.Second))
		kind, b, err := ws.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			return
		}
		hello, err := protocol.ParseHello(b, int(version[0]-'0'))
		if err != nil || !hello.Features["playback_progress_v1"] || !hello.Features["playback_drain_v1"] {
			ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "playback_drain_required"), time.Now().Add(time.Second))
			return
		}
		owner := Ownership{DeviceID: device.ID, ClientType: "xiaozhi"}
		// Firmware version is advisory. The authenticated binding, never
		// Client-Id or a hello-supplied identity, determines the record owner.
		if v := r.Header.Get("Client-Version"); voiceidentity.VersionPattern.MatchString(v) {
			owner.ClientVersion = v
		}
		dir, err := s.sessionDirectory(owner)
		if err != nil {
			return
		}
		session := NewSession(ws, s.cfg, dir, s.product)
		s.attachMemory(session)
		session.inputFrame = hello.AudioParams.FrameDuration * 16
		session.wire = newXiaozhiWire(ws, hello.Version, hello.AudioParams.FrameDuration, session.rec.owner.SessionID)
		session.wire.(*xiaozhiWire).mcp = hello.Features["mcp"]
		session.enableDeviceControls()
		session.serverStopping = func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
		s.runDevice(session)
	})
	return http.MaxBytesHandler(mux, 16384)
}
