package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// RestartUnlessStopped is the restart policy used for GoForge-managed containers.
const RestartUnlessStopped = "unless-stopped"

// ManagedLabelKey is the Docker label that marks a GoForge container or image.
const ManagedLabelKey = "goforge.managed"

// ManagedLabelValue is the value of ManagedLabelKey.
const ManagedLabelValue = "true"

// ManagedLabelFilter selects Docker objects carrying ManagedLabelKey=ManagedLabelValue.
const ManagedLabelFilter = ManagedLabelKey + "=" + ManagedLabelValue

// ContainerOptions represents options for creating a container.
type ContainerOptions struct {
	Name          string
	Image         string
	Env           []string
	Labels        map[string]string
	PortMappings  map[string]string // container_port -> host_port
	NetworkName   string
	Binds         []string
	HealthCheck   *container.HealthConfig
	RestartPolicy string
	// Memory is the hard memory limit in bytes. Zero means no limit.
	Memory int64
	// NanoCPUs is the CPU quota in units of 1e-9 CPUs. Zero means no limit.
	NanoCPUs int64
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
		Resources: container.Resources{
			Memory:   opts.Memory,
			NanoCPUs: opts.NanoCPUs,
		},
	}
	if opts.RestartPolicy != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(opts.RestartPolicy)}
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

// StopContainer stops a container.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	if err := c.docker.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// RemoveContainer removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.docker.ContainerRemove(ctx, id, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// StartContainer starts an existing container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.docker.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	if err := c.docker.ContainerRestart(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

// InspectContainer gets details about a container.
func (c *Client) InspectContainer(ctx context.Context, id string) (types.ContainerJSON, error) {
	return c.docker.ContainerInspect(ctx, id)
}
