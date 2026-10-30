package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// dockerAPI is the subset of the Moby client (github.com/moby/moby/client)
// that GoForge uses. Tests substitute a fake.
type dockerAPI interface {
	ImageInspect(ctx context.Context, imageID string, opts ...client.ImageInspectOption) (client.ImageInspectResult, error)
	ImagePull(ctx context.Context, ref string, options client.ImagePullOptions) (client.ImagePullResponse, error)
	ImageBuild(ctx context.Context, buildContext io.Reader, options client.ImageBuildOptions) (client.ImageBuildResult, error)
	ImagePrune(ctx context.Context, options client.ImagePruneOptions) (client.ImagePruneResult, error)
	ImageRemove(ctx context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error)
	ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerRename(ctx context.Context, containerID string, options client.ContainerRenameOptions) (client.ContainerRenameResult, error)
	ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerPrune(ctx context.Context, options client.ContainerPruneOptions) (client.ContainerPruneResult, error)
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error)
	ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)
	ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error)
	DiskUsage(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error)
	Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error)
	Close() error
}

// Client wraps the Docker SDK client.
type Client struct {
	docker dockerAPI
}

// NewClient creates a new Docker client.
func NewClient(host string) (*Client, error) {
	var opts []client.Opt
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Client{docker: cli}, nil
}

// ImageExists reports whether imageName is present locally.
func (c *Client) ImageExists(ctx context.Context, imageName string) (bool, error) {
	_, err := c.docker.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}

// EnsureImage pulls an image if it doesn't exist locally. It never refreshes
// a local image, so locally built app images (which are not in any registry)
// are used as-is. Service deploys refresh mutable tags with PullImage instead.
func (c *Client) EnsureImage(ctx context.Context, imageName string) error {
	if _, err := c.docker.ImageInspect(ctx, imageName); err == nil {
		return nil
	}
	return c.PullImage(ctx, imageName)
}

// PullImage pulls imageName from its registry even when a local copy exists,
// so a moved tag such as latest resolves to the registry's current image.
// Pulling an unchanged tag only checks the manifest and downloads nothing.
func (c *Client) PullImage(ctx context.Context, imageName string) error {
	reader, err := c.docker.ImagePull(ctx, imageName, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Drain the reader to complete the pull
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
	}

	return nil
}

// IsDigestReference reports whether ref pins an image by content digest
// (name@sha256:...). A digest always names the same image; any tag, including
// an implied latest, can be moved upstream.
func IsDigestReference(ref string) bool {
	return strings.Contains(ref, "@")
}

// GetStats returns a one-shot statistics snapshot for a container. The caller
// closes Body.
func (c *Client) GetStats(ctx context.Context, id string) (client.ContainerStatsResult, error) {
	return c.docker.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false})
}

// Ping checks the connection to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.docker.Ping(ctx, client.PingOptions{})
	return err
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.docker.Close()
}

// DiskUsage returns system-wide disk usage information, with per-object items.
func (c *Client) DiskUsage(ctx context.Context) (*client.DiskUsageResult, error) {
	usage, err := c.docker.DiskUsage(ctx, client.DiskUsageOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		BuildCache: true,
		Verbose:    true,
	})
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

// ListContainers returns GoForge-managed containers from Docker.
func (c *Client) ListContainers(ctx context.Context, all bool) ([]container.Summary, error) {
	options := client.ContainerListOptions{
		All:     all,
		Filters: make(client.Filters).Add("label", ManagedLabelFilter),
	}
	res, err := c.docker.ContainerList(ctx, options)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}
