package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moby/moby/client"
)

func TestCleanupSystem_ScopesToManagedLabel(t *testing.T) {
	t.Parallel()

	var imageFilters, containerFilters client.Filters
	var buildOpts *client.BuildCachePruneOptions
	var order []string
	api := &mockDockerAPI{
		imagesPruneFunc: func(_ context.Context, opts client.ImagePruneOptions) (client.ImagePruneResult, error) {
			imageFilters = opts.Filters
			order = append(order, "images")
			return client.ImagePruneResult{}, nil
		},
		containersPrune: func(_ context.Context, opts client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
			containerFilters = opts.Filters
			order = append(order, "containers")
			return client.ContainerPruneResult{}, nil
		},
		buildCachePruneFunc: func(_ context.Context, opts client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
			buildOpts = &opts
			order = append(order, "build-cache")
			return client.BuildCachePruneResult{}, nil
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
	// t1876u: build cache is pruned last, all unused records (engine-wide,
	// no label filter exists for build cache), with no space limits.
	if buildOpts == nil {
		t.Fatal("CleanupSystem did not call BuildCachePrune")
	}
	if !buildOpts.All || buildOpts.ReservedSpace != 0 || buildOpts.MaxUsedSpace != 0 || buildOpts.MinFreeSpace != 0 || len(buildOpts.Filters) != 0 {
		t.Fatalf("build cache prune opts = %#v, want All only", *buildOpts)
	}
	if want := []string{"containers", "images", "build-cache"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("prune order = %v, want %v", order, want)
	}
}

func TestCleanupSystem_BuildCacheError(t *testing.T) {
	t.Parallel()

	api := &mockDockerAPI{
		buildCachePruneFunc: func(context.Context, client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
			return client.BuildCachePruneResult{}, errors.New("boom")
		},
	}
	err := (&Client{docker: api}).CleanupSystem(context.Background())
	if err == nil || err.Error() != "failed to prune build cache: boom" {
		t.Fatalf("err = %v, want wrapped build cache error", err)
	}
}

func TestCleanupSystem_ImageErrorSkipsBuildCache(t *testing.T) {
	t.Parallel()

	called := false
	api := &mockDockerAPI{
		imagesPruneFunc: func(context.Context, client.ImagePruneOptions) (client.ImagePruneResult, error) {
			return client.ImagePruneResult{}, errors.New("nope")
		},
		buildCachePruneFunc: func(context.Context, client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
			called = true
			return client.BuildCachePruneResult{}, nil
		},
	}
	if err := (&Client{docker: api}).CleanupSystem(context.Background()); err == nil {
		t.Fatal("want image prune error")
	}
	if called {
		t.Fatal("build cache prune ran after image prune failed")
	}
}

// exactMatch reports whether term is set to exactly the single value want.
func exactMatch(f client.Filters, term, want string) bool {
	return len(f[term]) == 1 && f[term][want]
}
