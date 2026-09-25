-- +goose Up
-- Somewhere to keep the earlier part of a long session, condensed.
--
-- A history is held under three ceilings: the number of messages the service
-- takes, the context window of the model behind it, and a budget of our own.
-- Reaching any of them used to mean the oldest messages simply fell off the
-- front, so a long conversation forgot its own beginning without saying so.
--
-- The condensation lives on the session rather than among its messages
-- because nobody said it. Keeping it here leaves the transcript a record of
-- what was actually said, and makes the summary what it is: derived, and
-- safe to discard and rebuild.
ALTER TABLE sessions
    ADD COLUMN summary MEDIUMTEXT NULL AFTER title,
    -- The last message the summary accounts for. Zero means none of them, so
    -- existing sessions start uncondensed without a backfill.
    ADD COLUMN summarised_through_seq INT NOT NULL DEFAULT 0 AFTER summary;

-- +goose Down
ALTER TABLE sessions
    DROP COLUMN summarised_through_seq,
    DROP COLUMN summary;
