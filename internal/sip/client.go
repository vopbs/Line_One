package sip

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	username    string
	password    string
	server      *net.UDPAddr
	conn        *net.UDPConn
	advertiseIP string
	localPort   int

	mu      sync.Mutex
	waiters map[string]chan Message
	cseq    int
	onBye   func()
	trace   *TraceBuffer
}

type Dialog struct {
	URI      string
	CallID   string
	FromTag  string
	To       string
	CSeq     int
	RouteSet []string
}

func NewClient(username, password, server, advertiseIP string, localPort int) (*Client, error) {
	serverAddr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: localPort})
	if err != nil {
		return nil, err
	}
	c := &Client{
		username: username, password: password, server: serverAddr,
		conn: conn, advertiseIP: advertiseIP, localPort: localPort,
		waiters: make(map[string]chan Message), cseq: 1,
	}
	go c.receive()
	return c, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) SetOnBye(callback func()) {
	c.mu.Lock()
	c.onBye = callback
	c.mu.Unlock()
}

func (c *Client) SetTrace(trace *TraceBuffer) {
	c.mu.Lock()
	c.trace = trace
	c.mu.Unlock()
}

func (c *Client) Register(ctx context.Context) error {
	return c.register(ctx, 300)
}

func (c *Client) Unregister(ctx context.Context) error {
	return c.register(ctx, 0)
}

func (c *Client) register(ctx context.Context, expires int) error {
	callID := token() + "@" + c.advertiseIP
	fromTag := token()
	uri := "sip:" + c.server.String()

	response, err := c.registerAttempt(ctx, uri, callID, fromTag, expires, "", "")
	if err != nil {
		return err
	}
	if response.StatusCode() == 401 || response.StatusCode() == 407 {
		challenge := response.Header("www-authenticate")
		if challenge == "" {
			challenge = response.Header("proxy-authenticate")
		}
		auth, authErr := DigestAuthorization(challenge, c.username, c.password, "REGISTER", uri)
		if authErr != nil {
			return authErr
		}
		authHeader := "Authorization"
		if response.StatusCode() == 407 {
			authHeader = "Proxy-Authorization"
		}
		response, err = c.registerAttempt(ctx, uri, callID, fromTag, expires, authHeader, auth)
		if err != nil {
			return err
		}
	}
	if code := response.StatusCode(); code < 200 || code >= 300 {
		return fmt.Errorf("REGISTER failed: %s", response.StartLine)
	}
	return nil
}

func (c *Client) registerAttempt(ctx context.Context, uri, callID, tag string, expires int, authHeader, authorization string) (Message, error) {
	cseq := c.nextCSeq()
	branch := "z9hG4bK-" + token()
	headers := c.commonHeaders(branch, callID, tag, fmt.Sprintf("sip:%s@%s", c.username, c.server.String()), cseq, "REGISTER")
	headers = append(headers,
		[2]string{"Contact", fmt.Sprintf("<sip:%s@%s:%d;transport=udp>", c.username, c.advertiseIP, c.localPort)},
		[2]string{"Expires", strconv.Itoa(expires)},
	)
	if authorization != "" {
		headers = append(headers, [2]string{authHeader, authorization})
	}
	return c.transact(ctx, callID, cseq, Request("REGISTER", uri, headers, ""))
}

func (c *Client) Invite(ctx context.Context, destination, sdp string) (Message, *Dialog, error) {
	return c.InviteWithCallID(ctx, destination, sdp, c.NewCallID())
}

func (c *Client) NewCallID() string {
	return token() + "@" + c.advertiseIP
}

func (c *Client) InviteWithCallID(ctx context.Context, destination, sdp, callID string) (Message, *Dialog, error) {
	tag := token()
	uri := fmt.Sprintf("sip:%s@%s", destination, c.server.String())
	return c.inviteAttempt(ctx, uri, callID, tag, sdp, "", "")
}

