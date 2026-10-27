package deploy

import (
	"bytes"
	"context"
	"strings"
)

// logWriter splits clone and build output into lines and records each one.
type logWriter struct {
	ctx          context.Context
	deploymentID string
	source       string
	write        func(ctx context.Context, deploymentID, level, source, message string) error
	buf          []byte
}

func newLogWriter(ctx context.Context, deploymentID, source string, write func(context.Context, string, string, string, string) error) *logWriter {
	return &logWriter{
		ctx:          ctx,
		deploymentID: deploymentID,
		source:       source,
		write:        write,
	}
}

// Write implements io.Writer. Progress streams use both newlines and carriage returns.
func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	w.drain(false)
	return len(p), nil
}

// Flush records any trailing text that was not terminated.
func (w *logWriter) Flush() {
	w.drain(true)
}

func (w *logWriter) drain(flush bool) {
	for {
		idx := bytes.IndexAny(w.buf, "\n\r")
		if idx < 0 {
			break
		}
		w.emit(w.buf[:idx])
		w.buf = w.buf[idx+1:]
	}
	if flush && len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

func (w *logWriter) emit(line []byte) {
	message := strings.TrimSpace(string(line))
	if message == "" || w.write == nil {
		return
	}
	if err := w.write(w.ctx, w.deploymentID, "info", w.source, message); err != nil {
		// Log already reported the storage failure. Keep reading the stream.
		return
	}
}
