package realtimelive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"familyrobot/internal/voiceidentity"
)

var factoryDeviceID = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)
var factorySerial = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Only the configured, literal loopback authority may answer factory-device
// admission. The public client cannot select an authority or record owner.
func validOnboardingAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Opaque != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "/api/internal/device-auth" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	return ip != nil && ip.IsLoopback() && err == nil && port > 0 && port <= 65535
}

func (c DeviceConfig) WithOnboardingAuthURL(raw string) (DeviceConfig, error) {
	c.OnboardingAuthURL = raw
	return validateDeviceConfig(c)
}

type onboardingAuthority struct {
	endpoint string
	client   *http.Client
}

func newOnboardingAuthority(endpoint string) *onboardingAuthority {
	if endpoint == "" {
		return nil
	}
	return &onboardingAuthority{endpoint: endpoint, client: &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: time.Second}).DialContext,
			ResponseHeaderTimeout:  time.Second,
			MaxResponseHeaderBytes: 4096,
			MaxIdleConnsPerHost:    4,
			MaxIdleConns:           4,
			IdleConnTimeout:        30 * time.Second,
		},
	}}
}

func validFactoryBearer(header string) (string, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if len(token) < 32 || len(token) > 512 {
		return "", false
	}
	for _, c := range []byte(token) {
		if c <= 32 || c >= 127 {
			return "", false
		}
	}
	return token, true
}

func (a *onboardingAuthority) authenticate(ctx context.Context, mac, token string) (voiceidentity.Device, int) {
	raw, _ := json.Marshal(map[string]string{"mac": mac, "token": token})
	r, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(raw))
	if err != nil {
		return voiceidentity.Device{}, http.StatusServiceUnavailable
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(r)
	if err != nil {
		return voiceidentity.Device{}, http.StatusServiceUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return voiceidentity.Device{}, http.StatusForbidden
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != 200 || mediaErr != nil || mediaType != "application/json" {
		return voiceidentity.Device{}, http.StatusServiceUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return voiceidentity.Device{}, http.StatusServiceUnavailable
	}
	d, err := decodeFactoryIdentity(body)
	if err != nil {
		return voiceidentity.Device{}, http.StatusServiceUnavailable
	}
	return d, 0
}

func decodeFactoryIdentity(raw []byte) (voiceidentity.Device, error) {
	invalid := errors.New("factory_identity_invalid")
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return voiceidentity.Device{}, invalid
	}
	fields := map[string]string{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || (key != "device_id" && key != "serial" && key != "model") {
			return voiceidentity.Device{}, invalid
		}
		if _, exists := fields[key]; exists {
			return voiceidentity.Device{}, invalid
		}
		var value string
		if d.Decode(&value) != nil {
			return voiceidentity.Device{}, invalid
		}
		fields[key] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return voiceidentity.Device{}, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF || !factoryDeviceID.MatchString(fields["device_id"]) || !factorySerial.MatchString(fields["serial"]) || (fields["model"] != "微雪 ESP32-S3" && fields["model"] != "M5 ESP32-S3") {
		return voiceidentity.Device{}, invalid
	}
	return voiceidentity.Device{ID: fields["device_id"], ClientType: "xiaozhi"}, nil
}
