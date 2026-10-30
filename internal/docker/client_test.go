package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

// pullResponse adapts a plain reader to client.ImagePullResponse for tests.
type pullResponse struct {
	io.ReadCloser
}

func (pullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

func (pullResponse) Wait(context.Context) error { return nil }

func pullBody(body string) client.ImagePullResponse {
	return pullResponse{io.NopCloser(strings.NewReader(body))}
}

// mockDockerAPI implements dockerAPI for testing.
type mockDockerAPI struct {
	imageInspectFunc     func(ctx context.Context, imageID string) (client.ImageInspectResult, error)
	imagePullFunc        func(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	imageBuildFunc       func(ctx context.Context, buildContext io.Reader, options client.ImageBuildOptions) (client.ImageBuildResult, error)
	imagesPruneFunc      func(ctx context.Context, options client.ImagePruneOptions) (client.ImagePruneResult, error)
	imageRemoveFunc      func(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error)
	containerCreateFunc  func(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	containerStartFunc   func(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	containerRestartFunc func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	containerStopFunc    func(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	containerRemoveFunc  func(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	containerRenameFunc  func(ctx context.Context, containerID string, options client.ContainerRenameOptions) (client.ContainerRenameResult, error)
	containerStatsFunc   func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	containerLogsFunc    func(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	containersPrune      func(ctx context.Context, options client.ContainerPruneOptions) (client.ContainerPruneResult, error)
	pingFunc             func(ctx context.Context) (client.PingResult, error)
	closeFunc            func() error
	containerInspectFunc func(ctx context.Context, containerID string) (client.ContainerInspectResult, error)
	execCreateFunc       func(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error)
	execAttachFunc       func(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)
	execInspectFunc      func(ctx context.Context, execID string) (client.ExecInspectResult, error)
}

func (m *mockDockerAPI) ImageInspect(ctx context.Context, imageID string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	if m.imageInspectFunc != nil {
		return m.imageInspectFunc(ctx, imageID)
	}
	return client.ImageInspectResult{}, nil
}

func (m *mockDockerAPI) ImagePull(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	if m.imagePullFunc != nil {
		return m.imagePullFunc(ctx, ref, options)
	}
	return pullBody(""), nil
}

func (m *mockDockerAPI) ImageBuild(ctx context.Context, buildContext io.Reader, options client.ImageBuildOptions) (client.ImageBuildResult, error) {
	if m.imageBuildFunc != nil {
		return m.imageBuildFunc(ctx, buildContext, options)
	}
	return client.ImageBuildResult{Body: io.NopCloser(strings.NewReader(""))}, nil
}

func (m *mockDockerAPI) ImagePrune(ctx context.Context, options client.ImagePruneOptions) (client.ImagePruneResult, error) {
	if m.imagesPruneFunc != nil {
		return m.imagesPruneFunc(ctx, options)
	}
	return client.ImagePruneResult{}, nil
}

func (m *mockDockerAPI) ImageRemove(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	if m.imageRemoveFunc != nil {
		return m.imageRemoveFunc(ctx, imageID, options)
	}
	return client.ImageRemoveResult{}, nil
}

func (m *mockDockerAPI) ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	if m.containerCreateFunc != nil {
		return m.containerCreateFunc(ctx, options)
	}
	return client.ContainerCreateResult{ID: "mock-container-id"}, nil
}

func (m *mockDockerAPI) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	if m.containerStartFunc != nil {
		return m.containerStartFunc(ctx, containerID, options)
	}
	return client.ContainerStartResult{}, nil
}

func (m *mockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	if m.containerRestartFunc != nil {
		return m.containerRestartFunc(ctx, containerID, options)
	}
	return client.ContainerRestartResult{}, nil
}

func (m *mockDockerAPI) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	if m.containerStopFunc != nil {
		return m.containerStopFunc(ctx, containerID, options)
	}
	return client.ContainerStopResult{}, nil
}

func (m *mockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	if m.containerRemoveFunc != nil {
		return m.containerRemoveFunc(ctx, containerID, options)
	}
	return client.ContainerRemoveResult{}, nil
}

func (m *mockDockerAPI) ContainerRename(ctx context.Context, containerID string, options client.ContainerRenameOptions) (client.ContainerRenameResult, error) {
	if m.containerRenameFunc != nil {
		return m.containerRenameFunc(ctx, containerID, options)
	}
	return client.ContainerRenameResult{}, nil
}

func (m *mockDockerAPI) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	if m.containerStatsFunc != nil {
		return m.containerStatsFunc(ctx, containerID, options)
	}
	return client.ContainerStatsResult{}, nil
}

func (m *mockDockerAPI) ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	if m.containerLogsFunc != nil {
		return m.containerLogsFunc(ctx, containerID, options)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDockerAPI) ContainerPrune(ctx context.Context, options client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
	if m.containersPrune != nil {
		return m.containersPrune(ctx, options)
	}
	return client.ContainerPruneResult{}, nil
}

func (m *mockDockerAPI) Ping(ctx context.Context, _ client.PingOptions) (client.PingResult, error) {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return client.PingResult{}, nil
}

func (m *mockDockerAPI) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockDockerAPI) DiskUsage(context.Context, client.DiskUsageOptions) (client.DiskUsageResult, error) {
	return client.DiskUsageResult{}, nil
}

func (m *mockDockerAPI) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{}, nil
}

func (m *mockDockerAPI) ContainerInspect(ctx context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.containerInspectFunc != nil {
		return m.containerInspectFunc(ctx, containerID)
	}
	return client.ContainerInspectResult{}, nil
}

func (m *mockDockerAPI) ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	if m.execCreateFunc != nil {
		return m.execCreateFunc(ctx, containerID, options)
	}
	return client.ExecCreateResult{ID: "mock-exec-id"}, nil
}

