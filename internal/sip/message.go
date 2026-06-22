package sip

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type Message struct {
	StartLine string
	Headers   map[string][]string
	Body      []byte
}

func Parse(data []byte) (Message, error) {
	head, body, _ := bytes.Cut(data, []byte("\r\n\r\n"))
	s := bufio.NewScanner(bytes.NewReader(head))
	if !s.Scan() {
		return Message{}, fmt.Errorf("empty SIP packet")
	}
	m := Message{StartLine: s.Text(), Headers: make(map[string][]string), Body: body}
	for s.Scan() {
		line := s.Text()
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		m.Headers[key] = append(m.Headers[key], strings.TrimSpace(val))
	}
	return m, s.Err()
}

func (m Message) Header(name string) string {
	values := m.Headers[strings.ToLower(name)]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (m Message) HeaderValues(name string) []string {
	values := m.Headers[strings.ToLower(name)]
	return append([]string(nil), values...)
}

func (m Message) StatusCode() int {
	parts := strings.Fields(m.StartLine)
	if len(parts) < 2 || !strings.HasPrefix(m.StartLine, "SIP/2.0") {
		return 0
	}
	code, _ := strconv.Atoi(parts[1])
	return code
}

func Request(method, uri string, headers [][2]string, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s SIP/2.0\r\n", method, uri)
	for _, h := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", h[0], h[1])
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return []byte(b.String())
}

func Response(code int, reason string, request Message, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "SIP/2.0 %d %s\r\n", code, reason)
	for _, name := range []string{"via", "from", "to", "call-id", "cseq"} {
		for _, value := range request.Headers[name] {
			fmt.Fprintf(&b, "%s: %s\r\n", canonicalHeader(name), value)
		}
	}
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return []byte(b.String())
}

func canonicalHeader(name string) string {
	switch name {
	case "call-id":
		return "Call-ID"
	case "cseq":
		return "CSeq"
	default:
		return strings.ToUpper(name[:1]) + name[1:]
	}
}
