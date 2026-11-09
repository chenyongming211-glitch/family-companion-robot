package realtimelive

import (
	"context"
	"errors"
	"testing"
)

func TestPlaybackWindowUsesCommonAuthenticatedRobotPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, device, client string
		ownerErr             error
		wantExtended         bool
	}{
		{"robot one", "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "xiaozhi", nil, true},
		{"robot two", "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "xiaozhi", nil, true},
		{"missing owner", "", "xiaozhi", nil, false},
		{"malformed owner", "untrusted", "xiaozhi", nil, false},
		{"invalid manifest", "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "xiaozhi", errors.New("invalid_session_manifest"), false},
		{"native client", "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "mac_fixture", nil, false},
		{"missing client", "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{ctx: context.Background(), ownerErr: tc.ownerErr, rec: recorder{owner: Ownership{DeviceID: tc.device, ClientType: tc.client}}}
			for i := 0; i < 3; i++ {
				rt := s.addTurn()
				t.Cleanup(rt.cancel)
				rt.turn.Sent = 7200
				if rt.turn.CanSend() != tc.wantExtended {
					t.Fatalf("turn %d: common authenticated robot policy extended=%v, want %v", i, rt.turn.CanSend(), tc.wantExtended)
				}
				rt.turn.Sent = 14400
				if rt.turn.CanSend() {
					t.Fatal("playback credit exceeded 600ms")
				}
			}
		})
	}
}
