package sip

import (
	"strings"
	"testing"
)

func TestParseResponse(t *testing.T) {
	raw := "SIP/2.0 401 Unauthorized\r\n" +
		"Call-ID: abc\r\n" +
		"CSeq: 7 REGISTER\r\n" +
		"WWW-Authenticate: Digest realm=\"vos\", nonce=\"123\"\r\n" +
		"Content-Length: 0\r\n\r\n"
	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.StatusCode() != 401 {
		t.Fatalf("status = %d", msg.StatusCode())
	}
	if msg.Header("call-id") != "abc" {
		t.Fatalf("call-id = %q", msg.Header("call-id"))
	}
}

func TestDigestWithoutQOP(t *testing.T) {
	got, err := DigestAuthorization(
		`Digest realm="testrealm@host.com", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093"`,
		"Mufasa", "Circle Of Life", "GET", "/dir/index.html",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `response="670fd8c2df070c60b045671b8b24ff02"`) {
		t.Fatalf("unexpected digest: %s", got)
	}
}

func TestDigestWithQOP(t *testing.T) {
	got, err := DigestAuthorization(
		`Digest realm="vos", nonce="abc", qop="auth,auth-int"`,
		"80082", "secret", "REGISTER", "sip:example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"qop=auth", "nc=00000001", `cnonce="`} {
		if !strings.Contains(got, want) {
			t.Fatalf("digest %q does not contain %q", got, want)
		}
	}
}

func TestRequestContentLength(t *testing.T) {
	packet := string(Request("INVITE", "sip:100@example.com", nil, "hello"))
	if !strings.Contains(packet, "Content-Length: 5\r\n\r\nhello") {
		t.Fatalf("unexpected packet: %q", packet)
	}
}

func TestResponseCopiesDialogHeaders(t *testing.T) {
	request, err := Parse([]byte("BYE sip:80082@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.10:5060;branch=z9\r\n" +
		"From: <sip:100@example.com>;tag=from\r\n" +
		"To: <sip:80082@example.com>;tag=to\r\n" +
		"Call-ID: call-1\r\n" +
		"CSeq: 9 BYE\r\nContent-Length: 0\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	response := string(Response(200, "OK", request, ""))
	for _, want := range []string{
		"SIP/2.0 200 OK",
		"Via: SIP/2.0/UDP 192.0.2.10:5060;branch=z9",
		"Call-ID: call-1",
		"CSeq: 9 BYE",
	} {
		if !strings.Contains(response, want) {
			t.Fatalf("response %q does not contain %q", response, want)
		}
	}
}
