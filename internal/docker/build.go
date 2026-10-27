// Package docker provides Docker SDK integration.
package docker

import (
	"context"
	"encoding/json"
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
		Labels:     map[string]string{ManagedLabelKey: ManagedLabelValue},
	}

	res, err := c.docker.ImageBuild(ctx, tar, buildOptions)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer res.Body.Close()

	type buildMessage struct {
		Stream      string `json:"stream"`
		Error       string `json:"error"`
		ErrorDetail struct {
			Message string `json:"message"`
		} `json:"errorDetail"`
	}

	decoder := json.NewDecoder(res.Body)
	for {
		var msg buildMessage
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode build output: %w", err)
		}

		if msg.Error != "" {
			return fmt.Errorf("docker build failed: %s", msg.Error)
		}

		if msg.Stream != "" {
			if _, err := output.Write([]byte(msg.Stream)); err != nil {
				return fmt.Errorf("failed to write build output: %w", err)
			}
		}
	}

	return nil
}
