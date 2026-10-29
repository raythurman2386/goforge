-- API token scopes. Existing tokens keep their old reach through the legacy
-- 'full' scope. New tokens can be limited to read, write, and protected for
-- the /api/v1 JSON surface that the GoForge MCP server calls.
ALTER TABLE api_tokens
    ADD COLUMN scopes TEXT[] NOT NULL DEFAULT '{full}';

UPDATE api_tokens SET scopes = '{full}' WHERE scopes IS NULL OR cardinality(scopes) = 0;

ALTER TABLE api_tokens
    ADD CONSTRAINT api_tokens_scopes_valid
    CHECK (cardinality(scopes) > 0 AND scopes <@ ARRAY['full', 'read', 'write', 'protected']::TEXT[]);
