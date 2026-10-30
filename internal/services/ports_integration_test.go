//go:build integration

package services

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// TestGiteaSSHPublish deploys the repo's Gitea template on a throwaway network
// and volume with ssh_port set to a free port, through the real Docker client,
// and checks that SSH is published on 127.0.0.1 only. It then recreates the
// container onto a host port the test holds open and checks that the failed
// recreate restores the old container. It touches only resources it creates
// (names carry a random it42 suffix). Run it with:
//
//	sudo -n env "PATH=$PATH" mise x go@1.26.6 -- go test -tags integration \
//	  -run TestGiteaSSHPublish ./internal/services/ -count=1 -v
func TestGiteaSSHPublish(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	api, err := client.New(client.FromEnv)
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	defer api.Close()
	if _, pingErr := api.Ping(ctx, client.PingOptions{}); pingErr != nil {
		t.Skipf("docker not reachable: %v", pingErr)
	}
	dc, err := docker.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()

	suffix := make([]byte, 4)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	id := "it42-" + hex.EncodeToString(suffix)
	netName := "goforge-it42-" + hex.EncodeToString(suffix)
	containerName := "goforge-service-" + id

	if _, err = api.NetworkCreate(ctx, netName, client.NetworkCreateOptions{}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	// Registered first, so it runs last: containers, then volume, then network.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		if _, rmErr := api.NetworkRemove(cctx, netName, client.NetworkRemoveOptions{}); rmErr != nil {
			t.Logf("remove network %s: %v", netName, rmErr)
		}
	})
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		// Every container whose name carries this run's id, including a parked one.
		list, listErr := api.ContainerList(cctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("name", id)})
		if listErr != nil {
			t.Logf("list containers: %v", listErr)
		}
		for _, c := range list.Items {
			if _, rmErr := api.ContainerRemove(cctx, c.ID, client.ContainerRemoveOptions{Force: true}); rmErr != nil {
				t.Logf("remove container %v: %v", c.Names, rmErr)
			}
		}
		vol := VolumeName(id, "data")
		if _, rmErr := api.VolumeRemove(cctx, vol, client.VolumeRemoveOptions{Force: true}); rmErr != nil {
			t.Logf("remove volume %s: %v", vol, rmErr)
		}
	})

	sshPort := freeLoopbackPort(t)
	const sshDomain = "minis-it42"

	store := newMemStore(id)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewInstanceService(store.instances(), store.containers(), dc, loadRepoCatalog(t), netName, false, logger)
	tmpl, err := svc.catalog.GetTemplate("gitea")
	if err != nil {
		t.Fatal(err)
	}

	config := map[string]interface{}{
		"name": "gitea-it42", "app_name": "it42", "disable_registration": "true", "require_signin_view": "true",
		"admin_username": "gitea-admin", "admin_email": "",
		"ssh_port": strconv.Itoa(sshPort), "ssh_domain": sshDomain,
	}
	if err = fillGeneratedConfig(tmpl, config, true); err != nil {
		t.Fatal(err)
	}
	if err = normalizeConfig(tmpl, config); err != nil {
		t.Fatal(err)
	}
	store.inst.Version = sql.NullString{String: tmpl.DefaultVersion, Valid: true}
	spec, err := svc.buildContainerSpec(tmpl, store.inst, config)
	if err != nil {
		t.Fatal(err)
	}
	isolate(spec)

	logAttrs := []any{"instance_id", id, "template", "gitea"}
	start := time.Now()
	svc.startServiceContainer(ctx, tmpl, spec, nil, logAttrs)
	if st := store.status(); st != models.ServiceStatusRunning {
		t.Fatalf("instance status after deploy = %s, want running", st)
	}
	first := store.current()
	t.Logf("deployed %s in %s, ssh_port %d", containerName, time.Since(start).Round(time.Second), sshPort)

	info, err := inspectContainer(ctx, api, first.DockerID)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := info.State.StartedAt

	t.Run("PortBindings loopback only", func(t *testing.T) {
		want := []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(sshPort)}}
		got := info.HostConfig.PortBindings[network.MustParsePort("22/tcp")]
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("HostConfig.PortBindings[22/tcp] = %v, want %v", got, want)
		}
		if len(info.HostConfig.PortBindings) != 1 {
			t.Errorf("HostConfig.PortBindings = %v, want only 22/tcp", info.HostConfig.PortBindings)
		}
		for port, bindings := range info.NetworkSettings.Ports {
			for _, b := range bindings {
				if b.HostIP != netip.MustParseAddr("127.0.0.1") {
					t.Errorf("NetworkSettings.Ports[%s] binds %s:%s, want 127.0.0.1 only", port, b.HostIP, b.HostPort)
				}
			}
		}
		t.Logf("HostConfig.PortBindings = %v", info.HostConfig.PortBindings)
	})

	t.Run("SSH banner on loopback", func(t *testing.T) {
		banner, bErr := sshBanner(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(sshPort)))
		if bErr != nil {
			t.Fatal(bErr)
		}
		if !strings.HasPrefix(banner, "SSH-2.0-") {
			t.Errorf("banner = %q, want SSH-2.0-", banner)
		}
		t.Logf("127.0.0.1:%d banner %q", sshPort, banner)
	})

	t.Run("ss shows a loopback listener only", func(t *testing.T) {
		out, ssErr := exec.CommandContext(ctx, "ss", "-ltnH", "sport = :"+strconv.Itoa(sshPort)).CombinedOutput()
		if ssErr != nil {
			t.Skipf("ss: %v", ssErr)
		}
		lines := strings.TrimSpace(string(out))
		if lines == "" {
			t.Fatalf("ss shows no listener on :%d", sshPort)
		}
		for _, line := range strings.Split(lines, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			if local := fields[3]; !strings.HasPrefix(local, "127.0.0.1:") {
				t.Errorf("listener on %s, want 127.0.0.1 only", local)
			}
		}
		t.Logf("ss -ltn sport = :%d:\n%s", sshPort, lines)
	})

	t.Run("not reachable on other host addresses", func(t *testing.T) {
		addrs := nonLoopbackIPv4(t)
		if len(addrs) == 0 {
			t.Skip("no non-loopback IPv4 address")
		}
		for _, a := range addrs {
			target := net.JoinHostPort(a, strconv.Itoa(sshPort))
			conn, dErr := net.DialTimeout("tcp", target, 2*time.Second)
			if dErr == nil {
				conn.Close()
				t.Errorf("%s accepted a connection; SSH must be loopback only", target)
				continue
			}
			t.Logf("%s refused: %v", target, dErr)
		}
	})

	t.Run("env SSH_PORT and SSH_DOMAIN", func(t *testing.T) {
		env := map[string]string{}
		for _, kv := range info.Config.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		if got := env["GITEA__SERVER__SSH_PORT"]; got != strconv.Itoa(sshPort) {
			t.Errorf("GITEA__SERVER__SSH_PORT = %q, want %d", got, sshPort)
		}
		if got := env["GITEA__SERVER__SSH_DOMAIN"]; got != sshDomain {
			t.Errorf("GITEA__SERVER__SSH_DOMAIN = %q, want %s", got, sshDomain)
		}
	})

	t.Run("container row records the port", func(t *testing.T) {
		ports, pErr := first.GetPorts()
		if pErr != nil {
			t.Fatal(pErr)
		}
		want := models.PortMapping{Host: sshPort, Container: 22, Protocol: "tcp", HostIP: "127.0.0.1"}
		if len(ports) != 1 || ports[0] != want {
			t.Errorf("containers.ports = %+v, want [%+v]", ports, want)
		}
	})

	t.Run("recreate onto a taken port restores the old container", func(t *testing.T) {
		// Hold a loopback port so the new container cannot bind it.
		taken, lErr := net.Listen("tcp", "127.0.0.1:0")
		if lErr != nil {
			t.Fatal(lErr)
		}
		defer taken.Close()
		takenPort := taken.Addr().(*net.TCPAddr).Port

		edited := make(map[string]interface{}, len(config))
		for k, v := range config {
			edited[k] = v
		}
		edited["ssh_port"] = strconv.Itoa(takenPort)
		spec2, sErr := svc.buildContainerSpec(tmpl, store.inst, edited)
		if sErr != nil {
			t.Fatal(sErr)
		}
		isolate(spec2)

		svc.recreateContainer(ctx, tmpl, spec2, pullPreferRemote, models.ServiceStatusRunning)

		if st := store.status(); st != models.ServiceStatusFailed {
			t.Errorf("status = %s, want failed (edit did not apply)", st)
		}
		if cur := store.current(); cur == nil || cur.DockerID != first.DockerID {
			t.Errorf("container row = %+v, want the original %s", cur, first.DockerID)
		}
		after, iErr := inspectContainer(ctx, api, first.DockerID)
		if iErr != nil {
			t.Fatalf("old container gone: %v", iErr)
		}
		if after.Name != "/"+containerName {
			t.Errorf("old container name = %s, want /%s", after.Name, containerName)
		}
		if !after.State.Running {
			t.Errorf("old container state = %s, want running", after.State.Status)
		}
		t.Logf("old container StartedAt %s -> %s (stopped for the attempt, then started again)", startedAt, after.State.StartedAt)
		got := after.HostConfig.PortBindings[network.MustParsePort("22/tcp")]
		if len(got) != 1 || got[0].HostIP != netip.MustParseAddr("127.0.0.1") || got[0].HostPort != strconv.Itoa(sshPort) {
			t.Errorf("old container PortBindings = %v, want 127.0.0.1:%d", got, sshPort)
		}
		list, _ := api.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("name", id)})
		if len(list.Items) != 1 {
			names := make([]string, 0, len(list.Items))
			for _, c := range list.Items {
				names = append(names, strings.Join(c.Names, ","))
			}
			t.Errorf("containers for %s = %v, want only the restored one", id, names)
		}
		if waitErr := dc.WaitHealthy(ctx, first.DockerID, 2*time.Second); waitErr != nil {
			t.Fatalf("restored container not healthy: %v", waitErr)
		}
		banner, bErr := sshBanner(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(sshPort)))
		if bErr != nil || !strings.HasPrefix(banner, "SSH-2.0-") {
			t.Errorf("restored container SSH banner = %q, %v", banner, bErr)
		}
		t.Logf("taken port %d: new container refused, old %s restored and serving %q on 127.0.0.1:%d", takenPort, first.DockerID[:12], banner, sshPort)
	})
}

