package backup

import (
	"context"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
)

// RestorePointsView is the read-only Restore points list on the Settings →
// Profile Backups card. Restoring is CLI-only (goforge backup restore).
type RestorePointsView struct {
	Bucket, Prefix string
	// Schedule is "HH:MM <zone>", used to show times in the schedule's zone.
	Schedule   string
	Points     []RestorePoint
	Incomplete int
	// Error is a redacted reason the list is unavailable, or "".
	Error string
}

// noBucketMessage is the restore points error when no bucket is set.
const noBucketMessage = "No backup bucket is configured."

// RestorePointLister reads restore points with the live backup settings
// (Manager.Current). It only lists keys and reads manifest.json files.
type RestorePointLister struct {
	Current  func() (config.BackupConfig, string)
	Redactor func(extra ...config.BackupConfig) *Redactor
	// NewStore defaults to NewS3Store.
	NewStore func(config.BackupConfig) (Store, error)
	// Limit is how many points to show (default 10).
	Limit int
	// Timeout bounds the listing (default 10s).
	Timeout time.Duration
}

// List returns the newest restore points. It never returns a secret: errors
// are redacted with every key in play.
func (l *RestorePointLister) List(ctx context.Context) *RestorePointsView {
	cfg, _ := l.Current()
	v := &RestorePointsView{Bucket: cfg.Bucket, Prefix: cfg.Prefix, Schedule: cfg.Schedule}
	limit := l.Limit
	if limit <= 0 {
		limit = 10
	}
	v.Points, v.Incomplete, v.Error = l.points(ctx, cfg, limit)
	return v
}

// points opens the bucket and reads up to limit restore points. errMsg is
// a redacted, user-facing reason when the list is unavailable.
func (l *RestorePointLister) points(ctx context.Context, cfg config.BackupConfig, limit int) (points []RestorePoint, incomplete int, errMsg string) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, 0, noBucketMessage
	}
	red := NewRedactor()
	if l.Redactor != nil {
		red = l.Redactor(cfg)
	}
	newStore := l.NewStore
	if newStore == nil {
		newStore = func(c config.BackupConfig) (Store, error) { return NewS3Store(c) }
	}
	timeout := l.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	store, err := newStore(cfg)
	if err != nil {
		return nil, 0, "Could not open the bucket: " + red.Redact(err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	points, incomplete, err = RestorePoints(ctx, store, cfg.Prefix, limit)
	if err != nil {
		return nil, 0, "Could not list the bucket: " + red.Redact(err.Error())
	}
	return points, incomplete, ""
}

// InstanceRestorePoint is one backup run that holds a service instance's
// data.
type InstanceRestorePoint struct {
	RunKey    string
	StartedAt time.Time
	// Bytes is the size of this instance's artifacts in the run.
	Bytes     int64
	Artifacts int
}

// InstanceRestorePointsView is the Restore points card on a service page.
type InstanceRestorePointsView struct {
	InstanceID string
	// Template is the instance's template as recorded by the backups ("" when
	// no run holds the instance).
	Template string
	// Schedule is "HH:MM <zone>", used to show times in the schedule's zone.
	Schedule string
	Points   []InstanceRestorePoint
	// Skipped is how many scanned runs skipped the instance (stopped, ...).
	Skipped int
	// Error is a redacted reason the list is unavailable, or "".
	Error string
}

// Restorable reports whether goforge backup restore-service handles the
// instance's template.
func (v *InstanceRestorePointsView) Restorable() bool {
	return RestoreTemplate(v.Template)
}

// instanceScanRuns is how many of the newest runs ListInstance reads.
const instanceScanRuns = 30

// ListInstance returns the newest restore points (at most Limit, default
// 10) that hold artifacts for one service instance, scanning the newest 30
// runs. It reads manifests only and never returns a secret.
func (l *RestorePointLister) ListInstance(ctx context.Context, instanceID string) *InstanceRestorePointsView {
	cfg, _ := l.Current()
	v := &InstanceRestorePointsView{InstanceID: instanceID, Schedule: cfg.Schedule}
	limit := l.Limit
	if limit <= 0 {
		limit = 10
	}
	points, _, errMsg := l.points(ctx, cfg, instanceScanRuns)
	if errMsg != "" {
		v.Error = errMsg
		return v
	}
	for _, p := range points {
		if p.Manifest == nil {
			continue
		}
		arts := p.Manifest.InstanceArtifacts(instanceID)
		if len(arts) == 0 {
			for _, s := range p.Manifest.Skipped {
				if s.InstanceID == instanceID {
					v.Skipped++
					break
				}
			}
			continue
		}
		if len(v.Points) >= limit {
			continue
		}
		ip := InstanceRestorePoint{RunKey: p.RunKey, StartedAt: p.Manifest.StartedAt, Artifacts: len(arts)}
		for _, a := range arts {
			ip.Bytes += a.Bytes
		}
		if v.Template == "" {
			v.Template = arts[0].Template
		}
		v.Points = append(v.Points, ip)
	}
	return v
}

// BacksUpTemplate reports whether nightly backups include instances of the
// template (postgres, gitea, vaultwarden).
func BacksUpTemplate(name string) bool {
	switch name {
	case templatePostgres, templateGitea, templateVaultwarden:
		return true
	}
	return false
}

// RestoreTemplate reports whether goforge backup restore-service restores
// instances of the template (postgres, gitea, vaultwarden).
func RestoreTemplate(name string) bool {
	_, ok := serviceMounts[name]
	return ok
}

// KeptDir is where restore-service keeps the instance's previous data,
// with a placeholder for the time.
func (v *InstanceRestorePointsView) KeptDir() string {
	return ServiceMount(v.Template) + "/" + preRestorePrefix + "<time>"
}

// Location is the backup schedule's time zone, or UTC.
func (v *InstanceRestorePointsView) Location() *time.Location {
	if f := strings.Fields(v.Schedule); len(f) == 2 {
		if l, err := time.LoadLocation(f[1]); err == nil {
			return l
		}
	}
	return time.UTC
}

// RestoreServiceCommand is the CLI line that previews restoring the
// instance from runKey (add --yes to restore).
func RestoreServiceCommand(runKey, instanceID string) string {
	return "docker compose exec goforge ./goforge backup restore-service " + runKey + " " + instanceID
}
