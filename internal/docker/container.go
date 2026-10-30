package docker

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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
	Name  string
	Image string
	// Cmd replaces the image's CMD. Empty keeps the image default.
	Cmd    []string
	Env    []string
	Labels map[string]string
	// PortMappings publishes container ports on the host. Each binding names
	// its host IP; an empty HostIP is refused so a binding never falls back
	// to Docker's all-interfaces default.
	PortMappings  []PortBinding
	NetworkName   string
	Binds         []string
	HealthCheck   *container.HealthConfig
	RestartPolicy string
	// Memory is the hard memory limit in bytes. Zero means no limit.
	Memory int64
	// NanoCPUs is the CPU quota in units of 1e-9 CPUs. Zero means no limit.
	NanoCPUs int64
}

// PortBinding publishes one container port on a host IP and port.
type PortBinding struct {
	// ContainerPort is the port inside the container, such as "22".
	ContainerPort string
	// Protocol is tcp or udp. Empty means tcp.
	Protocol string
	// HostIP is the host address to bind, such as 127.0.0.1. Required.
	HostIP string
	// HostPort is the host port to bind, such as "2223".
	HostPort string
}

// portBindings converts bindings into Docker's exposed-port set and port map.
func portBindings(bindings []PortBinding) (exposed network.PortSet, pm network.PortMap, err error) {
	if len(bindings) == 0 {
		return nil, network.PortMap{}, nil
	}
	exposed = make(network.PortSet, len(bindings))
	pm = make(network.PortMap, len(bindings))
	for _, b := range bindings {
		proto := b.Protocol
		if proto == "" {
			proto = "tcp"
		}
		p, err := network.ParsePort(b.ContainerPort + "/" + proto)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid port %s/%s: %w", b.ContainerPort, proto, err)
		}
		if b.HostIP == "" {
			return nil, nil, fmt.Errorf("port %s: host IP is required; refusing to publish on all interfaces", p)
		}
		ip, ipErr := netip.ParseAddr(b.HostIP)
		if ipErr != nil || ip.IsUnspecified() {
			return nil, nil, fmt.Errorf("port %s: host IP %q is not allowed; publish on a specific address such as 127.0.0.1", p, b.HostIP)
		}
		if b.HostPort == "" {
			return nil, nil, fmt.Errorf("port %s: host port is required", p)
		}
		exposed[p] = struct{}{}
		pm[p] = append(pm[p], network.PortBinding{HostIP: ip, HostPort: b.HostPort})
	}
	return exposed, pm, nil
}

// CreateContainer creates and starts a new container. When the container is
// created but cannot start (for example, its host port is taken), the returned
// ID is the created container and the error says why; the caller removes it.
func (c *Client) CreateContainer(ctx context.Context, opts ContainerOptions) (string, error) {
	// Ensure the image exists locally, pull if needed
	if err := c.EnsureImage(ctx, opts.Image); err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	exposed, bindings, err := portBindings(opts.PortMappings)
	if err != nil {
		return "", err
	}

	config := &container.Config{
		Image:        opts.Image,
		Cmd:          opts.Cmd,
		ExposedPorts: exposed,
		Env:          opts.Env,
		Labels:       opts.Labels,
		Healthcheck:  opts.HealthCheck,
	}

	hostConfig := &container.HostConfig{
		PortBindings: bindings,
		Binds:        opts.Binds,
		Resources: container.Resources{
			Memory:   opts.Memory,
			NanoCPUs: opts.NanoCPUs,
		},
	}
	if opts.RestartPolicy != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(opts.RestartPolicy)}
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: make(map[string]*network.EndpointSettings),
	}

	if opts.NetworkName != "" {
		networkingConfig.EndpointsConfig[opts.NetworkName] = &network.EndpointSettings{}
	}

	resp, err := c.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Name:             opts.Name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if _, err := c.docker.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return resp.ID, fmt.Errorf("failed to start container: %w", err)
	}

	return resp.ID, nil
}

// StopContainer stops a container.
func (c *Client) StopContainer(ctx context.Context, id string) error {
	if _, err := c.docker.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// RemoveContainer removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if _, err := c.docker.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// RenameContainer gives an existing container a new name.
func (c *Client) RenameContainer(ctx context.Context, id, newName string) error {
	if _, err := c.docker.ContainerRename(ctx, id, client.ContainerRenameOptions{NewName: newName}); err != nil {
		return fmt.Errorf("failed to rename container: %w", err)
	}
	return nil
}

// StartContainer starts an existing container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	if _, err := c.docker.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// RestartContainer restarts a container.
func (c *Client) RestartContainer(ctx context.Context, id string) error {
	if _, err := c.docker.ContainerRestart(ctx, id, client.ContainerRestartOptions{}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

// InspectContainer gets details about a container.
func (c *Client) InspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	res, err := c.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return res.Container, nil
}
