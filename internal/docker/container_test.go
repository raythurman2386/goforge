package docker

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestCreateContainer_ResourceLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		memory     int64
		nanoCPUs   int64
		wantMemory int64
		wantCPUs   int64
	}{
		{name: "limits set", memory: 256 * 1024 * 1024, nanoCPUs: 250_000_000, wantMemory: 256 * 1024 * 1024, wantCPUs: 250_000_000},
		{name: "unset stays unlimited", memory: 0, nanoCPUs: 0, wantMemory: 0, wantCPUs: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got *container.HostConfig
			c := &Client{docker: &mockDockerAPI{
				containerCreateFunc: func(_ context.Context, _ *container.Config, hc *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					got = hc
					return container.CreateResponse{ID: "c1"}, nil
				},
			}}
			if _, err := c.CreateContainer(context.Background(), ContainerOptions{
				Name:     "svc",
				Image:    "nginx:latest",
				Memory:   tt.memory,
				NanoCPUs: tt.nanoCPUs,
			}); err != nil {
				t.Fatalf("CreateContainer() error = %v", err)
			}
			if got == nil {
				t.Fatal("ContainerCreate was not called")
			}
			if got.Memory != tt.wantMemory {
				t.Errorf("HostConfig.Memory = %d, want %d", got.Memory, tt.wantMemory)
			}
			if got.NanoCPUs != tt.wantCPUs {
				t.Errorf("HostConfig.NanoCPUs = %d, want %d", got.NanoCPUs, tt.wantCPUs)
			}
		})
	}
}

func TestCreateContainer_PortBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mappings    []PortBinding
		wantBinding map[string][]nat.PortBinding
		wantExposed []string
		wantErr     string
	}{
		{
			name:        "no mappings publishes nothing",
			wantBinding: map[string][]nat.PortBinding{},
		},
		{
			name:        "loopback tcp",
			mappings:    []PortBinding{{ContainerPort: "22", Protocol: "tcp", HostIP: "127.0.0.1", HostPort: "2223"}},
			wantBinding: map[string][]nat.PortBinding{"22/tcp": {{HostIP: "127.0.0.1", HostPort: "2223"}}},
			wantExposed: []string{"22/tcp"},
		},
		{
			name:        "empty protocol means tcp",
			mappings:    []PortBinding{{ContainerPort: "22", HostIP: "127.0.0.1", HostPort: "2223"}},
			wantBinding: map[string][]nat.PortBinding{"22/tcp": {{HostIP: "127.0.0.1", HostPort: "2223"}}},
			wantExposed: []string{"22/tcp"},
		},
		{name: "empty host IP refused", mappings: []PortBinding{{ContainerPort: "22", HostPort: "2223"}}, wantErr: "host IP is required"},
		{name: "0.0.0.0 refused", mappings: []PortBinding{{ContainerPort: "22", HostIP: "0.0.0.0", HostPort: "2223"}}, wantErr: "not allowed"},
		{name: ":: refused", mappings: []PortBinding{{ContainerPort: "22", HostIP: "::", HostPort: "2223"}}, wantErr: "not allowed"},
		{name: "empty host port refused", mappings: []PortBinding{{ContainerPort: "22", HostIP: "127.0.0.1"}}, wantErr: "host port is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotHC *container.HostConfig
			var gotCfg *container.Config
			c := &Client{docker: &mockDockerAPI{
				containerCreateFunc: func(_ context.Context, cfg *container.Config, hc *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					gotCfg, gotHC = cfg, hc
					return container.CreateResponse{ID: "c1"}, nil
				},
			}}
			_, err := c.CreateContainer(context.Background(), ContainerOptions{Name: "svc", Image: "gitea/gitea:1", PortMappings: tt.mappings})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				if gotHC != nil {
					t.Fatal("ContainerCreate called for a refused binding")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateContainer() error = %v", err)
			}
			if len(gotHC.PortBindings) != len(tt.wantBinding) {
				t.Fatalf("PortBindings = %v, want %v", gotHC.PortBindings, tt.wantBinding)
			}
			for port, want := range tt.wantBinding {
				got := gotHC.PortBindings[nat.Port(port)]
				if !slices.Equal(got, want) {
					t.Errorf("PortBindings[%s] = %v, want %v", port, got, want)
				}
			}
			if len(gotCfg.ExposedPorts) != len(tt.wantExposed) {
				t.Errorf("ExposedPorts = %v, want %v", gotCfg.ExposedPorts, tt.wantExposed)
			}
			for _, p := range tt.wantExposed {
				if _, ok := gotCfg.ExposedPorts[nat.Port(p)]; !ok {
					t.Errorf("ExposedPorts missing %s", p)
				}
			}
		})
	}
}

func TestRenameContainer(t *testing.T) {
	t.Parallel()
	var gotID, gotName string
	c := &Client{docker: &mockDockerAPI{
		containerRenameFunc: func(_ context.Context, id, name string) error {
			gotID, gotName = id, name
			return nil
		},
	}}
	if err := c.RenameContainer(context.Background(), "abc", "svc-replaced"); err != nil {
		t.Fatal(err)
	}
	if gotID != "abc" || gotName != "svc-replaced" {
		t.Errorf("rename(%q, %q), want (abc, svc-replaced)", gotID, gotName)
	}
}

func TestCreateContainer_Cmd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  []string
	}{
		{name: "command replaces image CMD", cmd: []string{"sh", "-c", "exec redis-server /tmp/r.conf"}},
		{name: "no command keeps image CMD", cmd: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got *container.Config
			c := &Client{docker: &mockDockerAPI{
				containerCreateFunc: func(_ context.Context, cfg *container.Config, _ *container.HostConfig, _ *network.NetworkingConfig, _ *ocispec.Platform, _ string) (container.CreateResponse, error) {
					got = cfg
					return container.CreateResponse{ID: "c1"}, nil
				},
			}}
			if _, err := c.CreateContainer(context.Background(), ContainerOptions{Name: "svc", Image: "redis:7", Cmd: tt.cmd}); err != nil {
				t.Fatalf("CreateContainer() error = %v", err)
			}
			if got == nil {
				t.Fatal("ContainerCreate was not called")
			}
			if !slices.Equal([]string(got.Cmd), tt.cmd) {
				t.Errorf("Config.Cmd = %q, want %q", got.Cmd, tt.cmd)
			}
		})
	}
}
