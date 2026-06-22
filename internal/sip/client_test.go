package sip

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestByeIsSentOverUDP(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := NewClient("80082", "secret", server.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	trace := NewTraceBuffer(10)
	client.SetTrace(trace)

	received := make(chan Message, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := server.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		request, parseErr := Parse(buf[:n])
		if parseErr != nil {
			return
		}
		received <- request
		_, _ = server.WriteToUDP(Response(200, "OK", request, ""), peer)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = client.Bye(ctx, &Dialog{
		URI:     "sip:callee@127.0.0.1",
		CallID:  "call-123",
		FromTag: "from-tag",
		To:      "<sip:callee@example.com>;tag=to-tag",
		CSeq:    10,
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case request := <-received:
		if !strings.HasPrefix(request.StartLine, "BYE ") {
			t.Fatalf("start line = %q", request.StartLine)
		}
		if request.Header("call-id") != "call-123" {
			t.Fatalf("Call-ID = %q", request.Header("call-id"))
		}
		if !strings.Contains(request.Header("to"), "tag=to-tag") {
			t.Fatalf("To = %q", request.Header("to"))
		}
	case <-time.After(time.Second):
		t.Fatal("BYE was not received")
	}

	entries := trace.Query(0, "call-123")
	if len(entries) == 0 || !strings.HasPrefix(entries[0].Summary, "BYE ") {
		t.Fatalf("BYE missing from trace: %+v", entries)
	}
}

func TestByeIncludesDialogRoutes(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := NewClient("80082", "secret", server.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	received := make(chan Message, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := server.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		request, parseErr := Parse(buf[:n])
		if parseErr != nil {
			return
		}
		received <- request
		_, _ = server.WriteToUDP(Response(200, "OK", request, ""), peer)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = client.Bye(ctx, &Dialog{
		URI:      "sip:callee@127.0.0.1",
		CallID:   "call-routed",
		FromTag:  "from-tag",
		To:       "<sip:callee@example.com>;tag=to-tag",
		CSeq:     10,
		RouteSet: []string{"<sip:proxy.example.com;lr>", "<sip:edge.example.com;lr>"},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case request := <-received:
		routes := request.HeaderValues("route")
		if len(routes) != 2 {
			t.Fatalf("Route count = %d, want 2", len(routes))
		}
		if routes[0] != "<sip:proxy.example.com;lr>" || routes[1] != "<sip:edge.example.com;lr>" {
			t.Fatalf("Route = %+v", routes)
		}
	case <-time.After(time.Second):
		t.Fatal("BYE was not received")
	}
}

func TestInviteStopsRetransmittingAfterProvisionalResponse(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := NewClient("80082", "secret", server.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	serverResult := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := server.ReadFromUDP(buf)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		request, parseErr := Parse(buf[:n])
		if parseErr != nil {
			serverResult <- parseErr
			return
		}
		_, _ = server.WriteToUDP(Response(100, "Trying", request, ""), peer)

		_ = server.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
		n, _, readErr = server.ReadFromUDP(buf)
		if readErr == nil {
			duplicate, _ := Parse(buf[:n])
			serverResult <- fmt.Errorf("unexpected retransmission after 100 Trying: %s", duplicate.StartLine)
			return
		}
		if netErr, ok := readErr.(net.Error); !ok || !netErr.Timeout() {
			serverResult <- readErr
			return
		}

		_ = server.SetReadDeadline(time.Time{})
		_, _ = server.WriteToUDP(Response(486, "Busy Here", request, ""), peer)
		serverResult <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, _, err := client.Invite(ctx, "10086", "v=0\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != 486 {
		t.Fatalf("status = %d, want 486", response.StatusCode())
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestInviteWithCallIDUsesProvidedCallID(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := NewClient("80082", "secret", server.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	serverResult := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := server.ReadFromUDP(buf)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		request, parseErr := Parse(buf[:n])
		if parseErr != nil {
			serverResult <- parseErr
			return
		}
		if request.Header("call-id") != "fixed-call-id" {
			serverResult <- fmt.Errorf("Call-ID = %q, want fixed-call-id", request.Header("call-id"))
			return
		}
		_, _ = server.WriteToUDP(Response(486, "Busy Here", request, ""), peer)
		serverResult <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, _, err := client.InviteWithCallID(ctx, "10086", "v=0\r\n", "fixed-call-id")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode() != 486 {
		t.Fatalf("status = %d, want 486", response.StatusCode())
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestInviteStoresRecordRouteInDialog(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	client, err := NewClient("80082", "secret", server.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	serverResult := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := server.ReadFromUDP(buf)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		request, parseErr := Parse(buf[:n])
		if parseErr != nil {
			serverResult <- parseErr
			return
		}
		response := "SIP/2.0 200 OK\r\n" +
			"Via: " + request.Header("via") + "\r\n" +
			"From: " + request.Header("from") + "\r\n" +
			"To: <sip:10086@example.com>;tag=to-tag\r\n" +
			"Call-ID: " + request.Header("call-id") + "\r\n" +
			"CSeq: " + request.Header("cseq") + "\r\n" +
			"Contact: <sip:10086@127.0.0.1>\r\n" +
			"Record-Route: <sip:proxy.example.com;lr>, <sip:edge.example.com;lr>\r\n" +
			"Content-Length: 0\r\n\r\n"
		_, _ = server.WriteToUDP([]byte(response), peer)

		_ = server.SetReadDeadline(time.Now().Add(time.Second))
		n, _, readErr = server.ReadFromUDP(buf)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		ack, parseErr := Parse(buf[:n])
		if parseErr != nil {
			serverResult <- parseErr
			return
		}
		routes := ack.HeaderValues("route")
		if len(routes) != 2 {
			serverResult <- fmt.Errorf("ACK Route count = %d, want 2", len(routes))
			return
		}
		serverResult <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, dialog, err := client.Invite(ctx, "10086", "v=0\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if dialog == nil {
		t.Fatal("dialog is nil")
	}
	if len(dialog.RouteSet) != 2 {
		t.Fatalf("RouteSet count = %d, want 2", len(dialog.RouteSet))
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}
