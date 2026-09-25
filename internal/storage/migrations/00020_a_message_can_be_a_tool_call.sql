-- +goose Up
-- Room in the transcript for what the assistant did, not only what it said.
--
-- A turn that calls a tool produces two more messages: the assistant asking
-- for it, and the result coming back. Both have to be kept. A model given the
-- answer without the call that produced it cannot tell what it already knows
-- from what it guessed, and a person reading the conversation cannot tell
-- what was done on their behalf.
--
-- role is already a varchar, so "tool" and "system" need no change to it.
ALTER TABLE messages
    -- Either prose or tool calls, never both. An assistant message asking for
    -- a tool carries no words, and until now every message had to have some.
    MODIFY COLUMN content MEDIUMTEXT NULL,

    -- What the assistant asked for: an array of {id, name, arguments}.
    -- JSON rather than another table because these are read and written whole
    -- and are never queried into; a row per argument would buy nothing and
    -- cost a join on every turn.
    ADD COLUMN tool_calls JSON NULL AFTER content,

    -- What came back: an array of {id, outcome, content}. The id matches the
    -- call, because a turn may ask for several at once and the answers do not
    -- arrive in order.
    ADD COLUMN tool_results JSON NULL AFTER tool_calls;

-- +goose Down
-- Rows that carry tool calls have no words to put back, so they go. Leaving
-- them with an empty content would make the column NOT NULL again a lie.
DELETE FROM messages WHERE content IS NULL;

ALTER TABLE messages
    DROP COLUMN tool_results,
    DROP COLUMN tool_calls,
    MODIFY COLUMN content MEDIUMTEXT NOT NULL;
