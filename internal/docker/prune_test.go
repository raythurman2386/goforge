package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

func TestCleanupSystem_ScopesToManagedLabel(t *testing.T) {
	t.Parallel()

	var imageFilters, containerFilters filters.Args
	api := &mockDockerAPI{
		imagesPruneFunc: func(_ context.Context, pruneFilters filters.Args) (image.PruneReport, error) {
			imageFilters = pruneFilters
			return image.PruneReport{}, nil
		},
		containersPrune: func(_ context.Context, pruneFilters filters.Args) (container.PruneReport, error) {
			containerFilters = pruneFilters
			return container.PruneReport{}, nil
		},
	}

	client := &Client{docker: api}
	if err := client.CleanupSystem(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !imageFilters.ExactMatch("label", ManagedLabelFilter) {
		t.Fatalf("image prune filters = %#v", imageFilters)
	}
	if !imageFilters.ExactMatch("dangling", "false") {
		t.Fatal("image prune should include dangling=false")
	}
	if !containerFilters.ExactMatch("label", ManagedLabelFilter) {
		t.Fatalf("container prune filters = %#v", containerFilters)
	}
	if len(containerFilters.Get("dangling")) != 0 {
		t.Fatalf("container prune should not set dangling: %#v", containerFilters)
	}
}
