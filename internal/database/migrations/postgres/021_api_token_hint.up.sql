-- A non-secret hint per API token (for example "gf_…a1b2") so a user can
-- match a row in Settings → Profile to the client config that holds it.
-- Tokens created before this migration have no hint and keep working: the
-- stored hash covers the whole raw string, prefix or not.
ALTER TABLE api_tokens
    ADD COLUMN token_hint VARCHAR(32) NOT NULL DEFAULT '';
