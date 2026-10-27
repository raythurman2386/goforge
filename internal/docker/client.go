package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// dockerAPI defines the Docker client interface for testing.
type dockerAPI interface {
	ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
	ImagesPrune(ctx context.Context, pruneFilters filters.Args) (image.PruneReport, error)
	ImageRemove(ctx context.Context, imageID string, options image.RemoveOptions) ([]image.DeleteResponse, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	ContainersPrune(ctx context.Context, pruneFilters filters.Args) (container.PruneReport, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
	Ping(ctx context.Context) (types.Ping, error)
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
	opts = append(opts, client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Client{docker: cli}, nil
}

// EnsureImage pulls an image if it doesn't exist locally.
func (c *Client) EnsureImage(ctx context.Context, imageName string) error {
	// Check if image exists locally
	_, _, err := c.docker.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		// Image exists locally
		return nil
	}

	// Image doesn't exist, pull it
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
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

// GetStats returns statistics for a container.
func (c *Client) GetStats(ctx context.Context, id string) (container.StatsResponseReader, error) {
	return c.docker.ContainerStats(ctx, id, false)
}

// Ping checks the connection to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.docker.Ping(ctx)
	return err
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.docker.Close()
}

// DiskUsage returns system-wide disk usage information.
func (c *Client) DiskUsage(ctx context.Context) (*types.DiskUsage, error) {
	usage, err := c.docker.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return nil, err
	}
	return &usage, nil
}

// ListContainers returns GoForge-managed containers from Docker.
func (c *Client) ListContainers(ctx context.Context, all bool) ([]types.Container, error) {
	options := container.ListOptions{
		All:     all,
		Filters: filters.NewArgs(filters.Arg("label", ManagedLabelFilter)),
	}
	return c.docker.ContainerList(ctx, options)
}
