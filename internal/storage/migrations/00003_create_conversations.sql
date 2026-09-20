-- +goose Up
-- A conversation groups the tasks of one exchange, so that a follow-up or a
-- correction can be understood in the light of what came before it.
CREATE TABLE conversations (
    -- 'conv_' followed by a 26 character ULID.
    id         CHAR(31)    NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- Nullable, because tasks created before conversations existed belong to none.
-- Every task created from now on has one.
ALTER TABLE tasks
    ADD COLUMN conversation_id CHAR(31) NULL AFTER id,
    ADD CONSTRAINT fk_tasks_conversation
        FOREIGN KEY (conversation_id) REFERENCES conversations (id)
        ON DELETE CASCADE;

-- Serves reading a conversation's history in order. The identifier is a ULID,
-- so ordering by it orders by creation time.
CREATE INDEX idx_tasks_conversation ON tasks (conversation_id, id);

-- +goose Down
ALTER TABLE tasks
    DROP FOREIGN KEY fk_tasks_conversation,
    DROP INDEX idx_tasks_conversation,
    DROP COLUMN conversation_id;

DROP TABLE conversations;
