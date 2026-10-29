package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// mockDockerAPI implements dockerAPI for testing.
type mockDockerAPI struct {
	imageInspectFunc     func(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
	imagePullFunc        func(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	imageBuildFunc       func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
	imagesPruneFunc      func(ctx context.Context, pruneFilters filters.Args) (image.PruneReport, error)
	imageRemoveFunc      func(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error)
	containerCreateFunc  func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	containerStartFunc   func(ctx context.Context, containerID string, options container.StartOptions) error
	containerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
	containerStopFunc    func(ctx context.Context, containerID string, options container.StopOptions) error
	containerRemoveFunc  func(ctx context.Context, containerID string, options container.RemoveOptions) error
	containerRenameFunc  func(ctx context.Context, containerID, newName string) error
	containerStatsFunc   func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	containerLogsFunc    func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	containersPrune      func(ctx context.Context, pruneFilters filters.Args) (container.PruneReport, error)
	pingFunc             func(ctx context.Context) (types.Ping, error)
	closeFunc            func() error
	containerInspectFunc func(ctx context.Context, containerID string) (types.ContainerJSON, error)
	execCreateFunc       func(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error)
	execAttachFunc       func(ctx context.Context, execID string, options container.ExecAttachOptions) (types.HijackedResponse, error)
	execInspectFunc      func(ctx context.Context, execID string) (container.ExecInspect, error)
}

func (m *mockDockerAPI) ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error) {
	if m.imageInspectFunc != nil {
		return m.imageInspectFunc(ctx, imageID)
	}
	return types.ImageInspect{}, nil, nil
}

func (m *mockDockerAPI) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	if m.imagePullFunc != nil {
		return m.imagePullFunc(ctx, ref, options)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerAPI) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	if m.imageBuildFunc != nil {
		return m.imageBuildFunc(ctx, buildContext, options)
	}
	return types.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockDockerAPI) ImagesPrune(ctx context.Context, pruneFilters filters.Args) (image.PruneReport, error) {
	if m.imagesPruneFunc != nil {
		return m.imagesPruneFunc(ctx, pruneFilters)
	}
	return image.PruneReport{}, nil
}

func (m *mockDockerAPI) ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error) {
	if m.imageRemoveFunc != nil {
		return m.imageRemoveFunc(ctx, imageID, options)
	}
	return nil, nil
}

func (m *mockDockerAPI) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	if m.containerCreateFunc != nil {
		return m.containerCreateFunc(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}
	return container.CreateResponse{ID: "mock-container-id"}, nil
}

func (m *mockDockerAPI) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if m.containerStartFunc != nil {
		return m.containerStartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.containerRestartFunc != nil {
		return m.containerRestartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.containerStopFunc != nil {
		return m.containerStopFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if m.containerRemoveFunc != nil {
		return m.containerRemoveFunc(ctx, containerID, options)
	}
	return nil
}

func (m *mockDockerAPI) ContainerRename(ctx context.Context, containerID, newName string) error {
	if m.containerRenameFunc != nil {
		return m.containerRenameFunc(ctx, containerID, newName)
	}
	return nil
}

func (m *mockDockerAPI) ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
	if m.containerStatsFunc != nil {
		return m.containerStatsFunc(ctx, containerID, stream)
	}
	return container.StatsResponseReader{}, nil
}

func (m *mockDockerAPI) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	if m.containerLogsFunc != nil {
		return m.containerLogsFunc(ctx, containerID, options)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerAPI) ContainersPrune(ctx context.Context, pruneFilters filters.Args) (container.PruneReport, error) {
	if m.containersPrune != nil {
		return m.containersPrune(ctx, pruneFilters)
	}
	return container.PruneReport{}, nil
}

func (m *mockDockerAPI) Ping(ctx context.Context) (types.Ping, error) {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return types.Ping{}, nil
}

func (m *mockDockerAPI) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockDockerAPI) DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
	return types.DiskUsage{}, nil
}

func (m *mockDockerAPI) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	return nil, nil
}

func (m *mockDockerAPI) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.containerInspectFunc != nil {
		return m.containerInspectFunc(ctx, containerID)
	}
	return types.ContainerJSON{}, nil
}

