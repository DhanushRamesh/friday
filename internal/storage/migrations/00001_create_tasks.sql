-- +goose Up
-- Tasks are the server's unit of work. One row per request.
CREATE TABLE tasks (
    -- 'task_' followed by a 26 character ULID. Stored readable rather than as
    -- a BINARY(16) so the table can be inspected directly. The ULID orders by
    -- creation time, so listing needs no sort.
    id          CHAR(31)    NOT NULL,
    prompt      TEXT        NOT NULL,
    status      VARCHAR(20) NOT NULL,
    -- The final answer only. Progress messages live in task_messages, so this
    -- column does not grow into a transcript.
    response    MEDIUMTEXT  NULL,
    -- Why the task failed, phrased for a user to hear.
    error       TEXT        NULL,
    -- DATETIME(3) keeps milliseconds, which plain DATETIME truncates away.
    created_at  DATETIME(3) NOT NULL,
    updated_at  DATETIME(3) NOT NULL,
    started_at  DATETIME(3) NULL,
    finished_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    -- Serves the runner's query: the oldest pending task first.
    KEY idx_tasks_status_created (status, created_at)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE tasks;
