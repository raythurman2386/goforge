// Package sse provides Server-Sent Events hub and broadcasting.
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Event represents an SSE event.
type Event struct {
	Topic string      `json:"topic"`
	Data  interface{} `json:"data"`
}

// Hub manages SSE connections and broadcasts.
type Hub struct {
	clients    map[chan Event]string // client channel -> topic
	register   chan clientInfo
	unregister chan chan Event
	broadcast  chan Event
	quit       chan struct{}
	mu         sync.RWMutex
}

type clientInfo struct {
	ch    chan Event
	topic string
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[chan Event]string),
		register:   make(chan clientInfo),
		unregister: make(chan chan Event),
		broadcast:  make(chan Event),
		quit:       make(chan struct{}),
	}
}

// Run starts the SSE hub.
func (h *Hub) Run() {
	for {
		select {
		case <-h.quit:
			h.mu.Lock()
			for ch := range h.clients {
				close(ch)
			}
			h.clients = make(map[chan Event]string)
			h.mu.Unlock()
			return
		case info := <-h.register:
			h.mu.Lock()
			h.clients[info.ch] = info.topic
			h.mu.Unlock()
		case ch := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[ch]; ok {
				delete(h.clients, ch)
				close(ch)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.RLock()
			for ch, topic := range h.clients {
				if topic == "*" || topic == event.Topic {
					select {
					case ch <- event:
					default:
						// Client buffer full, skip or close?
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Stop stops the SSE hub and closes all client channels.
func (h *Hub) Stop() {
	close(h.quit)
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends an event to all subscribed clients.
func (h *Hub) Broadcast(topic string, data interface{}) {
	select {
	case h.broadcast <- Event{Topic: topic, Data: data}:
	case <-h.quit:
	}
}

// ServeHTTP handles SSE connections.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	topic := r.URL.Query().Get("topic")
	if topic == "" {
		topic = "*"
	}

	ch := make(chan Event, 10)
	h.register <- clientInfo{ch: ch, topic: topic}

	defer func() {
		h.unregister <- ch
	}()

	// Send heartbeat
	go func() {
		// ticker implementation ...
	}()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Topic, data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
