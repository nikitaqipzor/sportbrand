# ADR 0002: User-scoped idempotent outbox

Every mutable workout write carries a client mutation ID and immutable user ID. Sync submits each mutation once; the server deduplicates by user and mutation ID. Logout purges the current user's session, active snapshot and outbox before another account can sign in.
