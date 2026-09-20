-- +goose Up
-- The transient messages a provider produces while working. They are stored
-- rather than only streamed, so a client that reconnects can catch up and a
-- finished task can be asked what it said along the way.
CREATE TABLE task_messages (
    task_id    CHAR(31)    NOT NULL,
    -- Position within the task's stream, starting at 1.
    seq        INT         NOT NULL,
    -- Matches provider.Kind. Only 'update' is written today; tool calls and
    -- their results will be recorded the same way.
    kind       VARCHAR(16) NOT NULL,
    `text`     MEDIUMTEXT  NOT NULL,
    created_at DATETIME(3) NOT NULL,
    -- Orders a task's messages and keeps them together on disk.
    PRIMARY KEY (task_id, seq),
    CONSTRAINT fk_task_messages_task
        FOREIGN KEY (task_id) REFERENCES tasks (id)
        ON DELETE CASCADE
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE task_messages;
