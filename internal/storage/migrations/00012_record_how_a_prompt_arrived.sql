-- +goose Up
-- Record how a prompt arrived, because what may be done about it depends on
-- that and nothing stored says it.
--
-- The channel, not the client. What matters is whether there was a way to
-- confirm before acting: spoken, the only authorisation is a "yes" to
-- something possibly misheard, where a client can show what is about to
-- happen and wait. A client identifier would answer a different question, and
-- would answer this one wrongly the moment one client serves both.
--
-- Existing rows are backfilled as direct rather than left empty. They predate
-- the distinction, so either is a guess; direct is the one that grants less,
-- since nothing has yet been decided about what voice may not do.
ALTER TABLE chats
    ADD COLUMN channel VARCHAR(16) NOT NULL DEFAULT 'direct' AFTER prompt;

-- +goose Down
ALTER TABLE chats
    DROP COLUMN channel;
