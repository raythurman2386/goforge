package sse

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLogEvent_JSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		event    LogEvent
		wantKeys []string
	}{
		{
			name: "full event serialization",
			event: LogEvent{
				Timestamp: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
				Level:     LogLevelInfo,
				Message:   "Deployment started",
				Source:    "deploy",
			},
			wantKeys: []string{"timestamp", "level", "message", "source"},
		},
		{
			name: "error level event",
			event: LogEvent{
				Timestamp: time.Now(),
				Level:     LogLevelError,
				Message:   "Build failed",
				Source:    "build",
			},
			wantKeys: []string{"timestamp", "level", "message", "source"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var decoded map[string]interface{}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			for _, key := range tt.wantKeys {
				if _, ok := decoded[key]; !ok {
					t.Errorf("JSON missing key %q, got %v", key, decoded)
				}
			}
		})
	}
}

func TestLogLevel_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		level LogLevel
		want  string
	}{
		{LogLevelDebug, "debug"},
		{LogLevelInfo, "info"},
		{LogLevelWarn, "warn"},
		{LogLevelError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := string(tt.level); got != tt.want {
				t.Errorf("LogLevel string = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSSEMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType string
		data      interface{}
		wantEvent string
		wantData  bool
	}{
		{
			name:      "log event message",
			eventType: "log",
			data: LogEvent{
				Level:   LogLevelInfo,
				Message: "test message",
			},
			wantEvent: "event: log\n",
			wantData:  true,
		},
		{
			name:      "status update message",
			eventType: "status",
			data:      map[string]string{"status": "running"},
			wantEvent: "event: status\n",
			wantData:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := FormatSSEMessage(tt.eventType, tt.data)

			// Check event line
			if len(result) == 0 {
				t.Fatal("FormatSSEMessage returned empty result")
			}

			// Check it contains the event type
			if !containsString(result, tt.wantEvent) {
				t.Errorf("result = %q, missing event line %q", result, tt.wantEvent)
			}

			// Check it contains data prefix
			if tt.wantData && !containsString(result, "data: ") {
				t.Errorf("result = %q, missing data line", result)
			}

			// SSE messages must end with double newline
			if !endsWithDoubleNewline(result) {
				t.Errorf("result = %q, must end with double newline", result)
			}
		})
	}
}

func TestLogStream_Subscribe(t *testing.T) {
	t.Parallel()

	stream := NewLogStream("deployment-123", 10)

	// Subscribe should return a channel
	ch := stream.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	// Subscriber count should increase
	if count := stream.SubscriberCount(); count != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", count)
	}

	// Second subscription
	ch2 := stream.Subscribe()
	if ch2 == nil {
		t.Fatal("Subscribe() returned nil channel on second call")
	}
	if count := stream.SubscriberCount(); count != 2 {
		t.Errorf("SubscriberCount() = %d, want 2", count)
	}
}

func TestLogStream_Publish(t *testing.T) {
	t.Parallel()

	stream := NewLogStream("deployment-456", 10)
	ch := stream.Subscribe()

	event := LogEvent{
		Level:   LogLevelInfo,
		Message: "test log",
		Source:  "test",
	}

	// Publish should deliver to subscriber
	stream.Publish(event)

	select {
	case received := <-ch:
		if received.Message != event.Message {
			t.Errorf("received.Message = %q, want %q", received.Message, event.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for published event")
	}
}

func TestLogStream_Unsubscribe(t *testing.T) {
	t.Parallel()

	stream := NewLogStream("deployment-789", 10)
	ch := stream.Subscribe()

	if count := stream.SubscriberCount(); count != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1", count)
	}

	stream.Unsubscribe(ch)

	if count := stream.SubscriberCount(); count != 0 {
		t.Errorf("SubscriberCount() after unsubscribe = %d, want 0", count)
	}
}

func TestLogStream_Close(t *testing.T) {
	t.Parallel()

	stream := NewLogStream("deployment-close", 10)
	ch := stream.Subscribe()

	stream.Close()

	// Channel should be closed after Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after Close()")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel not closed after Close()")
	}
}

// Helper functions for tests
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func endsWithDoubleNewline(s string) bool {
	return len(s) >= 2 && s[len(s)-2:] == "\n\n"
}
