package realtimestate

import "testing"

func TestExtendedWindowRetainsReceiptAndDrainBounds(t *testing.T) {
	turn := NewTurn(1)
	turn.PlaybackWindowSamples = ExtendedWindowSamples
	for turn.CanSend() {
		turn.Sent += PacketSamples
	}
	if turn.Sent != 14400 {
		t.Fatalf("600ms window stopped at %d samples", turn.Sent)
	}
	if turn.ReceiptWithBasis(map[string]any{"id": 1, "completed": 15840, "accepted": 15840, "basis": "i2s_dma"}, "i2s_dma") {
		t.Fatal("unsent audio credited")
	}
	if !turn.ReceiptWithBasis(map[string]any{"id": 1, "completed": 1440, "accepted": 14400, "basis": "i2s_dma"}, "i2s_dma") || !turn.CanSend() {
		t.Fatal("valid DMA completion did not release credit")
	}
	turn.SealForDrain()
	if turn.CanSend() || *turn.DrainTarget != 14400 {
		t.Fatal("extended window changed the cut or drain target")
	}
	if !turn.ReceiptWithBasis(map[string]any{"id": 1, "completed": 14400, "accepted": 14400, "basis": "i2s_dma", "terminal": "drained"}, "i2s_dma") {
		t.Fatal("exact drain completion rejected")
	}
	select {
	case <-turn.DrainDone:
	default:
		t.Fatal("drain completion was not signalled")
	}
}

func TestUnconfiguredAndInvalidWindowsKeepDefault(t *testing.T) {
	for _, window := range []int64{0, -1, 7200, 14401, 1 << 60} {
		turn := NewTurn(1)
		turn.PlaybackWindowSamples = window
		turn.Sent = WindowSamples
		if turn.CanSend() {
			t.Fatalf("unsupported window %d changed the default bound", window)
		}
	}
}
