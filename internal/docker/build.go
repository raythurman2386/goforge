// Package docker provides Docker SDK integration.
package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/archive"
)

// BuildOptions represents options for building a Docker image.
type BuildOptions struct {
	ImageName  string
	Dockerfile string
	ContextDir string
	BuildArgs  map[string]*string
}

// BuildImage builds a Docker image from a context directory.
func (c *Client) BuildImage(ctx context.Context, opts BuildOptions, output io.Writer) error {
	tar, err := archive.TarWithOptions(opts.ContextDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("failed to create tar archive: %w", err)
	}

	buildOptions := types.ImageBuildOptions{
		Dockerfile: opts.Dockerfile,
		Tags:       []string{opts.ImageName},
		BuildArgs:  opts.BuildArgs,
		Remove:     true,
	}

	res, err := c.docker.ImageBuild(ctx, tar, buildOptions)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer res.Body.Close()

	_, err = io.Copy(output, res.Body)
	return err
}
