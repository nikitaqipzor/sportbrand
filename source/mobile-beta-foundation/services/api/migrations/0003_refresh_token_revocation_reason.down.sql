-- 0003_refresh_token_revocation_reason rollback.
ALTER TABLE refresh_tokens DROP CONSTRAINT IF EXISTS refresh_tokens_revoked_reason_check;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS revoked_reason;
