//go:build integration

package services

import (
	"bufio"
	"context"
	"crypto/rand"
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
// its data across two restarts. Postgres must be 17 with the pgadmin admin,
// and scripts/shared-data/pg-tenant.sh must provision throwaway tenants
// idempotently (it shells out to docker; set GOFORGE_IT_DOCKER to override).
// Run it with:
//
//	sudo -n env "PATH=$PATH" mise x go@1.26.6 -- go test -tags integration \
//	  -run TestDataServices_AuthAndLabels ./internal/services/ -count=1 -v
func TestDataServices_AuthAndLabels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	api, err := client.New(client.FromEnv, client.WithAPIVersionNegotiation())
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

	// start renders tmplName for a new instance and starts it, returning the
	// container ID, its IP on netName, and the rendered spec.
	start := func(t *testing.T, tmplName, id string, config map[string]interface{}) (string, string, *containerSpec) {
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
		inst := &models.ServiceInstance{ID: id, Version: sql.NullString{String: tmpl.DefaultVersion, Valid: true}}
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
			dockerID, ip, spec := start(t, "redis", id, config)
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
			res, eErr := dc.Exec(ctx, dockerID, docker.ExecOptions{Cmd: []string{"sh", "-c", `tr '\0' ' ' </proc/1/cmdline; echo; awk '/^Uid:/{print $2}' /proc/1/status; stat -c '%a %U' /tmp/goforge-redis.conf`}})
			if eErr != nil {
				t.Fatal(eErr)
			}
			if strings.Contains(res.Output, password) {
				t.Error("password appears in PID 1 argv")
			}
			lines := strings.Split(strings.TrimSpace(res.Output), "\n")
			if len(lines) != 3 || !strings.HasPrefix(lines[0], "redis-server") || lines[1] == "0" || lines[2] != "600 redis" {
				t.Errorf("pid1 argv / uid / conf mode = %q, want redis-server, non-root, 600 redis", lines)
			} else {
				t.Logf("pid1 %q uid %s conf %s", lines[0], lines[1], lines[2])
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

			// The conf file is recreated on every start. Without rm -f, a
			// second start fails: sticky /tmp plus fs.protected_regular=1
			// denies root writing the redis-owned file (exit 2).
			t.Run("restart", func(t *testing.T) {
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
					t.Logf("restart %d: running, -NOAUTH, +OK|+PONG, conf 600 redis, key kept, appendonly yes", n)
				}
			})
		})
	}

	t.Run("postgres", func(t *testing.T) {
		// Health check used to be the literal "pg_isready -U {{ .Config.username }}".
		dockerID, _, spec := start(t, "postgres", "itdata-pg-"+tag, map[string]interface{}{"name": "pg-it"})
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
			dockerCmd := os.Getenv("GOFORGE_IT_DOCKER")
			if dockerCmd == "" {
				dockerCmd = "docker"
			}
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

// resp sends cmds to the Redis server at addr as one RESP pipeline, then
// returns the first line of each reply joined by "|". A command with spaces
// is split into arguments, except for AUTH whose second word is kept whole.
func resp(t *testing.T, addr string, cmds ...string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var commands [][]string
	for i := 0; i < len(cmds); i++ {
		if cmds[i] == "AUTH" && i+1 < len(cmds) {
			commands = append(commands, []string{"AUTH", cmds[i+1]})
			i++
			continue
		}
		commands = append(commands, []string{cmds[i]})
	}
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
		line, rErr := r.ReadString('\n')
		if rErr != nil {
			t.Fatalf("read reply: %v", rErr)
		}
		replies = append(replies, strings.TrimRight(line, "\r\n"))
	}
	return strings.Join(replies, "|")
}
