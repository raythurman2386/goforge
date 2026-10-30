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

func backupSourceLine(f *backup.SettingsView) string {
	if f.Source == backup.SourceSettings {
		s := "Saved in GoForge. These settings win over BACKUP_* in your env file (.env)."
		if f.UpdatedAt != nil {
			s += " Last changed " + f.UpdatedAt.UTC().Format(backupTimeLayout) + "."
		}
		return s
	}
	return "Read from BACKUP_* in your env file (.env). Saving here stores them in GoForge, and the saved settings win from then on."
}

// keysUnreadableNote is shown next to each key when the saved keys do not
// decrypt with the current ENCRYPTION_KEY.
const keysUnreadableNote = "Saved, but it does not decrypt with the current ENCRYPTION_KEY. Enter it again."

// backupOffLine explains why backups are off and what turns them on. It
// never tells the admin to edit BACKUP_* once saved settings exist, because
// the environment is ignored then.
func backupOffLine(st *backup.Status, form *backup.SettingsView) string {
	switch {
	case form != nil && form.KeysUnreadable:
		return "Backups are off: the saved keys do not decrypt with the current ENCRYPTION_KEY. Enter both keys again below, tick Enabled, and save."
	case form != nil:
		return "Backups are off. Fill in the destination below and tick Enabled to turn on nightly backups."
	case st.ConfiguredBy == backup.SourceSettings:
		return "Backups are off. The saved backup settings could not be loaded; see the GoForge log."
	default:
		return "Backups are off. Set BACKUP_S3_BUCKET and the other BACKUP_* settings in your env file (.env) to turn on nightly backups."
	}
}

func backupOffClass(form *backup.SettingsView) string {
	if form != nil && form.KeysUnreadable {
		return "text-amber-700 dark:text-amber-300"
	}
	return "text-gray-500 dark:text-gray-400"
}

func accessKeyState(f *backup.SettingsView) string {
	switch {
	case f.KeysUnreadable:
		return keysUnreadableNote
	case f.AccessKeyHint != "":
		return "Set, ending in " + f.AccessKeyHint + ". Leave blank to keep it."
	case f.AccessKeySet:
		return "Set. Leave blank to keep it."
	default:
		return "Not set."
	}
}

func accessKeyPlaceholder(f *backup.SettingsView) string {
	if f.AccessKeySet {
		return "Leave blank to keep the stored key"
	}
	return ""
}

func secretState(f *backup.SettingsView) string {
	if f.KeysUnreadable {
		return keysUnreadableNote
	}
	if f.SecretSet {
		return "Set. Leave blank to keep it; type a new one to rotate it."
	}
	return "Not set."
}

func secretPlaceholder(f *backup.SettingsView) string {
	if f.SecretSet {
		return "••••••••"
	}
	return ""
}

// restorePointTime shows a run's start in the schedule's zone, like the
// rest of the card.
func restorePointTime(v *backup.RestorePointsView, t time.Time) string {
	return backupTime(&backup.Status{Schedule: v.Schedule}, t)
}

// restoreCommand is the CLI restore runbook for the newest point.
func restoreCommand(v *backup.RestorePointsView) string {
	key := "<run key>"
	if len(v.Points) > 0 {
		key = v.Points[0].RunKey
	}
	return "docker compose stop goforge\n" +
		"docker compose run --rm --no-deps goforge ./goforge backup restore " + key + "\n" +
		"docker compose start goforge"
}
