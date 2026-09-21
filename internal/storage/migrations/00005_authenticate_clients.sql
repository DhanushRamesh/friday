-- +goose Up
-- A client proves itself with a token. Only the hash is stored, so a copy of
-- this table does not hand over anyone's devices.
ALTER TABLE clients
    ADD COLUMN token_hash CHAR(64) NULL AFTER name,
    -- When the client was revoked, and nil while it is still usable. Kept
    -- rather than deleting the row, so a revoked device's conversations
    -- remain readable and it is clear what happened.
    ADD COLUMN revoked_at DATETIME(3) NULL AFTER active_conversation_id,
    -- Authentication looks a client up by this, so it must be indexed. Unique
    -- because two clients sharing a token would make either indistinguishable
    -- from the other; MySQL permits many NULLs, which is what clients
    -- registered before tokens existed hold.
    ADD UNIQUE INDEX idx_clients_token (token_hash);

-- +goose Down
ALTER TABLE clients
    DROP INDEX idx_clients_token,
    DROP COLUMN revoked_at,
    DROP COLUMN token_hash;
