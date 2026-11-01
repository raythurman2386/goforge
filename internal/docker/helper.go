package docker

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// HelperOptions describes a short-lived helper container that borrows
// another container's volumes, such as the one `goforge backup
// restore-service` uses to write into a stopped service's data volume.
type HelperOptions struct {
	Name string
	// Image is normally the image ID of the container whose volumes are
	// borrowed, so no pull happens and the users and tools match.
	Image string
	// VolumesFrom is the container whose volumes are mounted, read-write.
	VolumesFrom string
	// Entrypoint replaces the image's entrypoint (for example sleep).
	Entrypoint []string
	Cmd        []string
	Labels     map[string]string
}

// StartHelper creates and starts a helper container: run as root, no
// network, no restart policy, nothing published. It does not pull: the
// image must be present. When the container is created but cannot start,
// the returned ID is set and the caller removes it with RemoveHelper.
func (c *Client) StartHelper(ctx context.Context, opts HelperOptions) (string, error) {
	if opts.Image == "" || opts.VolumesFrom == "" || len(opts.Entrypoint) == 0 {
		return "", errors.New("helper container needs an image, a volumes-from container, and an entrypoint")
	}
	resp, err := c.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      opts.Image,
			Entrypoint: opts.Entrypoint,
			Cmd:        opts.Cmd,
			User:       "0:0",
			Labels:     opts.Labels,
		},
		HostConfig: &container.HostConfig{
			VolumesFrom: []string{opts.VolumesFrom},
			NetworkMode: container.NetworkMode("none"),
		},
		Name: opts.Name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create helper container: %w", err)
	}
	if _, err := c.docker.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return resp.ID, fmt.Errorf("failed to start helper container: %w", err)
	}
	return resp.ID, nil
}

// RemoveHelper force-removes a helper container and never its volumes: the
// volumes it borrowed belong to another container.
func (c *Client) RemoveHelper(ctx context.Context, id string) error {
	if _, err := c.docker.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true, RemoveVolumes: false}); err != nil {
		return fmt.Errorf("failed to remove helper container: %w", err)
	}
	return nil
}

// CopyToContainer extracts a tar stream (plain or gzip) at dest inside the
// container. Ownership in the tar headers is kept.
func (c *Client) CopyToContainer(ctx context.Context, id, dest string, tarStream io.Reader) error {
	if _, err := c.docker.CopyToContainer(ctx, id, client.CopyToContainerOptions{
		DestinationPath: dest,
		Content:         tarStream,
		CopyUIDGID:      true,
	}); err != nil {
		return fmt.Errorf("failed to copy into container: %w", err)
	}
	return nil
}
