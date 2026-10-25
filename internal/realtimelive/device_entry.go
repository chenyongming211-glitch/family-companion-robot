package realtimelive

import (
	"encoding/json"
	"familyrobot/internal/voiceidentity"
	"io"
	"net/http"
	"strings"
)

func decodeEntry(r *http.Request, value any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(value); e != nil {
		return e
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return voiceidentity.ErrIdentity
	}
	return nil
}
func (s *Server) enrollDevice(w http.ResponseWriter, r *http.Request) {
	if s.initErr != nil {
		w.WriteHeader(503)
		return
	}
	if r.Header.Get("Origin") != "" {
		w.WriteHeader(403)
		return
	}
	var v struct {
		ClientType string `json:"client_type"`
	}
	if decodeEntry(r, &v) != nil || !voiceidentity.ValidEnrollmentClient(v.ClientType) {
		w.WriteHeader(400)
		return
	}
	device, credential, e := s.devices.Enroll(v.ClientType)
	if e != nil {
		w.WriteHeader(503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 201, map[string]any{"device_id": device.ID, "credential": credential, "client_type": device.ClientType})
}
func (s *Server) pairOwner(r *http.Request) (Ownership, int) {
	owner := Ownership{ClientType: "legacy_unattributed"}
	var v struct {
		DeviceID      string `json:"device_id"`
		ClientVersion string `json:"client_version"`
	}
	e := decodeEntry(r, &v)
	if e == io.EOF && r.Header.Get("Authorization") == "" {
		return owner, 200
	}
	if e != nil {
		return Ownership{}, 400
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return Ownership{}, 403
	}
	if !voiceidentity.VersionPattern.MatchString(v.ClientVersion) {
		return Ownership{}, 400
	}
	device, e := s.devices.Authenticate(v.DeviceID, strings.TrimPrefix(auth, "Bearer "))
	if e != nil || !voiceidentity.ValidEnrollmentClient(device.ClientType) {
		return Ownership{}, 403
	}
	return Ownership{DeviceID: device.ID, ClientType: device.ClientType, ClientVersion: v.ClientVersion}, 200
}
