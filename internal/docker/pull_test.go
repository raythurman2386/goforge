package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
)

// errReader fails mid-stream, like a pull that drops after it starts.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stream reset") }

func TestClient_PullImage_PullsEvenWhenImageIsLocal(t *testing.T) {
	t.Parallel()
	var inspected bool
	var pulled []string
	c := &Client{docker: &mockDockerAPI{
		imageInspectFunc: func(context.Context, string) (types.ImageInspect, []byte, error) {
			inspected = true
			return types.ImageInspect{ID: "sha256:old"}, nil, nil
		},
		imagePullFunc: func(_ context.Context, ref string, _ image.PullOptions) (io.ReadCloser, error) {
			pulled = append(pulled, ref)
			return io.NopCloser(strings.NewReader(`{"status":"Downloaded newer image"}`)), nil
		},
	}}

	if err := c.PullImage(context.Background(), "vaultwarden/server:latest"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if inspected {
		t.Error("PullImage inspected the local image; it must always ask the registry")
	}
	if len(pulled) != 1 || pulled[0] != "vaultwarden/server:latest" {
		t.Errorf("pulled = %v, want [vaultwarden/server:latest]", pulled)
	}
}

func TestClient_PullImage_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pull func(context.Context, string, image.PullOptions) (io.ReadCloser, error)
	}{
		{"registry refuses", func(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
			return nil, errors.New("unauthorized")
		}},
		{"stream breaks", func(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
			return io.NopCloser(errReader{}), nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{docker: &mockDockerAPI{imagePullFunc: tt.pull}}
			if err := c.PullImage(context.Background(), "minio/minio:latest"); err == nil {
				t.Fatal("PullImage returned nil, want error")
			}
		})
	}
}

func TestIsDigestReference(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"vaultwarden/server:latest":                     false,
		"vaultwarden/server":                            false,
		"gitea/gitea:1.25.5":                            false,
		"registry.local:5000/team/app:v1":               false,
		"registry.local:5000/team/app":                  false,
		"postgres@sha256:" + strings.Repeat("a", 64):    true,
		"postgres:16@sha256:" + strings.Repeat("b", 64): true,
	}
	for ref, want := range tests {
		if got := IsDigestReference(ref); got != want {
			t.Errorf("IsDigestReference(%q) = %v, want %v", ref, got, want)
		}
	}
}
