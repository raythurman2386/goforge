package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
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
	ContainerRename(ctx context.Context, containerID, newContainerName string) error
	ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	ContainersPrune(ctx context.Context, pruneFilters filters.Args) (container.PruneReport, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerExecCreate(ctx context.Context, containerID string, options container.ExecOptions) (types.IDResponse, error)
	ContainerExecAttach(ctx context.Context, execID string, options container.ExecAttachOptions) (types.HijackedResponse, error)
	ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
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

// ImageExists reports whether imageName is present locally.
func (c *Client) ImageExists(ctx context.Context, imageName string) (bool, error) {
	_, _, err := c.docker.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return true, nil
	}
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
}

// EnsureImage pulls an image if it doesn't exist locally. It never refreshes
// a local image, so locally built app images (which are not in any registry)
// are used as-is. Service deploys refresh mutable tags with PullImage instead.
func (c *Client) EnsureImage(ctx context.Context, imageName string) error {
	if _, _, err := c.docker.ImageInspectWithRaw(ctx, imageName); err == nil {
		return nil
	}
	return c.PullImage(ctx, imageName)
}

// PullImage pulls imageName from its registry even when a local copy exists,
// so a moved tag such as latest resolves to the registry's current image.
// Pulling an unchanged tag only checks the manifest and downloads nothing.
func (c *Client) PullImage(ctx context.Context, imageName string) error {
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

// IsDigestReference reports whether ref pins an image by content digest
// (name@sha256:...). A digest always names the same image; any tag, including
// an implied latest, can be moved upstream.
func IsDigestReference(ref string) bool {
	return strings.Contains(ref, "@")
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