func (m *mockDockerAPI) ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error) {
	if m.execCreateFunc != nil {
		return m.execCreateFunc(ctx, containerID, options)
	}
	return types.IDResponse{ID: "mock-exec-id"}, nil
}

func (m *mockDockerAPI) ContainerExecAttach(ctx context.Context, execID string, options container.ExecAttachOptions) (types.HijackedResponse, error) {
	if m.execAttachFunc != nil {
		return m.execAttachFunc(ctx, execID, options)
	}
	return types.HijackedResponse{}, errors.New("exec attach not mocked")
}

func (m *mockDockerAPI) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	if m.execInspectFunc != nil {
		return m.execInspectFunc(ctx, execID)
	}
	return container.ExecInspect{}, nil
}

func TestClient_EnsureImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		imageName        string
		imageInspectFunc func(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
		imagePullFunc    func(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
		wantErr          bool
		wantPull         bool
	}{
		{
			name:      "Image exists locally",
			imageName: "postgres:16",
			imageInspectFunc: func(_ context.Context, _ string) (types.ImageInspect, []byte, error) {
				return types.ImageInspect{ID: "sha256:abc123"}, nil, nil
			},
			wantErr:  false,
			wantPull: false,
		},
		{
			name:      "Image not found - pull succeeds",
			imageName: "redis:7",
			imageInspectFunc: func(_ context.Context, _ string) (types.ImageInspect, []byte, error) {
				return types.ImageInspect{}, nil, errors.New("image not found")
			},
			imagePullFunc: func(_ context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
				if ref != "redis:7" {
					return nil, errors.New("unexpected image name")
				}
				return io.NopCloser(strings.NewReader(`{"status":"Pulling"}`)), nil
			},
			wantErr:  false,
			wantPull: true,
		},
		{
			name:      "Image not found - pull fails",
			imageName: "invalid:image",
			imageInspectFunc: func(_ context.Context, _ string) (types.ImageInspect, []byte, error) {
				return types.ImageInspect{}, nil, errors.New("image not found")
			},
			imagePullFunc: func(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
				return nil, errors.New("pull failed: unauthorized")
			},
			wantErr:  true,
			wantPull: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pullCalled := false
			mockClient := &mockDockerAPI{
				imageInspectFunc: tt.imageInspectFunc,
				imagePullFunc: func(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
					pullCalled = true
					if tt.imagePullFunc != nil {
						return tt.imagePullFunc(ctx, ref, options)
					}
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			c := &Client{docker: mockClient}
			err := c.EnsureImage(context.Background(), tt.imageName)

			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureImage() error = %v, wantErr %v", err, tt.wantErr)
			}

			if pullCalled != tt.wantPull {
				t.Errorf("EnsureImage() pullCalled = %v, wantPull %v", pullCalled, tt.wantPull)
			}
		})
	}
}

func TestClient_Ping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pingFunc func(ctx context.Context) (types.Ping, error)
		wantErr  bool
	}{
		{
			name: "Ping succeeds",
			pingFunc: func(_ context.Context) (types.Ping, error) {
				return types.Ping{APIVersion: "1.45"}, nil
			},
			wantErr: false,
		},
		{
			name: "Ping fails",
			pingFunc: func(_ context.Context) (types.Ping, error) {
				return types.Ping{}, errors.New("connection refused")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := &mockDockerAPI{
				pingFunc: tt.pingFunc,
			}

			c := &Client{docker: mockClient}
			err := c.Ping(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_ImageExists(t *testing.T) {
	tests := []struct {
		name       string
		inspectErr error
		want       bool
		wantErr    bool
	}{
		{"present", nil, true, false},
		{"missing", errdefs.NotFound(errors.New("No such image")), false, false},
		{"daemon error", errors.New("connection refused"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{docker: &mockDockerAPI{
				imageInspectFunc: func(context.Context, string) (types.ImageInspect, []byte, error) {
					return types.ImageInspect{}, nil, tt.inspectErr
				},
			}}
			got, err := c.ImageExists(context.Background(), "example/web:1.0")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("ImageExists() = %v, %v; want %v, wantErr %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
