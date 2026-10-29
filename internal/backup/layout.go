package backup

import (
	"regexp"
	"strings"
	"time"
)

const (
	dateLayout  = "2006-01-02"
	runIDLayout = "20060102T150405Z"
	// ManifestName is the object that marks a run complete.
	ManifestName = "manifest.json"
	// ManifestVersion is bumped when the manifest shape changes incompatibly.
	ManifestVersion = 1
)

// RunKey is the run's path under the prefix: <YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>.
func RunKey(started time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return started.In(loc).Format(dateLayout) + "/" + started.UTC().Format(runIDLayout)
}

// ObjectKey joins the prefix, run key, and an artifact path.
func ObjectKey(prefix, runKey, path string) string {
	return prefix + runKey + "/" + strings.TrimPrefix(path, "/")
}

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeSegment makes a database or instance name safe as one key segment.
func safeSegment(s string) string {
	s = unsafeKeyChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, ".")
	if s == "" {
		return "_"
	}
	return s
}

// Manifest is manifest.json, written last in every successful run.
type Manifest struct {
	Version        int                `json:"version"`
	RunID          string             `json:"run_id"`
	RunKey         string             `json:"run_key"`
	Date           string             `json:"date"`
	Trigger        string             `json:"trigger"`
	GoForgeVersion string             `json:"goforge_version"`
	StartedAt      time.Time          `json:"started_at"`
	FinishedAt     time.Time          `json:"finished_at"`
	Artifacts      []ManifestArtifact `json:"artifacts"`
	Skipped        []Skipped          `json:"skipped"`
}

// ManifestArtifact describes one uploaded object.
type ManifestArtifact struct {
	// Key is relative to the run directory, e.g. app/goforge-db.dump.
	Key        string `json:"key"`
	Target     string `json:"target"`
	Kind       Kind   `json:"kind"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	InstanceID string `json:"instance_id,omitempty"`
	Template   string `json:"template,omitempty"`
	Database   string `json:"database,omitempty"`
}

// Skipped is an instance the run did not back up, and why.
type Skipped struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Template   string `json:"template"`
	Reason     string `json:"reason"`
}
