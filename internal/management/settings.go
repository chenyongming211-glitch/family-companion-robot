package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"familyrobot/internal/deviceprofile"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errSettingsConflict = errors.New("settings conflict")

type DeviceSettingsDTO struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Version  string `json:"version"`
	CanEdit  bool   `json:"canEdit"`
}

func locationName(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", ErrInvalid
		}
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 24 {
		return "", ErrInvalid
	}
	return value, nil
}

func (s *Server) settings(c Config, actor, id string) (DeviceSettingsDTO, error) {
	if !device.MatchString(id) {
		return DeviceSettingsDTO{}, ErrInvalid
	}
	binding, err := c.Authorize(actor, "family", id, s.now(), false)
	if err != nil {
		return DeviceSettingsDTO{}, err
	}
	for _, d := range c.Devices {
		if d.ID != id {
			continue
		}
		name := d.Name
		if d.DisplayName != "" {
			name = d.DisplayName
		} else if s.opts.ProfilesPath != "" {
			store, err := deviceprofile.Open(s.opts.ProfilesPath)
			if err != nil {
				return DeviceSettingsDTO{}, ErrSource
			}
			profile, err := store.Resolve(id, d.Name)
			if err == nil {
				name = profile.Name
			} else if !errors.Is(err, deviceprofile.ErrNotFound) && !errors.Is(err, deviceprofile.ErrUnavailable) {
				return DeviceSettingsDTO{}, ErrSource
			}
		}
		// Only presentation/binding facts determine the optimistic version, never secrets.
		raw, _ := json.Marshal(struct {
			Device  DeviceConfig
			Name    string
			Binding Binding
		}{d, name, binding})
		hash := sha256.Sum256(raw)
		out := DeviceSettingsDTO{id, name, d.Location, hex.EncodeToString(hash[:]), false}
		a, _ := c.AccountByID(actor)
		for _, m := range a.Memberships {
			if m.FamilyID == binding.FamilyID && m.CanAddDevices {
				out.CanEdit = true
			}
		}
		return out, nil
	}
	return DeviceSettingsDTO{}, ErrNotFound
}

func (s *Server) deviceSettings(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if len(parts) != 3 || parts[2] == "" {
		sourceError(w, ErrNotFound)
		return
	}
	if r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "PUT" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	current, err := s.settings(requestConfig(r), a.ID, parts[2])
	if err != nil {
		sourceError(w, err)
		return
	}
	if r.Method == "GET" {
		writeJSON(w, current)
		return
	}
	if !current.CanEdit {
		sourceError(w, ErrForbidden)
		return
	}
	var in struct {
		Name     *string `json:"name"`
		Location *string `json:"location"`
		Version  *string `json:"version"`
	}
	var fields map[string]json.RawMessage
	if !decode(w, r, &fields) {
		return
	}
	// encoding/json matches struct keys case-insensitively. Decode exact keys
	// first so Name/name aliases cannot silently override a submitted field.
	if len(fields) != 3 || fields["name"] == nil || fields["location"] == nil || fields["version"] == nil ||
		json.Unmarshal(fields["name"], &in.Name) != nil || json.Unmarshal(fields["location"], &in.Location) != nil || json.Unmarshal(fields["version"], &in.Version) != nil ||
		in.Name == nil || in.Location == nil || in.Version == nil || !digest.MatchString(*in.Version) {
		sourceError(w, ErrInvalid)
		return
	}
	name, err := deviceprofile.NormalizeName(*in.Name)
	if err != nil {
		sourceError(w, ErrInvalid)
		return
	}
	location, err := locationName(*in.Location)
	if err != nil {
		sourceError(w, err)
		return
	}
	var out DeviceSettingsDTO
	err = (ConfigStore{s.opts.ConfigPath}).Transaction(func(c *Config) error {
		// Permission and session are checked again after acquiring the writer lock.
		freshRequest := r.WithContext(context.WithValue(r.Context(), configContextKey{}, *c))
		freshActor, ok := s.account(freshRequest, "family")
		if !ok || freshActor.ID != a.ID {
			return ErrForbidden
		}
		latest, err := s.settings(*c, a.ID, parts[2])
		if err != nil {
			return err
		}
		if !latest.CanEdit {
			return ErrForbidden
		}
		if latest.Version != *in.Version {
			return errSettingsConflict
		}
		if err = s.auditAction(a.ID, "family", "update_device_settings_requested", parts[2], ""); err != nil {
			return err
		}
		for i := range c.Devices {
			if c.Devices[i].ID == parts[2] {
				c.Devices[i].DisplayName = name
				c.Devices[i].Location = location
			}
		}
		out, err = s.settings(*c, a.ID, parts[2])
		return err
	})
	if errors.Is(err, errSettingsConflict) {
		apiError(w, 409, "settings_conflict", "设置已被修改，请刷新后重试")
		return
	}
	if err != nil {
		sourceError(w, err)
		return
	}
	writeJSON(w, out)
}