// isolate keeps a throwaway container off any proxy and from restarting.
func isolate(spec *containerSpec) {
	spec.opts.Labels = map[string]string{"goforge.integration": "42"}
	spec.opts.RestartPolicy = ""
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func nonLoopbackIPv4(t *testing.T) []string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		out = append(out, ipn.IP.String())
	}
	return out
}

// sshBanner reads the server identification line, retrying while sshd starts.
func sshBanner(ctx context.Context, addr string) (string, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			line, rErr := bufio.NewReader(conn).ReadString('\n')
			conn.Close()
			if rErr == nil {
				return strings.TrimSpace(line), nil
			}
			err = rErr
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", lastErr
}

// memStore is a one-instance, in-memory instance and container repository.
type memStore struct {
	mu   sync.Mutex
	inst *models.ServiceInstance
	rows map[string]*models.Container
}

func newMemStore(id string) *memStore {
	return &memStore{
		inst: &models.ServiceInstance{ID: id, TemplateName: "gitea", Status: models.ServiceStatusPending},
		rows: map[string]*models.Container{},
	}
}

func (m *memStore) status() models.ServiceInstanceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inst.Status
}

func (m *memStore) current() *models.Container {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.rows {
		cp := *c
		return &cp
	}
	return nil
}

func (m *memStore) instances() *mockInstanceRepository {
	return &mockInstanceRepository{
		getByIDFunc: func(_ context.Context, _ string) (*models.ServiceInstance, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			cp := *m.inst
			return &cp, nil
		},
		updateFunc: func(_ context.Context, s *models.ServiceInstance) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			cp := *s
			m.inst = &cp
			return nil
		},
	}
}

func (m *memStore) containers() *mockContainerRepository {
	return &mockContainerRepository{
		createFunc: func(_ context.Context, c *models.Container) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			cp := *c
			cp.ID = "row-" + c.DockerID
			m.rows[cp.ID] = &cp
			return nil
		},
		getByServiceInstanceIDFunc: func(_ context.Context, _ string) (*models.Container, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			for _, c := range m.rows {
				cp := *c
				return &cp, nil
			}
			return nil, errors.New("not found")
		},
		deleteFunc: func(_ context.Context, id string) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.rows, id)
			return nil
		},
	}
}

// inspectContainer returns the inspect body for id, dropping the raw JSON the
// Moby client also returns.
func inspectContainer(ctx context.Context, api *client.Client, id string) (container.InspectResponse, error) {
	res, err := api.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	return res.Container, err
}
