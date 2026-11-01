package docker

import (
	"context"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

func TestStartHelper_RootNoNetworkVolumesFrom(t *testing.T) {
	var got client.ContainerCreateOptions
	started := ""
	m := &mockDockerAPI{
		containerCreateFunc: func(_ context.Context, o client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
			got = o
			return client.ContainerCreateResult{ID: "helper1"}, nil
		},
		containerStartFunc: func(_ context.Context, id string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
			started = id
			return client.ContainerStartResult{}, nil
		},
	}
	c := &Client{docker: m}
	id, err := c.StartHelper(context.Background(), HelperOptions{
		Name: "goforge-restore-x", Image: "sha256:abc", VolumesFrom: "svc", Entrypoint: []string{"sleep"}, Cmd: []string{"60"},
	})
	if err != nil || id != "helper1" || started != "helper1" {
		t.Fatalf("id=%q started=%q err=%v", id, started, err)
	}
	if got.Name != "goforge-restore-x" || got.Config.Image != "sha256:abc" || got.Config.User != "0:0" {
		t.Errorf("config = %+v", got.Config)
	}
	if strings.Join(got.HostConfig.VolumesFrom, ",") != "svc" || string(got.HostConfig.NetworkMode) != "none" {
		t.Errorf("host config = %+v", got.HostConfig)
	}
	if got.HostConfig.RestartPolicy.Name != "" || len(got.HostConfig.PortBindings) != 0 {
		t.Errorf("helper must not restart or publish: %+v", got.HostConfig)
	}

	if _, err := c.StartHelper(context.Background(), HelperOptions{Image: "x"}); err == nil {
		t.Error("helper without volumes-from and entrypoint was created")
	}
}

func TestRemoveHelper_KeepsVolumes(t *testing.T) {
	var got client.ContainerRemoveOptions
	m := &mockDockerAPI{containerRemoveFunc: func(_ context.Context, _ string, o client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
		got = o
		return client.ContainerRemoveResult{}, nil
	}}
	if err := (&Client{docker: m}).RemoveHelper(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	if !got.Force || got.RemoveVolumes {
		t.Fatalf("remove options = %+v; the borrowed volumes must never be removed", got)
	}
}

func TestCopyToContainer_KeepsOwnership(t *testing.T) {
	var got client.CopyToContainerOptions
	m := &mockDockerAPI{copyToContainerFunc: func(_ context.Context, _ string, o client.CopyToContainerOptions) (client.CopyToContainerResult, error) {
		got = o
		return client.CopyToContainerResult{}, nil
	}}
	if err := (&Client{docker: m}).CopyToContainer(context.Background(), "h", "/tmp", strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if got.DestinationPath != "/tmp" || !got.CopyUIDGID || got.AllowOverwriteDirWithFile {
		t.Fatalf("copy options = %+v", got)
	}
}
