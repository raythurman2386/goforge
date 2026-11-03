-- Audit log retention and tamper detection (v0.5.1).
--
-- audit_settings holds the admin retention choice. retention_saved is false
-- until an admin saves the Profile card, so a row created only to store the
-- chain boundary does not override GOFORGE_AUDIT_RETENTION_DAYS.
-- boundary_seq / boundary_hash are the chain prefix removed by retention
-- (0 and 32 zero bytes until the first purge). The sealer and verifier read
-- them; each purge also copies them onto an audit.retention.purge row.
--
-- audit_log_seals is the HMAC chain. Rows in audit_logs are never updated
-- to store a hash. key_epoch is 1 for the key derived from ENCRYPTION_KEY
-- with info goforge-audit-chain-v1. A future key rotation can bump it.
--
-- The user_id foreign key is dropped so deleting a user cannot rewrite a
-- sealed row (the old action was ON DELETE SET NULL). The column stays, so
-- the deleted user's id remains for forensics. Activity does not join users.
--
-- Triggers reject UPDATE, DELETE, and TRUNCATE unless this transaction has
-- set goforge.audit_purge to on. Any database session can set that flag;
-- it is not a privilege reserved for the purge job. The table owner can
-- also DISABLE TRIGGER. Even with the flag, DELETE is allowed only for
-- rows older than 30 days, and UPDATE is never allowed. TRUNCATE is
-- allowed only when every row is older than 30 days. The triggers stop
-- accidents. Detection is the hash chain: a boundary_seq above 0 is
-- accepted only when an audit.retention.purge row records the same
-- through_seq and through_hash and a purge_mac HMAC under the chain key
-- (goforge-audit-purge-v1). Sealing that row is not the proof: the sealer
-- seals every row. A superuser can still bypass triggers
-- (session_replication_role = replica). That is detected by the chain,
-- not prevented.

CREATE TABLE IF NOT EXISTS audit_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    retention_days INT NOT NULL DEFAULT 0 CHECK (retention_days = 0 OR retention_days BETWEEN 30 AND 3650),
    retention_saved BOOLEAN NOT NULL DEFAULT FALSE,
    boundary_seq BIGINT NOT NULL DEFAULT 0,
    boundary_hash BYTEA NOT NULL DEFAULT decode(repeat('00', 32), 'hex'),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT audit_settings_boundary_hash_len CHECK (octet_length(boundary_hash) = 32)
);

CREATE TABLE IF NOT EXISTS audit_log_seals (
    seq BIGINT PRIMARY KEY,
    audit_id UUID NOT NULL UNIQUE,
    prev_hash BYTEA NOT NULL,
    row_hash BYTEA NOT NULL,
    key_epoch SMALLINT NOT NULL DEFAULT 1,
    sealed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT audit_log_seals_prev_hash_len CHECK (octet_length(prev_hash) = 32),
    CONSTRAINT audit_log_seals_row_hash_len CHECK (octet_length(row_hash) = 32)
);

ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_user_id_fkey;

CREATE OR REPLACE FUNCTION goforge_audit_forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    row_created timestamptz;
BEGIN
    IF current_setting('goforge.audit_purge', true) IS DISTINCT FROM 'on' THEN
        RAISE EXCEPTION 'audit log is append-only'
            USING ERRCODE = '42501';
    END IF;

    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'audit log updates are not allowed'
            USING ERRCODE = '42501';
    END IF;

    IF TG_OP = 'TRUNCATE' THEN
        IF TG_TABLE_NAME = 'audit_logs' THEN
            IF EXISTS (
                SELECT 1 FROM audit_logs
                WHERE created_at >= NOW() - INTERVAL '30 days'
            ) THEN
                RAISE EXCEPTION 'audit purge refused: truncate would remove rows newer than 30 days'
                    USING ERRCODE = '42501';
            END IF;
        ELSIF EXISTS (
            SELECT 1
            FROM audit_log_seals s
            LEFT JOIN audit_logs a ON a.id = s.audit_id
            WHERE a.created_at IS NULL OR a.created_at >= NOW() - INTERVAL '30 days'
        ) THEN
            RAISE EXCEPTION 'audit purge refused: truncate would remove seals newer than 30 days'
                USING ERRCODE = '42501';
        END IF;
        RETURN NULL;
    END IF;

    IF TG_TABLE_NAME = 'audit_logs' THEN
        row_created := OLD.created_at;
    ELSE
        SELECT a.created_at INTO row_created
        FROM audit_logs a
        WHERE a.id = OLD.audit_id;
    END IF;
    IF row_created IS NULL OR row_created >= NOW() - INTERVAL '30 days' THEN
        RAISE EXCEPTION 'audit purge refused: row is newer than 30 days'
            USING ERRCODE = '42501';
    END IF;
    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS audit_logs_no_mutate ON audit_logs;
CREATE TRIGGER audit_logs_no_mutate
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW
    EXECUTE FUNCTION goforge_audit_forbid_mutation();

DROP TRIGGER IF EXISTS audit_logs_no_truncate ON audit_logs;
CREATE TRIGGER audit_logs_no_truncate
    BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT
    EXECUTE FUNCTION goforge_audit_forbid_mutation();

DROP TRIGGER IF EXISTS audit_log_seals_no_mutate ON audit_log_seals;
CREATE TRIGGER audit_log_seals_no_mutate
    BEFORE UPDATE OR DELETE ON audit_log_seals
    FOR EACH ROW
    EXECUTE FUNCTION goforge_audit_forbid_mutation();

DROP TRIGGER IF EXISTS audit_log_seals_no_truncate ON audit_log_seals;
CREATE TRIGGER audit_log_seals_no_truncate
    BEFORE TRUNCATE ON audit_log_seals
    FOR EACH STATEMENT
    EXECUTE FUNCTION goforge_audit_forbid_mutation();
