package docker

import (
	"context"
	"io"

	"github.com/moby/moby/client"
)

// StreamLogs streams logs for a container.
func (c *Client) StreamLogs(ctx context.Context, id string, follow bool) (io.ReadCloser, error) {
	opts := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	}

	return c.docker.ContainerLogs(ctx, id, opts)
}

// LogsOptions bounds a one-shot log read.
type LogsOptions struct {
	// Tail is the number of lines from the end, or "all".
	Tail string
	// Since is a Unix timestamp or a Go duration such as "10m", relative to now.
	Since string
}

// ContainerLogs returns a bounded, non-following log stream for a container.
// The stream is multiplexed for non-TTY containers; demux it with stdcopy.
func (c *Client) ContainerLogs(ctx context.Context, id string, opts LogsOptions) (io.ReadCloser, error) {
	return c.docker.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     false,
		Timestamps: true,
		Tail:       opts.Tail,
		Since:      opts.Since,
	})
}
