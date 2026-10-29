package docker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

func TestClient_ExecStream_LargeStdoutNotTruncated(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 3*maxExecOutput+17)
	api := &mockDockerAPI{
		execCreateFunc: func(_ context.Context, _ string, _ container.ExecOptions) (types.IDResponse, error) {
			return types.IDResponse{ID: "exec-1"}, nil
		},
		execAttachFunc: func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
			return hijacked(t, big, "warn\n"), nil
		},
		execInspectFunc: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{Running: false, ExitCode: 0}, nil
		},
	}
	c := &Client{docker: api}

	var out bytes.Buffer
	res, err := c.ExecStream(context.Background(), "ctr", ExecOptions{Cmd: []string{"pg_dump"}}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != big {
		t.Fatalf("stdout len = %d, want %d", out.Len(), len(big))
	}
	if res.Bytes != int64(len(big)) {
		t.Errorf("Bytes = %d, want %d", res.Bytes, len(big))
	}
	if res.Stderr != "warn\n" {
		t.Errorf("Stderr = %q, want stderr kept apart from stdout", res.Stderr)
	}
}

func TestClient_ExecStream_ExitCodeAndStderrCap(t *testing.T) {
	t.Parallel()

	api := &mockDockerAPI{
		execCreateFunc: func(_ context.Context, _ string, _ container.ExecOptions) (types.IDResponse, error) {
			return types.IDResponse{ID: "exec-1"}, nil
		},
		execAttachFunc: func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
			return hijacked(t, "", strings.Repeat("e", maxStreamStderr+100)), nil
		},
		execInspectFunc: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{ExitCode: 2}, nil
		},
	}
	c := &Client{docker: api}
	res, err := c.ExecStream(context.Background(), "ctr", ExecOptions{Cmd: []string{"false"}}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", res.ExitCode)
	}
	if len(res.Stderr) != maxStreamStderr {
		t.Errorf("stderr len = %d, want capped at %d", len(res.Stderr), maxStreamStderr)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("upload aborted") }

func TestClient_ExecStream_WriterErrorFails(t *testing.T) {
	t.Parallel()

	api := &mockDockerAPI{
		execCreateFunc: func(_ context.Context, _ string, _ container.ExecOptions) (types.IDResponse, error) {
			return types.IDResponse{ID: "exec-1"}, nil
		},
		execAttachFunc: func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
			return hijacked(t, "data", ""), nil
		},
		execInspectFunc: func(_ context.Context, _ string) (container.ExecInspect, error) {
			return container.ExecInspect{}, nil
		},
	}
	c := &Client{docker: api}
	if _, err := c.ExecStream(context.Background(), "ctr", ExecOptions{Cmd: []string{"cat"}}, failingWriter{}); err == nil {
		t.Fatal("expected an error when the stdout writer fails")
	}
}

func TestClient_ExecStream_EmptyCommand(t *testing.T) {
	t.Parallel()
	c := &Client{docker: &mockDockerAPI{}}
	if _, err := c.ExecStream(context.Background(), "ctr", ExecOptions{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}
