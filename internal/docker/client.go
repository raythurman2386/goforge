package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Client wraps the Docker SDK client.
type Client struct {
	docker *client.Client
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
