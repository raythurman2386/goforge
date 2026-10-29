//go:build integration

package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// TestGiteaPostStartBootstrap starts the repo's Gitea template on a throwaway
// network and volume, runs its post_start hook through the real Docker client,
// and checks that the install page is gone and the admin can log in. It never
// prints the generated password or secret key. Run it with:
//
//	sudo -n env "PATH=$PATH" mise x go@1.26.6 -- go test -tags integration \
//	  -run TestGiteaPostStartBootstrap ./internal/services/ -count=1 -v
func TestGiteaPostStartBootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	defer api.Close()
	if _, pingErr := api.Ping(ctx); pingErr != nil {
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
	id := "it43-" + hex.EncodeToString(suffix)
	netName := "goforge-it43-" + hex.EncodeToString(suffix)

	if _, err = api.NetworkCreate(ctx, netName, network.CreateOptions{}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		if rmErr := api.NetworkRemove(cctx, netName); rmErr != nil {
			t.Logf("remove network %s: %v", netName, rmErr)
		}
	})

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	svc := NewInstanceService(nil, nil, dc, loadRepoCatalog(t), netName, false, logger)
	tmpl, err := svc.catalog.GetTemplate("gitea")
	if err != nil {
		t.Fatal(err)
	}

	config := map[string]interface{}{
		"name": "gitea-it43", "app_name": "it43", "disable_registration": "true", "require_signin_view": "true",
		"admin_username": "gitea-admin", "admin_email": "",
	}
	if err = fillGeneratedConfig(tmpl, config, true); err != nil {
		t.Fatal(err)
	}
	if err = normalizeConfig(tmpl, config); err != nil {
		t.Fatal(err)
	}
	password := config["admin_password"].(string)
	secretKey := config["secret_key"].(string)

	inst := &models.ServiceInstance{ID: id, Version: sql.NullString{String: tmpl.DefaultVersion, Valid: true}}
	spec, err := svc.buildContainerSpec(tmpl, inst, config)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the throwaway container off any proxy and from restarting.
	spec.opts.Labels = map[string]string{"goforge.integration": "43"}
	spec.opts.RestartPolicy = ""

	dockerID, err := dc.CreateContainer(ctx, spec.opts)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		if dockerID != "" {
			if rmErr := dc.RemoveContainer(cctx, dockerID); rmErr != nil {
				t.Logf("remove container: %v", rmErr)
			}
		}
		vol := VolumeName(id, "data")
		if rmErr := api.VolumeRemove(cctx, vol, true); rmErr != nil {
			t.Logf("remove volume %s: %v", vol, rmErr)
		}
	})
	if err != nil {
		t.Fatalf("create container: %v", err)
	}

	logAttrs := []any{"instance_id", id, "template", "gitea"}
	start := time.Now()
	if err = svc.runPostStartHooks(ctx, dockerID, spec, logAttrs); err != nil {
		t.Fatalf("post_start (first run): %v", err)
	}
	t.Logf("first post_start run finished %s after create", time.Since(start).Round(time.Second))

	info, err := api.ContainerInspect(ctx, dockerID)
	if err != nil {
		t.Fatal(err)
	}
	ip := info.NetworkSettings.Networks[netName].IPAddress
	base := fmt.Sprintf("http://%s:3000", ip)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	t.Run("no install page", func(t *testing.T) {
		resp, getErr := httpClient.Get(base + "/")
		if getErr != nil {
			t.Fatal(getErr)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if strings.HasPrefix(resp.Request.URL.Path, "/install") {
			t.Errorf("GET / ended on %s, want no installer", resp.Request.URL.Path)
		}
		if strings.Contains(string(body), "Installation") {
			t.Error("GET / body has an Installation heading")
		}
		t.Logf("GET / -> %d %s", resp.StatusCode, resp.Request.URL.Path)
	})

	t.Run("admin can log in", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/user", nil)
		req.SetBasicAuth("gitea-admin", password)
		resp, getErr := httpClient.Do(req)
		if getErr != nil {
			t.Fatal(getErr)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/user = %d, want 200", resp.StatusCode)
		}
		if !strings.Contains(string(body), `"is_admin":true`) {
			t.Error(`/api/v1/user has no "is_admin":true`)
		}
		if want := `"email":"admin@` + spec.opts.Name + `"`; !strings.Contains(string(body), want) {
			t.Errorf("admin email is not admin@<container name> when Domain is empty")
		}
		t.Logf("GET /api/v1/user as gitea-admin -> %d, is_admin true", resp.StatusCode)
	})

	t.Run("app.ini locked with our secret key", func(t *testing.T) {
		res, execErr := dc.Exec(ctx, dockerID, docker.ExecOptions{
			User: "git",
			Env:  []string{"EXPECT_KEY=" + secretKey},
			Cmd: []string{"sh", "-c", `f=/data/gitea/conf/app.ini
grep -Eq '^INSTALL_LOCK *= *true$' "$f" || { echo "INSTALL_LOCK not true"; exit 1; }
grep -q "^SECRET_KEY *= *$EXPECT_KEY\$" "$f" || { echo "SECRET_KEY is not the generated key"; exit 1; }`},
		})
		if execErr != nil {
			t.Fatal(execErr)
		}
		if res.ExitCode != 0 {
			t.Errorf("app.ini check exit %d: %s", res.ExitCode, RedactSecrets(res.Output, []string{secretKey, password}))
		}
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		if runErr := svc.runPostStartHooks(ctx, dockerID, spec, logAttrs); runErr != nil {
			t.Fatalf("post_start (second run): %v", runErr)
		}
		res, execErr := dc.Exec(ctx, dockerID, docker.ExecOptions{
			User: "git",
			Cmd:  []string{"sh", "-c", "gitea admin user list --admin | tail -n +2 | grep -c ."},
		})
		if execErr != nil {
			t.Fatal(execErr)
		}
		if got := strings.TrimSpace(res.Output); got != "1" {
			t.Errorf("admin count = %q, want 1", got)
		}
	})

	if logs.contains(password) || logs.contains(secretKey) {
		t.Error("GoForge logs contain the admin password or secret key")
	}
	recs := logs.records(t)
	finished := 0
	for _, r := range recs {
		if r["msg"] == "post_start hook finished" {
			finished++
			t.Logf("log: %s hook=%v exit_code=%v duration=%v", r["msg"], r["hook"], r["exit_code"], r["duration"])
		}
	}
	if finished != 2 {
		t.Errorf("post_start hook finished logs = %d, want 2", finished)
	}
}
