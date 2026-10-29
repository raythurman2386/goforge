package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
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
