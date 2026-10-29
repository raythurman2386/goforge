// Package settings holds the Settings pages.
package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/models"
)

const (
	statusRunning = string(models.BackupRunRunning)
	statusFailed  = string(models.BackupRunFailed)
)

const backupTimeLayout = "Jan 2, 2006 15:04 MST"

// backupLastLine is the one-line summary: last success, or why there is none.
func backupLastLine(st *backup.Status) string {
	last, ok := st.LastRun, st.LastSuccess
	switch {
	case last == nil:
		return "Last backup: never"
	case last.Status == statusRunning:
		return "Last backup: running since " + backupTime(st, last.StartedAt)
	case last.Status == statusFailed && ok != nil:
		return fmt.Sprintf("Last backup: failed %s. Last success %s (%s).",
			backupTime(st, last.StartedAt), backupTime(st, ok.StartedAt), humanBytes(ok.Bytes))
	case last.Status == statusFailed:
		return "Last backup: failed " + backupTime(st, last.StartedAt) + ". No successful backup yet."
	default:
		return fmt.Sprintf("Last backup: %s, %d objects, %s.", backupTime(st, last.StartedAt), last.Objects, humanBytes(last.Bytes))
	}
}

func backupLineClass(st *backup.Status) string {
	if st.LastRun != nil && st.LastRun.Status == statusFailed {
		return "text-red-700"
	}
	return "text-gray-700"
}

func backupScheduleLine(st *backup.Status) string {
	s := fmt.Sprintf("Schedule %s, keeping %d days, to %s/%s.", st.Schedule, st.RetentionDaily, st.Bucket, st.Prefix)
	if st.NextRunAt != nil {
		s += " Next run " + backupTime(st, *st.NextRunAt) + "."
	}
	return s
}

// backupTime shows t in the schedule's zone ("02:00 America/Chicago"), so
// the card matches the schedule line. Without a schedule it uses UTC.
func backupTime(st *backup.Status, t time.Time) string {
	loc := time.UTC
	if f := strings.Fields(st.Schedule); len(f) == 2 {
		if l, err := time.LoadLocation(f[1]); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format(backupTimeLayout)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
