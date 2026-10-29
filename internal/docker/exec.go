package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// maxExecOutput caps how much exec output is kept in memory. Callers only use
// it for a short failure message.
const maxExecOutput = 16 * 1024

// execPollInterval is how often Exec checks that a finished command has
// reported its exit code.
var execPollInterval = 100 * time.Millisecond

// ExecOptions describes a command to run inside a running container.
type ExecOptions struct {
	// Cmd is the command and its arguments.
	Cmd []string
	// User runs the command as this user (name or uid). Empty uses the image default.
	User string
	// Env adds KEY=value pairs to the container's environment for this command only.
	Env []string
}

// ExecResult is the outcome of a command run with Exec.
type ExecResult struct {
	ExitCode int
	// Output is the combined stdout and stderr, truncated to the first 16 KiB.
	// It can contain whatever the command printed, so redact before logging.
	Output string
}

// Exec runs a command inside a running container, waits for it to finish, and
// returns its exit code and output. A non-zero exit code is not an error.
func (c *Client) Exec(ctx context.Context, id string, opts ExecOptions) (ExecResult, error) {
	if len(opts.Cmd) == 0 {
		return ExecResult{}, errors.New("exec: empty command")
	}
	created, err := c.docker.ContainerExecCreate(ctx, id, container.ExecOptions{
		User:         opts.User,
		Env:          opts.Env,
		Cmd:          opts.Cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := c.docker.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("failed to attach exec: %w", err)
	}
	// StdCopy does not watch ctx, so a command that never exits would block it
	// forever. Closing the attach connection when ctx ends unblocks the read.
	stopClose := context.AfterFunc(ctx, resp.Close)
	out := &cappedBuffer{limit: maxExecOutput}
	_, copyErr := stdcopy.StdCopy(out, out, resp.Reader)
	stopClose()
	resp.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ExecResult{}, ctxErr
	}
	if copyErr != nil {
		return ExecResult{}, fmt.Errorf("failed to read exec output: %w", copyErr)
	}

	// The stream ends when the process exits, but the daemon can report the
	// exit code a moment later.
	for {
		inspect, inspErr := c.docker.ContainerExecInspect(ctx, created.ID)
		if inspErr != nil {
			return ExecResult{}, fmt.Errorf("failed to inspect exec: %w", inspErr)
		}
		if !inspect.Running {
			return ExecResult{ExitCode: inspect.ExitCode, Output: out.String()}, nil
		}
		select {
		case <-ctx.Done():
			return ExecResult{}, ctx.Err()
		case <-time.After(execPollInterval):
		}
	}
}

// WaitHealthy blocks until the container's health check reports healthy, or,
// when the container has no health check, until it is running. It returns an
// error when the container turns unhealthy or stops, or when ctx ends.
func (c *Client) WaitHealthy(ctx context.Context, id string, poll time.Duration) error {
	for {
		info, err := c.docker.ContainerInspect(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}
		if done, stateErr := healthReady(info.State); done || stateErr != nil {
			return stateErr
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("container did not become healthy: %w", ctx.Err())
		case <-time.After(poll):
		}
	}
}

// healthReady reports whether a container in state st is ready for commands.
// A nil error with done false means keep waiting.
func healthReady(st *types.ContainerState) (bool, error) {
	if st == nil {
		return false, errors.New("container has no state")
	}
	if !st.Running {
		if st.Restarting || st.Status == "created" {
			return false, nil
		}
		return false, fmt.Errorf("container is %s (exit code %d)", st.Status, st.ExitCode)
	}
	if st.Health == nil || st.Health.Status == "" || st.Health.Status == types.NoHealthcheck {
		return true, nil
	}
	switch st.Health.Status {
	case types.Healthy:
		return true, nil
	case types.Unhealthy:
		return false, errors.New("container health check reports unhealthy")
	default:
		return false, nil
	}
}

// cappedBuffer keeps the first limit bytes written and drops the rest, while
// reporting every write as complete so the stream is still drained.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }
