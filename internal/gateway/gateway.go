package gateway

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"webrtc-sip/internal/config"
	"webrtc-sip/internal/media"
	"webrtc-sip/internal/sip"
)

type Gateway struct {
	cfg        config.Config
	sip        *sip.Client
	rtpConn    *net.UDPConn
	mu         sync.Mutex
	callState  string
	lastError  string
	peer       *webrtc.PeerConnection
	sipTarget  *net.UDPAddr
	webTrack   *webrtc.TrackLocalStaticRTP
	dialog     *sip.Dialog
	codec      media.Codec
	callID     string
	registered bool
	sipServer  string
	sipUser    string
	callSeq    uint64
	callCancel context.CancelFunc
	registerMu sync.Mutex
	rtpUp      uint64
	rtpDown    uint64
	trace      *sip.TraceBuffer
	audioMode  string
	audioPCM   []int16
	rtpReady   chan struct{}
	rtpReadyOnce sync.Once
	mediaTarget string
	announcementState string
}

type registerRequest struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type offerRequest struct {
	SDP         string `json:"sdp"`
	Destination string `json:"destination"`
	Codec       string `json:"codec"`
	AudioMode   string `json:"audioMode"`
}

type offerResponse struct {
	SDP string `json:"sdp"`
}

func New(cfg config.Config) (*Gateway, error) {
	rtpConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: cfg.RTPPort})
	if err != nil {
		return nil, err
	}
	g := &Gateway{cfg: cfg, rtpConn: rtpConn, callState: "idle", trace: sip.NewTraceBuffer(500)}
	go g.readSIPRTP()
	return g, nil
}

func (g *Gateway) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("web")))
	mux.HandleFunc("/api/register", g.handleRegister)
	mux.HandleFunc("/api/logout", g.handleLogout)
	mux.HandleFunc("/api/signaling", g.handleSignaling)
	mux.HandleFunc("/api/audio", g.handleAudio)
	mux.HandleFunc("/api/offer", g.handleOffer)
	mux.HandleFunc("/api/status", g.handleStatus)
	mux.HandleFunc("/api/hangup", g.handleHangup)
	return mux
}

func (g *Gateway) Close() {
	g.mu.Lock()
	pc := g.peer
	dialog := g.dialog
	client := g.sip
	g.clearCallLocked()
	g.sip = nil
	g.registered = false
	g.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
	if client != nil {
		if dialog != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if err := client.Bye(ctx, dialog); err != nil {
				log.Printf("SIP hangup error during shutdown: %v", err)
			}
			cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := client.Unregister(ctx); err != nil {
			log.Printf("SIP unregister error during shutdown: %v", err)
		}
		cancel()
		_ = client.Close()
	}
	if g.rtpConn != nil {
		_ = g.rtpConn.Close()
	}
}

