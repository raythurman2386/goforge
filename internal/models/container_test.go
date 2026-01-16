package models

import (
	"encoding/json"
	"testing"
)

func TestContainer_GetPorts(t *testing.T) {
	tests := []struct {
		name      string
		ports     json.RawMessage
		wantLen   int
		wantFirst *PortMapping
		wantErr   bool
	}{
		{
			name:    "nil ports",
			ports:   nil,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "empty ports",
			ports:   json.RawMessage([]byte{}),
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "single port mapping",
			ports:   json.RawMessage(`[{"host": 8080, "container": 80, "protocol": "tcp"}]`),
			wantLen: 1,
			wantFirst: &PortMapping{
				Host:      8080,
				Container: 80,
				Protocol:  "tcp",
			},
			wantErr: false,
		},
		{
			name:    "multiple port mappings",
			ports:   json.RawMessage(`[{"host": 8080, "container": 80}, {"host": 443, "container": 443, "protocol": "tcp"}]`),
			wantLen: 2,
			wantFirst: &PortMapping{
				Host:      8080,
				Container: 80,
				Protocol:  "",
			},
			wantErr: false,
		},
		{
			name:    "port without protocol (optional field)",
			ports:   json.RawMessage(`[{"host": 3000, "container": 3000}]`),
			wantLen: 1,
			wantFirst: &PortMapping{
				Host:      3000,
				Container: 3000,
				Protocol:  "",
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			ports:   json.RawMessage(`not valid json`),
			wantErr: true,
		},
		{
			name:    "wrong JSON structure",
			ports:   json.RawMessage(`{"host": 8080}`),
			wantErr: true,
		},
		{
			name:    "empty array",
			ports:   json.RawMessage(`[]`),
			wantLen: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{Ports: tt.ports}
			got, err := c.GetPorts()

			if (err != nil) != tt.wantErr {
				t.Errorf("Container.GetPorts() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != tt.wantLen {
				t.Errorf("Container.GetPorts() len = %v, want %v", len(got), tt.wantLen)
				return
			}

			if tt.wantFirst != nil && len(got) > 0 {
				if got[0].Host != tt.wantFirst.Host {
					t.Errorf("Container.GetPorts()[0].Host = %v, want %v", got[0].Host, tt.wantFirst.Host)
				}
				if got[0].Container != tt.wantFirst.Container {
					t.Errorf("Container.GetPorts()[0].Container = %v, want %v", got[0].Container, tt.wantFirst.Container)
				}
				if got[0].Protocol != tt.wantFirst.Protocol {
					t.Errorf("Container.GetPorts()[0].Protocol = %v, want %v", got[0].Protocol, tt.wantFirst.Protocol)
				}
			}
		})
	}
}

func TestContainer_GetLabels(t *testing.T) {
	tests := []struct {
		name    string
		labels  json.RawMessage
		wantLen int
		wantKey string
		wantVal string
		wantErr bool
	}{
		{
			name:    "nil labels",
			labels:  nil,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "empty labels",
			labels:  json.RawMessage([]byte{}),
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "single label",
			labels:  json.RawMessage(`{"app": "goforge"}`),
			wantLen: 1,
			wantKey: "app",
			wantVal: "goforge",
			wantErr: false,
		},
		{
			name:    "multiple labels",
			labels:  json.RawMessage(`{"app": "goforge", "env": "production", "version": "1.0.0"}`),
			wantLen: 3,
			wantKey: "app",
			wantVal: "goforge",
			wantErr: false,
		},
		{
			name:    "traefik labels",
			labels:  json.RawMessage(`{"traefik.enable": "true", "traefik.http.routers.app.rule": "Host(\u0060example.com\u0060)"}`),
			wantLen: 2,
			wantKey: "traefik.enable",
			wantVal: "true",
			wantErr: false,
		},
		{
			name:    "empty object",
			labels:  json.RawMessage(`{}`),
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			labels:  json.RawMessage(`not valid json`),
			wantErr: true,
		},
		{
			name:    "array instead of object",
			labels:  json.RawMessage(`["label1", "label2"]`),
			wantErr: true,
		},
		{
			name:    "nested object (invalid)",
			labels:  json.RawMessage(`{"nested": {"key": "value"}}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Container{Labels: tt.labels}
			got, err := c.GetLabels()

			if (err != nil) != tt.wantErr {
				t.Errorf("Container.GetLabels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(got) != tt.wantLen {
				t.Errorf("Container.GetLabels() len = %v, want %v", len(got), tt.wantLen)
				return
			}

			if tt.wantKey != "" {
				if val, ok := got[tt.wantKey]; !ok {
					t.Errorf("Container.GetLabels() missing key %q", tt.wantKey)
				} else if val != tt.wantVal {
					t.Errorf("Container.GetLabels()[%q] = %v, want %v", tt.wantKey, val, tt.wantVal)
				}
			}
		})
	}
}
