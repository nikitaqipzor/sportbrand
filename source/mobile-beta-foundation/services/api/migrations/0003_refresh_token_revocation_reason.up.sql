-- 0003_refresh_token_revocation_reason: tell "spent by rotation" apart from
-- "ended by logout".
--
-- Replaying a token that rotation already spent is a compromise signal and
-- revokes the whole family. Presenting a token the user themselves logged out
-- is not: without this column the two are the same row shape, so a background
-- refresh racing a logout would sign the athlete out of every other device.

ALTER TABLE refresh_tokens ADD COLUMN revoked_reason text NOT NULL DEFAULT '';

ALTER TABLE refresh_tokens
    ADD CONSTRAINT refresh_tokens_revoked_reason_check
    CHECK (revoked_reason IN ('', 'rotated', 'logout', 'reuse'));

-- Rows revoked before this migration were revoked by rotation or by reuse
-- detection; logout did not exist yet.
UPDATE refresh_tokens SET revoked_reason = 'rotated' WHERE revoked_at IS NOT NULL;
