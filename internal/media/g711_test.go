package media

import "testing"

func TestG711SilenceValues(t *testing.T) {
	if got := EncodePCMU(0); got != 0xff {
		t.Fatalf("PCMU silence = %#x, want 0xff", got)
	}
	if got := EncodePCMA(0); got != 0xd5 {
		t.Fatalf("PCMA silence = %#x, want 0xd5", got)
	}
}
