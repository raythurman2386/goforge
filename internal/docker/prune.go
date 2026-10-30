package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// managedPruneFilters limits prune operations to objects GoForge created.
func managedPruneFilters() client.Filters {
	args := make(client.Filters)
	args.Add("label", ManagedLabelFilter)
	// dangling=false removes unused tagged images, not only dangling ones.
	// The label filter keeps this from touching images GoForge did not build.
	args.Add("dangling", "false")
	return args
}

// PruneImages removes unused images built by GoForge.
func (c *Client) PruneImages(ctx context.Context) (image.PruneReport, error) {
	res, err := c.docker.ImagePrune(ctx, client.ImagePruneOptions{Filters: managedPruneFilters()})
	if err != nil {
		return image.PruneReport{}, err
	}
	return res.Report, nil
}

// PruneContainers removes stopped GoForge containers.
func (c *Client) PruneContainers(ctx context.Context) (container.PruneReport, error) {
	args := make(client.Filters).Add("label", ManagedLabelFilter)
	res, err := c.docker.ContainerPrune(ctx, client.ContainerPruneOptions{Filters: args})
	if err != nil {
		return container.PruneReport{}, err
	}
	return res.Report, nil
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
	_, err := c.docker.ImageRemove(ctx, imageID, client.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	})
	if err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
	}
	return nil
}
