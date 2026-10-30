package docker

import (
	"context"
	"testing"

	"github.com/moby/moby/client"
)

func TestCleanupSystem_ScopesToManagedLabel(t *testing.T) {
	t.Parallel()

	var imageFilters, containerFilters client.Filters
	api := &mockDockerAPI{
		imagesPruneFunc: func(_ context.Context, opts client.ImagePruneOptions) (client.ImagePruneResult, error) {
			imageFilters = opts.Filters
			return client.ImagePruneResult{}, nil
		},
		containersPrune: func(_ context.Context, opts client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
			containerFilters = opts.Filters
			return client.ContainerPruneResult{}, nil
		},
	}

	c := &Client{docker: api}
	if err := c.CleanupSystem(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !exactMatch(imageFilters, "label", ManagedLabelFilter) {
		t.Fatalf("image prune filters = %#v", imageFilters)
	}
	if !exactMatch(imageFilters, "dangling", "false") {
		t.Fatal("image prune should include dangling=false")
	}
	if !exactMatch(containerFilters, "label", ManagedLabelFilter) {
		t.Fatalf("container prune filters = %#v", containerFilters)
	}
	if len(containerFilters["dangling"]) != 0 {
		t.Fatalf("container prune should not set dangling: %#v", containerFilters)
	}
}

// exactMatch reports whether term is set to exactly the single value want.
func exactMatch(f client.Filters, term, want string) bool {
	return len(f[term]) == 1 && f[term][want]
}
