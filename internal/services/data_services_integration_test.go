//go:build integration

package services

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// TestDataServices_AuthAndLabels starts the repo's redis and postgres
// templates on a throwaway network and volume through the real Docker client.
// Redis must refuse an unauthenticated PING and accept the password from
// connection_info; both must report healthy and carry traefik.enable=false.
// Redis must run with AOF, maxmemory 256mb and noeviction, and keep auth and
// its data across two restarts. Its default user's password lives in
// /data/users.acl (no requirepass), and scripts/shared-data/redis-tenant.sh
// must create per-app users confined to <app>:* that, like the default
// user, survive two restarts; SCAN, RANDOMKEY, PUBSUB, and FUNCTION are
// denied to them. The script must also work on redis 6.2 and refuse, changing
// nothing, an instance without an ACL file. Postgres must be 17 with the pgadmin admin,
// and scripts/shared-data/pg-tenant.sh must provision throwaway tenants
// idempotently. Both tenant scripts shell out to docker; set
// GOFORGE_IT_DOCKER to override (for example "sudo -n docker").
// Run it with:
//
//	sudo -n env "PATH=$PATH" mise x go@1.26.6 -- go test -tags integration \
//	  -run TestDataServices_AuthAndLabels ./internal/services/ -count=1 -v
func TestDataServices_AuthAndLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
	tag := hex.EncodeToString(suffix)
	netName := "goforge-itdata-" + tag
	if _, err = api.NetworkCreate(ctx, netName, client.NetworkCreateOptions{}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
		defer ccancel()
		if _, rmErr := api.NetworkRemove(cctx, netName, client.NetworkRemoveOptions{}); rmErr != nil {
			t.Logf("remove network %s: %v", netName, rmErr)
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewInstanceService(nil, nil, dc, loadRepoCatalog(t), netName, false, logger)

	// start renders tmplName at version (the template default when empty) for
	// a new instance and starts it, returning the container ID, its IP on
	// netName, and the rendered spec.
	start := func(t *testing.T, tmplName, version, id string, config map[string]interface{}) (string, string, *containerSpec) {
		t.Helper()
		tmpl, tErr := svc.catalog.GetTemplate(tmplName)
		if tErr != nil {
			t.Fatal(tErr)
		}
		if tErr = fillGeneratedConfig(tmpl, config, true); tErr != nil {
			t.Fatal(tErr)
		}
		if tErr = normalizeConfig(tmpl, config); tErr != nil {
			t.Fatal(tErr)
		}
		if version == "" {
			version = tmpl.DefaultVersion
		}
		inst := &models.ServiceInstance{ID: id, Version: sql.NullString{String: version, Valid: true}}
		spec, tErr := svc.buildContainerSpec(tmpl, inst, config)
		if tErr != nil {
			t.Fatal(tErr)
		}
		spec.opts.RestartPolicy = ""
		spec.opts.Labels["goforge.integration"] = "data-services"
		dockerID, tErr := dc.CreateContainer(ctx, spec.opts)
		t.Cleanup(func() {
			cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
			defer ccancel()
			if dockerID != "" {
				if rmErr := dc.RemoveContainer(cctx, dockerID); rmErr != nil {
					t.Logf("remove container: %v", rmErr)
				}
			}
			for _, v := range tmpl.Docker.Volumes {
				if _, rmErr := api.VolumeRemove(cctx, VolumeName(id, v.Name), client.VolumeRemoveOptions{Force: true}); rmErr != nil {
					t.Logf("remove volume: %v", rmErr)
				}
			}
		})
		if tErr != nil {
			t.Fatalf("create container: %v", tErr)
		}
		hctx, hcancel := context.WithTimeout(ctx, 2*time.Minute)
		defer hcancel()
		if tErr = dc.WaitHealthy(hctx, dockerID, time.Second); tErr != nil {
			t.Fatalf("%s did not become healthy: %v", tmplName, tErr)
		}
		info, tErr := inspectContainer(ctx, api, dockerID)
		if tErr != nil {
			t.Fatal(tErr)
		}
		if got := info.Config.Labels["traefik.enable"]; got != "false" {
			t.Errorf("label traefik.enable = %q, want false", got)
		}
		for k := range info.Config.Labels {
			if strings.HasPrefix(k, "traefik.http.") {
				t.Errorf("unexpected routing label %s", k)
			}
		}
		if len(info.HostConfig.PortBindings) != 0 {
			t.Errorf("PortBindings = %v, want none", info.HostConfig.PortBindings)
		}
		return dockerID, info.NetworkSettings.Networks[netName].IPAddress.String(), spec
	}

	// sh runs script in the container and returns its trimmed output; a
	// non-zero exit fails the test unless allowFail.
	sh := func(t *testing.T, dockerID, script string, env []string, allowFail bool) (string, int) {
		t.Helper()
		res, eErr := dc.Exec(ctx, dockerID, docker.ExecOptions{Cmd: []string{"sh", "-c", script}, Env: env})
		if eErr != nil {
			t.Fatal(eErr)
		}
		if res.ExitCode != 0 && !allowFail {
			t.Fatalf("exec %q exited %d: %s", script, res.ExitCode, res.Output)
		}
		return strings.TrimSpace(res.Output), res.ExitCode
	}
	const rcli = `REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --no-auth-warning `
	dockerCmd := os.Getenv("GOFORGE_IT_DOCKER")
	if dockerCmd == "" {
		dockerCmd = "docker"
	}
	// tenantScript runs scripts/shared-data/<script> with args and stdin.
	tenantScript := func(script, stdin string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "sh", append([]string{"../../scripts/shared-data/" + script}, args...)...)
		cmd.Env = append(os.Environ(), "DOCKER="+dockerCmd)
		cmd.Stdin = strings.NewReader(stdin)
		b, cErr := cmd.CombinedOutput()
		return string(b), cErr
	}

	// Generated passwords are alphanumeric; the second case proves the
	// config-file escaping for a user-chosen password.
	for _, tc := range []struct {
		name     string
		password string
	}{
		{name: "generated password"},
		{name: "password with quotes and backslash", password: `p"a\ss w'rd$x`},
	} {
		t.Run("redis/"+tc.name, func(t *testing.T) {
			id := "itdata-redis-" + tag + "-" + fmt.Sprint(len(tc.password))
			config := map[string]interface{}{"name": "redis-it"}
			if tc.password != "" {
				config["password"] = tc.password
			}
			dockerID, ip, spec := start(t, "redis", "", id, config)
			password := config["password"].(string)
			addr := net.JoinHostPort(ip, "6379")

			if got := resp(t, addr, "PING"); !strings.HasPrefix(got, "-NOAUTH") {
				t.Errorf("unauthenticated PING = %q, want -NOAUTH", got)
			} else {
				t.Logf("unauthenticated PING -> %s", got)
			}
			if got := resp(t, addr, "AUTH", "wrong-password", "PING"); !strings.HasPrefix(got, "-WRONGPASS") {
				t.Errorf("AUTH wrong-password = %q, want -WRONGPASS", got)
			}

			authPass := password
			if tc.password == "" {
				// Use the password from the rendered connection_info URL.
				var info map[string]string
				if jErr := json.Unmarshal(svc.renderConnectionInfo(svc.catalog.templates["redis"], spec.data, id), &info); jErr != nil {
					t.Fatal(jErr)
				}
				u, uErr := url.Parse(info["internal"])
				if uErr != nil {
					t.Fatal(uErr)
				}
				if u.Hostname() != spec.opts.Name || u.Port() != "6379" {
					t.Errorf("connection_info host = %s, want %s:6379", u.Host, spec.opts.Name)
				}
				authPass, _ = u.User.Password()
			}
			if got := resp(t, addr, "AUTH", authPass, "PING"); got != "+OK|+PONG" {
				t.Errorf("AUTH <password>; PING = %q, want +OK|+PONG", got)
			} else {
				t.Log("AUTH with the instance password then PING -> +OK|+PONG")
			}

			info, iErr := inspectContainer(ctx, api, dockerID)
			if iErr != nil {
				t.Fatal(iErr)
			}
			if strings.Contains(strings.Join(info.Config.Cmd, " "), password) || strings.Contains(strings.Join(info.Config.Healthcheck.Test, " "), password) {
				t.Error("password appears in container Cmd or health check")
			}
			res, eErr := dc.Exec(ctx, dockerID, docker.ExecOptions{Cmd: []string{"sh", "-c", `tr '\0' ' ' </proc/1/cmdline; echo; awk '/^Uid:/{print $2}' /proc/1/status; stat -c '%a %U' /tmp/goforge-redis.conf /data/users.acl`}})
			if eErr != nil {
				t.Fatal(eErr)
			}
			if strings.Contains(res.Output, password) {
				t.Error("password appears in PID 1 argv")
			}
			lines := strings.Split(strings.TrimSpace(res.Output), "\n")
			if len(lines) != 4 || !strings.HasPrefix(lines[0], "redis-server") || lines[1] == "0" || lines[2] != "600 redis" || lines[3] != "600 redis" {
				t.Errorf("pid1 argv / uid / conf mode / acl mode = %q, want redis-server, non-root, 600 redis, 600 redis", lines)
			} else {
				t.Logf("pid1 %q uid %s conf %s users.acl %s", lines[0], lines[1], lines[2], lines[3])
			}

			// The default user's password is the SHA-256 in /data/users.acl,
			// not requirepass, and the plain password is in neither file.
			// ACL SAVE writes the same user back with "sanitize-payload".
			wantDefault := "user default on #" + sha256Hex(password) + " ~* &* +@all"
			savedDefault := "user default on sanitize-payload #" + sha256Hex(password) + " ~* &* +@all"
			checkFiles := func(t *testing.T, label string, afterSave bool) {
				t.Helper()
				out, _ := sh(t, dockerID, "cat /tmp/goforge-redis.conf /data/users.acl", nil, false)
				if strings.Contains(out, password) {
					t.Errorf("%s: plain password in the conf or ACL file", label)
				}
				conf, _ := sh(t, dockerID, "cat /tmp/goforge-redis.conf", nil, false)
				if strings.Contains(conf, "requirepass") || !strings.Contains(conf, "aclfile /data/users.acl") {
					t.Errorf("%s: conf = %q, want aclfile /data/users.acl and no requirepass", label, conf)
				}
				acl, _ := sh(t, dockerID, "cat /data/users.acl", nil, false)
				if first := strings.SplitN(acl, "\n", 2)[0]; first != wantDefault && !(afterSave && first == savedDefault) {
					t.Errorf("%s: users.acl first line = %q, want %q", label, first, wantDefault)
				}
				if n := strings.Count(acl, "user default "); n != 1 {
					t.Errorf("%s: users.acl has %d default user lines, want 1", label, n)
				}
			}
			checkFiles(t, "start", false)
			if out, _ := sh(t, dockerID, rcli+"config get requirepass", nil, false); out != "requirepass" {
				t.Errorf("CONFIG GET requirepass = %q, want empty (password in the ACL file)", out)
			}

			// Sessions/queues settings from the conf file.
			for k, want := range map[string]string{"appendonly": "yes", "maxmemory": "268435456", "maxmemory-policy": "noeviction", "appendfsync": "everysec", "dir": "/data"} {
				out, _ := sh(t, dockerID, rcli+"config get "+k, nil, false)
				if got := strings.Split(out, "\n"); len(got) != 2 || got[1] != want {
					t.Errorf("CONFIG GET %s = %q, want %s", k, out, want)
				}
			}
			if out, _ := sh(t, dockerID, "ls /data/appendonlydir", nil, false); !strings.Contains(out, ".aof") {
				t.Errorf("/data/appendonlydir = %q, want AOF files", out)
			}
			// noeviction: with a tiny limit a write is refused, not evicted.
			sh(t, dockerID, rcli+"set it:keep v1 && "+rcli+"config set maxmemory 1", nil, false)
			if out, _ := sh(t, dockerID, rcli+"set it:big $(head -c 65536 /dev/zero | tr '\\0' x)", nil, true); !strings.HasPrefix(out, "OOM") {
				t.Errorf("SET over maxmemory = %q, want OOM (noeviction)", out)
			}
			sh(t, dockerID, rcli+"config set maxmemory 256mb", nil, false)
			if out, _ := sh(t, dockerID, rcli+"get it:keep", nil, false); out != "v1" {
				t.Errorf("GET it:keep = %q after OOM, want v1 (not evicted)", out)
			}
			t.Log("appendonly yes, maxmemory 256mb, noeviction refuses writes with OOM and keeps keys")

			// Per-app users from redis-tenant.sh: each one only reaches its
			// own <app>:* keys and channels.
			const tenantA, tenantB, tenantC = "it_tenant_a", "it_tenant_b", "it_tenant_c"
			pwA, pwB := `p'w\x"y $z`, "tenant-b-pass"
			tenants := redisTenants{a: tenantA, pwA: pwA, b: tenantB, pwB: pwB, revoked: tenantC, defaultPass: authPass, major: 7}
			isolation := func(t *testing.T, addr string) {
				t.Helper()
				checkRedisTenants(t, addr, tenants)
			}
			t.Run("redis-tenant", func(t *testing.T) {
				for _, p := range []struct{ app, pw string }{{tenantA, pwA}, {tenantA, pwA}, {tenantB, pwB}, {tenantC, "tenant-c-pass"}} {
					if out, tErr := tenantScript("redis-tenant.sh", p.pw, spec.opts.Name, p.app); tErr != nil {
						t.Fatalf("redis-tenant.sh %s: %v: %s", p.app, tErr, out)
					}
				}
				if got := respCmds(t, addr, []string{"AUTH", tenantC, "tenant-c-pass"}, []string{"SET", tenantC + ":k", "c"}); got != "+OK|+OK" {
					t.Errorf("tenant c before revoke = %q, want +OK|+OK", got)
				}
				for i := 0; i < 2; i++ { // revoke is idempotent
					if out, tErr := tenantScript("redis-tenant.sh", "", "--revoke", spec.opts.Name, tenantC); tErr != nil {
						t.Fatalf("redis-tenant.sh --revoke %s: %v: %s", tenantC, tErr, out)
					}
				}
				// A function library loaded by the default user; tenants must
				// neither see nor flush it (checked by isolation).
				if got := respCmds(t, addr, []string{"AUTH", authPass}, []string{"FUNCTION", "LOAD", "REPLACE", itFunctionLib}); got != "+OK|$5" {
					t.Fatalf("default FUNCTION LOAD = %q, want +OK|$5", got)
				}
				isolation(t, addr)

				// Rotation: the old password stops working.
				if out, tErr := tenantScript("redis-tenant.sh", "rotated", spec.opts.Name, tenantB); tErr != nil {
					t.Fatalf("rotate: %v: %s", tErr, out)
				}
				if got := respCmds(t, addr, []string{"AUTH", tenantB, pwB}); !strings.HasPrefix(got, "-WRONGPASS") {
					t.Errorf("old tenant b password after rotation = %q, want -WRONGPASS", got)
				}
				if out, tErr := tenantScript("redis-tenant.sh", pwB, spec.opts.Name, tenantB); tErr != nil {
					t.Fatalf("rotate back: %v: %s", tErr, out)
				}

				// Refusals change nothing.
				for _, bad := range []struct{ stdin, app string }{{"x", "default"}, {"x", "Bad"}, {"x", "a:b"}, {"x", "a*"}, {"", "it_empty"}} {
					if out, tErr := tenantScript("redis-tenant.sh", bad.stdin, spec.opts.Name, bad.app); tErr == nil {
						t.Errorf("redis-tenant.sh %q succeeded: %s", bad.app, out)
					}
				}
				acl, _ := sh(t, dockerID, "cat /data/users.acl", nil, false)
				for _, want := range []string{"user " + tenantA + " on ", "~" + tenantA + ":* ", "&" + tenantA + ":* ", "user " + tenantB + " on "} {
					if !strings.Contains(acl, want) {
						t.Errorf("users.acl missing %q after ACL SAVE", want)
					}
				}
				if strings.Contains(acl, tenantC) || strings.Contains(acl, pwA) || strings.Contains(acl, "it_empty") {
					t.Error("users.acl has the revoked or refused user, or a plain tenant password")
				}
				checkFiles(t, "after tenants", true)
				t.Log("redis-tenant.sh: idempotent, quoted password, <app>:* keys and channels only, FLUSHALL/KEYS/SCAN/RANDOMKEY/PUBSUB/FUNCTION/CONFIG/ACL denied, rotation and revoke work, bad names refused")
			})

			// The conf file is recreated on every start. Without rm -f, a
			// second start fails: sticky /tmp plus fs.protected_regular=1
			// denies root writing the redis-owned file (exit 2).
			// Every start rewrites the default user from REDIS_PASSWORD and
			// keeps the tenant lines. A planted nopass default line must not
			// survive the first restart.
			t.Run("restart", func(t *testing.T) {
				sh(t, dockerID, `sed -i 's/^user default .*/user default on nopass ~* \&* +@all/' /data/users.acl && grep -q '^user default on nopass' /data/users.acl`, nil, false)
				for n := 1; n <= 2; n++ {
					if _, rErr := api.ContainerRestart(ctx, dockerID, client.ContainerRestartOptions{}); rErr != nil {
						t.Fatalf("restart %d: %v", n, rErr)
					}
					hctx, hcancel := context.WithTimeout(ctx, 2*time.Minute)
					hErr := dc.WaitHealthy(hctx, dockerID, time.Second)
					hcancel()
					st, sErr := inspectContainer(ctx, api, dockerID)
					if sErr != nil {
						t.Fatal(sErr)
					}
					if hErr != nil || !st.State.Running {
						t.Fatalf("restart %d: running=%v exit=%d healthy err=%v", n, st.State.Running, st.State.ExitCode, hErr)
					}
					ip := st.NetworkSettings.Networks[netName].IPAddress.String()
					raddr := net.JoinHostPort(ip, "6379")
					if got := resp(t, raddr, "PING"); !strings.HasPrefix(got, "-NOAUTH") {
						t.Errorf("restart %d: unauthenticated PING = %q, want -NOAUTH", n, got)
					}
					if got := resp(t, raddr, "AUTH", authPass, "PING"); got != "+OK|+PONG" {
						t.Errorf("restart %d: AUTH; PING = %q, want +OK|+PONG", n, got)
					}
					cr, cErr := dc.Exec(ctx, dockerID, docker.ExecOptions{Cmd: []string{"stat", "-c", "%a %U", "/tmp/goforge-redis.conf"}})
					if cErr != nil {
						t.Fatal(cErr)
					}
					if got := strings.TrimSpace(cr.Output); got != "600 redis" {
						t.Errorf("restart %d: conf mode = %q, want 600 redis", n, got)
					}
					if out, _ := sh(t, dockerID, rcli+"get it:keep", nil, false); out != "v1" {
						t.Errorf("restart %d: GET it:keep = %q, want v1 (AOF)", n, out)
					}
					if out, _ := sh(t, dockerID, rcli+"config get appendonly", nil, false); !strings.HasSuffix(out, "yes") {
						t.Errorf("restart %d: appendonly = %q, want yes", n, out)
					}
					if got := resp(t, raddr, "AUTH", password, "PING"); got != "+OK|+PONG" {
						t.Errorf("restart %d: AUTH <REDIS_PASSWORD>; PING = %q, want +OK|+PONG", n, got)
					}
					checkFiles(t, fmt.Sprintf("restart %d", n), false)
					isolation(t, raddr)
					t.Logf("restart %d: running, -NOAUTH, +OK|+PONG, conf 600 redis, key kept, appendonly yes, default user from users.acl, tenants isolated, revoked tenant still gone", n)
				}
			})
		})
	}

	// redis 6.2 (the template's "6"): reset keeps allchannels there, and
	// FUNCTION does not exist. The script must still create isolated users.
	t.Run("redis/6.2 tenants", func(t *testing.T) {
		config := map[string]interface{}{"name": "redis6-it"}
		dockerID, ip, spec := start(t, "redis", "6", "itdata-redis6-"+tag, config)
		if v, _ := sh(t, dockerID, "redis-server --version", nil, false); !strings.Contains(v, "v=6.2.") {
			t.Fatalf("redis:6 is %q, want 6.2", v)
		}
		password := config["password"].(string)
		ten := redisTenants{a: "it6_a", pwA: "six-a-pass", b: "it6_b", pwB: "six-b-pass", defaultPass: password, major: 6}
		for _, p := range [][2]string{{ten.a, ten.pwA}, {ten.b, ten.pwB}} {
			if out, tErr := tenantScript("redis-tenant.sh", p[1], spec.opts.Name, p[0]); tErr != nil {
				t.Fatalf("redis-tenant.sh %s on 6.2: %v: %s", p[0], tErr, out)
			} else if !strings.Contains(out, "redis 6") {
				t.Errorf("redis-tenant.sh output %q, want redis 6", out)
			}
		}
		if acl, _ := sh(t, dockerID, rcli+"acl getuser "+ten.a, nil, false); strings.Contains(acl, "function") {
			t.Errorf("6.2 user has a function rule: %q", acl)
		}
		checkRedisTenants(t, net.JoinHostPort(ip, "6379"), ten)
		if _, rErr := api.ContainerRestart(ctx, dockerID, client.ContainerRestartOptions{}); rErr != nil {
			t.Fatal(rErr)
		}
		hctx, hcancel := context.WithTimeout(ctx, 2*time.Minute)
		defer hcancel()
		if hErr := dc.WaitHealthy(hctx, dockerID, time.Second); hErr != nil {
			t.Fatalf("redis 6.2 after restart: %v", hErr)
		}
		st, sErr := inspectContainer(ctx, api, dockerID)
		if sErr != nil {
			t.Fatal(sErr)
		}
		raddr := net.JoinHostPort(st.NetworkSettings.Networks[netName].IPAddress.String(), "6379")
		if got := resp(t, raddr, "PING"); !strings.HasPrefix(got, "-NOAUTH") {
			t.Errorf("6.2 after restart: unauthenticated PING = %q, want -NOAUTH", got)
		}
		checkRedisTenants(t, raddr, ten)
		t.Log("redis 6.2: resetchannels rule accepted, no -function, tenants isolated before and after a restart")
	})

	// A redis started the pre-#60 way (requirepass, no ACL file) must be
	// refused before any user is created or deleted, even in memory.
	t.Run("redis/pre-60 instance refused", func(t *testing.T) {
		base, tErr := svc.catalog.GetTemplate("redis")
		if tErr != nil {
			t.Fatal(tErr)
		}
		pre := *base
		pre.Name = "redis-pre60"
		pre.Docker.Command = []string{"sh", "-c", `set -eu
umask 077
conf=/tmp/pre60.conf
rm -f "$conf"
printf 'requirepass "%s"\n' "$REDIS_PASSWORD" > "$conf"
if [ "$(id -u)" = 0 ]; then chown redis "$conf"; fi
exec docker-entrypoint.sh redis-server "$conf"`}
		svc.catalog.mu.Lock()
		svc.catalog.templates[pre.Name] = &pre
		svc.catalog.mu.Unlock()
		dockerID, _, spec := start(t, pre.Name, "", "itdata-redispre-"+tag, map[string]interface{}{"name": "redis-pre60-it"})
		if out, _ := sh(t, dockerID, rcli+"config get aclfile", nil, false); out != "aclfile" {
			t.Fatalf("pre-60 CONFIG GET aclfile = %q, want empty", out)
		}
		// One user the default user made by hand, which --revoke must not delete.
		sh(t, dockerID, rcli+"acl setuser it_pre_keep on nopass", nil, false)
		before, _ := sh(t, dockerID, rcli+"acl list", nil, false)
		for _, args := range [][]string{{spec.opts.Name, "it_pre_new"}, {"--revoke", spec.opts.Name, "it_pre_keep"}} {
			out, sErr := tenantScript("redis-tenant.sh", "pre-pass", args...)
			if sErr == nil {
				t.Errorf("redis-tenant.sh %v succeeded on a pre-#60 instance: %s", args, out)
			} else if !strings.Contains(out, "no ACL file") || !strings.Contains(out, "Nothing was changed") {
				t.Errorf("redis-tenant.sh %v error = %q, want no ACL file, Nothing was changed", args, out)
			}
		}
		if after, _ := sh(t, dockerID, rcli+"acl list", nil, false); after != before {
			t.Errorf("ACL LIST changed on a refused run:\nbefore %q\nafter  %q", before, after)
		}
		t.Log("pre-#60 instance (requirepass, no aclfile): create and revoke refused, ACL LIST unchanged")
	})

	t.Run("postgres", func(t *testing.T) {
		// Health check used to be the literal "pg_isready -U {{ .Config.username }}".
		dockerID, _, spec := start(t, "postgres", "", "itdata-pg-"+tag, map[string]interface{}{"name": "pg-it"})
		t.Log("postgres healthy with pg_isready -U $POSTGRES_USER -d $POSTGRES_DB")
		if spec.opts.Image != "postgres:17" {
			t.Errorf("image = %s, want postgres:17", spec.opts.Image)
		}
		const admin = `psql -X -tA -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c `
		out, _ := sh(t, dockerID, admin+`"select current_setting('server_version_num')::int / 10000, current_user, (select rolsuper from pg_roles where rolname = current_user)"`, nil, false)
		if out != "17|pgadmin|t" {
			t.Errorf("major|user|superuser = %q, want 17|pgadmin|t", out)
		} else {
			t.Log("server 17, admin pgadmin (superuser)")
		}

		t.Run("pg-tenant", func(t *testing.T) {
			provision := func(t *testing.T, app, password string) {
				t.Helper()
				cmd := exec.CommandContext(ctx, "sh", "../../scripts/shared-data/pg-tenant.sh", spec.opts.Name, app)
				cmd.Env = append(os.Environ(), "DOCKER="+dockerCmd)
				cmd.Stdin = strings.NewReader(password)
				if b, cErr := cmd.CombinedOutput(); cErr != nil {
					t.Fatalf("pg-tenant.sh %s: %v: %s", app, cErr, b)
				}
			}
			// login connects over TCP to the container's own address, where
			// pg_hba requires a password (the local socket is trust).
			login := func(t *testing.T, user, password, db string) (string, int) {
				t.Helper()
				return sh(t, dockerID, `psql -X -tA -h "$(hostname -i | cut -d' ' -f1)" -U "$U" -d "$D" -c 'select current_user, current_database()'`,
					[]string{"PGPASSWORD=" + password, "U=" + user, "D=" + db}, true)
			}
			const a, b = "it_tenant_a", "it_tenant_b"
			pwA := `p'w\x"y $z`
			provision(t, a, pwA)
			provision(t, a, pwA) // idempotent
			provision(t, b, "tenant-b-pass")
			if out, code := login(t, a, pwA, a); code != 0 || out != a+"|"+a {
				t.Errorf("tenant a login = %d %q, want %s|%s", code, out, a, a)
			}
			if _, code := login(t, a, "wrong", a); code == 0 {
				t.Error("tenant a logged in with a wrong password")
			}
			if out, code := login(t, b, "tenant-b-pass", a); code == 0 || !strings.Contains(out, "permission denied") {
				t.Errorf("tenant b on tenant a's database = %d %q, want permission denied", code, out)
			}
			attrs, _ := sh(t, dockerID, admin+`"select rolsuper, rolcreatedb, rolcreaterole from pg_roles where rolname = '`+a+`'"`, nil, false)
			owner, _ := sh(t, dockerID, admin+`"select pg_get_userbyid(datdba) from pg_database where datname = '`+a+`'"`, nil, false)
			if attrs != "f|f|f" || owner != a {
				t.Errorf("tenant a attrs %q owner %q, want f|f|f owned by %s", attrs, owner, a)
			}
			provision(t, a, "rotated-pass")
			if _, code := login(t, a, pwA, a); code == 0 {
				t.Error("old password still works after rotation")
			}
			if _, code := login(t, a, "rotated-pass", a); code != 0 {
				t.Error("rotated password does not work")
			}
			// A tenant named after an existing database it does not own (the
			// instance's default POSTGRES_DB "app", owned by pgadmin) must fail
			// closed and leave ownership, grants, and roles alone.
			cmd := exec.CommandContext(ctx, "sh", "../../scripts/shared-data/pg-tenant.sh", spec.opts.Name, "app")
			cmd.Env = append(os.Environ(), "DOCKER="+dockerCmd)
			cmd.Stdin = strings.NewReader("app-pass")
			if out, cErr := cmd.CombinedOutput(); cErr == nil {
				t.Errorf("pg-tenant.sh app succeeded on a pgadmin-owned database: %s", out)
			} else if !strings.Contains(string(out), "owned by pgadmin") {
				t.Errorf("pg-tenant.sh app error = %q, want owned by pgadmin", out)
			}
			if owner, _ := sh(t, dockerID, admin+`"select pg_get_userbyid(datdba) from pg_database where datname = 'app'"`, nil, false); owner != "pgadmin" {
				t.Errorf("owner of app = %q, want pgadmin", owner)
			}
			if n, _ := sh(t, dockerID, admin+`"select count(*) from pg_roles where rolname = 'app'"`, nil, false); n != "0" {
				t.Errorf("role app count = %q, want 0 (no role created on refusal)", n)
			}
			if acl, _ := sh(t, dockerID, admin+`"select coalesce(datacl::text, '') from pg_database where datname = 'app'"`, nil, false); acl != "" {
				t.Errorf("app datacl = %q, want default (untouched)", acl)
			}
			t.Log("pg-tenant.sh: idempotent, quoted password, LOGIN only, owns its database, other tenants denied CONNECT, rotation works, foreign-owned database refused")
		})
	})
}