func (c *Client) inviteAttempt(ctx context.Context, uri, callID, tag, sdp, authHeader, authorization string) (Message, *Dialog, error) {
	cseq := c.nextCSeq()
	branch := "z9hG4bK-" + token()
	headers := c.commonHeaders(branch, callID, tag, uri, cseq, "INVITE")
	headers = append(headers,
		[2]string{"Contact", fmt.Sprintf("<sip:%s@%s:%d;transport=udp>", c.username, c.advertiseIP, c.localPort)},
		[2]string{"Content-Type", "application/sdp"},
	)
	if authorization != "" {
		headers = append(headers, [2]string{authHeader, authorization})
	}
	response, err := c.transactFinal(ctx, callID, cseq, Request("INVITE", uri, headers, sdp))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.sendCancel(uri, callID, tag, branch, cseq)
		}
		return Message{}, nil, err
	}
	c.sendACK(uri, callID, tag, branch, cseq, response)
	if response.StatusCode() == 401 || response.StatusCode() == 407 {
		challenge := response.Header("proxy-authenticate")
		if challenge == "" {
			challenge = response.Header("www-authenticate")
		}
		auth, authErr := DigestAuthorization(challenge, c.username, c.password, "INVITE", uri)
		if authErr != nil {
			return Message{}, nil, authErr
		}
		authHeader := "Authorization"
		if response.StatusCode() == 407 {
			authHeader = "Proxy-Authorization"
		}
		return c.inviteAttempt(ctx, uri, callID, tag, sdp, authHeader, auth)
	}
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		return response, nil, nil
	}
	dialogURI := contactURI(response.Header("contact"))
	if dialogURI == "" {
		dialogURI = uri
	}
	dialog := &Dialog{
		URI: dialogURI, CallID: callID, FromTag: tag,
		To: response.Header("to"), CSeq: cseq, RouteSet: routeSet(response),
	}
	return response, dialog, nil
}

func (c *Client) sendACK(uri, callID, tag, inviteBranch string, cseq int, response Message) {
	branch := inviteBranch
	if target := contactURI(response.Header("contact")); target != "" && response.StatusCode() >= 200 && response.StatusCode() < 300 {
		uri = target
		branch = "z9hG4bK-" + token()
	}
	to := response.Header("to")
	if to == "" {
		to = "<" + uri + ">"
	}
	headers := [][2]string{
		{"Via", fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s;rport", c.advertiseIP, c.localPort, branch)},
		{"Max-Forwards", "70"},
		{"From", fmt.Sprintf("<sip:%s@%s>;tag=%s", c.username, c.server.String(), tag)},
		{"To", to},
		{"Call-ID", callID},
		{"CSeq", fmt.Sprintf("%d ACK", cseq)},
		{"User-Agent", "local-webrtc-sip/0.1"},
	}
	headers = appendRouteHeaders(headers, routeSet(response))
	packet := Request("ACK", uri, headers, "")
	c.tracePacket("out", c.server.String(), packet)
	_, _ = c.conn.WriteToUDP(packet, c.server)
}

func (c *Client) sendCancel(uri, callID, tag, branch string, cseq int) {
	headers := c.commonHeaders(branch, callID, tag, uri, cseq, "CANCEL")
	packet := Request("CANCEL", uri, headers, "")
	c.tracePacket("out", c.server.String(), packet)
	_, _ = c.conn.WriteToUDP(packet, c.server)
}

func (c *Client) Bye(ctx context.Context, dialog *Dialog) error {
	if dialog == nil {
		return nil
	}
	cseq := c.nextCSeq()
	headers := [][2]string{
		{"Via", fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=z9hG4bK-%s;rport", c.advertiseIP, c.localPort, token())},
		{"Max-Forwards", "70"},
		{"From", fmt.Sprintf("<sip:%s@%s>;tag=%s", c.username, c.server.String(), dialog.FromTag)},
		{"To", dialog.To},
		{"Call-ID", dialog.CallID},
		{"CSeq", fmt.Sprintf("%d BYE", cseq)},
		{"User-Agent", "local-webrtc-sip/0.1"},
	}
	headers = appendRouteHeaders(headers, dialog.RouteSet)
	response, err := c.transact(ctx, dialog.CallID, cseq, Request("BYE", dialog.URI, headers, ""))
	if err != nil {
		return err
	}
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		return fmt.Errorf("BYE failed: %s", response.StartLine)
	}
	return nil
}

func (c *Client) commonHeaders(branch, callID, tag, toURI string, cseq int, method string) [][2]string {
	return [][2]string{
		{"Via", fmt.Sprintf("SIP/2.0/UDP %s:%d;branch=%s;rport", c.advertiseIP, c.localPort, branch)},
		{"Max-Forwards", "70"},
		{"From", fmt.Sprintf("<sip:%s@%s>;tag=%s", c.username, c.server.String(), tag)},
		{"To", "<" + toURI + ">"},
		{"Call-ID", callID},
		{"CSeq", fmt.Sprintf("%d %s", cseq, method)},
		{"User-Agent", "local-webrtc-sip/0.1"},
	}
}

func routeSet(response Message) []string {
	records := response.HeaderValues("record-route")
	routes := make([]string, 0, len(records))
	for _, record := range records {
		for _, route := range splitHeaderValues(record) {
			if route != "" {
				routes = append(routes, route)
			}
		}
	}
	return routes
}

func appendRouteHeaders(headers [][2]string, routes []string) [][2]string {
	for _, route := range routes {
		if route != "" {
			headers = append(headers, [2]string{"Route", route})
		}
	}
	return headers
}

func splitHeaderValues(value string) []string {
	var values []string
	start := 0
	inQuotes := false
	angleDepth := 0
	for i, r := range value {
		switch r {
		case '"':
			inQuotes = !inQuotes
		case '<':
			if !inQuotes {
				angleDepth++
			}
		case '>':
			if !inQuotes && angleDepth > 0 {
				angleDepth--
			}
		case ',':
			if !inQuotes && angleDepth == 0 {
				values = append(values, strings.TrimSpace(value[start:i]))
				start = i + 1
			}
		}
	}
	values = append(values, strings.TrimSpace(value[start:]))
	return values
}

func (c *Client) transact(ctx context.Context, callID string, cseq int, packet []byte) (Message, error) {
	return c.transactWithMode(ctx, callID, cseq, packet, false)
}

func (c *Client) transactFinal(ctx context.Context, callID string, cseq int, packet []byte) (Message, error) {
	return c.transactWithMode(ctx, callID, cseq, packet, true)
}

func (c *Client) transactWithMode(ctx context.Context, callID string, cseq int, packet []byte, final bool) (Message, error) {
	key := transactionKey(callID, cseq)
	ch := make(chan Message, 8)
	c.mu.Lock()
	c.waiters[key] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, key)
		c.mu.Unlock()
	}()

	c.tracePacket("out", c.server.String(), packet)
	if _, err := c.conn.WriteToUDP(packet, c.server); err != nil {
		return Message{}, err
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	timerC := timer.C
	retries := 0
	for {
		select {
		case msg := <-ch:
			if !final || msg.StatusCode() >= 200 {
				return msg, nil
			}
			// A provisional response confirms that the INVITE reached the
			// server. RFC 3261 requires UDP INVITE retransmissions to stop
			// while the transaction continues waiting for a final response.
			if timerC != nil {
				timer.Stop()
				timerC = nil
			}
		case <-timerC:
			if retries >= 6 {
				return Message{}, fmt.Errorf("SIP transaction timed out")
			}
			retries++
			c.tracePacket("out", c.server.String(), packet)
			_, _ = c.conn.WriteToUDP(packet, c.server)
			timer.Reset(time.Duration(1<<min(retries, 3)) * 500 * time.Millisecond)
		case <-ctx.Done():
			return Message{}, ctx.Err()
		}
	}
}

