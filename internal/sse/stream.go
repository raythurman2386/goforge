package sse

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log message.
type LogLevel string

const (
	// LogLevelDebug represents debug level logs.
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo represents info level logs.
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn represents warning level logs.
	LogLevelWarn LogLevel = "warn"
	// LogLevelError represents error level logs.
	LogLevelError LogLevel = "error"
)

// LogEvent represents a structured log entry for SSE streaming.
type LogEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Level     LogLevel  `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
}

// FormatSSEMessage formats data as an SSE message with the given event type.
func FormatSSEMessage(eventType string, data interface{}) string {
	jsonData, err := json.Marshal(data)
	if err != nil {
		jsonData = []byte(fmt.Sprintf(`{"error": "marshal failed: %s"}`, err.Error()))
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonData)
}

// LogStream manages subscribers for a specific log stream (deployment, container, etc.).
type LogStream struct {
	id          string
	subscribers map[chan LogEvent]struct{}
	bufferSize  int
	mu          sync.RWMutex
	closed      bool
}

// NewLogStream creates a new LogStream with the given ID and buffer size.
func NewLogStream(id string, bufferSize int) *LogStream {
	if bufferSize <= 0 {
		bufferSize = 10
	}
	return &LogStream{
		id:          id,
		subscribers: make(map[chan LogEvent]struct{}),
		bufferSize:  bufferSize,
	}
}

// Subscribe creates a new subscription channel for this stream.
func (s *LogStream) Subscribe() chan LogEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		ch := make(chan LogEvent)
		close(ch)
		return ch
	}

	ch := make(chan LogEvent, s.bufferSize)
	s.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscription channel from this stream.
func (s *LogStream) Unsubscribe(ch chan LogEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.subscribers[ch]; ok {
		delete(s.subscribers, ch)
		close(ch)
	}
}

// Publish sends a log event to all subscribers.
func (s *LogStream) Publish(event LogEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return
	}

	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer full, skip (non-blocking)
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (s *LogStream) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// ID returns the stream identifier.
func (s *LogStream) ID() string {
	return s.id
}

// Close closes the stream and all subscriber channels.
func (s *LogStream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	for ch := range s.subscribers {
		close(ch)
	}
	s.subscribers = make(map[chan LogEvent]struct{})
}
