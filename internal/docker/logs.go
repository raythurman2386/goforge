package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
)

// StreamLogs streams logs for a container.
func (c *Client) StreamLogs(ctx context.Context, id string, follow bool) (io.ReadCloser, error) {
	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	}

	return c.docker.ContainerLogs(ctx, id, opts)
}
