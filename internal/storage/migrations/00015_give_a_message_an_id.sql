-- +goose Up
-- Give a message an identifier of its own.
--
-- It was addressed by (session_id, seq), which is unique and stable while the
-- table is only ever appended to. It stops being an identity the moment
-- anything edits or removes a message part-way through a session: seq is a
-- position, and positions move. Editing a turn and re-running from it is the
-- reason this is wanted, and the reference has to survive that.
--
-- It also makes messages the only table addressed by two columns; everything
-- else is one prefixed identifier.
ALTER TABLE messages
    ADD COLUMN id CHAR(30) NULL FIRST;

-- Existing rows get a synthesised identifier rather than a real ULID, because
-- SQL has no way to make one. SHA2 is hexadecimal, whose characters are all in
-- the Crockford alphabet a ULID uses, so the result is the right shape and
-- length and is stable for a given message. It is not sortable by time the way
-- a real one is; nothing relies on that, since ordering is seq.
UPDATE messages
   SET id = CONCAT('msg_', UPPER(LEFT(SHA2(CONCAT(session_id, ':', seq), 256), 26)))
 WHERE id IS NULL;

ALTER TABLE messages
    MODIFY COLUMN id CHAR(30) NOT NULL,
    DROP PRIMARY KEY,
    ADD PRIMARY KEY (id),
    -- Position stays unique within a session: it is what orders the
    -- conversation, and it is what stops two appends landing on the same
    -- number.
    ADD UNIQUE INDEX idx_messages_session_seq (session_id, seq);

-- +goose Down
ALTER TABLE messages
    DROP PRIMARY KEY,
    DROP INDEX idx_messages_session_seq,
    ADD PRIMARY KEY (session_id, seq),
    DROP COLUMN id;
