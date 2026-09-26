-- +goose Up
-- A vector for each exchange, so anything ever said can be found again.
--
-- Curated memories are what the assistant believes: few, distilled, and
-- editable. This is what happened: everything, never edited, including what
-- the decoder misheard. The two answer different questions and are kept
-- apart rather than merged.
--
-- The row is keyed by the message somebody sent, and text holds that message
-- together with the reply it drew. A turn on its own is often unsearchable --
-- "yes", "try again", "do both" -- and means something only beside what it
-- answered. Storing the text rather than rebuilding it keeps the words that
-- were embedded and the words that are shown from drifting apart.
--
-- user_id is copied from the conversation so a search is one table and no
-- join. Nothing else reads it, so it cannot disagree with anything.
--
-- No foreign key to messages. Indexing is housekeeping: it must never be the
-- reason a message cannot be written, and a vector left behind by a deleted
-- message costs a row.
CREATE TABLE message_vectors (
    message_id      CHAR(30)     NOT NULL,
    user_id         CHAR(30)     NOT NULL,
    conversation_id CHAR(31)     NOT NULL,
    text            TEXT         NOT NULL,
    embedding       MEDIUMBLOB   NOT NULL,
    embed_model     VARCHAR(128) NOT NULL,
    said_at         DATETIME(3)  NOT NULL,
    PRIMARY KEY (message_id, embed_model),
    KEY idx_message_vectors_search (user_id, embed_model, said_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE message_vectors;