func (g *Gateway) handleAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 100<<20))
	if err != nil || len(data) == 0 {
		http.Error(w, "invalid audio data", http.StatusBadRequest)
		return
	}
	var samples []int16
	if r.URL.Query().Get("format") == "wav" {
		wav, parseErr := media.ParseWAV(data)
		if parseErr != nil {
			http.Error(w, parseErr.Error(), http.StatusBadRequest)
			return
		}
		samples = wav.Samples
	} else {
		if len(data)%2 != 0 {
			http.Error(w, "invalid 8kHz mono PCM audio", http.StatusBadRequest)
			return
		}
		samples = make([]int16, len(data)/2)
		for i := range samples {
			samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		}
	}
	g.mu.Lock()
	if g.callActiveLocked() {
		g.mu.Unlock()
		http.Error(w, "通话期间不能更换音频", http.StatusConflict)
		return
	}
	g.audioPCM = samples
	g.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req registerRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil ||
		req.Server == "" || req.Username == "" || req.Password == "" {
		http.Error(w, "服务器、账号和密码不能为空", http.StatusBadRequest)
		return
	}

	g.registerMu.Lock()
	defer g.registerMu.Unlock()

	g.mu.Lock()
	if g.callActiveLocked() {
		g.mu.Unlock()
		http.Error(w, "通话期间不能更换 SIP 注册", http.StatusConflict)
		return
	}
	g.clearCallLocked()
	oldClient := g.sip
	g.sip = nil
	g.registered = false
	g.sipServer = req.Server
	g.sipUser = req.Username
	g.lastError = ""
	g.mu.Unlock()
	if oldClient != nil {
		_ = oldClient.Close()
	}

	client, err := sip.NewClient(req.Username, req.Password, req.Server, g.cfg.AdvertiseIP, g.cfg.SIPPort)
	if err != nil {
		g.registrationFailed(err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	client.SetTrace(g.trace)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	err = client.Register(ctx)
	cancel()
	if err != nil {
		_ = client.Close()
		g.registrationFailed(err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	g.mu.Lock()
	g.sip = client
	g.registered = true
	g.lastError = ""
	g.mu.Unlock()
	client.SetOnBye(g.remoteHangup)
	go g.keepRegistered(client)
	log.Printf("SIP account %s registered at %s via UDP", req.Username, req.Server)
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) handleSignaling(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		callID := r.URL.Query().Get("callId")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g.trace.Query(after, callID))
	case http.MethodDelete:
		g.trace.Clear()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	g.registerMu.Lock()
	defer g.registerMu.Unlock()

	g.mu.Lock()
	if g.callActiveLocked() {
		g.mu.Unlock()
		http.Error(w, "请先挂断当前通话", http.StatusConflict)
		return
	}
	client := g.sip
	g.sip = nil
	g.registered = false
	g.sipServer = ""
	g.sipUser = ""
	g.lastError = ""
	g.clearCallLocked()
	g.mu.Unlock()

	if client != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		if err := client.Unregister(ctx); err != nil {
			log.Printf("SIP unregister error: %v", err)
		}
		cancel()
		_ = client.Close()
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) keepRegistered(client *sip.Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		g.mu.Lock()
		active := g.sip == client && g.registered
		g.mu.Unlock()
		if !active {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := client.Register(ctx)
		cancel()
		if err != nil {
			g.mu.Lock()
			if g.sip == client {
				g.registered = false
				g.lastError = err.Error()
			}
			g.mu.Unlock()
			log.Printf("SIP re-registration failed: %v", err)
			return
		}
	}
}

func (g *Gateway) handleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req offerRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.SDP == "" || req.Destination == "" {
		http.Error(w, "invalid offer", http.StatusBadRequest)
		return
	}
	codec, err := media.CodecByName(req.Codec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if req.AudioMode != "" && req.AudioMode != "microphone" && req.AudioMode != "file" {
		http.Error(w, "invalid audio mode", http.StatusUnprocessableEntity)
		return
	}

	g.mu.Lock()
	if g.callActiveLocked() {
		g.mu.Unlock()
		http.Error(w, "gateway is busy", http.StatusConflict)
		return
	}
	g.clearCallLocked()
	if !g.registered || g.sip == nil {
		g.mu.Unlock()
		http.Error(w, "请先注册 SIP 账号", http.StatusConflict)
		return
	}
	g.codec = codec
	g.audioMode = req.AudioMode
	if g.audioMode == "" {
		g.audioMode = "microphone"
	}
	if g.audioMode == "file" && len(g.audioPCM) == 0 {
		g.audioMode = ""
		g.mu.Unlock()
		http.Error(w, "请先选择并上传音频文件", http.StatusConflict)
		return
	}
	g.callState = "connecting"
	g.lastError = ""
	g.callSeq++
	g.rtpReady = make(chan struct{})
	g.rtpReadyOnce = sync.Once{}
	g.mediaTarget = ""
	g.announcementState = ""
	callSeq := g.callSeq
	callCtx, callCancel := context.WithCancel(context.Background())
	g.callCancel = callCancel
	g.mu.Unlock()

	pc, err := g.newPeer(codec)
	if err != nil {
		g.failCall(callSeq, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}); err != nil {
		g.failCall(callSeq, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		g.failCall(callSeq, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		g.failCall(callSeq, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	<-gatherDone

	g.mu.Lock()
	if g.callSeq != callSeq || g.callState != "connecting" {
		g.mu.Unlock()
		_ = pc.Close()
		http.Error(w, "call was cancelled", http.StatusConflict)
		return
	}
	g.peer = pc
	g.mu.Unlock()
	go g.dial(callCtx, callSeq, req.Destination)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(offerResponse{SDP: pc.LocalDescription().SDP})
}

func (g *Gateway) newPeer(codec media.Codec) (*webrtc.PeerConnection, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: codec.MimeType, ClockRate: 8000, Channels: 1},
		PayloadType:        webrtc.PayloadType(codec.PayloadType),
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	interceptors := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, interceptors); err != nil {
		return nil, err
	}
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(interceptors),
	)
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	out, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: codec.MimeType, ClockRate: 8000, Channels: 1},
		"audio", "sip",
	)
	if err != nil {
		return nil, err
	}
	sender, err := pc.AddTrack(out)
	if err != nil {
		return nil, err
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, readErr := sender.Read(buf); readErr != nil {
				return
			}
		}
	}()
	g.mu.Lock()
	g.webTrack = out
	g.mu.Unlock()

	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		buf := make([]byte, 1600)
		for {
			n, _, readErr := track.Read(buf)
			if readErr != nil {
				return
			}
			var packet rtp.Packet
			if err := packet.Unmarshal(buf[:n]); err != nil {
				continue
			}
			// WebRTC may negotiate RTP header extensions that a traditional
			// SIP/RTP endpoint does not understand. Re-marshal plain PCMU RTP.
			packet.PayloadType = codec.PayloadType
			packet.Extension = false
			packet.Extensions = nil
			packet.ExtensionProfile = 0
			packet.Padding = false
			packet.PaddingSize = 0
			plainRTP, err := packet.Marshal()
			if err != nil {
				continue
			}
			g.mu.Lock()
			target := g.sipTarget
			forward := g.audioMode != "file"
			if target != nil && forward {
				g.rtpUp++
			}
			g.mu.Unlock()
			if target != nil && forward {
				if _, err := g.rtpConn.WriteToUDP(plainRTP, target); err != nil {
					log.Printf("SIP RTP send error to %s: %v", target, err)
				}
			}
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTC state: %s", state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			g.mu.Lock()
			active := g.callActiveLocked()
			g.mu.Unlock()
			if active {
				g.endCall(true)
			}
		}
	})
	return pc, nil
}

