-- +goose Up
-- Somewhere for the few settings that belong to the assistant rather than to
-- a user, a client or a conversation.
--
-- The manner it answers in is the first: one assistant, one manner, chosen
-- from the settings screen and expected to still be chosen tomorrow. Held in
-- memory while the server runs, because it is read on every prompt, and read
-- back from here when it starts.
--
-- A table of name and value rather than a column per setting. There is no
-- row this belongs to: it is not the user's, since the same assistant answers
-- whoever is asking, and not the client's, since it is the same assistant at
-- every one of them.
CREATE TABLE settings (
    name       VARCHAR(64)  NOT NULL,
    value      VARCHAR(255) NOT NULL,
    updated_at DATETIME(3)  NOT NULL,
    PRIMARY KEY (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE settings;
