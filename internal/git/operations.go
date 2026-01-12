package git

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// CloneOptions represents options for cloning a repository.
type CloneOptions struct {
	URL    string
	Branch string
	Path   string
	Depth  int
}

// Clone clones a repository to the specified path.
func (s *Service) Clone(ctx context.Context, opts CloneOptions) error {
	cloneOpts := &git.CloneOptions{
		URL:           opts.URL,
		ReferenceName: plumbing.NewBranchReferenceName(opts.Branch),
		SingleBranch:  true,
		Progress:      os.Stdout,
	}

	if opts.Depth > 0 {
		cloneOpts.Depth = opts.Depth
	}

	_, err := git.PlainCloneContext(ctx, opts.Path, false, cloneOpts)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

// Pull pulls latest changes for a repository.
func (s *Service) Pull(ctx context.Context, path string) error {
	r, err := git.PlainOpen(path)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.PullContext(ctx, &git.PullOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull repository: %w", err)
	}

	return nil
}
