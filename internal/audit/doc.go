// Package audit keeps the audit log bounded and detects changes that did
// not go through the retention purge.
//
// Retention is off unless an admin sets it (or GOFORGE_AUDIT_RETENTION_DAYS
// is set while nothing has been saved). A daily job hard-deletes a prefix
// of the hash chain older than the cutoff, in batches, and writes one
// audit.retention.purge row.
//
// Every audit row is sealed into an HMAC-SHA256 chain keyed by a key
// derived from ENCRYPTION_KEY. The chain detects a database-only attacker.
// It does not stop someone who can read ENCRYPTION_KEY, including anyone
// with the Docker socket or root on the host. Rows are sealed a few seconds
// after insert, and rows that already existed at upgrade are only proven
// unchanged since the first seal.
package audit
