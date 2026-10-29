package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
)

// AppDBTarget dumps the GoForge database with the local pg_dump client.
//
// pg_dump gets a minimal environment built from DATABASE_URL (PG* variables
// plus PATH). It never inherits the GoForge process environment, so
// ENCRYPTION_KEY and the S3 keys cannot reach it, and no secret is on argv.
type AppDBTarget struct {
	DatabaseURL string
	// PgDump is the pg_dump binary; empty means look it up on PATH.
	PgDump string
	// Redactor scrubs pg_dump's stderr before it becomes an error.
	Redactor *Redactor
}

// Name implements Target.
func (t *AppDBTarget) Name() string { return "app-db" }

// Plan implements Target.
func (t *AppDBTarget) Plan(context.Context) ([]Artifact, error) {
	env, err := pgEnvFromURL(t.DatabaseURL)
	if err != nil {
		return nil, err
	}
	bin := t.PgDump
	if bin == "" {
		bin, err = exec.LookPath("pg_dump")
		if err != nil {
			return nil, errors.New("pg_dump not found on PATH; the runtime image needs postgresql17-client")
		}
	}
	return []Artifact{{
		Path:        "app/goforge-db.dump",
		Kind:        KindPgDump,
		ContentType: "application/octet-stream",
		Database:    "goforge",
		Dump: func(ctx context.Context, w io.Writer) error {
			cmd := exec.CommandContext(ctx, bin, "--format=custom", "--no-password") //nolint:gosec // bin is pg_dump from PATH or config, args are constant
			cmd.Env = env
			cmd.Stdout = w
			stderr := &bytes.Buffer{}
			cmd.Stderr = &limitedWriter{w: stderr, n: 16 << 10}
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("pg_dump: %w: %s", err, t.Redactor.Redact(stderr.String()))
			}
			return nil
		},
	}}, nil
}

// pgEnvFromURL turns a postgres:// URL into libpq environment variables.
func pgEnvFromURL(raw string) ([]string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, errors.New("DATABASE_URL must be a postgres:// URL for backups")
	}
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	add := func(k, v string) {
		if v != "" {
			env = append(env, k+"="+v)
		}
	}
	q := u.Query()
	add("PGHOST", u.Hostname())
	if h := q.Get("host"); h != "" && u.Hostname() == "" {
		add("PGHOST", h)
	}
	add("PGPORT", u.Port())
	if u.User != nil {
		add("PGUSER", u.User.Username())
		if p, ok := u.User.Password(); ok {
			add("PGPASSWORD", p)
		}
	}
	add("PGDATABASE", strings.TrimPrefix(u.Path, "/"))
	add("PGSSLMODE", q.Get("sslmode"))
	add("PGSSLROOTCERT", q.Get("sslrootcert"))
	add("PGCONNECT_TIMEOUT", q.Get("connect_timeout"))
	return env, nil
}

// limitedWriter keeps the first n bytes and drops the rest without failing.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k]) //nolint:errcheck // bytes.Buffer never fails
		l.n -= k
	}
	return len(p), nil
}
