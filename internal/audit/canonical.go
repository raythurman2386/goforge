package audit

// canonicalExpr is the one text form the sealer and the verifier hash.
// It uses Postgres's own text, not Go's in-memory values, so a raw SQL
// insert (backup restore) seals the same way as a repository insert.
// Fields are joined with decimal byte-length prefixes so the encoding is
// unambiguous. JSONB text is deterministic. Timestamps are UTC with
// microsecond precision.
const canonicalExpr = `` +
	`octet_length(a.id::text)::text || ':' || a.id::text || ` +
	`octet_length(coalesce(a.user_id::text, ''))::text || ':' || coalesce(a.user_id::text, '') || ` +
	`octet_length(a.action)::text || ':' || a.action || ` +
	`octet_length(a.resource_type)::text || ':' || a.resource_type || ` +
	`octet_length(coalesce(a.resource_id::text, ''))::text || ':' || coalesce(a.resource_id::text, '') || ` +
	`octet_length(coalesce(a.details, '{}'::jsonb)::text)::text || ':' || coalesce(a.details, '{}'::jsonb)::text || ` +
	`octet_length(coalesce(host(a.ip_address), ''))::text || ':' || coalesce(host(a.ip_address), '') || ` +
	`octet_length(coalesce(a.user_agent, ''))::text || ':' || coalesce(a.user_agent, '') || ` +
	`octet_length(coalesce(to_char(a.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), ''))::text || ':' || ` +
	`coalesce(to_char(a.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'), '')`

// Reasons Verify reports. The text is the CLI's reason field.
const (
	ReasonHashMismatch = "hash mismatch"
	ReasonMissingRow   = "missing row"
	ReasonUnsealed     = "unsealed row older than 5 minutes"
	ReasonBoundary     = "boundary mismatch"
	// ReasonBoundaryPurge means boundary_seq is past the genesis value but
	// no audit.retention.purge row carries a valid chain-key MAC over that
	// boundary. A sealed purge row is not enough: the sealer seals every
	// row, including one an attacker inserted.
	ReasonBoundaryPurge = "boundary without purge"
)
