package realtimelive

import (
	"context"
	"testing"
)

func TestPlaybackWindowUsesAuthenticatedDeviceOnly(t *testing.T) {
	selected := "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, configured, device, client string
		wantExtended                     bool
	}{
		{"selected robot", selected, selected, "xiaozhi", true},
		{"disabled", "", selected, "xiaozhi", false},
		{"another robot", selected, "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "xiaozhi", false},
		{"missing owner", selected, "", "xiaozhi", false},
		{"mac using same owner", selected, selected, "mac_fixture", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{ctx: context.Background(), cfg: Config{ExtendedPlaybackDevice: tc.configured}, rec: recorder{owner: Ownership{DeviceID: tc.device, ClientType: tc.client}}}
			for i := 0; i < 2; i++ {
				rt := s.addTurn()
				t.Cleanup(rt.cancel)
				rt.turn.Sent = 7200
				if rt.turn.CanSend() != tc.wantExtended {
					t.Fatal("playback window leaked outside the selected robot or was not applied to a new turn")
				}
			}
		})
	}
}

func TestPlaybackWindowRejectsMalformedSelector(t *testing.T) {
	app := NewServer(Config{ExtendedPlaybackDevice: "untrusted"}, nil, t.TempDir(), "test")
	if app.InitializationError() == nil {
		t.Fatal("invalid authenticated device selector accepted")
	}
}