func (g *Gateway) dial(callCtx context.Context, callSeq uint64, destination string) {
	g.mu.Lock()
	client := g.sip
	g.mu.Unlock()
	if client == nil {
		g.failCall(callSeq, fmt.Errorf("SIP account is not registered"))
		return
	}
	ctx, cancel := context.WithTimeout(callCtx, 35*time.Second)
	defer cancel()
	g.mu.Lock()
	codec := g.codec
	callID := client.NewCallID()
	if g.callSeq == callSeq && g.callState == "connecting" {
		g.callID = callID
	}
	g.mu.Unlock()
	response, dialog, err := client.InviteWithCallID(ctx, destination, media.Offer(g.cfg.AdvertiseIP, g.cfg.RTPPort, codec), callID)
	if err != nil {
		g.failCall(callSeq, err)
		return
	}
	g.mu.Lock()
	if g.callSeq == callSeq {
		g.callID = response.Header("call-id")
	}
	g.mu.Unlock()
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		g.failCall(callSeq, fmt.Errorf("call failed: %s", response.StartLine))
		return
	}
	target, err := media.AudioTarget(string(response.Body))
	if err != nil {
		g.failCall(callSeq, err)
		return
	}
	if !media.AcceptsCodec(string(response.Body), codec) {
		g.failCall(callSeq, fmt.Errorf("remote SDP did not accept %s", codec.Name))
		return
	}
	g.mu.Lock()
	if g.callSeq != callSeq || g.callState != "connecting" {
		g.mu.Unlock()
		if dialog != nil {
			go g.bye(client, dialog)
		}
		return
	}
	if g.sipTarget == nil {
		g.sipTarget = target
	}
	g.mediaTarget = g.sipTarget.String()
	g.dialog = dialog
	if dialog != nil {
		g.callID = dialog.CallID
	}
	g.callState = "connected"
	audioMode := g.audioMode
	g.mu.Unlock()
	log.Printf("SIP media target from SDP: %s", target)
	if audioMode == "file" {
		go g.playAnnouncement(callSeq)
	}
}

func (g *Gateway) readSIPRTP() {
	buf := make([]byte, 2000)
	for {
		n, source, err := g.rtpConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(buf[:n]); err != nil {
			continue
		}
		g.mu.Lock()
		track := g.webTrack
		// VOS3000 commonly uses symmetric RTP. The packet source is more
		// reliable than the SDP target when media traverses NAT or a relay.
		if g.callActiveLocked() && (g.sipTarget == nil || !g.sipTarget.IP.Equal(source.IP) || g.sipTarget.Port != source.Port) {
			log.Printf("SIP symmetric RTP target updated: %v -> %v", g.sipTarget, source)
			g.sipTarget = source
			g.mediaTarget = source.String()
		}
		g.rtpReadyOnce.Do(func() {
			if g.rtpReady != nil {
				close(g.rtpReady)
			}
		})
		g.rtpDown++
		g.mu.Unlock()
		if track != nil {
			_ = track.WriteRTP(&packet)
		}
	}
}

func (g *Gateway) handleStatus(w http.ResponseWriter, _ *http.Request) {
	g.mu.Lock()
	status := map[string]any{
		"state":      g.callState,
		"error":      g.lastError,
		"registered": g.registered,
		"server":     g.sipServer,
		"username":   g.sipUser,
		"rtpUp":      g.rtpUp,
		"rtpDown":    g.rtpDown,
		"codec":      g.codec.Name,
		"callId":     g.callID,
		"localSip":   fmt.Sprintf("%s:%d", g.cfg.AdvertiseIP, g.cfg.SIPPort),
		"mediaTarget": g.mediaTarget,
		"announcementState": g.announcementState,
	}
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (g *Gateway) handleHangup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g.endCall(true)
	w.WriteHeader(http.StatusNoContent)
}

