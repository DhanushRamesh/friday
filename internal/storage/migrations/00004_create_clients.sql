-- +goose Up
-- A client is one thing that talks to the server: a phone, a laptop, a speaker.
-- Conversations belong to a client, and one of them is the active one, which
-- is where a prompt lands when the caller does not name a conversation.
CREATE TABLE clients (
    -- 'cli_' followed by a 26 character ULID.
    id                     CHAR(30)     NOT NULL,
    name                   VARCHAR(100) NOT NULL DEFAULT '',
    -- The conversation a prompt from this client joins. Not a foreign key:
    -- clients and conversations reference each other, and a cycle makes
    -- deleting either awkward for no benefit. Code keeps it honest.
    active_conversation_id CHAR(31)     NULL,
    created_at             DATETIME(3)  NOT NULL,
    updated_at             DATETIME(3)  NOT NULL,
    PRIMARY KEY (id)
) ENGINE = InnoDB
  DEFAULT CHARSET = utf8mb4
  COLLATE = utf8mb4_0900_ai_ci;

-- Nullable, because conversations created before clients existed have no
-- owner. Every conversation created from now on has one.
ALTER TABLE conversations
    ADD COLUMN client_id CHAR(30) NULL AFTER id,
    ADD COLUMN title VARCHAR(200) NOT NULL DEFAULT '' AFTER client_id,
    ADD CONSTRAINT fk_conversations_client
        FOREIGN KEY (client_id) REFERENCES clients (id)
        ON DELETE CASCADE;

-- Serves listing one client's conversations, most recently used first.
CREATE INDEX idx_conversations_client ON conversations (client_id, updated_at);

-- +goose Down
ALTER TABLE conversations
    DROP FOREIGN KEY fk_conversations_client,
    DROP INDEX idx_conversations_client,
    DROP COLUMN title,
    DROP COLUMN client_id;

DROP TABLE clients;
