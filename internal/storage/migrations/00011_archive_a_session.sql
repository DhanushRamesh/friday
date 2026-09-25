-- +goose Up
-- A way to put a session away without destroying it.
--
-- Deleting is also offered, and cascades: the chats and the transcript go with
-- the row. That is the right thing when a conversation should not exist, and
-- the wrong thing when it is merely finished with, which is most of the time.
-- Archiving is the difference between the two.
--
-- Nullable rather than a boolean, because when it was put away is worth as
-- much as whether it was, and a timestamp answers both.
ALTER TABLE sessions
    ADD COLUMN archived_at DATETIME(3) NULL AFTER updated_at;

-- A listing asks for the sessions that are not archived, ordered by when they
-- last moved. Without the archived column in the index that query reads rows
-- it then throws away.
CREATE INDEX idx_sessions_user_archived
    ON sessions (user_id, archived_at, updated_at);

-- +goose Down
DROP INDEX idx_sessions_user_archived ON sessions;

ALTER TABLE sessions
    DROP COLUMN archived_at;
