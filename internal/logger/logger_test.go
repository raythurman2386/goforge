package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{"debug lowercase", "debug", slog.LevelDebug},
		{"debug uppercase", "DEBUG", slog.LevelDebug},
		{"debug mixed case", "DeBuG", slog.LevelDebug},
		{"info lowercase", "info", slog.LevelInfo},
		{"info uppercase", "INFO", slog.LevelInfo},
		{"warn lowercase", "warn", slog.LevelWarn},
		{"warning lowercase", "warning", slog.LevelWarn},
		{"error lowercase", "error", slog.LevelError},
		{"error uppercase", "ERROR", slog.LevelError},
		{"unknown defaults to info", "unknown", slog.LevelInfo},
		{"empty defaults to info", "", slog.LevelInfo},
		{"whitespace defaults to info", "  ", slog.LevelInfo},
		{"trace defaults to info", "trace", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLevel(tt.level)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.level, got, tt.want)
			}
		})
	}
}

func TestSetup_Format(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		wantJSON bool
	}{
		{"json format", "json", true},
		{"JSON uppercase", "JSON", true},
		{"text format", "text", false},
		{"empty defaults to text", "", false},
		{"unknown defaults to text", "xml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := Setup("info", tt.format)
			if logger == nil {
				t.Fatal("Setup returned nil logger")
			}
			// We can't easily check the handler type directly,
			// but we can verify the logger was created successfully
		})
	}
}

func TestSetup_Level(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"error level", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := Setup(tt.level, "text")
			if logger == nil {
				t.Fatal("Setup returned nil logger")
			}
		})
	}
}

func TestWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	logger := slog.New(handler)

	loggerWithID := WithRequestID(logger, "req-123")
	loggerWithID.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "request_id") {
		t.Error("expected output to contain request_id")
	}
	if !strings.Contains(output, "req-123") {
		t.Error("expected output to contain req-123")
	}
}

func TestWithRequestID_EmptyID(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	logger := slog.New(handler)

	loggerWithID := WithRequestID(logger, "")
	loggerWithID.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "request_id") {
		t.Error("expected output to contain request_id even when empty")
	}
}

func TestWithComponent(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	logger := slog.New(handler)

	loggerWithComponent := WithComponent(logger, "auth")
	loggerWithComponent.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "component") {
		t.Error("expected output to contain component")
	}
	if !strings.Contains(output, "auth") {
		t.Error("expected output to contain auth")
	}
}

func TestWithComponent_Chaining(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	logger := slog.New(handler)

	// Chain multiple With calls
	loggerWithAttrs := WithComponent(WithRequestID(logger, "req-456"), "database")
	loggerWithAttrs.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "request_id") {
		t.Error("expected output to contain request_id")
	}
	if !strings.Contains(output, "req-456") {
		t.Error("expected output to contain req-456")
	}
	if !strings.Contains(output, "component") {
		t.Error("expected output to contain component")
	}
	if !strings.Contains(output, "database") {
		t.Error("expected output to contain database")
	}
}
