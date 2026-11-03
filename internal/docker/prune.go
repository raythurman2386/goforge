package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/build"
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

// PruneBuildCache removes build cache that no build is using. Build cache
// records carry no labels, so this is engine-wide, like the Build Cache size
// on the dashboard. All is set so every unused record goes, not only dangling
// ones; that matches the reclaimable figure docker system df reports. Records
// in use by a running build are kept by the daemon.
func (c *Client) PruneBuildCache(ctx context.Context) (build.CachePruneReport, error) {
	res, err := c.docker.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: true})
	if err != nil {
		return build.CachePruneReport{}, err
	}
	return res.Report, nil
}

// PruneSummary totals what CleanupSystem removed, from the reports Docker
// returns for each prune step.
type PruneSummary struct {
	// SpaceReclaimed is the bytes freed across all three steps.
	SpaceReclaimed uint64
	// Containers is the number of stopped managed containers removed.
	Containers int
	// Images counts deleted images only. ImagesDeleted also lists untag
	// entries, so an image with two tags would otherwise count three times.
	Images int
	// BuildCacheRecords is the number of build cache records removed.
	BuildCacheRecords int
}

// Empty reports whether nothing was removed and no space was freed.
func (s PruneSummary) Empty() bool {
	return s.SpaceReclaimed == 0 && s.Containers == 0 && s.Images == 0 && s.BuildCacheRecords == 0
}

// CleanupSystem prunes stopped managed containers, unused managed images, and
// unused (engine-wide) build cache, in that order, and stops at the first
// error. The summary covers the steps that finished, also when one fails.
func (c *Client) CleanupSystem(ctx context.Context) (PruneSummary, error) {
	var sum PruneSummary
	cr, err := c.PruneContainers(ctx)
	if err != nil {
		return sum, fmt.Errorf("failed to prune containers: %w", err)
	}
	sum.SpaceReclaimed += cr.SpaceReclaimed
	sum.Containers = len(cr.ContainersDeleted)

	ir, err := c.PruneImages(ctx)
	if err != nil {
		return sum, fmt.Errorf("failed to prune images: %w", err)
	}
	sum.SpaceReclaimed += ir.SpaceReclaimed
	for _, d := range ir.ImagesDeleted {
		if d.Deleted != "" {
			sum.Images++
		}
	}

	br, err := c.PruneBuildCache(ctx)
	if err != nil {
		return sum, fmt.Errorf("failed to prune build cache: %w", err)
	}
	sum.SpaceReclaimed += br.SpaceReclaimed
	sum.BuildCacheRecords = len(br.CachesDeleted)
	return sum, nil
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
