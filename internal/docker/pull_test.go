package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// errReader fails mid-stream, like a pull that drops after it starts.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stream reset") }

func TestClient_PullImage_PullsEvenWhenImageIsLocal(t *testing.T) {
	t.Parallel()
	var inspected bool
	var pulled []string
	c := &Client{docker: &mockDockerAPI{
		imageInspectFunc: func(context.Context, string) (client.ImageInspectResult, error) {
			inspected = true
			return client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:old"}}, nil
		},
		imagePullFunc: func(_ context.Context, ref string, _ client.ImagePullOptions) (client.ImagePullResponse, error) {
			pulled = append(pulled, ref)
			return pullBody(`{"status":"Downloaded newer image"}`), nil
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
		pull func(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error)
	}{
		{"registry refuses", func(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error) {
			return nil, errors.New("unauthorized")
		}},
		{"stream breaks", func(context.Context, string, client.ImagePullOptions) (client.ImagePullResponse, error) {
			return pullResponse{io.NopCloser(errReader{})}, nil
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
