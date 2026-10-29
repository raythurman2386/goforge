package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// hijacked returns an exec attach response whose stream carries stdout and
// stderr in Docker's multiplexed framing.
func hijacked(t *testing.T, stdout, stderr string) types.HijackedResponse {
	t.Helper()
	var framed bytes.Buffer
	if stdout != "" {
		if _, err := stdcopy.NewStdWriter(&framed, stdcopy.Stdout).Write([]byte(stdout)); err != nil {
			t.Fatal(err)
		}
	}
	if stderr != "" {
		if _, err := stdcopy.NewStdWriter(&framed, stdcopy.Stderr).Write([]byte(stderr)); err != nil {
			t.Fatal(err)
		}
	}
	client, server := net.Pipe()
	go func() {
		_, _ = server.Write(framed.Bytes())
		_ = server.Close()
	}()
	return types.HijackedResponse{Conn: client, Reader: bufio.NewReader(client)}
}

func TestClient_Exec(t *testing.T) {
	t.Parallel()

	var gotOpts container.ExecOptions
	inspects := 0
	api := &mockDockerAPI{
		execCreateFunc: func(_ context.Context, id string, opts container.ExecOptions) (types.IDResponse, error) {
			if id != "ctr" {
				t.Errorf("exec container = %q, want ctr", id)
			}
			gotOpts = opts
			return types.IDResponse{ID: "exec-1"}, nil
		},
		execAttachFunc: func(_ context.Context, _ string, _ container.ExecAttachOptions) (types.HijackedResponse, error) {
			return hijacked(t, "out\n", "err\n"), nil
		},
		execInspectFunc: func(_ context.Context, _ string) (container.ExecInspect, error) {
			inspects++
			// The first inspect still reports running, like a slow daemon.
			return container.ExecInspect{Running: inspects < 2, ExitCode: 3}, nil
		},
	}
	c := &Client{docker: api}

	res, err := c.Exec(context.Background(), "ctr", ExecOptions{Cmd: []string{"sh", "-c", "x"}, User: "git", Env: []string{"A=b"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
	if !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Errorf("Output = %q, want stdout and stderr", res.Output)
	}
	if gotOpts.User != "git" || len(gotOpts.Env) != 1 || gotOpts.Env[0] != "A=b" || !gotOpts.AttachStdout || !gotOpts.AttachStderr {
		t.Errorf("exec options = %+v", gotOpts)
	}
	if inspects != 2 {
		t.Errorf("inspects = %d, want 2 (poll until not running)", inspects)
	}
}

func TestClient_Exec_Errors(t *testing.T) {
	t.Parallel()
	c := &Client{docker: &mockDockerAPI{}}
	if _, err := c.Exec(context.Background(), "ctr", ExecOptions{}); err == nil {
		t.Error("empty command: want error")
	}

	c = &Client{docker: &mockDockerAPI{
		execCreateFunc: func(context.Context, string, container.ExecOptions) (types.IDResponse, error) {
			return types.IDResponse{}, errors.New("no such container")
		},
	}}
	if _, err := c.Exec(context.Background(), "ctr", ExecOptions{Cmd: []string{"true"}}); err == nil {
		t.Error("create failure: want error")
	}
}

// TestClient_Exec_ContextEndsWhileStreaming checks that a command whose output
// stream never closes does not block Exec past ctx.
func TestClient_Exec_ContextEndsWhileStreaming(t *testing.T) {
	t.Parallel()
	c := &Client{docker: &mockDockerAPI{
		execCreateFunc: func(context.Context, string, container.ExecOptions) (types.IDResponse, error) {
			return types.IDResponse{ID: "exec-1"}, nil
		},
		execAttachFunc: func(context.Context, string, container.ExecAttachOptions) (types.HijackedResponse, error) {
			// The server end stays open and silent, like a hung hook.
			client, server := net.Pipe()
			t.Cleanup(func() { _ = server.Close() })
			return types.HijackedResponse{Conn: client, Reader: bufio.NewReader(client)}, nil
		},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Exec(ctx, "ctr", ExecOptions{Cmd: []string{"sleep", "infinity"}})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Exec error = %v, want deadline exceeded", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Exec still blocked 3s after its context expired")
	}
}

func TestCappedBuffer(t *testing.T) {
	t.Parallel()
	b := &cappedBuffer{limit: 4}
	n, err := b.Write([]byte("abcdef"))
	if err != nil || n != 6 {
		t.Fatalf("Write = %d, %v; want 6, nil", n, err)
	}
	_, _ = b.Write([]byte("gh"))
	if b.String() != "abcd" {
		t.Errorf("String = %q, want abcd", b.String())
	}
}

func state(running bool, status, health string) types.ContainerJSON {
	st := &types.ContainerState{Running: running, Status: status, ExitCode: 1}
	if health != "" {
		st.Health = &types.Health{Status: health}
	}
	return types.ContainerJSON{ContainerJSONBase: &types.ContainerJSONBase{State: st}}
}

func TestClient_WaitHealthy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		states  []types.ContainerJSON
		wantErr bool
	}{
		{name: "starting then healthy", states: []types.ContainerJSON{
			state(true, "running", types.Starting), state(true, "running", types.Starting), state(true, "running", types.Healthy),
		}},
		{name: "no health check returns once running", states: []types.ContainerJSON{
			state(false, "created", ""), state(true, "running", ""),
		}},
		{name: "unhealthy fails", states: []types.ContainerJSON{state(true, "running", types.Unhealthy)}, wantErr: true},
		{name: "exited fails", states: []types.ContainerJSON{state(false, "exited", types.Starting)}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			i := 0
			c := &Client{docker: &mockDockerAPI{
				containerInspectFunc: func(context.Context, string) (types.ContainerJSON, error) {
					s := tt.states[min(i, len(tt.states)-1)]
					i++
					return s, nil
				},
			}}
			err := c.WaitHealthy(context.Background(), "ctr", time.Millisecond)
			if (err != nil) != tt.wantErr {
				t.Fatalf("WaitHealthy error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && i != len(tt.states) {
				t.Errorf("inspects = %d, want %d", i, len(tt.states))
			}
		})
	}
}

func TestClient_WaitHealthy_ContextEnds(t *testing.T) {
	t.Parallel()
	c := &Client{docker: &mockDockerAPI{
		containerInspectFunc: func(context.Context, string) (types.ContainerJSON, error) {
			return state(true, "running", types.Starting), nil
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.WaitHealthy(ctx, "ctr", time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("WaitHealthy error = %v, want deadline exceeded", err)
	}
}
