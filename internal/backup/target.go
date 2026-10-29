package backup

import (
	"context"
	"io"
)

// Kind says what an artifact holds and how to restore it.
type Kind string

const (
	// KindPgDump is a pg_dump custom-format archive (pg_restore).
	KindPgDump Kind = "pg_dump"
	// KindPgRoles is pg_dumpall --roles-only SQL. It contains role password
	// hashes.
	KindPgRoles Kind = "pg_roles"
	// KindSQLite is an online SQLite snapshot.
	KindSQLite Kind = "sqlite"
	// KindTarGz is a gzipped tar of a data volume.
	KindTarGz Kind = "tar_gz"
)

// Artifact is one object a target uploads.
type Artifact struct {
	// Path is relative to the run directory, e.g. services/<id>/app.dump.
	Path        string
	Kind        Kind
	ContentType string
	InstanceID  string
	Template    string
	Database    string
	// Dump writes the artifact to w. It must return an error if the output
	// is incomplete, so a truncated dump never looks like a good one.
	Dump func(ctx context.Context, w io.Writer) error
}

// Target is one thing to back up: the app DB or one service instance.
type Target interface {
	Name() string
	// Plan lists the artifacts. It may run quick probes (listing databases).
	Plan(ctx context.Context) ([]Artifact, error)
}

// Source yields targets for a run, plus instances it chose to skip.
type Source interface {
	Targets(ctx context.Context) ([]Target, []Skipped, error)
}

// Static is a Source with a fixed target list.
type Static []Target

// Targets implements Source.
func (s Static) Targets(context.Context) ([]Target, []Skipped, error) { return s, nil, nil }
