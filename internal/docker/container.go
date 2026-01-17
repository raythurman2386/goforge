package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// ContainerOptions represents options for creating a container.
type ContainerOptions struct {
	Name         string
	Image        string
	Env          []string
	Labels       map[string]string
	PortMappings map[string]string // container_port -> host_port
	NetworkName  string
	Binds        []string
	HealthCheck  *container.HealthConfig
}

// CreateContainer creates and starts a new container.
func (c *Client) CreateContainer(ctx context.Context, opts ContainerOptions) (string, error) {
	// Ensure the image exists locally, pull if needed
	if err := c.EnsureImage(ctx, opts.Image); err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	config := &container.Config{
		Image:       opts.Image,
		Env:         opts.Env,
		Labels:      opts.Labels,
		Healthcheck: opts.HealthCheck,
	}

	hostConfig := &container.HostConfig{
		PortBindings: make(nat.PortMap),
		Binds:        opts.Binds,
	}

	for containerPort, hostPort := range opts.PortMappings {
		p, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			return "", fmt.Errorf("invalid port %s: %w", containerPort, err)
		}
		hostConfig.PortBindings[p] = []nat.PortBinding{
			{HostPort: hostPort},
		}
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: make(map[string]*network.EndpointSettings),
	}

	if opts.NetworkName != "" {
		networkingConfig.EndpointsConfig[opts.NetworkName] = &network.EndpointSettings{}
	}

	resp, err := c.docker.ContainerCreate(ctx, config, hostConfig, networkingConfig, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := c.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return resp.ID, fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// StopContainer stops and removes a container.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	if err := c.docker.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	if err := c.docker.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	return nil
}