func (g *Gateway) failCall(callSeq uint64, err error) {
	log.Printf("call error: %v", err)
	var callID string
	if g.trace != nil {
		callID = g.trace.LatestCallID("INVITE ")
	}
	g.mu.Lock()
	if g.callSeq != callSeq {
		g.mu.Unlock()
		return
	}
	g.callState = "failed"
	g.lastError = err.Error()
	pc := g.peer
	g.peer = nil
	if g.callID == "" {
		g.callID = callID
	}
	g.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}

func (g *Gateway) registrationFailed(err error) {
	log.Printf("SIP registration error: %v", err)
	g.mu.Lock()
	g.sip = nil
	g.registered = false
	g.lastError = err.Error()
	g.mu.Unlock()
}

func (g *Gateway) reset() {
	g.endCall(false)
}

func (g *Gateway) endCall(sendBye bool) {
	g.mu.Lock()
	pc := g.peer
	dialog := g.dialog
	client := g.sip
	g.clearCallLocked()
	g.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
	if sendBye && dialog != nil && client != nil {
		go g.bye(client, dialog)
	}
}

func (g *Gateway) callActiveLocked() bool {
	return g.callState == "connecting" || g.callState == "connected"
}

func (g *Gateway) clearCallLocked() {
	g.callSeq++
	if g.callCancel != nil {
		g.callCancel()
		g.callCancel = nil
	}
	g.peer = nil
	g.webTrack = nil
	g.sipTarget = nil
	g.dialog = nil
	g.callID = ""
	g.audioMode = ""
	g.rtpReady = nil
	g.rtpReadyOnce = sync.Once{}
	g.mediaTarget = ""
	g.announcementState = ""
	g.rtpUp = 0
	g.rtpDown = 0
	g.callState = "idle"
	g.lastError = ""
}

func (g *Gateway) playAnnouncement(callSeq uint64) {
	g.mu.Lock()
	samples := append([]int16(nil), g.audioPCM...)
	codec := g.codec
	ready := g.rtpReady
	g.announcementState = "waiting-media"
	g.mu.Unlock()

	if ready != nil {
		select {
		case <-ready:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	g.mu.Lock()
	if g.callSeq != callSeq || g.callState != "connected" {
		g.mu.Unlock()
		return
	}
	g.announcementState = "playing"
	g.mu.Unlock()

	var seed [8]byte
	_, _ = rand.Read(seed[:])
	sequence := binary.BigEndian.Uint16(seed[:2])
	timestamp := binary.BigEndian.Uint32(seed[2:6])
	ssrc := binary.BigEndian.Uint32(seed[4:8])
	next := time.Now()

	for offset := 0; offset < len(samples); offset += 160 {
		g.mu.Lock()
		if g.callSeq != callSeq || g.callState != "connected" {
			g.mu.Unlock()
			return
		}
		target := g.sipTarget
		g.mu.Unlock()
		if target == nil {
			return
		}

		payload := make([]byte, 160)
		for i := range payload {
			sample := int16(0)
			if offset+i < len(samples) {
				sample = samples[offset+i]
			}
			if codec.Name == "pcma" {
				payload[i] = media.EncodePCMA(sample)
			} else {
				payload[i] = media.EncodePCMU(sample)
			}
		}
		packet := rtp.Packet{
			Header: rtp.Header{
				Version: 2, PayloadType: codec.PayloadType,
				SequenceNumber: sequence, Timestamp: timestamp, SSRC: ssrc,
			},
			Payload: payload,
		}
		raw, err := packet.Marshal()
		if err != nil {
			g.failCall(callSeq, err)
			return
		}
		next = next.Add(20 * time.Millisecond)
		time.Sleep(time.Until(next))
		if _, err := g.rtpConn.WriteToUDP(raw, target); err != nil {
			g.failCall(callSeq, err)
			return
		}
		g.mu.Lock()
		if g.callSeq == callSeq {
			g.rtpUp++
		}
		g.mu.Unlock()
		sequence++
		timestamp += 160
	}
	g.mu.Lock()
	if g.callSeq == callSeq {
		g.announcementState = "completed"
	}
	g.mu.Unlock()
	g.endCallForSeq(callSeq, true)
}

func (g *Gateway) endCallForSeq(callSeq uint64, sendBye bool) {
	g.mu.Lock()
	if g.callSeq != callSeq {
		g.mu.Unlock()
		return
	}
	pc := g.peer
	dialog := g.dialog
	client := g.sip
	g.clearCallLocked()
	g.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
	if sendBye && dialog != nil && client != nil {
		go g.bye(client, dialog)
	}
}

func (g *Gateway) remoteHangup() {
	log.Printf("SIP remote party ended the call")
	g.reset()
}

func (g *Gateway) bye(client *sip.Client, dialog *sip.Dialog) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Bye(ctx, dialog); err != nil {
		log.Printf("SIP hangup error: %v", err)
	}
}
