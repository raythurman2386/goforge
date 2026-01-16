package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// PruneImages removes unused images.
func (c *Client) PruneImages(ctx context.Context) (types.ImagesPruneReport, error) {
	// Prune dangling images
	return c.docker.ImagesPrune(ctx, filters.NewArgs())
}

// PruneContainers removes stopped containers.
func (c *Client) PruneContainers(ctx context.Context) (types.ContainersPruneReport, error) {
	return c.docker.ContainersPrune(ctx, filters.NewArgs())
}

// RemoveImage removes an image by ID.
func (c *Client) RemoveImage(ctx context.Context, imageID string) error {
	_, err := c.docker.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         true,
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}
	return nil
}
