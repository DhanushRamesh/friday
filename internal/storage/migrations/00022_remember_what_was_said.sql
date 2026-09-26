-- +goose Up
-- What the assistant has been asked to remember.
--
-- A conversation holds what was said in it and is condensed as it grows. This
-- is the other thing: a fact worth keeping after the conversation it was said
-- in has been archived, reachable from any other one.
--
-- tier decides how a memory reaches the model. 'always' goes into every
-- system prompt and is therefore capped and small; 'recall' is found by
-- searching, and is most of them.
--
-- subject is a single line saying what the memory is about, and body is the
-- memory itself. Both are embedded together, because a question is usually
-- closer to a description of a fact than to the fact as it was written down.
--
-- embedding holds float32s little-endian, and embed_model names what produced
-- them. Vectors from two models cannot be compared, so the name is stored
-- rather than assumed: a model swapped underneath would otherwise silently
-- rank every memory wrongly instead of failing.
--
-- MySQL Community has no distance function, so nothing here searches vectors.
-- They are read out and compared in Go.
CREATE TABLE memories (
    id           CHAR(30)     NOT NULL,
    user_id      CHAR(30)     NOT NULL,
    tier         VARCHAR(16)  NOT NULL,
    subject      VARCHAR(160) NOT NULL,
    body         TEXT         NOT NULL,
    embedding    MEDIUMBLOB   NULL,
    embed_model  VARCHAR(128) NOT NULL DEFAULT '',
    created_at   DATETIME(3)  NOT NULL,
    updated_at   DATETIME(3)  NOT NULL,
    last_used_at DATETIME(3)  NULL,
    uses         INT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    KEY idx_memories_user_tier (user_id, tier),
    -- The fallback. When there is no embedding server, or a memory has not
    -- been embedded yet, searching falls back to words.
    FULLTEXT KEY ft_memories_text (subject, body),
    CONSTRAINT fk_memories_user FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE memories;
