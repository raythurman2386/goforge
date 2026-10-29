// Package backup writes nightly, backup-only copies of GoForge state to an
// S3-compatible bucket (R2, MinIO, B2, AWS S3).
//
// A run takes a Postgres advisory lock, records a backup_runs row, streams
// every artifact straight to the bucket (nothing is staged on local disk),
// and writes manifest.json last. The manifest marks the run complete:
// retention only counts runs that have one, and a failed run never deletes
// anything.
//
// Object layout under the configured prefix:
//
//	<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/manifest.json
//	<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/app/goforge-db.dump
//	<prefix><YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>/services/<instance-id>/...
//
// The date is the run's start in the schedule's zone. The second segment is
// the start in UTC, so runs sort by name.
package backup
