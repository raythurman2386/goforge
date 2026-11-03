package docker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/moby/moby/api/types/build"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
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
	if _, err := c.CleanupSystem(context.Background()); err != nil {
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
	_, err := (&Client{docker: api}).CleanupSystem(context.Background())
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
	sum, err := (&Client{docker: api}).CleanupSystem(context.Background())
	if err == nil || err.Error() != "failed to prune images: nope" {
		t.Fatalf("err = %v, want wrapped image prune error", err)
	}
	if called {
		t.Fatal("build cache prune ran after image prune failed")
	}
	if !sum.Empty() {
		t.Fatalf("summary = %+v, want empty (containers pruned nothing)", sum)
	}
}

// t1905u: CleanupSystem sums SpaceReclaimed across the three prunes and
// counts containers, deleted images (not untag entries), and build cache
// records, so the dashboard toast can say what was freed.
func TestCleanupSystem_Summary(t *testing.T) {
	t.Parallel()

	api := &mockDockerAPI{
		containersPrune: func(context.Context, client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
			return client.ContainerPruneResult{Report: container.PruneReport{
				ContainersDeleted: []string{"c1", "c2"},
				SpaceReclaimed:    1000,
			}}, nil
		},
		imagesPruneFunc: func(context.Context, client.ImagePruneOptions) (client.ImagePruneResult, error) {
			return client.ImagePruneResult{Report: image.PruneReport{
				ImagesDeleted: []image.DeleteResponse{
					{Untagged: "goforge/app:old"},
					{Deleted: "sha256:aaa"},
					{Deleted: "sha256:bbb"},
					{Untagged: "goforge/app:older"},
					{Deleted: "sha256:ccc"},
				},
				SpaceReclaimed: 20000,
			}}, nil
		},
		buildCachePruneFunc: func(context.Context, client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
			return client.BuildCachePruneResult{Report: build.CachePruneReport{
				CachesDeleted:  []string{"r1", "r2", "r3", "r4"},
				SpaceReclaimed: 300000,
			}}, nil
		},
	}
	sum, err := (&Client{docker: api}).CleanupSystem(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := PruneSummary{SpaceReclaimed: 321000, Containers: 2, Images: 3, BuildCacheRecords: 4}
	if sum != want {
		t.Fatalf("summary = %+v, want %+v", sum, want)
	}
	if sum.Empty() {
		t.Fatal("non-zero summary reports Empty")
	}
}

// t1905u: an image prune error stops the run, and the summary keeps what the
// container step already freed.
func TestCleanupSystem_ImageErrorKeepsContainerSummary(t *testing.T) {
	t.Parallel()

	buildCalled := false
	api := &mockDockerAPI{
		containersPrune: func(context.Context, client.ContainerPruneOptions) (client.ContainerPruneResult, error) {
			return client.ContainerPruneResult{Report: container.PruneReport{ContainersDeleted: []string{"c1"}, SpaceReclaimed: 512}}, nil
		},
		imagesPruneFunc: func(context.Context, client.ImagePruneOptions) (client.ImagePruneResult, error) {
			return client.ImagePruneResult{}, errors.New("nope")
		},
		buildCachePruneFunc: func(context.Context, client.BuildCachePruneOptions) (client.BuildCachePruneResult, error) {
			buildCalled = true
			return client.BuildCachePruneResult{}, nil
		},
	}
	sum, err := (&Client{docker: api}).CleanupSystem(context.Background())
	if err == nil {
		t.Fatal("want image prune error")
	}
	if buildCalled {
		t.Fatal("build cache prune ran after image prune failed")
	}
	if want := (PruneSummary{SpaceReclaimed: 512, Containers: 1}); sum != want {
		t.Fatalf("summary = %+v, want %+v", sum, want)
	}
}

// exactMatch reports whether term is set to exactly the single value want.
func exactMatch(f client.Filters, term, want string) bool {
	return len(f[term]) == 1 && f[term][want]
}