func (m *mockDockerAPI) ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error) {
	if m.execAttachFunc != nil {
		return m.execAttachFunc(ctx, execID, options)
	}
	return client.ExecAttachResult{}, errors.New("exec attach not mocked")
}

func (m *mockDockerAPI) ExecInspect(ctx context.Context, execID string, _ client.ExecInspectOptions) (client.ExecInspectResult, error) {
	if m.execInspectFunc != nil {
		return m.execInspectFunc(ctx, execID)
	}
	return client.ExecInspectResult{}, nil
}

func TestClient_EnsureImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		imageName        string
		imageInspectFunc func(ctx context.Context, imageID string) (client.ImageInspectResult, error)
		imagePullFunc    func(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error)
		wantErr          bool
		wantPull         bool
	}{
		{
			name:      "Image exists locally",
			imageName: "postgres:16",
			imageInspectFunc: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
				return client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:abc123"}}, nil
			},
			wantErr:  false,
			wantPull: false,
		},
		{
			name:      "Image not found - pull succeeds",
			imageName: "redis:7",
			imageInspectFunc: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
				return client.ImageInspectResult{}, errors.New("image not found")
			},
			imagePullFunc: func(_ context.Context, ref string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
				if ref != "redis:7" {
					return nil, errors.New("unexpected image name")
				}
				return pullBody(`{"status":"Pulling"}`), nil
			},
			wantErr:  false,
			wantPull: true,
		},
		{
			name:      "Image not found - pull fails",
			imageName: "invalid:image",
			imageInspectFunc: func(_ context.Context, _ string) (client.ImageInspectResult, error) {
				return client.ImageInspectResult{}, errors.New("image not found")
			},
			imagePullFunc: func(_ context.Context, _ string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
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
				imagePullFunc: func(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
					pullCalled = true
					if tt.imagePullFunc != nil {
						return tt.imagePullFunc(ctx, ref, options)
					}
					return pullBody(""), nil
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
		pingFunc func(ctx context.Context) (client.PingResult, error)
		wantErr  bool
	}{
		{
			name: "Ping succeeds",
			pingFunc: func(_ context.Context) (client.PingResult, error) {
				return client.PingResult{APIVersion: "1.45"}, nil
			},
			wantErr: false,
		},
		{
			name: "Ping fails",
			pingFunc: func(_ context.Context) (client.PingResult, error) {
				return client.PingResult{}, errors.New("connection refused")
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
		{"missing", fmt.Errorf("No such image: %w", cerrdefs.ErrNotFound), false, false},
		{"daemon error", errors.New("connection refused"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{docker: &mockDockerAPI{
				imageInspectFunc: func(context.Context, string) (client.ImageInspectResult, error) {
					return client.ImageInspectResult{}, tt.inspectErr
				},
			}}
			got, err := c.ImageExists(context.Background(), "example/web:1.0")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("ImageExists() = %v, %v; want %v, wantErr %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