// sha256Hex is the lowercase hex SHA-256 of s, the form of a Redis ACL
// "#<hash>" password.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// resp sends cmds to the Redis server at addr as one RESP pipeline, then
// returns the first line of each reply joined by "|". Each command is one
// argument, except AUTH whose next element is its password argument.
func resp(t *testing.T, addr string, cmds ...string) string {
	t.Helper()
	var commands [][]string
	for i := 0; i < len(cmds); i++ {
		if cmds[i] == "AUTH" && i+1 < len(cmds) {
			commands = append(commands, []string{"AUTH", cmds[i+1]})
			i++
			continue
		}
		commands = append(commands, []string{cmds[i]})
	}
	return respCmds(t, addr, commands...)
}

// respCmds is respReplies joined by "|".
func respCmds(t *testing.T, addr string, commands ...[]string) string {
	t.Helper()
	return strings.Join(respReplies(t, addr, commands...), "|")
}

// respReplies sends commands (each a list of arguments) to the Redis server
// at addr as one RESP pipeline on a fresh connection, then returns the first
// line of each reply.
func respReplies(t *testing.T, addr string, commands ...[]string) []string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var b strings.Builder
	for _, args := range commands {
		fmt.Fprintf(&b, "*%d\r\n", len(args))
		for _, a := range args {
			fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
		}
	}
	if _, err = conn.Write([]byte(b.String())); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	replies := make([]string, 0, len(commands))
	for range commands {
		line, rErr := readReply(r)
		if rErr != nil {
			t.Fatalf("read reply: %v", rErr)
		}
		replies = append(replies, line)
	}
	return replies
}

