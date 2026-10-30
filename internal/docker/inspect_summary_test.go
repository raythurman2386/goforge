package docker

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func TestSummarizeInspect(t *testing.T) {
	t.Parallel()

	if SummarizeInspect(nil) != nil || SummarizeInspect(&container.InspectResponse{}) != nil {
		t.Fatal("empty inspect must summarize to nil")
	}

	port, err := network.ParsePort("5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	info := &container.InspectResponse{
		ID:           "abc123",
		Name:         "/goforge-svc-pg",
		RestartCount: 3,
		Config: &container.Config{
			Image: "postgres:16",
			Env:   []string{"POSTGRES_PASSWORD=hunter2"},
			Cmd:   []string{"postgres", "-c", "password=hunter2"},
			Labels: map[string]string{
				"traefik.enable":               "true",
				"traefik.http.routers.pg.rule": "Host(`pg.example.test`)",
				"secret.label":                 "hunter2",
			},
		},
		State: &container.State{
			Status: "running", Running: true, OOMKilled: true, StartedAt: "2026-10-30T18:00:00Z",
			Health: &container.Health{Status: "healthy"},
		},
		HostConfig: &container.HostConfig{Resources: container.Resources{Memory: 512 << 20, NanoCPUs: 1_500_000_000}},
		NetworkSettings: &container.NetworkSettings{
			Ports:    network.PortMap{port: {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "15432"}}},
			Networks: map[string]*network.EndpointSettings{"b-net": {}, "a-net": {}},
		},
	}
	s := SummarizeInspect(info)
	if s.Name != "goforge-svc-pg" || s.Image != "postgres:16" || s.Health != "healthy" || !s.OOMKilled || s.RestartCount != 3 {
		t.Fatalf("summary = %+v", s)
	}
	if s.Limits.CPUs != 1.5 || s.Limits.MemoryBytes != 512<<20 {
		t.Fatalf("limits = %+v", s.Limits)
	}
	if len(s.Ports) != 1 || s.Ports[0].HostPort != "15432" || s.Ports[0].HostIP != "127.0.0.1" {
		t.Fatalf("ports = %+v", s.Ports)
	}
	if got := s.HostRules(); len(got) != 1 || got[0] != "Host(`pg.example.test`)" {
		t.Fatalf("host rules = %v", got)
	}
	if !reflect.DeepEqual(s.Networks, []string{"a-net", "b-net"}) {
		t.Fatalf("networks = %v", s.Networks)
	}
	if _, ok := s.Started(); !ok {
		t.Fatal("started time did not parse")
	}

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("summary JSON leaks a secret: %s", raw)
	}
	// The /api/v1 container state keys. Changing them changes the public API.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"docker_id", "exit_code", "health", "image", "limits", "name", "networks", "oom_killed", "ports",
		"restart_count", "running", "started_at", "state", "traefik_enable", "traefik_rules", "volumes"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("JSON keys = %v\nwant %v", keys, want)
	}
}
