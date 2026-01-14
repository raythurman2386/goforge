package sse

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHub_Broadcast(t *testing.T) {
	tests := []struct {
		name          string
		clientTopic   string
		msgTopic      string
		msgData       string
		shouldReceive bool
	}{
		{
			name:          "Match Topic",
			clientTopic:   "logs",
			msgTopic:      "logs",
			msgData:       "hello",
			shouldReceive: true,
		},
		{
			name:          "Wildcard Client",
			clientTopic:   "*",
			msgTopic:      "metrics",
			msgData:       "cpu=90",
			shouldReceive: true,
		},
		{
			name:          "Mismatch Topic",
			clientTopic:   "logs",
			msgTopic:      "metrics",
			msgData:       "cpu=90",
			shouldReceive: false,
		},
	}

	t.Parallel()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := NewHub()
			go h.Run()
			defer h.Stop()

			// Create a client channel
			ch := make(chan Event, 1)

			// Register
			h.register <- clientInfo{ch: ch, topic: tt.clientTopic}

			// Wait for registration
			if !waitForClients(h, 1) {
				t.Fatal("timed out waiting for client registration")
			}

			// Broadcast
			h.Broadcast(tt.msgTopic, tt.msgData)

			// Check reception
			select {
			case got := <-ch:
				if !tt.shouldReceive {
					t.Errorf("expected no message, got %v", got)
				} else {
					if got.Topic != tt.msgTopic {
						t.Errorf("got topic %s, want %s", got.Topic, tt.msgTopic)
					}
					if got.Data != tt.msgData {
						t.Errorf("got data %v, want %v", got.Data, tt.msgData)
					}
				}
			case <-time.After(50 * time.Millisecond):
				if tt.shouldReceive {
					t.Errorf("expected message, got timeout")
				}
			}

			// Cleanup
			h.unregister <- ch
		})
	}
}

func TestHub_Concurrency(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	var wg sync.WaitGroup
	clientCount := 50
	broadcastCount := 50

	// Launch clients
	for i := 0; i < clientCount; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			ch := make(chan Event, broadcastCount) // buffered to avoid blocking

			// Register
			select {
			case h.register <- clientInfo{ch: ch, topic: "*"}:
			case <-time.After(1 * time.Second):
				t.Error("timed out registering")
				return
			}

			// Receive some messages or wait
			timer := time.After(100 * time.Millisecond)
		loop:
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						break loop
					}
				case <-timer:
					break loop
				}
			}

			// Unregister
			select {
			case h.unregister <- ch:
			case <-time.After(1 * time.Second):
				t.Error("timed out unregistering")
			}

			// Drain channel
			for range ch { //nolint:revive // explicit drain
			}
		}(i)
	}

	// Launch Broadcasters
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < broadcastCount/5; j++ {
				h.Broadcast("test", "data")
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

func TestHub_ServeHTTP(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cleanup if test fails

	req := httptest.NewRequest("GET", "/sse?topic=test", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan bool)
	go func() {
		h.ServeHTTP(w, req)
		done <- true
	}()

	// Wait for registration
	if !waitForClients(h, 1) {
		t.Fatal("timed out waiting for http client registration")
	}

	msg := "hello http"
	h.Broadcast("test", msg)

	// We need to wait a bit for the message to be processed and written to the response recorder.
	// Since we don't have a way to peek into the recorder stream in real-time easily without blocking,
	// we'll rely on a small sleep here or we could continuously check the body?
	// Checking the body continuously is race-prone on the buffer.
	// Given we removed the sleep for registration, a small sleep for data propagation
	// in an integration test is acceptable, OR we can improve this by using a pipe?
	// For now, let's keep the sleep for propagation but we fixed the registration race.
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop ServeHTTP
	cancel()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("ServeHTTP did not return after context cancellation")
	}

	resp := w.Body.String()
	// event: test
	// data: "hello http"
	if !strings.Contains(resp, `data: "hello http"`) {
		t.Errorf("expected response to contain data, got %q", resp)
	}
}

// waitForClients polls until the expected number of clients are connected.
func waitForClients(h *Hub, expected int) bool {
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if h.ClientCount() == expected {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
