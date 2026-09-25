-- +goose Up
-- The transient stream had one purpose and it is gone.
--
-- chat_updates existed so a dropped server-sent-events connection could replay
-- what it had missed instead of starting over. There is no such connection any
-- more: a prompt is submitted at /api/chat and its answer arrives on that same
-- response, so there is nothing to rejoin.
--
-- What the table held was progress -- where a chat had got to while it ran --
-- which is worth hearing at the time and worth nothing afterwards. The answer
-- itself was never here; it is on the chat, and the conversation is in
-- messages. Nothing durable is lost.
DROP TABLE IF EXISTS chat_updates;

-- +goose Down
CREATE TABLE chat_updates (
    chat_id    CHAR(31)    NOT NULL,
    seq        INT         NOT NULL,
    kind       VARCHAR(16) NOT NULL,
    `text`     MEDIUMTEXT  NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (chat_id, seq),
    CONSTRAINT fk_chat_updates_chat
        FOREIGN KEY (chat_id) REFERENCES chats (id)
        ON DELETE CASCADE
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;
