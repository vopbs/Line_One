package sip

import (
	"strings"
	"sync"
	"time"
)

type TraceEntry struct {
	ID        uint64    `json:"id"`
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"`
	Peer      string    `json:"peer"`
	Summary   string    `json:"summary"`
	CallID    string    `json:"callId"`
	Message   string    `json:"message"`
}

type TraceBuffer struct {
	mu      sync.Mutex
	nextID  uint64
	limit   int
	entries []TraceEntry
}

func NewTraceBuffer(limit int) *TraceBuffer {
	return &TraceBuffer{limit: limit}
}

func (b *TraceBuffer) Add(direction, peer string, data []byte) {
	message := redactSIP(string(data))
	parsed, _ := Parse([]byte(message))

	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	b.entries = append(b.entries, TraceEntry{
		ID: b.nextID, Time: time.Now(), Direction: direction, Peer: peer,
		Summary: parsed.StartLine, CallID: parsed.Header("call-id"), Message: message,
	})
	if len(b.entries) > b.limit {
		b.entries = append([]TraceEntry(nil), b.entries[len(b.entries)-b.limit:]...)
	}
}

func (b *TraceBuffer) Since(after uint64) []TraceEntry {
	return b.Query(after, "")
}

func (b *TraceBuffer) Query(after uint64, callID string) []TraceEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]TraceEntry, 0)
	for _, entry := range b.entries {
		if entry.ID > after && (callID == "" || entry.CallID == callID) {
			result = append(result, entry)
		}
	}
	return result
}

func (b *TraceBuffer) LatestCallID(summaryPrefix string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.entries) - 1; i >= 0; i-- {
		entry := b.entries[i]
		if entry.CallID != "" && strings.HasPrefix(entry.Summary, summaryPrefix) {
			return entry.CallID
		}
	}
	return ""
}

func (b *TraceBuffer) Clear() {
	b.mu.Lock()
	b.entries = nil
	b.mu.Unlock()
}

func redactSIP(message string) string {
	lines := strings.Split(message, "\r\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "authorization:") || strings.HasPrefix(lower, "proxy-authorization:") {
			name, _, _ := strings.Cut(line, ":")
			lines[i] = name + ": [REDACTED]"
		}
	}
	return strings.Join(lines, "\r\n")
}
