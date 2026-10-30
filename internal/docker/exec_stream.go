package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// maxStreamStderr caps how much stderr ExecStream keeps. Stderr is only used
// for a short failure message.
const maxStreamStderr = 16 * 1024

// ExecStreamResult is the outcome of a command run with ExecStream.
type ExecStreamResult struct {
	ExitCode int
	// Stderr is the first 16 KiB the command wrote to stderr. It can contain
	// whatever the command printed, so redact before logging.
	Stderr string
	// Bytes is how many stdout bytes were written to the caller's writer.
	Bytes int64
}

// ExecStream runs a command inside a running container and copies its stdout
// to stdout as it arrives, with no size limit. Stderr is kept separately and
// capped. A non-zero exit code is not an error; callers must check ExitCode
// before trusting what they received.
//
// Exec stays the right call for short hook output: it caps the combined
// output at 16 KiB, which would silently truncate a dump.
func (c *Client) ExecStream(ctx context.Context, id string, opts ExecOptions, stdout io.Writer) (ExecStreamResult, error) {
	if len(opts.Cmd) == 0 {
		return ExecStreamResult{}, errors.New("exec: empty command")
	}
	if stdout == nil {
		return ExecStreamResult{}, errors.New("exec: nil stdout writer")
	}
	created, err := c.docker.ExecCreate(ctx, id, client.ExecCreateOptions{
		User:         opts.User,
		Env:          opts.Env,
		Cmd:          opts.Cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecStreamResult{}, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := c.docker.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return ExecStreamResult{}, fmt.Errorf("failed to attach exec: %w", err)
	}
	stopClose := context.AfterFunc(ctx, resp.Close)
	counted := &countingWriter{w: stdout}
	stderr := &cappedBuffer{limit: maxStreamStderr}
	_, copyErr := stdcopy.StdCopy(counted, stderr, resp.Reader)
	stopClose()
	resp.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ExecStreamResult{}, ctxErr
	}
	if copyErr != nil {
		return ExecStreamResult{}, fmt.Errorf("failed to stream exec output: %w", copyErr)
	}

	for {
		inspect, inspErr := c.docker.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
		if inspErr != nil {
			return ExecStreamResult{}, fmt.Errorf("failed to inspect exec: %w", inspErr)
		}
		if !inspect.Running {
			return ExecStreamResult{ExitCode: inspect.ExitCode, Stderr: stderr.String(), Bytes: counted.n}, nil
		}
		select {
		case <-ctx.Done():
			return ExecStreamResult{}, ctx.Err()
		case <-time.After(execPollInterval):
		}
	}
}

// countingWriter counts the bytes passed through to w.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
