ALTER TABLE api_tokens DROP CONSTRAINT IF EXISTS api_tokens_scopes_valid;
ALTER TABLE api_tokens DROP COLUMN IF EXISTS scopes;
