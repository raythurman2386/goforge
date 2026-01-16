package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// PruneImages removes unused images.
func (c *Client) PruneImages(ctx context.Context) (image.PruneReport, error) {
	return c.docker.ImagesPrune(ctx, filters.NewArgs())
}

// PruneContainers removes stopped containers.
func (c *Client) PruneContainers(ctx context.Context) (container.PruneReport, error) {
	return c.docker.ContainersPrune(ctx, filters.NewArgs())
}

// CleanupSystem runs both image and container pruning.
func (c *Client) CleanupSystem(ctx context.Context) error {
	if _, err := c.PruneContainers(ctx); err != nil {
		return fmt.Errorf("failed to prune containers: %w", err)
	}
	if _, err := c.PruneImages(ctx); err != nil {
		return fmt.Errorf("failed to prune images: %w", err)
	}
	return nil
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
