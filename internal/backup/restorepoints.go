package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
)

// AppDumpKey is the app database artifact, relative to the run directory.
// It is the only object `goforge backup restore` reads in v1.
const AppDumpKey = "app/goforge-db.dump"

// maxManifestBytes bounds how much of a manifest.json is read.
const maxManifestBytes = 16 << 20

var runKeyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}/\d{8}T\d{6}Z$`)

var (
	// ErrBadRunKey is a run key that is not <YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>.
	ErrBadRunKey = errors.New("run key must look like 2026-10-29/20261029T070000Z (see goforge backup list)")
	// ErrRunIncomplete is a run directory with no manifest.json.
	ErrRunIncomplete = errors.New("run has no manifest.json: it is incomplete or does not exist")
	// ErrManifestTooNew is a manifest written by a newer GoForge.
	ErrManifestTooNew = errors.New("manifest version is newer than this GoForge understands; upgrade GoForge first")
)

// ValidRunKey reports whether key has the run key shape.
func ValidRunKey(key string) bool { return runKeyRe.MatchString(key) }

// ReadManifest reads and checks <prefix><runKey>/manifest.json. It refuses
// a run without a manifest (ErrRunIncomplete), a manifest newer than
// ManifestVersion (ErrManifestTooNew), and one whose run_key is not runKey.
func ReadManifest(ctx context.Context, store Store, prefix, runKey string) (*Manifest, error) {
	if !ValidRunKey(runKey) {
		return nil, ErrBadRunKey
	}
	rc, err := store.Get(ctx, ObjectKey(prefix, runKey, ManifestName))
	if errors.Is(err, ErrObjectNotFound) {
		return nil, ErrRunIncomplete
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	body, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(body) > maxManifestBytes {
		return nil, errors.New("manifest.json is larger than 16 MiB")
	}
	return ParseManifest(body, runKey)
}

// ParseManifest decodes a manifest and checks its version and run key.
func ParseManifest(body []byte, runKey string) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest.json is not valid JSON: %w", err)
	}
	switch {
	case m.Version > ManifestVersion:
		return nil, fmt.Errorf("%w (manifest version %d, this GoForge reads up to %d)", ErrManifestTooNew, m.Version, ManifestVersion)
	case m.Version < 1:
		return nil, fmt.Errorf("manifest.json has no valid version (%d)", m.Version)
	case m.RunKey != runKey:
		return nil, fmt.Errorf("manifest.json run_key %q does not match the run directory %q", m.RunKey, runKey)
	}
	return &m, nil
}

// Artifact returns the manifest entry for key, or nil.
func (m *Manifest) Artifact(key string) *ManifestArtifact {
	for i := range m.Artifacts {
		if m.Artifacts[i].Key == key {
			return &m.Artifacts[i]
		}
	}
	return nil
}

// TotalBytes is the sum of every artifact's size.
func (m *Manifest) TotalBytes() int64 {
	var n int64
	for _, a := range m.Artifacts {
		n += a.Bytes
	}
	return n
}

// RestorePoint is one complete run in the bucket.
type RestorePoint struct {
	RunKey string
	// Manifest is nil when Problem is set.
	Manifest *Manifest
	// Problem says why the manifest could not be used (unreadable, newer
	// version). The run is still listed so the operator sees it.
	Problem string
}

// RestorePoints lists the complete runs (those with manifest.json) under
// prefix, newest first, reading each manifest. incomplete counts run
// directories with no manifest. Keys that are not <date>/<run id>/... are
// ignored, like retention ignores them.
func RestorePoints(ctx context.Context, store Store, prefix string, limit int) (points []RestorePoint, incomplete int, err error) {
	objs, err := store.List(ctx, prefix)
	if err != nil {
		return nil, 0, err
	}
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	var complete []string
	for _, rs := range groupRuns(prefix, keys) {
		for _, r := range rs {
			if r.complete {
				complete = append(complete, r.date+"/"+r.id)
			} else {
				incomplete++
			}
		}
	}
	// Newest first by run id (UTC start), then date.
	sort.Slice(complete, func(i, j int) bool {
		a, b := complete[i][len(dateLayout)+1:], complete[j][len(dateLayout)+1:]
		if a != b {
			return a > b
		}
		return complete[i] > complete[j]
	})
	if limit > 0 && len(complete) > limit {
		complete = complete[:limit]
	}
	points = make([]RestorePoint, 0, len(complete))
	for _, k := range complete {
		p := RestorePoint{RunKey: k}
		m, merr := ReadManifest(ctx, store, prefix, k)
		if merr != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			p.Problem = merr.Error()
		} else {
			p.Manifest = m
		}
		points = append(points, p)
	}
	return points, incomplete, nil
}
