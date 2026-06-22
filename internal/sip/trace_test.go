package sip

import (
	"strings"
	"testing"
)

func TestTraceRedactsAuthorizationAndFiltersCallID(t *testing.T) {
	trace := NewTraceBuffer(10)
	trace.Add("out", "127.0.0.1:5060", []byte(
		"REGISTER sip:example.com SIP/2.0\r\n"+
			"Call-ID: register-call\r\n"+
			"Authorization: Digest username=\"80082\", response=\"secret\"\r\n\r\n",
	))
	trace.Add("out", "127.0.0.1:5060", []byte(
		"INVITE sip:100@example.com SIP/2.0\r\nCall-ID: invite-call\r\n\r\n",
	))

	register := trace.Query(0, "register-call")
	if len(register) != 1 {
		t.Fatalf("register entries = %d", len(register))
	}
	if strings.Contains(register[0].Message, "response=\"secret\"") ||
		!strings.Contains(register[0].Message, "Authorization: [REDACTED]") {
		t.Fatalf("authorization was not redacted: %s", register[0].Message)
	}
	if entries := trace.Query(0, "invite-call"); len(entries) != 1 || entries[0].Summary[:6] != "INVITE" {
		t.Fatalf("unexpected invite trace: %+v", entries)
	}
}