func (c *Client) receive() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		c.tracePacket("in", addr.String(), buf[:n])
		msg, err := Parse(buf[:n])
		if err != nil {
			continue
		}
		if msg.StatusCode() == 0 {
			log.Printf("incoming SIP request from %s: %s", addr, msg.StartLine)
			if strings.HasPrefix(msg.StartLine, "BYE ") {
				packet := Response(200, "OK", msg, "")
				c.tracePacket("out", addr.String(), packet)
				_, _ = c.conn.WriteToUDP(packet, addr)
				c.mu.Lock()
				onBye := c.onBye
				c.mu.Unlock()
				if onBye != nil {
					go onBye()
				}
			}
			continue
		}
		cseqFields := strings.Fields(msg.Header("cseq"))
		if len(cseqFields) == 0 {
			continue
		}
		cseq, _ := strconv.Atoi(cseqFields[0])
		key := transactionKey(msg.Header("call-id"), cseq)
		c.mu.Lock()
		ch := c.waiters[key]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

func (c *Client) tracePacket(direction, peer string, packet []byte) {
	c.mu.Lock()
	trace := c.trace
	c.mu.Unlock()
	if trace != nil {
		trace.Add(direction, peer, packet)
	}
}

func (c *Client) nextCSeq() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.cseq
	c.cseq++
	return n
}

func transactionKey(callID string, cseq int) string {
	return fmt.Sprintf("%s/%d", callID, cseq)
}

func token() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func contactURI(contact string) string {
	start := strings.Index(contact, "<")
	end := strings.Index(contact, ">")
	if start >= 0 && end > start {
		return contact[start+1 : end]
	}
	if strings.HasPrefix(contact, "sip:") {
		if semi := strings.Index(contact, ";"); semi >= 0 {
			return contact[:semi]
		}
		return contact
	}
	return ""
}
