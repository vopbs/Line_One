package media

import "testing"

func TestAcceptsCodec(t *testing.T) {
	tests := []struct {
		name  string
		sdp   string
		codec Codec
		want  bool
	}{
		{"pcmu", "m=audio 19000 RTP/AVP 0 101\r\n", PCMU, true},
		{"pcma", "m=audio 19000 RTP/AVP 8 101\r\n", PCMA, true},
		{"not offered", "m=audio 19000 RTP/AVP 8 101\r\n", PCMU, false},
		{"rejected", "m=audio 0 RTP/AVP 0\r\n", PCMU, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AcceptsCodec(test.sdp, test.codec); got != test.want {
				t.Fatalf("AcceptsCodec() = %v, want %v", got, test.want)
			}
		})
	}
}