// readReply reads one whole RESP2 reply from r, including the payload of a
// bulk string and the elements of an array, and returns its first line.
func readReply(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 2 {
		return line, nil
	}
	n := 0
	switch line[0] {
	case '$':
		if _, err = fmt.Sscan(line[1:], &n); err == nil && n >= 0 {
			_, err = io.ReadFull(r, make([]byte, n+2))
		}
	case '*':
		if _, err = fmt.Sscan(line[1:], &n); err == nil {
			for i := 0; i < n && err == nil; i++ {
				_, err = readReply(r)
			}
		}
	}
	return line, err
}

// itFunctionLib is a Redis 7 function library the default user loads;
// tenants must not see or remove it.
const itFunctionLib = "#!lua name=itlib\nredis.register_function('itf', function() return 1 end)"

// redisTenants are two users made by redis-tenant.sh, a revoked one (or ""),
// the default user's password, and the server's major version.
type redisTenants struct {
	a, pwA, b, pwB, revoked, defaultPass string
	major                                int
}

// checkRedisTenants checks that each tenant reaches only its own <app>:*
// keys and channels on addr, cannot list other tenants' key or channel
// names or touch instance-wide state, and that the default user still
// reaches everything.
func checkRedisTenants(t *testing.T, addr string, ten redisTenants) {
	t.Helper()
	a, b := ten.a, ten.b
	authA, authB := []string{"AUTH", a, ten.pwA}, []string{"AUTH", b, ten.pwB}
	// want holds one prefix per reply.
	type check struct {
		cmds [][]string
		want []string
	}
	checks := []check{
		{[][]string{authA, {"SET", a + ":k", "a"}, {"GET", a + ":k"}, {"EVAL", "return redis.call('set', KEYS[1], 'e')", "1", a + ":e"}}, []string{"+OK", "+OK", "$1", "+OK"}},
		{[][]string{authB, {"SET", b + ":k", "b"}}, []string{"+OK", "+OK"}},
		{[][]string{authA, {"GET", b + ":k"}, {"SET", b + ":k", "x"}, {"DEL", b + ":k"}, {"GET", "it:keep"}}, []string{"+OK", "-NOPERM", "-NOPERM", "-NOPERM", "-NOPERM"}},
		{[][]string{authB, {"GET", a + ":k"}, {"PUBLISH", a + ":ch", "x"}, {"EVAL", "return redis.call('get', KEYS[1])", "1", a + ":k"}, {"EVAL", "return redis.call('get', ARGV[1])", "0", a + ":k"}}, []string{"+OK", "-NOPERM", "-NOPERM", "-NOPERM", "-ERR"}},
		{[][]string{authA, {"FLUSHALL"}, {"KEYS", "*"}, {"SCAN", "0"}, {"RANDOMKEY"}, {"CONFIG", "GET", "maxmemory"}, {"ACL", "LIST"}}, []string{"+OK", "-NOPERM", "-NOPERM", "-NOPERM", "-NOPERM", "-NOPERM", "-NOPERM"}},
		{[][]string{authA, {"PUBSUB", "CHANNELS"}, {"PUBSUB", "NUMSUB", b + ":ch"}, {"PUBSUB", "NUMPAT"}}, []string{"+OK", "-NOPERM", "-NOPERM", "-NOPERM"}},
		{[][]string{{"AUTH", a, "wrong"}}, []string{"-WRONGPASS"}},
	}
	if ten.major >= 7 {
		checks = append(checks, check{[][]string{authA, {"FUNCTION", "LIST"}, {"FUNCTION", "FLUSH"}, {"FUNCTION", "DELETE", "itlib"}, {"FUNCTION", "LOAD", "REPLACE", itFunctionLib}}, []string{"+OK", "-NOPERM", "-NOPERM", "-NOPERM", "-NOPERM"}})
	}
	if ten.revoked != "" {
		checks = append(checks, check{[][]string{{"AUTH", ten.revoked, "tenant-c-pass"}}, []string{"-WRONGPASS"}})
	}
	for _, c := range checks {
		got := respReplies(t, addr, c.cmds...)
		ok := len(got) == len(c.want)
		for i := 0; ok && i < len(got); i++ {
			ok = strings.HasPrefix(got[i], c.want[i])
		}
		if !ok {
			t.Errorf("%v = %q, want prefixes %q", c.cmds[1:], got, c.want)
		}
	}
	// The default user still reaches every key (and the function library).
	cmds := [][]string{{"AUTH", ten.defaultPass}, {"GET", a + ":k"}, {"GET", b + ":k"}}
	want := "+OK|$1|$1"
	if ten.major >= 7 {
		cmds = append(cmds, []string{"FUNCTION", "LIST", "LIBRARYNAME", "itlib"})
		want += "|*1"
	}
	if got := respCmds(t, addr, cmds...); got != want {
		t.Errorf("default user GET tenant keys / FUNCTION LIST = %q, want %s", got, want)
	}
}
