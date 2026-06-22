package gateway

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip/internal/media"
	"webrtc-sip/internal/sip"
)

func TestRegisterBlockedOnlyDuringActiveCall(t *testing.T) {
	for _, state := range []string{"connecting", "connected"} {
		t.Run(state, func(t *testing.T) {
			g := &Gateway{callState: state}
			request := httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewBufferString(
				`{"server":"127.0.0.1","username":"80082","password":"secret"}`,
			))
			response := httptest.NewRecorder()
			g.handleRegister(response, request)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
			}
		})
	}
}

func TestFailedCallCanBeClearedBeforeRegistration(t *testing.T) {
	g := &Gateway{callState: "failed", lastError: "previous call failed"}
	if g.callActiveLocked() {
		t.Fatal("failed call incorrectly reported as active")
	}
	g.clearCallLocked()
	if g.callState != "idle" {
		t.Fatalf("call state = %q, want idle", g.callState)
	}
}

func TestStaleCallFailureDoesNotOverwriteCurrentState(t *testing.T) {
	g := &Gateway{callState: "idle", callSeq: 4}
	g.failCall(3, errTest)
	if g.callState != "idle" {
		t.Fatalf("stale call changed state to %q", g.callState)
	}
}

func TestFailedCallKeepsInviteCallID(t *testing.T) {
	trace := sip.NewTraceBuffer(10)
	trace.Add("out", "127.0.0.1:5060", []byte("INVITE sip:10086@127.0.0.1 SIP/2.0\r\nCall-ID: failed-call\r\nCSeq: 1 INVITE\r\n\r\n"))
	g := &Gateway{callState: "connecting", callSeq: 4, trace: trace}
	g.failCall(4, errTest)
	if g.callState != "failed" {
		t.Fatalf("call state = %q, want failed", g.callState)
	}
	if g.callID != "failed-call" {
		t.Fatalf("call ID = %q, want failed-call", g.callID)
	}
}

func TestLogoutClearsIdleRegistration(t *testing.T) {
	g := &Gateway{
		callState:  "failed",
		registered: true,
		sipServer:  "example.com:5060",
		sipUser:    "80082",
	}
	request := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	response := httptest.NewRecorder()
	g.handleLogout(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if g.registered || g.sipServer != "" || g.sipUser != "" || g.callState != "idle" {
		t.Fatalf("logout did not clear state: %+v", g)
	}
}

func TestLogoutBlockedDuringCall(t *testing.T) {
	g := &Gateway{callState: "connected", registered: true}
	request := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	response := httptest.NewRecorder()
	g.handleLogout(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
	if !g.registered {
		t.Fatal("active call logout cleared registration")
	}
}

func TestAudioUploadStoresPCM(t *testing.T) {
	g := &Gateway{}
	raw := make([]byte, 6)
	values := []int16{100, -200, 300}
	for i, value := range values {
		binary.LittleEndian.PutUint16(raw[i*2:i*2+2], uint16(value))
	}
	request := httptest.NewRequest(http.MethodPost, "/api/audio", bytes.NewReader(raw))
	response := httptest.NewRecorder()
	g.handleAudio(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if len(g.audioPCM) != 3 || g.audioPCM[0] != 100 || g.audioPCM[1] != -200 || g.audioPCM[2] != 300 {
		t.Fatalf("PCM = %v", g.audioPCM)
	}
}

func TestAudioUploadRejectedDuringCall(t *testing.T) {
	g := &Gateway{callState: "connected"}
	request := httptest.NewRequest(http.MethodPost, "/api/audio", bytes.NewReader([]byte{0, 0}))
	response := httptest.NewRecorder()
	g.handleAudio(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
}

func TestAnnouncementCompletionSendsBye(t *testing.T) {
	sipServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sipServer.Close()
	rtpSink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer rtpSink.Close()
	rtpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer rtpConn.Close()
	client, err := sip.NewClient("80082", "secret", sipServer.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	byeReceived := make(chan struct{}, 1)
	rtpReceived := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := sipServer.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		request, parseErr := sip.Parse(buf[:n])
		if parseErr != nil || !strings.HasPrefix(request.StartLine, "BYE ") {
			return
		}
		_, _ = sipServer.WriteToUDP(sip.Response(200, "OK", request, ""), peer)
		byeReceived <- struct{}{}
	}()
	go func() {
		buf := make([]byte, 2048)
		_ = rtpSink.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, readErr := rtpSink.ReadFromUDP(buf)
		if readErr == nil {
			rtpReceived <- append([]byte(nil), buf[:n]...)
		}
	}()

	rtpReady := make(chan struct{})
	close(rtpReady)
	g := &Gateway{
		sip: client, rtpConn: rtpConn, callState: "connected", callSeq: 7,
		sipTarget: rtpSink.LocalAddr().(*net.UDPAddr),
		rtpReady:  rtpReady,
		dialog: &sip.Dialog{
			URI: "sip:callee@127.0.0.1", CallID: "announcement-call",
			FromTag: "from", To: "<sip:callee@example.com>;tag=to",
		},
		codec: media.PCMU, audioPCM: make([]int16, 160),
	}
	go g.playAnnouncement(7)

	select {
	case raw := <-rtpReceived:
		var packet rtp.Packet
		if err := packet.Unmarshal(raw); err != nil {
			t.Fatalf("decode RTP: %v", err)
		}
		if packet.PayloadType != media.PCMU.PayloadType {
			t.Fatalf("RTP payload type = %d, want %d", packet.PayloadType, media.PCMU.PayloadType)
		}
		if len(packet.Payload) != 160 {
			t.Fatalf("RTP payload length = %d, want 160", len(packet.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("announcement did not send RTP audio")
	}

	select {
	case <-byeReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("announcement completed without sending BYE")
	}
}

func TestHangupEndpointSendsBye(t *testing.T) {
	sipServer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sipServer.Close()
	client, err := sip.NewClient("80082", "secret", sipServer.LocalAddr().String(), "127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	byeReceived := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, readErr := sipServer.ReadFromUDP(buf)
		if readErr != nil {
			return
		}
		request, parseErr := sip.Parse(buf[:n])
		if parseErr != nil || !strings.HasPrefix(request.StartLine, "BYE ") {
			return
		}
		_, _ = sipServer.WriteToUDP(sip.Response(200, "OK", request, ""), peer)
		byeReceived <- struct{}{}
	}()

	g := &Gateway{
		sip: client, callState: "connected",
		dialog: &sip.Dialog{
			URI: "sip:callee@127.0.0.1", CallID: "hangup-call",
			FromTag: "from", To: "<sip:callee@example.com>;tag=to",
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/hangup", nil)
	response := httptest.NewRecorder()
	g.handleHangup(response, request)
	if response.Code != http.StatusNoContent || g.callState != "idle" {
		t.Fatalf("status=%d state=%s", response.Code, g.callState)
	}
	select {
	case <-byeReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("hangup endpoint did not send BYE")
	}
}

type testError string

func (e testError) Error() string { return string(e) }

const errTest = testError("test error")
