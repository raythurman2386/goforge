-- One row per GoForge-managed backup run (nightly scheduler or
-- `goforge backup run`). Holds counts and a short redacted error only; the
-- per-object list with sha256 lives in the run's manifest.json in the bucket.
CREATE TABLE backup_runs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    trigger VARCHAR(20) NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    status VARCHAR(20) NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'succeeded', 'failed')),
    run_key TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    objects INTEGER NOT NULL DEFAULT 0,
    bytes BIGINT NOT NULL DEFAULT 0,
    skipped INTEGER NOT NULL DEFAULT 0,
    pruned INTEGER NOT NULL DEFAULT 0,
    error_summary TEXT
);

CREATE INDEX idx_backup_runs_started_at ON backup_runs(started_at DESC);
CREATE INDEX idx_backup_runs_status_started_at ON backup_runs(status, started_at DESC);
